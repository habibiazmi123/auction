package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/example/auction/packages/contracts"
	"github.com/example/auction/packages/kafka"
	"github.com/example/auction/packages/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	kafkago "github.com/segmentio/kafka-go"
)

func TestBidConcurrentBids(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("set INTEGRATION_TEST=1 to run against Compose PostgreSQL and Kafka")
	}
	ctx := context.Background()
	pool := integrationPool(t, ctx)
	defer pool.Close()
	repository := NewRepository(pool)
	auction := insertLiveAuction(t, ctx, repository, 100, 10)

	bidder1 := uuid.NewString()
	bidder2 := uuid.NewString()
	commands := []contracts.BidCommand{
		{BidID: uuid.NewString(), CommandID: uuid.NewString(), AuctionID: auction.ID.String(), BidderID: bidder1, AmountCents: 150, IdempotencyKey: "idem-1"},
		{BidID: uuid.NewString(), CommandID: uuid.NewString(), AuctionID: auction.ID.String(), BidderID: bidder2, AmountCents: 200, IdempotencyKey: "idem-2"},
	}
	producer := integrationProducer(t)
	defer producer.Close()

	consumer := integrationConsumer(t, "auction.bid.commands.v1", "test-concurrent-"+uuid.NewString())
	defer consumer.Close()
	processor := NewBidProcessor(repository)
	wait := consumeAsync(t, ctx, consumer, processor, auction.ID.String(), len(commands))
	for _, command := range commands {
		payload, _ := json.Marshal(command)
		if err := producer.Publish(ctx, "auction.bid.commands.v1", command.AuctionID, payload); err != nil {
			t.Fatalf("publish command: %v", err)
		}
	}
	wait()

	updated, err := repository.Get(ctx, auction.ID)
	if err != nil {
		t.Fatalf("get auction: %v", err)
	}
	if updated.CurrentPriceCents != 200 || updated.CurrentWinnerID == nil || updated.CurrentWinnerID.String() != bidder2 {
		t.Fatalf("expected bidder2 to win at 200, got price=%d winner=%v", updated.CurrentPriceCents, updated.CurrentWinnerID)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM bids WHERE auction_id = $1`, auction.ID).Scan(&count); err != nil {
		t.Fatalf("count bids: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 bids, got %d", count)
	}
}

func TestBidDuplicateDelivery(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("set INTEGRATION_TEST=1 to run against Compose PostgreSQL and Kafka")
	}
	ctx := context.Background()
	pool := integrationPool(t, ctx)
	defer pool.Close()
	repository := NewRepository(pool)
	auction := insertLiveAuction(t, ctx, repository, 100, 10)

	command := contracts.BidCommand{
		BidID: uuid.NewString(), CommandID: uuid.NewString(), AuctionID: auction.ID.String(),
		BidderID: uuid.NewString(), AmountCents: 150, IdempotencyKey: "idem-dup",
	}
	producer := integrationProducer(t)
	defer producer.Close()
	payload, _ := json.Marshal(command)

	consumer := integrationConsumer(t, "auction.bid.commands.v1", "test-duplicate-"+uuid.NewString())
	defer consumer.Close()
	processor := NewBidProcessor(repository)
	wait := consumeAsync(t, ctx, consumer, processor, auction.ID.String(), 2)
	for i := 0; i < 2; i++ {
		if err := producer.Publish(ctx, "auction.bid.commands.v1", command.AuctionID, payload); err != nil {
			t.Fatalf("publish command: %v", err)
		}
	}
	wait()

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM bids WHERE auction_id = $1`, auction.ID).Scan(&count); err != nil {
		t.Fatalf("count bids: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 bid result from duplicate delivery, got %d", count)
	}
}

