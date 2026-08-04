package main

import (
	"context"
	"time"
)

type auctionCloser struct {
	repository AuctionRepository
	now        func() time.Time
}

func NewAuctionCloser(repository AuctionRepository) AuctionCloser {
	return &auctionCloser{repository: repository, now: func() time.Time { return time.Now().UTC() }}
}

func (c *auctionCloser) CloseDue(ctx context.Context, now time.Time) (int, error) {
	return c.repository.CloseDue(ctx, now)
}
