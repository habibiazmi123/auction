package main

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/example/auction/packages/contracts"
	"github.com/google/uuid"
)

func TestAuctionLifecycleTransitionsAndCloseIsIdempotent(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	auction := Auction{
		ID: uuid.New(), ProductID: uuid.New(), SellerID: uuid.New(), Status: AuctionStatusDraft, StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour),
		StartingPriceCents: 100, CurrentPriceCents: 100, MinimumIncrementCents: 10,
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

func TestAuctionRulesRejectMalformedPriceAndWinnerState(t *testing.T) {

	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	rules := AuctionRules{}

	for _, test := range []struct {
		name   string
		price  int64
		winner *uuid.UUID
	}{
		{name: "price below starting price", price: 99},
		{name: "price above starting price without winner", price: 101},
		{name: "nil winner id", price: 100, winner: uuidPtr(uuid.Nil)},
	} {
		t.Run(test.name, func(t *testing.T) {
			auction := liveAuction(now, uuid.New())
			auction.CurrentPriceCents, auction.CurrentWinnerID = test.price, test.winner
			if err := rules.ValidateBid(auction, uuid.New(), 100, now); !errors.Is(err, ErrInvalidAuction) {
				t.Fatalf("error: got %v want %v", err, ErrInvalidAuction)
			}
		})
	}
}

func TestAuctionServiceValidatesSnapshotBeforeLock(t *testing.T) {
	productID, sellerID := uuid.New(), uuid.New()
	input := CreateAuctionInput{
		ProductID: productID, StartingPriceCents: 100, MinimumIncrementCents: 10,
		StartsAt: time.Now().UTC().Add(time.Hour), EndsAt: time.Now().UTC().Add(2 * time.Hour),
	}
	for _, product := range []ProductSnapshot{
		{ID: productID, SellerID: sellerID, Name: "camera", Description: "used", Quantity: 0, Status: "available"},
		{ID: productID, SellerID: sellerID, Description: "used", Quantity: 1, Status: "available"},
		{ID: productID, SellerID: sellerID, Name: "camera", Quantity: 1, Status: "available"},
	} {
		products := &fakeAuctionProductClient{snapshot: product}
		_, err := NewAuctionService(&fakeAuctionRepository{}, products).Create(context.Background(), sellerID, input)
		if !errors.Is(err, ErrInvalidAuction) || products.lockCalls != 0 {
			t.Fatalf("snapshot=%#v err=%v lock=%d", product, err, products.lockCalls)
		}
	}
}

func TestBidResultAcceptsFirstBid(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	auction := liveAuction(now, uuid.New())
	updated, err := (AuctionRules{}).ApplyBid(auction, uuid.New(), auction.StartingPriceCents, now)
	if err != nil || updated.CurrentPriceCents != auction.StartingPriceCents || updated.CurrentWinnerID == nil {
		t.Fatalf("first bid: auction=%#v err=%v", updated, err)
	}
}

func TestBidResultRejectsBidAfterEnd(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	auction := liveAuction(now, uuid.New())
	if _, err := (AuctionRules{}).ApplyBid(auction, uuid.New(), auction.StartingPriceCents, auction.EndsAt); !errors.Is(err, ErrAuctionNotLive) {
		t.Fatalf("expected ErrAuctionNotLive, got %v", err)
	}
}

func TestBidResultDuplicateIdempotencyKeyReturnsOriginal(t *testing.T) {
	repo := newMemoryBidRepository()
	processor := NewBidProcessor(repo)
	cmd := contracts.BidCommand{
		BidID: uuid.NewString(), CommandID: uuid.NewString(), AuctionID: uuid.NewString(),
		BidderID: uuid.NewString(), AmountCents: 150, IdempotencyKey: "key-1",
	}
	if err := processor.Handle(context.Background(), cmd); err != nil {
		t.Fatalf("first handle: %v", err)
	}
	if len(repo.byCommand) != 1 {
		t.Fatalf("expected one command to be stored, got %d", len(repo.byCommand))
	}
	if err := processor.Handle(context.Background(), cmd); err != nil {
		t.Fatalf("second handle: %v", err)
	}
	if len(repo.byCommand) != 1 {
		t.Fatalf("duplicate should be idempotent, commands=%d", len(repo.byCommand))
	}
}

func TestBidResultConflictSameKeyDifferentPayload(t *testing.T) {
	repo := newMemoryBidRepository()
	processor := NewBidProcessor(repo)
	auctionID := uuid.NewString()
	cmd1 := contracts.BidCommand{
		BidID: uuid.NewString(), CommandID: uuid.NewString(), AuctionID: auctionID,
		BidderID: uuid.NewString(), AmountCents: 150, IdempotencyKey: "key-2",
	}
	cmd2 := contracts.BidCommand{
		BidID: uuid.NewString(), CommandID: uuid.NewString(), AuctionID: auctionID,
		BidderID: uuid.NewString(), AmountCents: 200, IdempotencyKey: "key-2",
	}
	if err := processor.Handle(context.Background(), cmd1); err != nil {
		t.Fatalf("first handle: %v", err)
	}
	if err := processor.Handle(context.Background(), cmd2); err != nil {
		t.Fatalf("second handle should be committed as conflict, got %v", err)
	}
	if len(repo.byCommand) != 1 || repo.byCommand[cmd1.CommandID].AmountCents != 150 {
		t.Fatalf("result should be from first command, got %#v", repo.byCommand)
	}
}

func TestBidResultRejectsBidTooLow(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	auction := liveAuction(now, uuid.New())
	_, err := (AuctionRules{}).ApplyBid(auction, uuid.New(), auction.StartingPriceCents-1, now)
	if !errors.Is(err, ErrBidTooLow) {
		t.Fatalf("expected ErrBidTooLow, got %v", err)
	}
}

func TestBidResultRejectsSellerBid(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	seller := uuid.New()
	auction := liveAuction(now, seller)
	_, err := (AuctionRules{}).ApplyBid(auction, seller, auction.StartingPriceCents, now)
	if !errors.Is(err, ErrSellerCannotBid) {
		t.Fatalf("expected ErrSellerCannotBid, got %v", err)
	}
}

func TestAuctionServiceUnlocksProductWhenCreatePersistenceFails(t *testing.T) {
	productID, sellerID := uuid.New(), uuid.New()
	products := &fakeAuctionProductClient{snapshot: ProductSnapshot{ID: productID, SellerID: sellerID, Name: "camera", Description: "used", Quantity: 1, Status: "available"}}
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

func (r *fakeAuctionRepository) ApplyBidTx(_ context.Context, _ contracts.BidCommand) (BidResult, error) {
	return BidResult{}, nil
}

func (r *fakeAuctionRepository) GetBid(_ context.Context, _ uuid.UUID) (Bid, error) {
	return Bid{}, ErrAuctionNotFound
}

func (r *fakeAuctionRepository) CloseDue(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}

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

type memoryBidRepository struct {
	byCommand map[string]BidResult
	byKey     map[string]string
}

func newMemoryBidRepository() *memoryBidRepository {
	return &memoryBidRepository{
		byCommand: make(map[string]BidResult),
		byKey:     make(map[string]string),
	}
}

func (r *memoryBidRepository) ApplyBidTx(_ context.Context, command contracts.BidCommand) (BidResult, error) {
	if result, ok := r.byCommand[command.CommandID]; ok {
		return result, nil
	}
	if priorCommandID, ok := r.byKey[command.IdempotencyKey]; ok && priorCommandID != command.CommandID {
		return BidResult{}, ErrBidIdempotencyConflict
	}
	result := BidResult{
		BidID:       uuid.MustParse(command.BidID),
		AuctionID:   uuid.MustParse(command.AuctionID),
		BidderID:    uuid.MustParse(command.BidderID),
		AmountCents: command.AmountCents,
		Status:      BidStatusAccepted,
	}
	r.byCommand[command.CommandID] = result
	r.byKey[command.IdempotencyKey] = command.CommandID
	return result, nil
}

func (r *memoryBidRepository) Create(_ context.Context, _ Auction) error { return nil }
func (r *memoryBidRepository) Get(_ context.Context, _ uuid.UUID) (Auction, error) { return Auction{}, nil }
func (r *memoryBidRepository) Update(_ context.Context, _ Auction) error { return nil }
func (r *memoryBidRepository) GetBid(_ context.Context, _ uuid.UUID) (Bid, error) { return Bid{}, ErrAuctionNotFound }
func (r *memoryBidRepository) CloseDue(_ context.Context, _ time.Time) (int, error) { return 0, nil }

func liveAuction(now time.Time, seller uuid.UUID) Auction {
	return Auction{
		ID: uuid.New(), ProductID: uuid.New(), SellerID: seller, Status: AuctionStatusLive,
		StartingPriceCents: 100, CurrentPriceCents: 100, MinimumIncrementCents: 10,
		StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour),
	}
}

func uuidPtr(value uuid.UUID) *uuid.UUID { return &value }
