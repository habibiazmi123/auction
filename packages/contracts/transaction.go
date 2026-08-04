package contracts

type SettlementCreated struct {
	SettlementID string `json:"settlement_id"`
	AuctionID    string `json:"auction_id"`
	BidID        string `json:"bid_id"`
	BuyerID      string `json:"buyer_id"`
	SellerID     string `json:"seller_id"`
	AmountCents  int64  `json:"amount_cents"`
}
