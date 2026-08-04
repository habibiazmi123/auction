package kafka

import (
	"context"
	"time"

	"github.com/segmentio/kafka-go"
)

type ConsumerConfig struct {
	Brokers     []string
	Topic       string
	GroupID     string
	MinBytes    int
	MaxBytes    int
	MaxWait     time.Duration
	StartOffset int64
}

type Message struct {
	Key       []byte
	Payload   []byte
	Topic     string
	Partition int
	Offset    int64
	Timestamp time.Time
}

type Handler func(context.Context, Message) error

type Consumer struct{ reader *kafka.Reader }

func NewConsumer(cfg ConsumerConfig) *Consumer {
	if cfg.MinBytes == 0 {
		cfg.MinBytes = 1
	}
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = 10e6
	}
	if cfg.MaxWait == 0 {
		cfg.MaxWait = 500 * time.Millisecond
	}
	return &Consumer{reader: kafka.NewReader(kafka.ReaderConfig{
		Brokers:        cfg.Brokers,
		Topic:          cfg.Topic,
		GroupID:        cfg.GroupID,
		MinBytes:       cfg.MinBytes,
		MaxBytes:       cfg.MaxBytes,
		MaxWait:        cfg.MaxWait,
		StartOffset:    cfg.StartOffset,
		CommitInterval: 0,
	})}
}

func (c *Consumer) Run(ctx context.Context, handler Handler) error {
	for {
		message, err := c.reader.FetchMessage(ctx)
		if err != nil {
			return err
		}
		if err := handler(ctx, Message{
			Key: message.Key, Payload: message.Value, Topic: message.Topic,
			Partition: message.Partition, Offset: message.Offset, Timestamp: message.Time,
		}); err != nil {
			return err
		}
		if err := c.reader.CommitMessages(ctx, message); err != nil {
			return err
		}
	}
}

func (c *Consumer) Consume(ctx context.Context, handler Handler) error { return c.Run(ctx, handler) }

func (c *Consumer) Close() error { return c.reader.Close() }
