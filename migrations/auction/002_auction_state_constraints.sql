ALTER TABLE auctions
    ADD CONSTRAINT auctions_current_price_at_least_starting_check
    CHECK (current_price_cents >= starting_price_cents),
    ADD CONSTRAINT auctions_winner_price_consistency_check
    CHECK (
        (current_winner_id IS NULL AND current_price_cents = starting_price_cents)
        OR current_winner_id IS NOT NULL
    );
