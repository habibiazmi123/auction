package contracts

type NotificationCreated struct {
	NotificationID string `json:"notification_id"`
	RecipientID    string `json:"recipient_id"`
	Type           string `json:"type"`
	AuctionID      string `json:"auction_id,omitempty"`
	Title          string `json:"title"`
	Body           string `json:"body"`
}
