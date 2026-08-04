CREATE TABLE IF NOT EXISTS products (
    id uuid PRIMARY KEY,
    seller_id uuid NOT NULL,
    name text NOT NULL CHECK (length(trim(name)) > 0),
    description text NOT NULL CHECK (length(trim(description)) > 0),
    quantity integer NOT NULL CHECK (quantity > 0),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'available', 'locked')),
    auction_id uuid,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT products_status_lock_consistency CHECK ((status = 'locked') = (auction_id IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS products_seller_id_idx ON products (seller_id);
CREATE INDEX IF NOT EXISTS products_status_idx ON products (status);
