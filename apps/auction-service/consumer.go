package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/example/auction/packages/contracts"
)

type bidProcessor struct {
	repository AuctionRepository
}

func NewBidProcessor(repository AuctionRepository) BidProcessor {
	return &bidProcessor{repository: repository}
}

func (p *bidProcessor) Handle(ctx context.Context, command contracts.BidCommand) error {
	_, err := p.repository.ApplyBidTx(ctx, command)
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrBidIdempotencyConflict) || errors.Is(err, ErrBidTooLow) ||
		errors.Is(err, ErrSellerCannotBid) || errors.Is(err, ErrAuctionNotLive) ||
		errors.Is(err, ErrInvalidAuction) || errors.Is(err, ErrAuctionNotFound) {
		slog.WarnContext(ctx, "bid command rejected, committing offset", "error", err)
		return nil
	}
	return err
}
