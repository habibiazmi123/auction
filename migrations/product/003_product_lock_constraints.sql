ALTER TABLE products
    DROP CONSTRAINT IF EXISTS products_status_lock_consistency;

ALTER TABLE products
    ADD CONSTRAINT products_status_lock_consistency
    CHECK ((status = 'locked') = (auction_id IS NOT NULL));
