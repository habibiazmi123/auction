package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/habibiazmi123/auction/packages/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OutboxRelay struct {
	Pool         *pgxpool.Pool
	Publisher    postgres.Publisher
	BatchSize    int
	PollInterval time.Duration
	MaxAttempts  int
	BaseDelay    time.Duration
}

func NewOutboxRelay(pool *pgxpool.Pool, publisher postgres.Publisher) *OutboxRelay {
	return &OutboxRelay{
		Pool:         pool,
		Publisher:    publisher,
		BatchSize:    100,
		PollInterval: time.Second,
		MaxAttempts:  5,
		BaseDelay:    time.Second,
	}
}

func (r *OutboxRelay) Run(ctx context.Context) error {
	if r.BatchSize <= 0 {
		r.BatchSize = 100
	}
	if r.PollInterval <= 0 {
		r.PollInterval = time.Second
	}
	if r.MaxAttempts <= 0 {
		r.MaxAttempts = 5
	}
	if r.BaseDelay <= 0 {
		r.BaseDelay = time.Second
	}
	for {
		count, err := r.RunOnce(ctx)
		if err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		timer := time.NewTimer(r.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (r *OutboxRelay) RunOnce(ctx context.Context) (int, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin outbox relay transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT e.id, e.topic, e.event_key, e.payload, e.attempts, e.dlq_topic
		FROM outbox_events e
		WHERE e.sent_at IS NULL
		  AND e.attempts < $1
		  AND e.next_at <= now()
		  AND NOT EXISTS (
			SELECT 1
			FROM outbox_events prior
			WHERE prior.aggregate_id = e.aggregate_id
			  AND prior.aggregate_version < e.aggregate_version
			  AND prior.sent_at IS NULL
		  )
		ORDER BY e.aggregate_id, e.aggregate_version, e.id
		LIMIT $2
		FOR UPDATE SKIP LOCKED`, r.MaxAttempts, r.BatchSize)
	if err != nil {
		return 0, fmt.Errorf("query outbox: %w", err)
	}
	defer rows.Close()

	type pendingEvent struct {
		id       int64
		topic    string
		key      string
		payload  []byte
		attempts int
		dlqTopic string
	}
	var events []pendingEvent
	for rows.Next() {
		var event pendingEvent
		if err := rows.Scan(&event.id, &event.topic, &event.key, &event.payload, &event.attempts, &event.dlqTopic); err != nil {
			return len(events), fmt.Errorf("scan outbox row: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read outbox rows: %w", err)
	}
	rows.Close()

	count := 0
	for _, event := range events {
		if err := r.Publisher.Publish(ctx, event.topic, event.key, event.payload); err != nil {
			if err := r.handleFailure(ctx, tx, event.id, event.attempts, event.dlqTopic, err); err != nil {
				return count, fmt.Errorf("handle publish failure for outbox row %d: %w", event.id, err)
			}
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE outbox_events SET sent_at = now() WHERE id = $1 AND sent_at IS NULL`, event.id); err != nil {
			return count, fmt.Errorf("mark outbox row %d sent: %w", event.id, err)
		}
		count++
	}
	if err := tx.Commit(ctx); err != nil {
		return count, fmt.Errorf("commit outbox relay transaction: %w", err)
	}
	return count, nil
}

func (r *OutboxRelay) handleFailure(ctx context.Context, tx pgx.Tx, id int64, attempts int, dlqTopic string, publishErr error) error {
	nextAttempts := attempts + 1
	if nextAttempts >= r.MaxAttempts && strings.TrimSpace(dlqTopic) != "" {
		if err := r.Publisher.Publish(ctx, dlqTopic, fmt.Sprintf("%d", id), []byte(publishErr.Error())); err != nil {
			slog.WarnContext(ctx, "failed to publish to DLQ", "outbox_id", id, "error", err)
			// ponytail: keep attempts below MaxAttempts so the row stays eligible for retry instead of being stranded.
			_, dbErr := tx.Exec(ctx, `
				UPDATE outbox_events
				SET next_at = $2, last_error = $3
				WHERE id = $1`, id, time.Now().UTC().Add(exponentialBackoff(r.BaseDelay, attempts)), publishErr.Error())
			return dbErr
		}
		_, err := tx.Exec(ctx, `UPDATE outbox_events SET sent_at = now(), last_error = $2 WHERE id = $1`, id, publishErr.Error())
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE outbox_events
		SET attempts = $2, next_at = $3, last_error = $4
		WHERE id = $1`, id, nextAttempts, time.Now().UTC().Add(exponentialBackoff(r.BaseDelay, attempts)), publishErr.Error())
	return err
}

func exponentialBackoff(base time.Duration, attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	factor := time.Duration(math.Pow(2, float64(attempt)))
	if factor <= 0 {
		factor = 1
	}
	delay := base * factor
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	return delay
}
