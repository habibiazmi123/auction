CREATE TABLE IF NOT EXISTS outbox_events (
    id bigserial PRIMARY KEY,
    event_id uuid NOT NULL UNIQUE,
    aggregate_id text NOT NULL,
    aggregate_version bigint NOT NULL,
    event_type text NOT NULL,
    topic text NOT NULL,
    event_key text NOT NULL,
    payload jsonb NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    next_at timestamptz NOT NULL DEFAULT now(),
    sent_at timestamptz,
    last_error text,
    dlq_topic text,
    UNIQUE (aggregate_id, aggregate_version)
);

CREATE INDEX IF NOT EXISTS outbox_events_pending_idx ON outbox_events (next_at, id) WHERE sent_at IS NULL;