func TestOutboxRetryRepublishes(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("set INTEGRATION_TEST=1 to run against Compose PostgreSQL and Kafka")
	}
	ctx := context.Background()
	pool := integrationPool(t, ctx)
	defer pool.Close()
	repository := NewRepository(pool)
	auction := insertLiveAuction(t, ctx, repository, 100, 10)

	command := contracts.BidCommand{
		BidID: uuid.NewString(), CommandID: uuid.NewString(), AuctionID: auction.ID.String(),
		BidderID: uuid.NewString(), AmountCents: 150, IdempotencyKey: "idem-retry",
	}
	result, err := repository.ApplyBidTx(ctx, command)
	if err != nil {
		t.Fatalf("apply bid: %v", err)
	}
	if result.Status != BidStatusAccepted {
		t.Fatalf("expected accepted bid, got %s", result.Status)
	}

	var eventID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM outbox_events WHERE aggregate_id = $1 AND sent_at IS NULL`, auction.ID.String()).Scan(&eventID); err != nil {
		t.Fatalf("find unsent outbox event: %v", err)
	}

	failOnce := &failingPublisher{failUntil: 1}
	relay := NewOutboxRelay(pool, failOnce)
	relay.PollInterval = 10 * time.Millisecond
	relay.BaseDelay = 1 * time.Millisecond
	relay.MaxAttempts = 5

	if _, err := relay.RunOnce(ctx); err != nil {
		t.Fatalf("first relay run: %v", err)
	}
	if failOnce.calls != 1 {
		t.Fatalf("expected first publish attempt to fail, calls=%d", failOnce.calls)
	}
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT attempts FROM outbox_events WHERE id = $1`, eventID).Scan(&attempts); err != nil {
		t.Fatalf("check attempts: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected attempts=1, got %d", attempts)
	}

	time.Sleep(100 * time.Millisecond)
	if _, err := relay.RunOnce(ctx); err != nil {
		t.Fatalf("second relay run: %v", err)
	}
	if failOnce.calls != 2 {
		t.Fatalf("expected retry publish attempt, calls=%d", failOnce.calls)
	}
	var sentAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT sent_at FROM outbox_events WHERE id = $1`, eventID).Scan(&sentAt); err != nil {
		t.Fatalf("check sent_at: %v", err)
	}
	if sentAt == nil {
		t.Fatal("expected outbox event to be marked sent after retry")
	}
	if failOnce.lastTopic != "auction.events.v1" || failOnce.lastKey != auction.ID.String() {
		t.Fatalf("expected topic=auction.events.v1 key=%s, got topic=%s key=%s", auction.ID.String(), failOnce.lastTopic, failOnce.lastKey)
	}
}

func TestCloseRetryEmitsOneCloseEvent(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("set INTEGRATION_TEST=1 to run against Compose PostgreSQL and Kafka")
	}
	ctx := context.Background()
	pool := integrationPool(t, ctx)
	defer pool.Close()
	repository := NewRepository(pool)
	now := time.Now().UTC()
	auction := Auction{
		ID: uuid.New(), ProductID: uuid.New(), SellerID: uuid.New(), ProductName: "camera", ProductDescription: "used", ProductQuantity: 1,
		Status: AuctionStatusLive, StartingPriceCents: 100, CurrentPriceCents: 100, MinimumIncrementCents: 10,
		StartsAt: now.Add(-time.Hour), EndsAt: now.Add(-time.Second), Version: 1, CreatedAt: now, UpdatedAt: now,
		AntiSnipingWindowSeconds: 0, AntiSnipingExtensionSeconds: 0,
	}
	if err := repository.Create(ctx, auction); err != nil {
		t.Fatalf("create auction: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM outbox_events WHERE aggregate_id = $1`, auction.ID.String())
		_, _ = pool.Exec(ctx, `DELETE FROM auctions WHERE id = $1`, auction.ID)
	}()

	closer := NewAuctionCloser(repository)
	first, err := closer.CloseDue(ctx, now)
	if err != nil {
		t.Fatalf("first close: %v", err)
	}
	if first != 1 {
		t.Fatalf("expected 1 auction closed, got %d", first)
	}
	second, err := closer.CloseDue(ctx, now)
	if err != nil {
		t.Fatalf("second close: %v", err)
	}
	if second != 0 {
		t.Fatalf("expected 0 auctions closed on retry, got %d", second)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1 AND event_type = 'auction.closed.v1'`, auction.ID.String()).Scan(&count); err != nil {
		t.Fatalf("count close events: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one close event, got %d", count)
	}
}

func integrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	pool, err := postgres.Open(ctx, auctionIntegrationDSN(t))
	if err != nil {
		t.Fatalf("open auction database: %v", err)
	}
	if err := postgres.ApplyMigrations(ctx, pool, os.DirFS(filepath.Join("..", "..")), "migrations/auction"); err != nil {
		t.Fatalf("apply auction migrations: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE bids, outbox_events, auctions CASCADE`); err != nil {
		t.Fatalf("truncate auction tables: %v", err)
	}
	return pool
}

