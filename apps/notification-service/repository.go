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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotificationNotFound = errors.New("notification not found")

type NotificationInput struct {
	RecipientID string
	Type        string
	AuctionID   string
	Title       string
	Body        string
}

type Notification struct {
	ID            uuid.UUID
	SourceEventID uuid.UUID
	RecipientID   uuid.UUID
	Type          string
	Payload       []byte
	ReadAt        *time.Time
	CreatedAt     time.Time
}

type NotificationRepository interface {
	CreateIfAbsent(ctx context.Context, eventID string, input NotificationInput) error
	GetByRecipient(ctx context.Context, recipientID uuid.UUID, limit int) ([]Notification, error)
}

type notificationRepository struct{ pool *pgxpool.Pool }

func NewNotificationRepository(pool *pgxpool.Pool) NotificationRepository {
	return &notificationRepository{pool: pool}
}

func (r *notificationRepository) CreateIfAbsent(ctx context.Context, eventID string, input NotificationInput) error {
	if eventID == "" {
		return fmt.Errorf("event id is required")
	}
	sourceEventID, err := uuid.Parse(eventID)
	if err != nil {
		return fmt.Errorf("parse event id: %w", err)
	}
	recipientID, err := uuid.Parse(input.RecipientID)
	if err != nil {
		return fmt.Errorf("parse recipient id: %w", err)
	}
	payload, err := json.Marshal(map[string]any{
		"auction_id": input.AuctionID,
		"title":      input.Title,
		"body":       input.Body,
	})
	if err != nil {
		return fmt.Errorf("marshal notification payload: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin notification transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	notificationID := uuid.New()
	var inserted bool
	var returnedID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO notifications (id, source_event_id, recipient_id, type, payload)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (source_event_id, recipient_id) DO NOTHING
		RETURNING id`,
		notificationID, sourceEventID, recipientID, input.Type, payload).Scan(&returnedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			inserted = false
		} else {
			return fmt.Errorf("insert notification: %w", err)
		}
	} else {
		inserted = true
		notificationID = returnedID
	}

	if inserted {
		if err := r.insertNotificationOutboxEvent(ctx, tx, notificationID, sourceEventID, recipientID, input.Type, input.AuctionID, input.Title, input.Body); err != nil {
			return fmt.Errorf("insert notification outbox: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit notification: %w", err)
	}
	return nil
}

func (r *notificationRepository) insertNotificationOutboxEvent(ctx context.Context, tx pgx.Tx, notificationID uuid.UUID, sourceEventID uuid.UUID, recipientID uuid.UUID, notificationType, auctionID, title, body string) error {
	version, err := nextAggregateVersion(ctx, tx, recipientID.String())
	if err != nil {
		return fmt.Errorf("resolve aggregate version: %w", err)
	}
	payload, err := contracts.EventEnvelope[contracts.NotificationCreated]{
		EventID:    uuid.NewString(),
		EventType:  "notification.created.v1",
		Version:    1,
		Producer:   "notification-service",
		Payload: contracts.NotificationCreated{
			NotificationID: notificationID.String(),
			RecipientID:    recipientID.String(),
			Type:           notificationType,
			AuctionID:      auctionID,
			Title:          title,
			Body:           body,
		},
	}.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal notification created event: %w", err)
	}
	return insertOutboxEvent(ctx, tx, "notification.created.v1", recipientID.String(), version, "notification.events.v1", recipientID.String(), payload)
}

func (r *notificationRepository) GetByRecipient(ctx context.Context, recipientID uuid.UUID, limit int) ([]Notification, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, source_event_id, recipient_id, type, payload, read_at, created_at
		FROM notifications
		WHERE recipient_id = $1
		ORDER BY created_at DESC
		LIMIT $2`, recipientID, limit)
	if err != nil {
		return nil, fmt.Errorf("query notifications: %w", err)
	}
	defer rows.Close()

	var notifications []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.SourceEventID, &n.RecipientID, &n.Type, &n.Payload, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}
		notifications = append(notifications, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read notifications: %w", err)
	}
	return notifications, nil
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
