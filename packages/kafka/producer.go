package kafka

import (
	"context"
	"time"

	"github.com/segmentio/kafka-go"
)

type ProducerConfig struct {
	Brokers      []string
	RequiredAcks int
	WriteTimeout time.Duration
	BatchTimeout time.Duration
}

type Producer struct{ writer *kafka.Writer }

func NewProducer(cfg ProducerConfig) *Producer {
	if cfg.RequiredAcks == 0 {
		cfg.RequiredAcks = int(kafka.RequireAll)
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 10 * time.Second
	}
	if cfg.BatchTimeout == 0 {
		cfg.BatchTimeout = 10 * time.Millisecond
	}
	return &Producer{writer: kafka.NewWriter(kafka.WriterConfig{
		Brokers:      cfg.Brokers,
		Balancer:     &kafka.Hash{},
		RequiredAcks: cfg.RequiredAcks,
		WriteTimeout: cfg.WriteTimeout,
		BatchTimeout: cfg.BatchTimeout,
	})}
}

func (p *Producer) Publish(ctx context.Context, topic, key string, value []byte) error {
	return p.writer.WriteMessages(ctx, kafka.Message{Topic: topic, Key: []byte(key), Value: value})
}

func (p *Producer) Close() error { return p.writer.Close() }