func insertLiveAuction(t *testing.T, ctx context.Context, repository *repository, startingPrice, increment int64) Auction {
	t.Helper()
	now := time.Now().UTC()
	auction := Auction{
		ID: uuid.New(), ProductID: uuid.New(), SellerID: uuid.New(), ProductName: "camera", ProductDescription: "used", ProductQuantity: 1,
		Status: AuctionStatusLive, StartingPriceCents: startingPrice, CurrentPriceCents: startingPrice, MinimumIncrementCents: increment,
		StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour), Version: 1, CreatedAt: now, UpdatedAt: now,
		AntiSnipingWindowSeconds: 0, AntiSnipingExtensionSeconds: 0,
	}
	if err := repository.Create(ctx, auction); err != nil {
		t.Fatalf("create auction: %v", err)
	}
	t.Cleanup(func() {
		_, _ = repository.pool.Exec(ctx, `DELETE FROM bids WHERE auction_id = $1`, auction.ID)
		_, _ = repository.pool.Exec(ctx, `DELETE FROM outbox_events WHERE aggregate_id = $1`, auction.ID.String())
		_, _ = repository.pool.Exec(ctx, `DELETE FROM auctions WHERE id = $1`, auction.ID)
	})
	return auction
}

func kafkaBrokers() []string {
	if brokers := os.Getenv("AUCTION_TEST_KAFKA"); brokers != "" {
		return strings.Split(brokers, ",")
	}
	if brokers := os.Getenv("KAFKA_BROKERS"); brokers != "" && strings.Contains(brokers, "localhost") {
		return strings.Split(brokers, ",")
	}
	return []string{"localhost:9094"}
}

func integrationProducer(t *testing.T) *kafka.Producer {
	t.Helper()
	return kafka.NewProducer(kafka.ProducerConfig{Brokers: kafkaBrokers()})
}

func integrationConsumer(t *testing.T, topic, groupID string) *kafka.Consumer {
	t.Helper()
	return kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:     kafkaBrokers(),
		Topic:       topic,
		GroupID:     groupID,
		StartOffset: kafkago.FirstOffset,
	})
}

func consumeAsync(t *testing.T, ctx context.Context, consumer *kafka.Consumer, processor BidProcessor, targetAuctionID string, n int) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	processed := 0
	go func() {
		done <- consumer.Run(ctx, func(ctx context.Context, message kafka.Message) error {
			var command contracts.BidCommand
			if err := json.Unmarshal(message.Payload, &command); err != nil {
				return fmt.Errorf("unmarshal command: %w", err)
			}
			if err := processor.Handle(ctx, command); err != nil {
				return err
			}
			if command.AuctionID == targetAuctionID {
				processed++
				if processed >= n {
					cancel()
				}
			}
			return nil
		})
	}()
	return func() {
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("consume commands: %v", err)
			}
			if processed != n {
				t.Fatalf("expected %d commands processed, got %d", n, processed)
			}
		case <-time.After(15 * time.Second):
			cancel()
			t.Fatalf("timed out waiting for %d commands", n)
		}
	}
}

type failingPublisher struct {
	calls      int
	failUntil  int
	lastTopic  string
	lastKey    string
	lastValue  []byte
}

func (p *failingPublisher) Publish(ctx context.Context, topic, key string, value []byte) error {
	p.calls++
	p.lastTopic, p.lastKey, p.lastValue = topic, key, value
	if p.calls <= p.failUntil {
		return fmt.Errorf("simulated publish failure %d", p.calls)
	}
	return nil
}
