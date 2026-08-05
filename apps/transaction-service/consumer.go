package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/habibiazmi123/auction/packages/contracts"
	"github.com/habibiazmi123/auction/packages/kafka"
)

type CloseEventProcessor interface {
	Process(ctx context.Context, eventID string, event contracts.AuctionClosed) error
}

type closeEventProcessor struct {
	repository SettlementRepository
}

func NewCloseEventProcessor(repository SettlementRepository) CloseEventProcessor {
	return &closeEventProcessor{repository: repository}
}

func (p *closeEventProcessor) Process(ctx context.Context, eventID string, event contracts.AuctionClosed) error {
	if _, err := p.repository.CreateFromCloseTx(ctx, eventID, event); err != nil {
		return err
	}
	return nil
}

func RunCloseEventConsumer(ctx context.Context, cfg kafka.ConsumerConfig, repository SettlementRepository) error {
	processor := NewCloseEventProcessor(repository)
	backoff := time.Second
	for {
		consumer := kafka.NewConsumer(cfg)
		err := consumer.Run(ctx, func(ctx context.Context, message kafka.Message) error {
			var envelope contracts.EventEnvelope[contracts.AuctionClosed]
			if err := json.Unmarshal(message.Payload, &envelope); err != nil {
				slog.WarnContext(ctx, "drop malformed auction closed event", "error", err)
				return nil
			}
			return processor.Process(ctx, envelope.EventID, envelope.Payload)
		})
		consumer.Close()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.Error("close event consumer stopped, reconnecting", "error", err, "backoff", backoff)
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
