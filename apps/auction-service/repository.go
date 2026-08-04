package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *repository { return &repository{pool: pool} }

func (r *repository) Create(ctx context.Context, auction Auction) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO auctions (
			id, product_id, seller_id, product_name, product_description, product_quantity,
			status, starting_price_cents, current_price_cents, current_winner_id,
			minimum_increment_cents, starts_at, ends_at, anti_sniping_window_seconds,
			anti_sniping_extension_seconds, version, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
		auction.ID, auction.ProductID, auction.SellerID, auction.ProductName, auction.ProductDescription, auction.ProductQuantity,
		auction.Status, auction.StartingPriceCents, auction.CurrentPriceCents, auction.CurrentWinnerID,
		auction.MinimumIncrementCents, auction.StartsAt, auction.EndsAt, auction.AntiSnipingWindowSeconds,
		auction.AntiSnipingExtensionSeconds, auction.Version, auction.CreatedAt, auction.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrAuctionConflict
		}
		return fmt.Errorf("insert auction: %w", err)
	}
	return nil
}

func (r *repository) Get(ctx context.Context, id uuid.UUID) (Auction, error) {
	auction, err := scanAuction(r.pool.QueryRow(ctx, auctionSelect+` WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Auction{}, ErrAuctionNotFound
	}
	if err != nil {
		return Auction{}, fmt.Errorf("get auction: %w", err)
	}
	return auction, nil
}

func (r *repository) Update(ctx context.Context, auction Auction) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin update auction: %w", err)
	}
	defer tx.Rollback(ctx)
	previous, err := scanAuction(tx.QueryRow(ctx, auctionSelect+` WHERE id = $1 FOR UPDATE`, auction.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAuctionNotFound
	}
	if err != nil {
		return fmt.Errorf("lock auction for update: %w", err)
	}
	if auction.Version != previous.Version {
		return ErrAuctionVersionConflict
	}
	nextVersion, err := safeAdd(previous.Version, 1)
	if err != nil {
		return ErrInvalidAuction
	}
	if _, err := tx.Exec(ctx, `
		UPDATE auctions SET status = $2, current_price_cents = $3, current_winner_id = $4,
		ends_at = $5, version = $6, updated_at = $7
		WHERE id = $1`, auction.ID, auction.Status, auction.CurrentPriceCents,
		auction.CurrentWinnerID, auction.EndsAt, nextVersion, auction.UpdatedAt); err != nil {
		return fmt.Errorf("update auction: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit update auction: %w", err)
	}
	return nil
}

const auctionSelect = `SELECT id, product_id, seller_id, product_name, product_description, product_quantity,
	status, starting_price_cents, current_price_cents, current_winner_id, minimum_increment_cents,
	starts_at, ends_at, anti_sniping_window_seconds, anti_sniping_extension_seconds, version, created_at, updated_at
	FROM auctions`

type rowScanner interface{ Scan(...any) error }

func scanAuction(row rowScanner) (Auction, error) {
	var auction Auction
	err := row.Scan(&auction.ID, &auction.ProductID, &auction.SellerID, &auction.ProductName, &auction.ProductDescription,
		&auction.ProductQuantity, &auction.Status, &auction.StartingPriceCents, &auction.CurrentPriceCents,
		&auction.CurrentWinnerID, &auction.MinimumIncrementCents, &auction.StartsAt, &auction.EndsAt,
		&auction.AntiSnipingWindowSeconds, &auction.AntiSnipingExtensionSeconds, &auction.Version,
		&auction.CreatedAt, &auction.UpdatedAt)
	return auction, err
}

type httpProductClient struct {
	baseURL    string
	client     *http.Client
	credential string
}

func NewHTTPProductClient(baseURL, credential string, client *http.Client) *httpProductClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &httpProductClient{baseURL: strings.TrimRight(baseURL, "/"), credential: credential, client: client}
}

func (c *httpProductClient) Ownership(ctx context.Context, productID, sellerID uuid.UUID) (ProductSnapshot, error) {
	path := fmt.Sprintf("%s/internal/products/%s/ownership", c.baseURL, productID)
	requestURL, err := url.Parse(path)
	if err != nil {
		return ProductSnapshot{}, fmt.Errorf("parse product ownership URL: %w", err)
	}
	query := requestURL.Query()
	query.Set("user_id", sellerID.String())
	requestURL.RawQuery = query.Encode()
	request, err := c.newRequest(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return ProductSnapshot{}, err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return ProductSnapshot{}, fmt.Errorf("%w: get product ownership: %w", ErrProductDependency, err)
	}
	defer response.Body.Close()
	if err := productResponseError(response); err != nil {
		return ProductSnapshot{}, err
	}
	var product ProductSnapshot
	if err := json.NewDecoder(response.Body).Decode(&product); err != nil {
		return ProductSnapshot{}, fmt.Errorf("%w: decode product ownership: %w", ErrProductDependency, err)
	}
	return product, nil
}

func (c *httpProductClient) Lock(ctx context.Context, productID, auctionID uuid.UUID) error {
	return c.changeLock(ctx, productID, auctionID, "auction-lock")
}

func (c *httpProductClient) Unlock(ctx context.Context, productID, auctionID uuid.UUID) error {
	return c.changeLock(ctx, productID, auctionID, "auction-unlock")
}

func (c *httpProductClient) changeLock(ctx context.Context, productID, auctionID uuid.UUID, action string) error {
	body := strings.NewReader(fmt.Sprintf(`{"auction_id":%q}`, auctionID.String()))
	request, err := c.newRequest(ctx, http.MethodPost, fmt.Sprintf("%s/internal/products/%s/%s", c.baseURL, productID, action), body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: change product lock: %w", ErrProductDependency, err)
	}
	defer response.Body.Close()
	return productResponseError(response)
}

func (c *httpProductClient) newRequest(ctx context.Context, method, requestURL string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, fmt.Errorf("create product request: %w", err)
	}
	request.Header.Set("X-Internal-Service-Credential", c.credential)
	return request, nil
}

func productResponseError(response *http.Response) error {
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	switch response.StatusCode {
	case http.StatusNotFound:
		return ErrProductNotFound
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: product service returned HTTP %d", ErrProductDependency, response.StatusCode)
	case http.StatusForbidden:
		return ErrProductNotOwner
	case http.StatusConflict:
		return ErrProductUnavailable
	default:
		if response.StatusCode >= http.StatusInternalServerError {
			return fmt.Errorf("%w: product service returned HTTP %d", ErrProductDependency, response.StatusCode)
		}
		return fmt.Errorf("%w: product service returned HTTP %d", ErrProductDependency, response.StatusCode)
	}
}
