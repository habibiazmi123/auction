package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	AuctionStatusDraft     = "draft"
	AuctionStatusScheduled = "scheduled"
	AuctionStatusLive      = "live"
	AuctionStatusClosed    = "closed"
)

var (
	ErrAuctionNotFound        = errors.New("auction not found")
	ErrInvalidAuction         = errors.New("invalid auction")
	ErrAuctionNotLive         = errors.New("auction is not live")
	ErrAuctionNotScheduled    = errors.New("auction is not scheduled")
	ErrAuctionNotStarted      = errors.New("auction has not started")
	ErrAuctionEnded           = errors.New("auction has ended")
	ErrAuctionNotEnded        = errors.New("auction has not ended")
	ErrBidTooLow              = errors.New("bid is below the minimum")
	ErrSellerCannotBid        = errors.New("seller cannot bid")
	ErrAuctionConflict        = errors.New("auction conflicts with an existing auction")
	ErrAuctionVersionConflict = errors.New("auction version conflict")
	ErrProductNotFound        = errors.New("product not found")
	ErrProductNotOwner        = errors.New("seller does not own product")
	ErrProductAuth            = errors.New("product service authorization failed")
	ErrProductDependency      = errors.New("product service dependency unavailable")
	ErrProductUnavailable     = errors.New("product is not available")
)

type Auction struct {
	ID                          uuid.UUID  `json:"id"`
	ProductID                   uuid.UUID  `json:"product_id"`
	SellerID                    uuid.UUID  `json:"seller_id"`
	ProductName                 string     `json:"product_name"`
	ProductDescription          string     `json:"product_description"`
	ProductQuantity             int        `json:"product_quantity"`
	Status                      string     `json:"status"`
	StartingPriceCents          int64      `json:"starting_price_cents"`
	CurrentPriceCents           int64      `json:"current_price_cents"`
	CurrentWinnerID             *uuid.UUID `json:"current_winner_id,omitempty"`
	MinimumIncrementCents       int64      `json:"minimum_increment_cents"`
	StartsAt                    time.Time  `json:"starts_at"`
	EndsAt                      time.Time  `json:"ends_at"`
	AntiSnipingWindowSeconds    int64      `json:"anti_sniping_window_seconds"`
	AntiSnipingExtensionSeconds int64      `json:"anti_sniping_extension_seconds"`
	Version                     int64      `json:"version"`
	CreatedAt                   time.Time  `json:"created_at"`
	UpdatedAt                   time.Time  `json:"updated_at"`
}

type AuctionView struct{ Auction }

type CreateAuctionInput struct {
	ProductID                   uuid.UUID `json:"product_id"`
	StartingPriceCents          int64     `json:"starting_price_cents"`
	MinimumIncrementCents       int64     `json:"minimum_increment_cents"`
	StartsAt                    time.Time `json:"starts_at"`
	EndsAt                      time.Time `json:"ends_at"`
	AntiSnipingWindowSeconds    int64     `json:"anti_sniping_window_seconds"`
	AntiSnipingExtensionSeconds int64     `json:"anti_sniping_extension_seconds"`
}

