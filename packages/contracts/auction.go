package contracts

type BidCommand struct {
	BidID          string `json:"bid_id,omitempty"`
	CommandID      string `json:"command_id"`
	AuctionID      string `json:"auction_id"`
	BidderID       string `json:"bidder_id"`
	AmountCents    int64  `json:"amount_cents"`
	IdempotencyKey string `json:"idempotency_key"`
}
