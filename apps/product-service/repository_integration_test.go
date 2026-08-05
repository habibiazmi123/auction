package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/habibiazmi123/auction/packages/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoryIntegrationWithProductDatabase(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("set INTEGRATION_TEST=1 to run against Compose PostgreSQL product_db")
	}

	ctx := context.Background()
	dsn := productIntegrationDSN(t)
	pool, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open product database: %v", err)
	}
	defer pool.Close()
	if err := postgres.ApplyMigrations(ctx, pool, os.DirFS(filepath.Join("..", "..")), "migrations/product"); err != nil {
		t.Fatalf("apply product migrations: %v", err)
	}

	repository := NewRepository(pool)
	product := Product{ID: uuid.New(), SellerID: uuid.New(), Name: "integration camera", Description: "integration test product", Quantity: 1, Status: ProductStatusAvailable, Version: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM outbox_events WHERE aggregate_id = $1`, product.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, product.ID)
	})

	if err := repository.Create(ctx, product); err != nil {
		t.Fatalf("create product transaction: %v", err)
	}
	assertProductOutboxCount(t, ctx, pool, product.ID, 1)

	product.Name = "updated integration camera"
	product.Description = "updated integration test product"
	if err := repository.Update(ctx, product); err != nil {
		t.Fatalf("update product transaction: %v", err)
	}
	assertProductOutboxCount(t, ctx, pool, product.ID, 2)

	auctionID, otherAuctionID := uuid.New(), uuid.New()
	if err := repository.Lock(ctx, product.ID, auctionID); err != nil {
		t.Fatalf("lock product: %v", err)
	}
	if err := repository.Lock(ctx, product.ID, auctionID); err != nil {
		t.Fatalf("idempotent lock: %v", err)
	}
	assertProductOutboxCount(t, ctx, pool, product.ID, 3)
	if err := repository.Lock(ctx, product.ID, otherAuctionID); !errors.Is(err, ErrProductLocked) {
		t.Fatalf("different lock error: got %v, want %v", err, ErrProductLocked)
	}
	if err := repository.Unlock(ctx, product.ID, otherAuctionID); !errors.Is(err, ErrProductLocked) {
		t.Fatalf("different unlock error: got %v, want %v", err, ErrProductLocked)
	}
	if err := repository.Unlock(ctx, product.ID, auctionID); err != nil {
		t.Fatalf("unlock product: %v", err)
	}
	assertProductOutboxCount(t, ctx, pool, product.ID, 4)
	if err := repository.Unlock(ctx, product.ID, auctionID); !errors.Is(err, ErrProductLocked) {
		t.Fatalf("unlocked product error: got %v, want %v", err, ErrProductLocked)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO products (id, seller_id, name, description, quantity, status, auction_id) VALUES ($1, $2, 'invalid', 'invalid', 1, 'available', $3)`, uuid.New(), product.SellerID, uuid.New()); err == nil {
		t.Fatal("expected invalid status/auction combination to fail")
	}
}

func productIntegrationDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("PRODUCT_TEST_DSN"); dsn != "" {
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
	return (&url.URL{Scheme: "postgres", Host: host + ":" + port, User: url.UserPassword(user, password), Path: "/product_db", RawQuery: "sslmode=disable"}).String()
}

func assertProductOutboxCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, productID uuid.UUID, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1`, productID).Scan(&got); err != nil {
		t.Fatalf("count product outbox events: %v", err)
	}
	if got != want {
		t.Fatalf("outbox events: got %d, want %d", got, want)
	}
}
