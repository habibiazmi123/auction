CREATE TABLE IF NOT EXISTS settlements (
    id uuid PRIMARY KEY,
    auction_id uuid NOT NULL UNIQUE,
    seller_id uuid NOT NULL,
    buyer_id uuid NOT NULL,
    bid_id uuid NOT NULL,
    amount_cents bigint NOT NULL CHECK (amount_cents > 0),
    status text NOT NULL CHECK (status IN ('pending', 'paid', 'failed')),
    source_event_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS settlements_auction_idx ON settlements (auction_id);
CREATE INDEX IF NOT EXISTS settlements_buyer_idx ON settlements (buyer_id);
CREATE INDEX IF NOT EXISTS settlements_seller_idx ON settlements (seller_id);
