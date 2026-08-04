CREATE TABLE IF NOT EXISTS notifications (
    id uuid PRIMARY KEY,
    source_event_id uuid NOT NULL,
    recipient_id uuid NOT NULL,
    type text NOT NULL,
    payload jsonb NOT NULL,
    read_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_event_id, recipient_id)
);

CREATE INDEX IF NOT EXISTS notifications_recipient_created_idx ON notifications (recipient_id, created_at DESC);
CREATE INDEX IF NOT EXISTS notifications_source_event_idx ON notifications (source_event_id);
