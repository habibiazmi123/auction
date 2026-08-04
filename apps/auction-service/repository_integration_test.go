package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/auction/packages/postgres"
	"github.com/google/uuid"
)

func TestAuctionRepositoryVersionUpdateAndConflict(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("set INTEGRATION_TEST=1 to run against Compose PostgreSQL auction_db")
	}

	ctx := context.Background()
	dsn := auctionIntegrationDSN(t)
	pool, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open auction database: %v", err)
	}
	defer pool.Close()
	if err := postgres.ApplyMigrations(ctx, pool, os.DirFS(filepath.Join("..", "..")), "migrations/auction"); err != nil {
		t.Fatalf("apply auction migrations: %v", err)
	}

	now := time.Now().UTC()
	auction := Auction{
		ID: uuid.New(), ProductID: uuid.New(), SellerID: uuid.New(), ProductName: "camera", ProductDescription: "used", ProductQuantity: 1,
		Status: AuctionStatusScheduled, StartingPriceCents: 100, CurrentPriceCents: 100, MinimumIncrementCents: 10,
		StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	repository := NewRepository(pool)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM auctions WHERE id = $1`, auction.ID) })
	if err := repository.Create(ctx, auction); err != nil {
		t.Fatalf("create auction: %v", err)
	}

	auction.Status = AuctionStatusLive
	if err := repository.Update(ctx, auction); err != nil {
		t.Fatalf("update auction: %v", err)
	}
	updated, err := repository.Get(ctx, auction.ID)
	if err != nil {
		t.Fatalf("get updated auction: %v", err)
	}
	if updated.Version != 2 || updated.Status != AuctionStatusLive {
		t.Fatalf("updated auction: version=%d status=%q", updated.Version, updated.Status)
	}

	if err := repository.Update(ctx, auction); !errors.Is(err, ErrAuctionVersionConflict) {
		t.Fatalf("stale update error: got %v want %v", err, ErrAuctionVersionConflict)
	}
	if err := repository.Update(ctx, Auction{ID: uuid.New(), Version: 1}); !errors.Is(err, ErrAuctionNotFound) {
		t.Fatalf("missing update error: got %v want %v", err, ErrAuctionNotFound)
	}
}

func auctionIntegrationDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("AUCTION_TEST_DSN"); dsn != "" {
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
	return (&url.URL{Scheme: "postgres", Host: host + ":" + port, User: url.UserPassword(user, password), Path: "/auction_db", RawQuery: "sslmode=disable"}).String()
}
