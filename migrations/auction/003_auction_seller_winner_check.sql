ALTER TABLE auctions
    ADD CONSTRAINT auctions_winner_not_seller_check
    CHECK (current_winner_id IS NULL OR current_winner_id != seller_id);
