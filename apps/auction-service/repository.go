package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/habibiazmi123/auction/packages/contracts"
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

func scanBid(row rowScanner) (Bid, error) {
	var bid Bid
	err := row.Scan(&bid.ID, &bid.AuctionID, &bid.BidderID, &bid.CommandID, &bid.IdempotencyKey, &bid.AmountCents,
		&bid.Status, &bid.RejectionCode, &bid.CreatedAt)
	return bid, err
}

const bidSelect = `SELECT id, auction_id, bidder_id, command_id, idempotency_key, amount_cents, status, rejection_code, created_at FROM bids`

func (r *repository) ApplyBidTx(ctx context.Context, command contracts.BidCommand) (BidResult, error) {
	auctionID, err := uuid.Parse(command.AuctionID)
	if err != nil {
		return BidResult{}, fmt.Errorf("parse auction id: %w", ErrInvalidAuction)
	}
	bidderID, err := uuid.Parse(command.BidderID)
	if err != nil {
		return BidResult{}, fmt.Errorf("parse bidder id: %w", ErrInvalidAuction)
	}
	commandID, err := uuid.Parse(command.CommandID)
	if err != nil {
		return BidResult{}, fmt.Errorf("parse command id: %w", ErrInvalidAuction)
	}
	bidID, err := uuid.Parse(command.BidID)
	if err != nil {
		return BidResult{}, fmt.Errorf("parse bid id: %w", ErrInvalidAuction)
	}
	if strings.TrimSpace(command.IdempotencyKey) == "" || command.AmountCents <= 0 {
		return BidResult{}, ErrInvalidAuction
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return BidResult{}, fmt.Errorf("begin bid transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	auction, err := scanAuction(tx.QueryRow(ctx, auctionSelect+` WHERE id = $1 FOR UPDATE`, auctionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return BidResult{}, ErrAuctionNotFound
	}
	if err != nil {
		return BidResult{}, fmt.Errorf("lock auction for bid: %w", err)
	}

	if existing, err := scanBid(tx.QueryRow(ctx, bidSelect+` WHERE command_id = $1`, commandID)); err == nil {
		return bidResultFrom(existing), nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return BidResult{}, fmt.Errorf("check processed command: %w", err)
	}

	if existing, err := scanBid(tx.QueryRow(ctx, bidSelect+` WHERE auction_id = $1 AND bidder_id = $2 AND idempotency_key = $3`, auctionID, bidderID, command.IdempotencyKey)); err == nil {
		if existing.CommandID == commandID {
			return bidResultFrom(existing), nil
		}
		return BidResult{}, ErrBidIdempotencyConflict
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return BidResult{}, fmt.Errorf("check idempotency key: %w", err)
	}

	now := time.Now().UTC()
	rules := AuctionRules{}
	validationErr := rules.ValidateBid(auction, bidderID, command.AmountCents, now)
	if validationErr != nil {
		bid := Bid{
			ID: bidID, AuctionID: auctionID, BidderID: bidderID, CommandID: commandID,
			IdempotencyKey: command.IdempotencyKey, AmountCents: command.AmountCents,
			Status: BidStatusRejected, RejectionCode: BidRejectionCode(validationErr), CreatedAt: now,
		}
		if err := insertBid(ctx, tx, bid); err != nil {
			return BidResult{}, fmt.Errorf("insert rejected bid: %w", err)
		}
		if err := insertBidOutboxEvent(ctx, tx, auction, bid, now); err != nil {
			return BidResult{}, fmt.Errorf("insert rejected bid outbox: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return BidResult{}, fmt.Errorf("commit rejected bid: %w", err)
		}
		return bidResultFrom(bid), nil
	}

	updated, err := rules.ApplyBid(auction, bidderID, command.AmountCents, now)
	if err != nil {
		return BidResult{}, err
	}
	nextVersion, err := safeAdd(auction.Version, 1)
	if err != nil {
		return BidResult{}, ErrInvalidAuction
	}
	bid := Bid{
		ID: bidID, AuctionID: auctionID, BidderID: bidderID, CommandID: commandID,
		IdempotencyKey: command.IdempotencyKey, AmountCents: command.AmountCents,
		Status: BidStatusAccepted, CreatedAt: now,
	}
	if err := insertBid(ctx, tx, bid); err != nil {
		return BidResult{}, fmt.Errorf("insert accepted bid: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE auctions SET current_price_cents = $2, current_winner_id = $3,
		ends_at = $4, version = $5, updated_at = $6
		WHERE id = $1`, auction.ID, updated.CurrentPriceCents, updated.CurrentWinnerID, updated.EndsAt, nextVersion, now); err != nil {
		return BidResult{}, fmt.Errorf("update auction after bid: %w", err)
	}
	if err := insertBidOutboxEvent(ctx, tx, updated, bid, now); err != nil {
		return BidResult{}, fmt.Errorf("insert accepted bid outbox: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return BidResult{}, fmt.Errorf("commit accepted bid: %w", err)
	}
	return bidResultFrom(bid), nil
}

func (r *repository) StartDue(ctx context.Context, now time.Time) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin start due transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		WITH due AS (
			SELECT id, version FROM auctions
			WHERE status = 'scheduled' AND starts_at <= $1 AND ends_at > $1
			ORDER BY id
			FOR UPDATE SKIP LOCKED
		)
		UPDATE auctions a
		SET status = 'live', version = a.version + 1, updated_at = $2
		FROM due
		WHERE a.id = due.id
		RETURNING a.id`, now.UTC(), now.UTC())
	if err != nil {
		return 0, fmt.Errorf("query scheduled auctions: %w", err)
	}

	var started []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan started auction: %w", err)
		}
		started = append(started, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("read scheduled auctions: %w", err)
	}
	rows.Close()

	if err := tx.Commit(ctx); err != nil {
		return len(started), fmt.Errorf("commit start due: %w", err)
	}
	return len(started), nil
}

func (r *repository) GetBid(ctx context.Context, bidID uuid.UUID) (Bid, error) {
	bid, err := scanBid(r.pool.QueryRow(ctx, bidSelect+` WHERE id = $1`, bidID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Bid{}, ErrAuctionNotFound
	}
	if err != nil {
		return Bid{}, fmt.Errorf("get bid: %w", err)
	}
	return bid, nil
}

func (r *repository) CloseDue(ctx context.Context, now time.Time) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin close due transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		WITH due AS (
			SELECT id, version FROM auctions
			WHERE status = 'live' AND ends_at <= $1
			ORDER BY id
			FOR UPDATE SKIP LOCKED
		)
		UPDATE auctions a
		SET status = 'closed', version = a.version + 1, updated_at = $2
		FROM due
		WHERE a.id = due.id
		RETURNING a.id, a.seller_id, a.version, a.current_price_cents, a.current_winner_id`, now.UTC(), now.UTC())
	if err != nil {
		return 0, fmt.Errorf("query due auctions: %w", err)
	}

	type closedAuction struct {
		id           uuid.UUID
		sellerID     uuid.UUID
		version      int64
		finalPrice   int64
		winnerID     *uuid.UUID
		winningBidID uuid.UUID
	}
	var closed []closedAuction
	for rows.Next() {
		var a closedAuction
		if err := rows.Scan(&a.id, &a.sellerID, &a.version, &a.finalPrice, &a.winnerID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan closed auction: %w", err)
		}
		closed = append(closed, a)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("read closed auctions: %w", err)
	}
	rows.Close()

	for _, a := range closed {
		bidID := a.winningBidID
		if bidID == uuid.Nil && a.winnerID != nil {
			if err := tx.QueryRow(ctx, `
				SELECT id FROM bids
				WHERE auction_id = $1 AND bidder_id = $2 AND status = 'accepted'
				ORDER BY amount_cents DESC, created_at DESC
				LIMIT 1`, a.id, *a.winnerID).Scan(&bidID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return len(closed), fmt.Errorf("resolve winning bid: %w", err)
			}
		}
		outboxVersion, err := nextAggregateVersion(ctx, tx, a.id.String())
		if err != nil {
			return len(closed), fmt.Errorf("resolve close aggregate version: %w", err)
		}
		if err := insertCloseOutboxEvent(ctx, tx, a.id, a.sellerID, bidID, outboxVersion, a.finalPrice, a.winnerID, now.UTC()); err != nil {
			return len(closed), fmt.Errorf("insert close outbox: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return len(closed), fmt.Errorf("commit close due: %w", err)
	}
	return len(closed), nil
}

func insertBid(ctx context.Context, tx pgx.Tx, bid Bid) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO bids (id, auction_id, bidder_id, command_id, idempotency_key, amount_cents, status, rejection_code, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		bid.ID, bid.AuctionID, bid.BidderID, bid.CommandID, bid.IdempotencyKey, bid.AmountCents, bid.Status, bid.RejectionCode, bid.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrBidIdempotencyConflict
		}
		return err
	}
	return nil
}

func nextAggregateVersion(ctx context.Context, tx pgx.Tx, aggregateID string) (int64, error) {
	var version sql.NullInt64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(aggregate_version), 0) FROM outbox_events WHERE aggregate_id = $1`, aggregateID).Scan(&version); err != nil {
		return 0, err
	}
	return version.Int64 + 1, nil
}

func insertBidOutboxEvent(ctx context.Context, tx pgx.Tx, auction Auction, bid Bid, now time.Time) error {
	version, err := nextAggregateVersion(ctx, tx, auction.ID.String())
	if err != nil {
		return fmt.Errorf("resolve aggregate version: %w", err)
	}
	payload, err := json.Marshal(contracts.EventEnvelope[BidPlaced]{
		EventID:    uuid.NewString(),
		EventType:  "auction.bid.placed.v1",
		Version:    1,
		OccurredAt: now,
		Producer:   "auction-service",
		Payload: BidPlaced{
			BidID:         bid.ID.String(),
			AuctionID:     bid.AuctionID.String(),
			BidderID:      bid.BidderID.String(),
			AmountCents:   bid.AmountCents,
			Status:        bid.Status,
			RejectionCode: bid.RejectionCode,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal bid placed event: %w", err)
	}
	return insertOutboxEvent(ctx, tx, "auction.bid.placed.v1", auction.ID.String(), version, "auction.events.v1", auction.ID.String(), payload)
}

func insertCloseOutboxEvent(ctx context.Context, tx pgx.Tx, auctionID uuid.UUID, sellerID uuid.UUID, bidID uuid.UUID, version int64, finalPrice int64, winnerID *uuid.UUID, now time.Time) error {
	var winner string
	if winnerID != nil {
		winner = winnerID.String()
	}
	var bid string
	if bidID != uuid.Nil {
		bid = bidID.String()
	}
	payload, err := json.Marshal(contracts.EventEnvelope[AuctionClosed]{
		EventID:    uuid.NewString(),
		EventType:  "auction.closed.v1",
		Version:    1,
		OccurredAt: now,
		Producer:   "auction-service",
		Payload:    AuctionClosed{AuctionID: auctionID.String(), SellerID: sellerID.String(), BidID: bid, FinalPriceCents: finalPrice, WinnerID: winner},
	})
	if err != nil {
		return fmt.Errorf("marshal auction closed event: %w", err)
	}
	return insertOutboxEvent(ctx, tx, "auction.closed.v1", auctionID.String(), version, "auction.events.v1", auctionID.String(), payload)
}

func insertOutboxEvent(ctx context.Context, tx pgx.Tx, eventType, aggregateID string, version int64, topic, key string, payload []byte) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO outbox_events (event_id, aggregate_id, aggregate_version, event_type, topic, event_key, payload, dlq_topic)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		uuid.New(), aggregateID, version, eventType, topic, key, payload, strings.Replace(topic, ".v1", ".dlq.v1", 1))
	if err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

func bidResultFrom(bid Bid) BidResult {
	return BidResult{
		BidID:         bid.ID,
		AuctionID:     bid.AuctionID,
		BidderID:      bid.BidderID,
		AmountCents:   bid.AmountCents,
		Status:        bid.Status,
		RejectionCode: bid.RejectionCode,
	}
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
