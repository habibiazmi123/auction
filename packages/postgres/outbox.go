package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Publisher interface {
	Publish(context.Context, string, string, []byte) error
}

type OutboxRelay struct {
	Pool         *pgxpool.Pool
	Publisher    Publisher
	BatchSize    int
	PollInterval time.Duration
}

func NewOutboxRelay(pool *pgxpool.Pool, publisher Publisher) *OutboxRelay {
	return &OutboxRelay{Pool: pool, Publisher: publisher, BatchSize: 100, PollInterval: time.Second}
}

func (r *OutboxRelay) Run(ctx context.Context) error {
	if r.BatchSize <= 0 {
		r.BatchSize = 100
	}
	if r.PollInterval <= 0 {
		r.PollInterval = time.Second
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
	rows, err := r.Pool.Query(ctx, `
        SELECT id, topic, event_key, payload
        FROM outbox_events
        WHERE sent_at IS NULL
        ORDER BY created_at, id
        LIMIT $1`, r.BatchSize)
	if err != nil {
		return 0, fmt.Errorf("query outbox: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id int64
		var topic, key string
		var payload []byte
		if err := rows.Scan(&id, &topic, &key, &payload); err != nil {
			return count, fmt.Errorf("scan outbox row: %w", err)
		}
		if err := r.Publisher.Publish(ctx, topic, key, payload); err != nil {
			return count, fmt.Errorf("publish outbox row %d: %w", id, err)
		}
		if _, err := r.Pool.Exec(ctx, `UPDATE outbox_events SET sent_at = now() WHERE id = $1 AND sent_at IS NULL`, id); err != nil {
			return count, fmt.Errorf("mark outbox row %d sent: %w", id, err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("read outbox rows: %w", err)
	}
	return count, nil
}
