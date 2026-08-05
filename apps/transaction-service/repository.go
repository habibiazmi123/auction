package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/habibiazmi123/auction/packages/contracts"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SettlementStatus string

const (
	SettlementStatusPending SettlementStatus = "pending"
	SettlementStatusPaid    SettlementStatus = "paid"
	SettlementStatusFailed  SettlementStatus = "failed"
)

type Settlement struct {
	ID            uuid.UUID
	AuctionID     uuid.UUID
	SellerID      uuid.UUID
	BuyerID       uuid.UUID
	BidID         uuid.UUID
	AmountCents   int64
	Status        SettlementStatus
	SourceEventID uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

var ErrSettlementNotFound = errors.New("settlement not found")

type SettlementRepository interface {
	CreateFromCloseTx(ctx context.Context, eventID string, event contracts.AuctionClosed) (Settlement, error)
	GetByAuctionID(ctx context.Context, auctionID uuid.UUID) (Settlement, error)
}

type settlementRepository struct{ pool *pgxpool.Pool }

func NewSettlementRepository(pool *pgxpool.Pool) SettlementRepository {
	return &settlementRepository{pool: pool}
}

func (r *settlementRepository) CreateFromCloseTx(ctx context.Context, eventID string, event contracts.AuctionClosed) (Settlement, error) {
	// Auctions that close with no bids have no settlement; commit the Kafka
	// offset by returning nil without error.
	if event.WinnerID == "" {
		return Settlement{}, nil
	}

	auctionID, err := uuid.Parse(event.AuctionID)
	if err != nil {
		return Settlement{}, fmt.Errorf("parse auction id: %w", err)
	}
	sellerID, err := uuid.Parse(event.SellerID)
	if err != nil {
		return Settlement{}, fmt.Errorf("parse seller id: %w", err)
	}
	buyerID, err := uuid.Parse(event.WinnerID)
	if err != nil {
		return Settlement{}, fmt.Errorf("parse winner id: %w", err)
	}
	if event.FinalPriceCents <= 0 {
		return Settlement{}, fmt.Errorf("invalid final price")
	}

	sourceEventID, err := uuid.Parse(eventID)
	if err != nil {
		return Settlement{}, fmt.Errorf("parse source event id: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Settlement{}, fmt.Errorf("begin settlement transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	bidID, err := uuid.Parse(event.BidID)
	if err != nil {
		bidID = uuid.Nil
	}

	settlement := Settlement{
		ID:            uuid.New(),
		AuctionID:     auctionID,
		SellerID:      sellerID,
		BuyerID:       buyerID,
		BidID:         bidID,
		AmountCents:   event.FinalPriceCents,
		Status:        SettlementStatusPending,
		SourceEventID: sourceEventID,
	}

	var inserted bool
	err = tx.QueryRow(ctx, `
		INSERT INTO settlements (id, auction_id, seller_id, buyer_id, bid_id, amount_cents, status, source_event_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (auction_id) DO NOTHING
		RETURNING id, source_event_id`,
		settlement.ID, settlement.AuctionID, settlement.SellerID, settlement.BuyerID,
		settlement.BidID, settlement.AmountCents, string(settlement.Status), settlement.SourceEventID,
	).Scan(&settlement.ID, &settlement.SourceEventID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			inserted = false
			if err := tx.QueryRow(ctx, `SELECT id, source_event_id FROM settlements WHERE auction_id = $1`, settlement.AuctionID).Scan(&settlement.ID, &settlement.SourceEventID); err != nil {
				return Settlement{}, fmt.Errorf("get existing settlement: %w", err)
			}
		} else {
			return Settlement{}, fmt.Errorf("insert settlement: %w", err)
		}
	} else {
		inserted = true
	}

	if inserted {
		if err := r.insertSettlementOutboxEvent(ctx, tx, settlement); err != nil {
			return Settlement{}, fmt.Errorf("insert settlement outbox: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Settlement{}, fmt.Errorf("commit settlement: %w", err)
	}
	return settlement, nil
}

func (r *settlementRepository) GetByAuctionID(ctx context.Context, auctionID uuid.UUID) (Settlement, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, auction_id, seller_id, buyer_id, bid_id, amount_cents, status, source_event_id, created_at, updated_at
		FROM settlements WHERE auction_id = $1`, auctionID)
	var s Settlement
	var status string
	if err := row.Scan(&s.ID, &s.AuctionID, &s.SellerID, &s.BuyerID, &s.BidID, &s.AmountCents, &status, &s.SourceEventID, &s.CreatedAt, &s.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Settlement{}, ErrSettlementNotFound
		}
		return Settlement{}, fmt.Errorf("get settlement: %w", err)
	}
	s.Status = SettlementStatus(status)
	return s, nil
}

func (r *settlementRepository) insertSettlementOutboxEvent(ctx context.Context, tx pgx.Tx, settlement Settlement) error {
	version, err := nextAggregateVersion(ctx, tx, settlement.AuctionID.String())
	if err != nil {
		return fmt.Errorf("resolve aggregate version: %w", err)
	}
	payload, err := contracts.EventEnvelope[contracts.SettlementCreated]{
		EventID:    uuid.NewString(),
		EventType:  "transaction.settlement.created.v1",
		Version:    1,
		Producer:   "transaction-service",
		Payload: contracts.SettlementCreated{
			SettlementID: settlement.ID.String(),
			AuctionID:    settlement.AuctionID.String(),
			BidID:        settlement.BidID.String(),
			BuyerID:      settlement.BuyerID.String(),
			SellerID:     settlement.SellerID.String(),
			AmountCents:  settlement.AmountCents,
		},
	}.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal settlement created event: %w", err)
	}
	return insertOutboxEvent(ctx, tx, "transaction.settlement.created.v1", settlement.AuctionID.String(), version, "auction.events.v1", settlement.AuctionID.String(), payload)
}

func nextAggregateVersion(ctx context.Context, tx pgx.Tx, aggregateID string) (int64, error) {
	var version int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(aggregate_version), 0) FROM outbox_events WHERE aggregate_id = $1`, aggregateID).Scan(&version); err != nil {
		return 0, err
	}
	return version + 1, nil
}

func insertOutboxEvent(ctx context.Context, tx pgx.Tx, eventType, aggregateID string, version int64, topic, key string, payload []byte) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO outbox_events (event_id, aggregate_id, aggregate_version, event_type, topic, event_key, payload, dlq_topic)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		uuid.New(), aggregateID, version, eventType, topic, key, payload, replaceV1WithDLQ(topic))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil
		}
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

func replaceV1WithDLQ(topic string) string {
	if len(topic) >= 3 && topic[len(topic)-3:] == ".v1" {
		return topic[:len(topic)-3] + ".dlq.v1"
	}
	return topic + ".dlq"
}
