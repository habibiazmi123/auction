CREATE TABLE IF NOT EXISTS outbox_events (
    id bigserial PRIMARY KEY,
    aggregate_type text NOT NULL,
    aggregate_id text NOT NULL,
    aggregate_version bigint NOT NULL,
    event_type text NOT NULL,
    event_key text NOT NULL,
    topic text NOT NULL DEFAULT 'product.events.v1',
    payload jsonb NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    sent_at timestamptz,
    UNIQUE (aggregate_id, aggregate_version)
);

CREATE INDEX IF NOT EXISTS outbox_events_pending_idx ON outbox_events (aggregate_id, aggregate_version, id) WHERE sent_at IS NULL;
