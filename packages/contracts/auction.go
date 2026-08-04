package contracts

type BidCommand struct {
	BidID          string `json:"bid_id,omitempty"`
	CommandID      string `json:"command_id"`
	AuctionID      string `json:"auction_id"`
	BidderID       string `json:"bidder_id"`
	AmountCents    int64  `json:"amount_cents"`
	IdempotencyKey string `json:"idempotency_key"`
}

type BidPlaced struct {
	BidID         string `json:"bid_id"`
	AuctionID     string `json:"auction_id"`
	BidderID      string `json:"bidder_id"`
	AmountCents   int64  `json:"amount_cents"`
	Status        string `json:"status"`
	RejectionCode string `json:"rejection_code,omitempty"`
}

type AuctionClosed struct {
	AuctionID       string `json:"auction_id"`
	SellerID        string `json:"seller_id"`
	BidID           string `json:"bid_id,omitempty"`
	FinalPriceCents int64  `json:"final_price_cents"`
	WinnerID        string `json:"winner_id,omitempty"`
}
