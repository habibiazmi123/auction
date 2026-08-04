CREATE TABLE IF NOT EXISTS bids (
    id uuid PRIMARY KEY,
    auction_id uuid NOT NULL REFERENCES auctions(id),
    bidder_id uuid NOT NULL,
    command_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    amount_cents bigint NOT NULL CHECK (amount_cents > 0),
    status text NOT NULL CHECK (status IN ('accepted', 'rejected')),
    rejection_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (auction_id, bidder_id, idempotency_key),
    CONSTRAINT bids_command_id_unique UNIQUE (command_id)
);

CREATE INDEX IF NOT EXISTS bids_auction_created_idx ON bids (auction_id, created_at);

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
