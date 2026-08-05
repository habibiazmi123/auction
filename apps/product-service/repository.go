package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/habibiazmi123/auction/packages/contracts"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *repository { return &repository{pool: pool} }

func (r *repository) Create(ctx context.Context, product Product) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin create product: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO products (id, seller_id, name, description, quantity, status, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		product.ID, product.SellerID, product.Name, product.Description, product.Quantity, product.Status, product.Version, product.CreatedAt, product.UpdatedAt); err != nil {
		return fmt.Errorf("insert product: %w", err)
	}
	if err := insertOutbox(ctx, tx, "product.created.v1", product, product.Version); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit create product: %w", err)
	}
	return nil
}

func (r *repository) Get(ctx context.Context, id uuid.UUID) (Product, error) {
	product, err := scanProduct(r.pool.QueryRow(ctx, `
		SELECT id, seller_id, name, description, quantity, status, auction_id, version, created_at, updated_at
		FROM products WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrProductNotFound
	}
	if err != nil {
		return Product{}, fmt.Errorf("get product: %w", err)
	}
	return product, nil
}

func (r *repository) List(ctx context.Context, page Page) ([]Product, PageInfo, error) {
	var err error
	page, err = normalizePage(page)
	if err != nil {
		return nil, PageInfo{}, err
	}
	offset := (page.Number - 1) * page.Size
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM products`).Scan(&total); err != nil {
		return nil, PageInfo{}, fmt.Errorf("count products: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, seller_id, name, description, quantity, status, auction_id, version, created_at, updated_at
		FROM products ORDER BY created_at DESC, id LIMIT $1 OFFSET $2`, page.Size, offset)
	if err != nil {
		return nil, PageInfo{}, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()
	products := make([]Product, 0, page.Size)
	for rows.Next() {
		product, err := scanProduct(rows)
		if err != nil {
			return nil, PageInfo{}, fmt.Errorf("scan product: %w", err)
		}
		products = append(products, product)
	}
	if err := rows.Err(); err != nil {
		return nil, PageInfo{}, fmt.Errorf("read products: %w", err)
	}
	return products, PageInfo{Page: page.Number, PageSize: page.Size, Total: total}, nil
}

func (r *repository) Update(ctx context.Context, product Product) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin update product: %w", err)
	}
	defer tx.Rollback(ctx)
	current, err := scanProduct(tx.QueryRow(ctx, `
		SELECT id, seller_id, name, description, quantity, status, auction_id, version, created_at, updated_at
		FROM products WHERE id = $1 FOR UPDATE`, product.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrProductNotFound
	}
	if err != nil {
		return fmt.Errorf("lock product for update: %w", err)
	}
	if current.SellerID != product.SellerID {
		return ErrProductNotOwner
	}
	if current.Status == ProductStatusLocked {
		return ErrProductLocked
	}
	product.Status, product.AuctionID, product.CreatedAt = current.Status, current.AuctionID, current.CreatedAt
	product.Version, product.UpdatedAt = current.Version+1, time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		UPDATE products SET name = $2, description = $3, quantity = $4, version = $5, updated_at = $6
		WHERE id = $1`, product.ID, product.Name, product.Description, product.Quantity, product.Version, product.UpdatedAt); err != nil {
		return fmt.Errorf("update product: %w", err)
	}
	if err := insertOutbox(ctx, tx, "product.updated.v1", product, product.Version); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit update product: %w", err)
	}
	return nil
}

func (r *repository) Lock(ctx context.Context, productID, auctionID uuid.UUID) error {
	return r.changeLock(ctx, productID, auctionID, true)
}

func (r *repository) Unlock(ctx context.Context, productID, auctionID uuid.UUID) error {
	return r.changeLock(ctx, productID, auctionID, false)
}

func (r *repository) changeLock(ctx context.Context, productID, auctionID uuid.UUID, lock bool) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin product lock change: %w", err)
	}
	defer tx.Rollback(ctx)
	product, err := scanProduct(tx.QueryRow(ctx, `
		SELECT id, seller_id, name, description, quantity, status, auction_id, version, created_at, updated_at
		FROM products WHERE id = $1 FOR UPDATE`, productID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrProductNotFound
	}
	if err != nil {
		return fmt.Errorf("lock product row: %w", err)
	}
	updated, changed, err := transitionProductLock(product, auctionID, lock)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	product = updated
	product.Version++
	product.UpdatedAt = time.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE products SET status = $2, auction_id = $3, version = $4, updated_at = $5 WHERE id = $1`, product.ID, product.Status, product.AuctionID, product.Version, product.UpdatedAt); err != nil {
		return fmt.Errorf("update product lock: %w", err)
	}
	if err := insertOutbox(ctx, tx, "product.updated.v1", product, product.Version); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit product lock change: %w", err)
	}
	return nil
}

type rowScanner interface{ Scan(...any) error }

func scanProduct(row rowScanner) (Product, error) {
	var product Product
	err := row.Scan(&product.ID, &product.SellerID, &product.Name, &product.Description, &product.Quantity, &product.Status, &product.AuctionID, &product.Version, &product.CreatedAt, &product.UpdatedAt)
	return product, err
}

func insertOutbox(ctx context.Context, tx pgx.Tx, eventType string, product Product, version int64) error {
	payload, err := json.Marshal(contracts.EventEnvelope[Product]{
		EventID: uuid.NewString(), EventType: eventType, Version: 1, OccurredAt: time.Now().UTC(), Producer: "product-service", Payload: product,
	})
	if err != nil {
		return fmt.Errorf("marshal %s event: %w", eventType, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO outbox_events (aggregate_type, aggregate_id, aggregate_version, event_type, event_key, payload)
		VALUES ('product', $1, $2, $3, $1, $4)`, product.ID, version, eventType, payload); err != nil {
		return fmt.Errorf("insert %s outbox event: %w", eventType, err)
	}
	return nil
}
