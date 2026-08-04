package main

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/auction/packages/contracts"
	"github.com/example/auction/packages/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func transactionIntegrationDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("TRANSACTION_TEST_DSN"); dsn != "" {
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
	return (&url.URL{Scheme: "postgres", Host: host + ":" + port, User: url.UserPassword(user, password), Path: "/transaction_db", RawQuery: "sslmode=disable"}).String()
}

func transactionIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	pool, err := postgres.Open(ctx, transactionIntegrationDSN(t))
	if err != nil {
		t.Fatalf("open transaction database: %v", err)
	}
	if err := postgres.ApplyMigrations(ctx, pool, os.DirFS(filepath.Join("..", "..")), "migrations/transaction"); err != nil {
		t.Fatalf("apply transaction migrations: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE settlements, outbox_events CASCADE`); err != nil {
		t.Fatalf("truncate transaction tables: %v", err)
	}
	return pool
}

func TestSettlementCreationIsIdempotent(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("set INTEGRATION_TEST=1 to run against Compose PostgreSQL transaction_db")
	}
	ctx := context.Background()
	pool := transactionIntegrationPool(t, ctx)
	defer pool.Close()

	repository := NewSettlementRepository(pool)
	auctionID := uuid.NewString()
	closeEvent := contracts.AuctionClosed{
		AuctionID:       auctionID,
		SellerID:        uuid.NewString(),
		BidID:           uuid.NewString(),
		FinalPriceCents: 500,
		WinnerID:        uuid.NewString(),
	}

	first, err := repository.CreateFromCloseTx(ctx, closeEvent)
	if err != nil {
		t.Fatalf("create settlement first: %v", err)
	}
	second, err := repository.CreateFromCloseTx(ctx, closeEvent)
	if err != nil {
		t.Fatalf("create settlement second: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected same settlement, got %s and %s", first.ID, second.ID)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM settlements WHERE auction_id = $1`, auctionID).Scan(&count); err != nil {
		t.Fatalf("count settlements: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one settlement row, got %d", count)
	}

	var outboxCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE event_type = 'transaction.settlement.created.v1' AND aggregate_id = $1`, auctionID).Scan(&outboxCount); err != nil {
		t.Fatalf("count settlement outbox events: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("expected exactly one settlement outbox event, got %d", outboxCount)
	}
}
