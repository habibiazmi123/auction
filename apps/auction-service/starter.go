package main

import (
	"context"
	"time"
)

type auctionStarter struct {
	repository AuctionRepository
	now        func() time.Time
}

func NewAuctionStarter(repository AuctionRepository) AuctionStarter {
	return &auctionStarter{repository: repository, now: func() time.Time { return time.Now().UTC() }}
}

func (s *auctionStarter) StartDue(ctx context.Context, now time.Time) (int, error) {
	return s.repository.StartDue(ctx, now)
}
