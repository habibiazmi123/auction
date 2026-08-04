package main

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuctionLifecycleTransitionsAndCloseIsIdempotent(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	auction := Auction{
		ID: uuid.New(), ProductID: uuid.New(), SellerID: uuid.New(), Status: AuctionStatusDraft, StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour),
		StartingPriceCents: 100, MinimumIncrementCents: 10,
	}
	rules := AuctionRules{}

	auction, err := rules.Schedule(auction, now)
	if err != nil || auction.Status != AuctionStatusScheduled {
		t.Fatalf("schedule: auction=%#v err=%v", auction, err)
	}
	if _, err := rules.Start(auction, now); !errors.Is(err, ErrAuctionNotStarted) {
		t.Fatalf("start too early: got %v", err)
	}
	auction, err = rules.Start(auction, auction.StartsAt)
	if err != nil || auction.Status != AuctionStatusLive {
		t.Fatalf("start: auction=%#v err=%v", auction, err)
	}
	if _, err := rules.Close(auction, auction.EndsAt.Add(-time.Nanosecond)); !errors.Is(err, ErrAuctionNotEnded) {
		t.Fatalf("close too early: got %v", err)
	}
	auction, err = rules.Close(auction, auction.EndsAt)
	if err != nil || auction.Status != AuctionStatusClosed {
		t.Fatalf("close: auction=%#v err=%v", auction, err)
	}
	closed, err := rules.Close(auction, auction.EndsAt.Add(time.Hour))
	if err != nil || closed.Status != AuctionStatusClosed {
		t.Fatalf("close retry: auction=%#v err=%v", closed, err)
	}
}

func TestAuctionRulesRejectSellerAndBidsOutsideWindow(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	seller := uuid.New()
	auction := liveAuction(now, seller)
	rules := AuctionRules{}

	for _, test := range []struct {
		name    string
		bidder  uuid.UUID
		at      time.Time
		wantErr error
	}{
		{name: "seller", bidder: seller, at: now, wantErr: ErrSellerCannotBid},
		{name: "before start", bidder: uuid.New(), at: auction.StartsAt.Add(-time.Nanosecond), wantErr: ErrAuctionNotLive},
		{name: "after end", bidder: uuid.New(), at: auction.EndsAt, wantErr: ErrAuctionNotLive},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := rules.ValidateBid(auction, test.bidder, 100, test.at); !errors.Is(err, test.wantErr) {
				t.Fatalf("error: got %v want %v", err, test.wantErr)
			}
		})
	}
}

func TestAuctionRulesRequireMinimumIncrement(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	auction := liveAuction(now, uuid.New())
	auction.CurrentWinnerID = uuidPtr(uuid.New())
	auction.CurrentPriceCents = 200
	rules := AuctionRules{}

	for _, amount := range []int64{199, 200, 209} {
		if err := rules.ValidateBid(auction, uuid.New(), amount, now); !errors.Is(err, ErrBidTooLow) {
			t.Fatalf("amount=%d error: got %v want %v", amount, err, ErrBidTooLow)
		}
	}
	updated, err := rules.ApplyBid(auction, uuid.New(), 210, now)
	if err != nil || updated.CurrentPriceCents != 210 || updated.CurrentWinnerID == nil {
		t.Fatalf("accepted bid: auction=%#v err=%v", updated, err)
	}
}

func TestAuctionRulesAcceptFirstBidAtStartingPrice(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	auction := liveAuction(now, uuid.New())
	auction.CurrentPriceCents = auction.StartingPriceCents
	updated, err := (AuctionRules{}).ApplyBid(auction, uuid.New(), auction.StartingPriceCents, now)
	if err != nil || updated.CurrentPriceCents != auction.StartingPriceCents || updated.CurrentWinnerID == nil {
		t.Fatalf("first bid: auction=%#v err=%v", updated, err)
	}
}

