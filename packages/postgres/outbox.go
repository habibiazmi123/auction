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

type outboxEvent struct {
	id      int64
	topic   string
	key     string
	payload []byte
}

type OutboxRelay struct {
	Pool         *pgxpool.Pool
	Publisher    Publisher
	BatchSize    int
	PollInterval time.Duration
}

// Migration contract: outbox_events must include these fields and constraint
// before this relay is used. aggregate_version is assigned monotonically per
// aggregate in the same transaction as the domain change.
const outboxOrderingContract = `
aggregate_id text NOT NULL,
aggregate_version bigint NOT NULL,
UNIQUE (aggregate_id, aggregate_version)
`

const outboxClaimQuery = `
SELECT e.id, e.topic, e.event_key, e.payload
FROM outbox_events e
WHERE e.sent_at IS NULL
  AND NOT EXISTS (
    SELECT 1
    FROM outbox_events prior
    WHERE prior.aggregate_id = e.aggregate_id
      AND prior.aggregate_version < e.aggregate_version
      AND prior.sent_at IS NULL
  )
ORDER BY e.aggregate_id, e.aggregate_version, e.id
LIMIT $1
FOR UPDATE SKIP LOCKED`

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
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin outbox relay transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, outboxClaimQuery, r.BatchSize)
	if err != nil {
		return 0, fmt.Errorf("query outbox: %w", err)
	}
	defer rows.Close()
	events := make([]outboxEvent, 0, r.BatchSize)
	for rows.Next() {
		var event outboxEvent
		if err := rows.Scan(&event.id, &event.topic, &event.key, &event.payload); err != nil {
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
			return count, fmt.Errorf("publish outbox row %d: %w", event.id, err)
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
