CREATE TABLE IF NOT EXISTS auctions (
    id uuid PRIMARY KEY,
    product_id uuid NOT NULL,
    seller_id uuid NOT NULL,
    product_name text NOT NULL,
    product_description text NOT NULL,
    product_quantity integer NOT NULL CHECK (product_quantity > 0),
    status text NOT NULL CHECK (status IN ('draft', 'scheduled', 'live', 'closed')),
    starting_price_cents bigint NOT NULL CHECK (starting_price_cents >= 0),
    current_price_cents bigint NOT NULL CHECK (current_price_cents >= 0),
    current_winner_id uuid,
    minimum_increment_cents bigint NOT NULL CHECK (minimum_increment_cents > 0),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL CHECK (ends_at > starts_at),
    anti_sniping_window_seconds bigint NOT NULL DEFAULT 0 CHECK (anti_sniping_window_seconds >= 0),
    anti_sniping_extension_seconds bigint NOT NULL DEFAULT 0 CHECK (anti_sniping_extension_seconds >= 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS auctions_status_ends_at_idx ON auctions (status, ends_at);
CREATE INDEX IF NOT EXISTS auctions_seller_id_idx ON auctions (seller_id);
CREATE UNIQUE INDEX IF NOT EXISTS auctions_active_product_idx ON auctions (product_id)
    WHERE status IN ('scheduled', 'live');