func TestAuctionRulesAntiSnipingExtendsOnlyConfiguredAuctions(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	auction := liveAuction(now, uuid.New())
	auction.AntiSnipingWindowSeconds = 60
	auction.AntiSnipingExtensionSeconds = 120
	auction.EndsAt = now.Add(30 * time.Second)
	updated, err := (AuctionRules{}).ApplyBid(auction, uuid.New(), auction.StartingPriceCents, now)
	if err != nil || !updated.EndsAt.Equal(now.Add(150*time.Second)) {
		t.Fatalf("extended auction: auction=%#v err=%v", updated, err)
	}

	auction = liveAuction(now, uuid.New())
	auction.EndsAt = now.Add(30 * time.Second)
	updated, err = (AuctionRules{}).ApplyBid(auction, uuid.New(), auction.StartingPriceCents, now)
	if err != nil || !updated.EndsAt.Equal(auction.EndsAt) {
		t.Fatalf("fixed auction: auction=%#v err=%v", updated, err)
	}
}

func TestAuctionRulesRejectNegativeAndOverflowMoney(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	for _, input := range []CreateAuctionInput{
		{ProductID: uuid.New(), StartingPriceCents: -1, MinimumIncrementCents: 1, StartsAt: now, EndsAt: now.Add(time.Hour)},
		{ProductID: uuid.New(), StartingPriceCents: 1, MinimumIncrementCents: -1, StartsAt: now, EndsAt: now.Add(time.Hour)},
		{ProductID: uuid.New(), StartingPriceCents: math.MaxInt64, MinimumIncrementCents: 1, StartsAt: now, EndsAt: now.Add(time.Hour)},
	} {
		if _, err := NewAuction(uuid.New(), ProductSnapshot{ID: input.ProductID, SellerID: uuid.New()}, input, now); !errors.Is(err, ErrInvalidAuction) {
			t.Fatalf("input=%#v error: got %v want %v", input, err, ErrInvalidAuction)
		}
	}
}

func TestAuctionServiceUnlocksProductWhenCreatePersistenceFails(t *testing.T) {
	productID, sellerID := uuid.New(), uuid.New()
	products := &fakeAuctionProductClient{snapshot: ProductSnapshot{ID: productID, SellerID: sellerID, Name: "camera", Status: "available"}}
	repository := &fakeAuctionRepository{createErr: errors.New("database unavailable")}
	service := NewAuctionService(repository, products)

	_, err := service.Create(context.Background(), sellerID, CreateAuctionInput{
		ProductID: productID, StartingPriceCents: 100, MinimumIncrementCents: 10,
		StartsAt: time.Now().UTC().Add(time.Hour), EndsAt: time.Now().UTC().Add(2 * time.Hour),
	})
	if err == nil || products.lockCalls != 1 || products.unlockCalls != 1 {
		t.Fatalf("create: err=%v lock=%d unlock=%d", err, products.lockCalls, products.unlockCalls)
	}
}

type fakeAuctionRepository struct {
	auction   Auction
	createErr error
}

func (r *fakeAuctionRepository) Create(_ context.Context, auction Auction) error {
	r.auction = auction
	return r.createErr
}

func (r *fakeAuctionRepository) Get(context.Context, uuid.UUID) (Auction, error) {
	return r.auction, nil
}

func (r *fakeAuctionRepository) Update(context.Context, Auction) error { return nil }

type fakeAuctionProductClient struct {
	snapshot    ProductSnapshot
	lockCalls   int
	unlockCalls int
}

func (c *fakeAuctionProductClient) Ownership(context.Context, uuid.UUID, uuid.UUID) (ProductSnapshot, error) {
	return c.snapshot, nil
}

func (c *fakeAuctionProductClient) Lock(context.Context, uuid.UUID, uuid.UUID) error {
	c.lockCalls++
	return nil
}

func (c *fakeAuctionProductClient) Unlock(context.Context, uuid.UUID, uuid.UUID) error {
	c.unlockCalls++
	return nil
}

func liveAuction(now time.Time, seller uuid.UUID) Auction {
	return Auction{
		ID: uuid.New(), ProductID: uuid.New(), SellerID: seller, Status: AuctionStatusLive,
		StartingPriceCents: 100, CurrentPriceCents: 100, MinimumIncrementCents: 10,
		StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour),
	}
}

func uuidPtr(value uuid.UUID) *uuid.UUID { return &value }
