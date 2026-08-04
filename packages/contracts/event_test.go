package contracts

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEventEnvelopeJSONRoundTrip(t *testing.T) {
	want := EventEnvelope[BidCommand]{
		EventID:       "evt-1",
		EventType:     "auction.bid.command",
		Version:       1,
		OccurredAt:    time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC),
		Producer:      "auction-service",
		CorrelationID: "corr-1",
		CausationID:   "cause-1",
		Payload: BidCommand{
			CommandID:      "cmd-1",
			AuctionID:      "auction-1",
			BidderID:       "buyer-1",
			AmountCents:    12500,
			IdempotencyKey: "idem-1",
		},
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	var got EventEnvelope[BidCommand]
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if got != want {
		t.Fatalf("round trip mismatch: got %#v, want %#v", got, want)
	}
}

func TestEventEnvelopeRejectsEmptyEventID(t *testing.T) {
	_, err := json.Marshal(EventEnvelope[struct{}]{Payload: struct{}{}})
	if err == nil {
		t.Fatal("expected empty event ID to be rejected")
	}
}

func TestPayloadSerializationIsStable(t *testing.T) {
	bid, err := json.Marshal(BidCommand{
		CommandID:      "cmd-1",
		AuctionID:      "auction-1",
		BidderID:       "buyer-1",
		AmountCents:    12500,
		IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatalf("marshal bid command: %v", err)
	}
	if got, want := string(bid), `{"command_id":"cmd-1","auction_id":"auction-1","bidder_id":"buyer-1","amount_cents":12500,"idempotency_key":"idem-1"}`; got != want {
		t.Fatalf("bid serialization changed: got %s, want %s", got, want)
	}

	settlement, err := json.Marshal(SettlementCreated{
		SettlementID: "settlement-1",
		AuctionID:    "auction-1",
		BidID:        "bid-1",
		BuyerID:      "buyer-1",
		SellerID:     "seller-1",
		AmountCents:  12500,
	})
	if err != nil {
		t.Fatalf("marshal settlement: %v", err)
	}
	if got, want := string(settlement), `{"settlement_id":"settlement-1","auction_id":"auction-1","bid_id":"bid-1","buyer_id":"buyer-1","seller_id":"seller-1","amount_cents":12500}`; got != want {
		t.Fatalf("settlement serialization changed: got %s, want %s", got, want)
	}
}
