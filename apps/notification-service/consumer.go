package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/example/auction/packages/contracts"
	"github.com/example/auction/packages/kafka"
)

type PublicEventProcessor interface {
	Process(ctx context.Context, eventID string, eventType string, payload json.RawMessage) error
}

type publicEventProcessor struct {
	repository NotificationRepository
}

func NewPublicEventProcessor(repository NotificationRepository) PublicEventProcessor {
	return &publicEventProcessor{repository: repository}
}

func (p *publicEventProcessor) Process(ctx context.Context, eventID string, eventType string, payload json.RawMessage) error {
	switch eventType {
	case "auction.bid.placed.v1":
		return p.processBidPlaced(ctx, eventID, payload)
	case "auction.closed.v1":
		return p.processAuctionClosed(ctx, eventID, payload)
	case "transaction.settlement.created.v1":
		return p.processSettlementCreated(ctx, eventID, payload)
	default:
		return nil
	}
}

func (p *publicEventProcessor) processBidPlaced(ctx context.Context, eventID string, payload json.RawMessage) error {
	var event contracts.BidPlaced
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil
	}
	if event.Status == "rejected" {
		return p.repository.CreateIfAbsent(ctx, eventID, NotificationInput{
			RecipientID: event.BidderID,
			Type:        "bid_rejected",
			AuctionID:   event.AuctionID,
			Title:       "Bid rejected",
			Body:        "Your bid was rejected.",
		})
	}
	return p.repository.CreateIfAbsent(ctx, eventID, NotificationInput{
		RecipientID: event.BidderID,
		Type:        "bid_accepted",
		AuctionID:   event.AuctionID,
		Title:       "Bid accepted",
		Body:        "Your bid is now the highest.",
	})
}

func (p *publicEventProcessor) processAuctionClosed(ctx context.Context, eventID string, payload json.RawMessage) error {
	var event contracts.AuctionClosed
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil
	}
	if event.WinnerID != "" {
		if err := p.repository.CreateIfAbsent(ctx, eventID, NotificationInput{
			RecipientID: event.WinnerID,
			Type:        "auction_won",
			AuctionID:   event.AuctionID,
			Title:       "Auction won",
			Body:        "You won the auction.",
		}); err != nil {
			return err
		}
	}
	return p.repository.CreateIfAbsent(ctx, eventID, NotificationInput{
		RecipientID: event.SellerID,
		Type:        "auction_closed",
		AuctionID:   event.AuctionID,
		Title:       "Auction closed",
		Body:        "Your auction has closed.",
	})
}

func (p *publicEventProcessor) processSettlementCreated(ctx context.Context, eventID string, payload json.RawMessage) error {
	var event contracts.SettlementCreated
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil
	}
	if err := p.repository.CreateIfAbsent(ctx, eventID, NotificationInput{
		RecipientID: event.BuyerID,
		Type:        "settlement_created",
		AuctionID:   event.AuctionID,
		Title:       "Settlement created",
		Body:        "A settlement has been created for your auction.",
	}); err != nil {
		return err
	}
	return p.repository.CreateIfAbsent(ctx, eventID, NotificationInput{
		RecipientID: event.SellerID,
		Type:        "settlement_created",
		AuctionID:   event.AuctionID,
		Title:       "Settlement created",
		Body:        "A settlement has been created for your auction.",
	})
}

func RunPublicEventConsumer(ctx context.Context, cfg kafka.ConsumerConfig, repository NotificationRepository) error {
	processor := NewPublicEventProcessor(repository)
	backoff := time.Second
	for {
		consumer := kafka.NewConsumer(cfg)
		err := consumer.Run(ctx, func(ctx context.Context, message kafka.Message) error {
			var envelope contracts.EventEnvelope[json.RawMessage]
			if err := json.Unmarshal(message.Payload, &envelope); err != nil {
				slog.WarnContext(ctx, "drop malformed public event", "error", err)
				return nil
			}
			return processor.Process(ctx, envelope.EventID, envelope.EventType, envelope.Payload)
		})
		consumer.Close()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.Error("public event consumer stopped, reconnecting", "error", err, "backoff", backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
	}
}