type ProductSnapshot struct {
	ID          uuid.UUID  `json:"id"`
	SellerID    uuid.UUID  `json:"seller_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Quantity    int        `json:"quantity"`
	Status      string     `json:"status"`
	AuctionID   *uuid.UUID `json:"auction_id,omitempty"`
}

type AuctionRepository interface {
	Create(context.Context, Auction) error
	Get(context.Context, uuid.UUID) (Auction, error)
	Update(context.Context, Auction) error
}

type ProductClient interface {
	Ownership(context.Context, uuid.UUID, uuid.UUID) (ProductSnapshot, error)
	Lock(context.Context, uuid.UUID, uuid.UUID) error
	Unlock(context.Context, uuid.UUID, uuid.UUID) error
}

type AuctionService interface {
	Create(context.Context, uuid.UUID, CreateAuctionInput) (Auction, error)
	Get(context.Context, uuid.UUID) (AuctionView, error)
}

type AuctionRules struct{}

func NewAuction(sellerID uuid.UUID, product ProductSnapshot, input CreateAuctionInput, now time.Time) (Auction, error) {
	if sellerID == uuid.Nil || !validProductSnapshot(product) || product.SellerID != sellerID || input.ProductID != product.ID || !validAuctionInput(input) {
		return Auction{}, ErrInvalidAuction
	}
	return Auction{
		ID:                          uuid.New(),
		ProductID:                   product.ID,
		SellerID:                    product.SellerID,
		ProductName:                 product.Name,
		ProductDescription:          product.Description,
		ProductQuantity:             product.Quantity,
		Status:                      AuctionStatusScheduled,
		StartingPriceCents:          input.StartingPriceCents,
		CurrentPriceCents:           input.StartingPriceCents,
		MinimumIncrementCents:       input.MinimumIncrementCents,
		StartsAt:                    input.StartsAt.UTC(),
		EndsAt:                      input.EndsAt.UTC(),
		AntiSnipingWindowSeconds:    input.AntiSnipingWindowSeconds,
		AntiSnipingExtensionSeconds: input.AntiSnipingExtensionSeconds,
		Version:                     1,
		CreatedAt:                   now.UTC(),
		UpdatedAt:                   now.UTC(),
	}, nil
}

func validAuctionInput(input CreateAuctionInput) bool {
	if input.ProductID == uuid.Nil || input.StartingPriceCents < 0 || input.MinimumIncrementCents <= 0 || input.StartsAt.IsZero() || input.EndsAt.IsZero() || !input.EndsAt.After(input.StartsAt) {
		return false
	}
	if input.AntiSnipingWindowSeconds < 0 || input.AntiSnipingExtensionSeconds < 0 || !validSeconds(input.AntiSnipingWindowSeconds) || !validSeconds(input.AntiSnipingExtensionSeconds) {
		return false
	}
	return input.StartingPriceCents <= math.MaxInt64-input.MinimumIncrementCents
}

func validProductSnapshot(product ProductSnapshot) bool {
	return product.ID != uuid.Nil && product.SellerID != uuid.Nil && strings.TrimSpace(product.Name) != "" && strings.TrimSpace(product.Description) != "" && product.Quantity > 0 && product.Status == "available" && product.AuctionID == nil
}

func (r AuctionRules) Schedule(auction Auction, now time.Time) (Auction, error) {
	if auction.Status != AuctionStatusDraft {
		return Auction{}, ErrAuctionNotScheduled
	}
	if !validAuction(auction) {
		return Auction{}, ErrInvalidAuction
	}
	auction.Status = AuctionStatusScheduled
	auction.UpdatedAt = now.UTC()
	return auction, nil
}

func (r AuctionRules) Start(auction Auction, now time.Time) (Auction, error) {
	if auction.Status != AuctionStatusScheduled {
		return Auction{}, ErrAuctionNotScheduled
	}
	now = now.UTC()
	if now.Before(auction.StartsAt) {
		return Auction{}, ErrAuctionNotStarted
	}
	if !now.Before(auction.EndsAt) {
		return Auction{}, ErrAuctionEnded
	}
	auction.Status, auction.UpdatedAt = AuctionStatusLive, now
	return auction, nil
}

func (r AuctionRules) Close(auction Auction, now time.Time) (Auction, error) {
	if auction.Status == AuctionStatusClosed {
		return auction, nil
	}
	if auction.Status != AuctionStatusLive {
		return Auction{}, ErrAuctionNotLive
	}
	if now.Before(auction.EndsAt) {
		return Auction{}, ErrAuctionNotEnded
	}
	auction.Status, auction.UpdatedAt = AuctionStatusClosed, now.UTC()
	return auction, nil
}

func (r AuctionRules) ValidateBid(auction Auction, bidderID uuid.UUID, amount int64, now time.Time) error {
	if auction.Status != AuctionStatusLive || now.Before(auction.StartsAt) || !now.Before(auction.EndsAt) {
		return ErrAuctionNotLive
	}
	if !validAuction(auction) {
		return ErrInvalidAuction
	}
	if bidderID == uuid.Nil || bidderID == auction.SellerID {
		return ErrSellerCannotBid
	}
	if amount < 0 || auction.CurrentPriceCents < 0 || auction.StartingPriceCents < 0 || auction.MinimumIncrementCents <= 0 {
		return ErrInvalidAuction
	}
	minimum := auction.StartingPriceCents
	if auction.CurrentWinnerID != nil {
		var err error
		minimum, err = safeAdd(auction.CurrentPriceCents, auction.MinimumIncrementCents)
		if err != nil {
			return ErrInvalidAuction
		}
	}
	if amount < minimum {
		return ErrBidTooLow
	}
	return nil
}

func (r AuctionRules) ApplyBid(auction Auction, bidderID uuid.UUID, amount int64, now time.Time) (Auction, error) {
	if err := r.ValidateBid(auction, bidderID, amount, now); err != nil {
		return Auction{}, err
	}
	auction.CurrentPriceCents = amount
	auction.CurrentWinnerID = &bidderID
	if auction.AntiSnipingWindowSeconds > 0 && auction.AntiSnipingExtensionSeconds > 0 && auction.EndsAt.Sub(now) <= secondsDuration(auction.AntiSnipingWindowSeconds) {
		auction.EndsAt = auction.EndsAt.Add(secondsDuration(auction.AntiSnipingExtensionSeconds))
	}
	auction.UpdatedAt = now.UTC()
	return auction, nil
}

type auctionService struct {
	repository AuctionRepository
	products   ProductClient
	now        func() time.Time
}

func NewAuctionService(repository AuctionRepository, products ProductClient) *auctionService {
	return &auctionService{repository: repository, products: products, now: func() time.Time { return time.Now().UTC() }}
}

func (s *auctionService) Create(ctx context.Context, sellerID uuid.UUID, input CreateAuctionInput) (Auction, error) {
	if sellerID == uuid.Nil || s.products == nil || s.repository == nil {
		return Auction{}, ErrInvalidAuction
	}
	product, err := s.products.Ownership(ctx, input.ProductID, sellerID)
	if err != nil {
		return Auction{}, err
	}
	if product.SellerID != sellerID || product.Status != "available" {
		return Auction{}, ErrProductUnavailable
	}
	auction, err := NewAuction(sellerID, product, input, s.now())
	if err != nil {
		return Auction{}, err
	}
	if err := s.products.Lock(ctx, auction.ProductID, auction.ID); err != nil {
		return Auction{}, err
	}
	if err := s.repository.Create(ctx, auction); err != nil {
		unlockErr := s.products.Unlock(context.WithoutCancel(ctx), auction.ProductID, auction.ID)
		return Auction{}, errors.Join(err, unlockErr)
	}
	return auction, nil
}

func (s *auctionService) Get(ctx context.Context, auctionID uuid.UUID) (AuctionView, error) {
	if auctionID == uuid.Nil || s.repository == nil {
		return AuctionView{}, ErrAuctionNotFound
	}
	auction, err := s.repository.Get(ctx, auctionID)
	if err != nil {
		return AuctionView{}, err
	}
	return AuctionView{Auction: auction}, nil
}

func validAuction(auction Auction) bool {
	if auction.ID == uuid.Nil || auction.ProductID == uuid.Nil || auction.SellerID == uuid.Nil || !auction.StartsAt.Before(auction.EndsAt) || auction.StartingPriceCents < 0 || auction.CurrentPriceCents < auction.StartingPriceCents || auction.MinimumIncrementCents <= 0 || auction.AntiSnipingWindowSeconds < 0 || auction.AntiSnipingExtensionSeconds < 0 || !validSeconds(auction.AntiSnipingWindowSeconds) || !validSeconds(auction.AntiSnipingExtensionSeconds) {
		return false
	}
	if auction.CurrentWinnerID == nil {
		return auction.CurrentPriceCents == auction.StartingPriceCents
	}
	return *auction.CurrentWinnerID != uuid.Nil && *auction.CurrentWinnerID != auction.SellerID
}

func validSeconds(value int64) bool { return value <= math.MaxInt64/int64(time.Second) }

func secondsDuration(value int64) time.Duration { return time.Duration(value) * time.Second }

func safeAdd(left, right int64) (int64, error) {
	if right > 0 && left > math.MaxInt64-right {
		return 0, fmt.Errorf("integer overflow")
	}
	return left + right, nil
}
