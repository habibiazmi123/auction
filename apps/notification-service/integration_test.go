package main

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/habibiazmi123/auction/packages/contracts"
	"github.com/habibiazmi123/auction/packages/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func notificationIntegrationDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("NOTIFICATION_TEST_DSN"); dsn != "" {
		return dsn
	}
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5432"
	}
	user, password := os.Getenv("POSTGRES_USER"), os.Getenv("POSTGRES_PASSWORD")
	if user == "" || password == "" {
		t.Fatal("POSTGRES_USER and POSTGRES_PASSWORD are required when INTEGRATION_TEST=1")
	}
	return (&url.URL{Scheme: "postgres", Host: host + ":" + port, User: url.UserPassword(user, password), Path: "/notification_db", RawQuery: "sslmode=disable"}).String()
}

func notificationIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	pool, err := postgres.Open(ctx, notificationIntegrationDSN(t))
	if err != nil {
		t.Fatalf("open notification database: %v", err)
	}
	if err := postgres.ApplyMigrations(ctx, pool, os.DirFS(filepath.Join("..", "..")), "migrations/notification"); err != nil {
		t.Fatalf("apply notification migrations: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE notifications, outbox_events CASCADE`); err != nil {
		t.Fatalf("truncate notification tables: %v", err)
	}
	return pool
}

func TestNotificationCreationIsIdempotentBySourceEvent(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("set INTEGRATION_TEST=1 to run against Compose PostgreSQL notification_db")
	}
	ctx := context.Background()
	pool := notificationIntegrationPool(t, ctx)
	defer pool.Close()

	repository := NewNotificationRepository(pool)
	eventID := uuid.NewString()
	recipientID := uuid.NewString()
	auctionID := uuid.NewString()

	input := NotificationInput{
		RecipientID: recipientID,
		Type:        "auction_closed",
		AuctionID:   auctionID,
		Title:       "Auction closed",
		Body:        "Your auction has closed.",
	}
	if err := repository.CreateIfAbsent(ctx, eventID, input); err != nil {
		t.Fatalf("create notification first: %v", err)
	}
	if err := repository.CreateIfAbsent(ctx, eventID, input); err != nil {
		t.Fatalf("create notification second: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE source_event_id = $1`, eventID).Scan(&count); err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one notification per source event, got %d", count)
	}

	var outboxCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE event_type = 'notification.created.v1' AND aggregate_id = $1`, recipientID).Scan(&outboxCount); err != nil {
		t.Fatalf("count notification outbox events: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("expected exactly one notification outbox event, got %d", outboxCount)
	}

	notifications, err := repository.GetByRecipient(ctx, uuid.MustParse(recipientID), 10)
	if err != nil {
		t.Fatalf("get notifications: %v", err)
	}
	if len(notifications) != 1 {
		t.Fatalf("expected one notification for recipient, got %d", len(notifications))
	}
}

func TestNotificationIDInPayload(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("set INTEGRATION_TEST=1 to run against Compose PostgreSQL notification_db")
	}
	ctx := context.Background()
	pool := notificationIntegrationPool(t, ctx)
	defer pool.Close()

	repository := NewNotificationRepository(pool)
	eventID := uuid.NewString()
	recipientID := uuid.NewString()
	auctionID := uuid.NewString()

	input := NotificationInput{
		RecipientID: recipientID,
		Type:        "auction_closed",
		AuctionID:   auctionID,
		Title:       "Auction closed",
		Body:        "Your auction has closed.",
	}
	if err := repository.CreateIfAbsent(ctx, eventID, input); err != nil {
		t.Fatalf("create notification: %v", err)
	}

	var rowID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM notifications WHERE source_event_id = $1 AND recipient_id = $2`, eventID, recipientID).Scan(&rowID); err != nil {
		t.Fatalf("query notification id: %v", err)
	}

	var payload []byte
	if err := pool.QueryRow(ctx, `SELECT payload FROM outbox_events WHERE event_type = 'notification.created.v1' AND aggregate_id = $1`, recipientID).Scan(&payload); err != nil {
		t.Fatalf("query outbox payload: %v", err)
	}

	var envelope contracts.EventEnvelope[contracts.NotificationCreated]
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("unmarshal outbox payload: %v", err)
	}

	if envelope.Payload.NotificationID != rowID {
		t.Fatalf("notification id mismatch: payload has %s, database row is %s", envelope.Payload.NotificationID, rowID)
	}
}

func TestNotificationProcessorCreatesForMultipleRecipients(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("set INTEGRATION_TEST=1 to run against Compose PostgreSQL notification_db")
	}
	ctx := context.Background()
	pool := notificationIntegrationPool(t, ctx)
	defer pool.Close()

	repository := NewNotificationRepository(pool)
	processor := NewPublicEventProcessor(repository)

	sellerID := uuid.NewString()
	buyerID := uuid.NewString()
	auctionID := uuid.NewString()
	eventID := uuid.NewString()
	payload, _ := json.Marshal(contracts.AuctionClosed{
		AuctionID:       auctionID,
		SellerID:        sellerID,
		FinalPriceCents: 500,
		WinnerID:        buyerID,
	})

	if err := processor.Process(ctx, eventID, "auction.closed.v1", payload); err != nil {
		t.Fatalf("process auction closed: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE source_event_id = $1`, eventID).Scan(&count); err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected notifications for seller and buyer, got %d", count)
	}
}
