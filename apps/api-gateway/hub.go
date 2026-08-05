package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/habibiazmi123/auction/packages/contracts"
	"github.com/habibiazmi123/auction/packages/kafka"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

var (
	defaultMaxPerAuction = 1000
	defaultMaxPerUser    = 10
	defaultMaxPerConn    = 1
	writeTimeout         = 5 * time.Second
	pingInterval         = 30 * time.Second
	pingTimeout          = 10 * time.Second
)

type subscriptionKind string

const (
	auctionKind      subscriptionKind = "auction"
	notificationKind subscriptionKind = "notification"
)

type Hub struct {
	mu            sync.RWMutex
	auctions      map[uuid.UUID]map[*connection]struct{}
	notifications map[uuid.UUID]map[*connection]struct{}
	users         map[string]map[*connection]struct{}
	connections   map[*connection]struct{}
	maxPerAuction int
	maxPerUser    int
	maxPerConn    int
}

type connection struct {
	conn          *websocket.Conn
	send          chan []byte
	done          chan struct{}
	hub           *Hub
	userID        string
	auctions      map[uuid.UUID]struct{}
	notifications map[uuid.UUID]struct{}
	ctx           context.Context
	mu            sync.Mutex
	closed        bool
	cancel        context.CancelFunc
}

func NewHub() *Hub {
	return &Hub{
		auctions:      make(map[uuid.UUID]map[*connection]struct{}),
		notifications: make(map[uuid.UUID]map[*connection]struct{}),
		users:         make(map[string]map[*connection]struct{}),
		connections:   make(map[*connection]struct{}),
		maxPerAuction: defaultMaxPerAuction,
		maxPerUser:    defaultMaxPerUser,
		maxPerConn:    defaultMaxPerConn,
	}
}

func (h *Hub) CanSubscribe(auctionID, userID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	userConns := h.users[userID.String()]
	if len(userConns) >= h.maxPerUser {
		return false
	}
	auctionConns := h.auctions[auctionID]
	if len(auctionConns) >= h.maxPerAuction {
		return false
	}
	return true
}

func (h *Hub) CanSubscribeNotification(userID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	userConns := h.users[userID.String()]
	return len(userConns) < h.maxPerUser
}

func (h *Hub) Subscribe(ctx context.Context, auctionID, userID uuid.UUID, conn *websocket.Conn) error {
	if auctionID == uuid.Nil || userID == uuid.Nil || conn == nil {
		return fmt.Errorf("invalid subscription")
	}

	c := &connection{
		conn:          conn,
		send:          make(chan []byte, 256),
		done:          make(chan struct{}),
		hub:           h,
		userID:        userID.String(),
		auctions:      make(map[uuid.UUID]struct{}),
		notifications: make(map[uuid.UUID]struct{}),
	}
	c.ctx, c.cancel = context.WithCancel(ctx)

	h.mu.Lock()
	defer h.mu.Unlock()

	userConns := h.users[c.userID]
	if len(userConns) >= h.maxPerUser {
		conn.Close(websocket.StatusPolicyViolation, "too many connections for user")
		return fmt.Errorf("user connection limit reached")
	}

	if len(c.auctions) >= h.maxPerConn {
		conn.Close(websocket.StatusPolicyViolation, "too many subscriptions for connection")
		return fmt.Errorf("connection subscription limit reached")
	}

	auctionConns := h.auctions[auctionID]
	if len(auctionConns) >= h.maxPerAuction {
		conn.Close(websocket.StatusPolicyViolation, "too many subscribers for auction")
		return fmt.Errorf("auction subscription limit reached")
	}

	if auctionConns == nil {
		auctionConns = make(map[*connection]struct{})
		h.auctions[auctionID] = auctionConns
	}
	auctionConns[c] = struct{}{}

	if userConns == nil {
		userConns = make(map[*connection]struct{})
		h.users[c.userID] = userConns
	}
	userConns[c] = struct{}{}
	h.connections[c] = struct{}{}
	c.auctions[auctionID] = struct{}{}

	go c.writePump()
	go c.readPump()

	return nil
}

func (h *Hub) SubscribeNotification(ctx context.Context, userID uuid.UUID, conn *websocket.Conn) error {
	if userID == uuid.Nil || conn == nil {
		return fmt.Errorf("invalid subscription")
	}

	c := &connection{
		conn:          conn,
		send:          make(chan []byte, 256),
		done:          make(chan struct{}),
		hub:           h,
		userID:        userID.String(),
		auctions:      make(map[uuid.UUID]struct{}),
		notifications: make(map[uuid.UUID]struct{}),
	}
	c.ctx, c.cancel = context.WithCancel(ctx)

	h.mu.Lock()
	defer h.mu.Unlock()

	userConns := h.users[c.userID]
	if len(userConns) >= h.maxPerUser {
		conn.Close(websocket.StatusPolicyViolation, "too many connections for user")
		return fmt.Errorf("user connection limit reached")
	}

	if len(c.notifications) >= h.maxPerConn {
		conn.Close(websocket.StatusPolicyViolation, "too many subscriptions for connection")
		return fmt.Errorf("connection subscription limit reached")
	}

	notificationConns := h.notifications[userID]
	if len(notificationConns) >= h.maxPerUser {
		conn.Close(websocket.StatusPolicyViolation, "too many notification subscribers")
		return fmt.Errorf("notification subscription limit reached")
	}

	if notificationConns == nil {
		notificationConns = make(map[*connection]struct{})
		h.notifications[userID] = notificationConns
	}
	notificationConns[c] = struct{}{}

	if userConns == nil {
		userConns = make(map[*connection]struct{})
		h.users[c.userID] = userConns
	}
	userConns[c] = struct{}{}
	h.connections[c] = struct{}{}
	c.notifications[userID] = struct{}{}

	go c.writePump()
	go c.readPump()

	return nil
}

func (h *Hub) Publish(ctx context.Context, auctionID uuid.UUID, event []byte) error {
	return h.publish(ctx, auctionKind, auctionID, event)
}

func (h *Hub) PublishNotification(ctx context.Context, recipientID uuid.UUID, event []byte) error {
	return h.publish(ctx, notificationKind, recipientID, event)
}

func (h *Hub) publish(ctx context.Context, kind subscriptionKind, id uuid.UUID, event []byte) error {
	if id == uuid.Nil {
		return nil
	}
	h.mu.RLock()
	var subs map[*connection]struct{}
	switch kind {
	case auctionKind:
		subs = h.auctions[id]
	case notificationKind:
		subs = h.notifications[id]
	}
	if len(subs) == 0 {
		h.mu.RUnlock()
		return nil
	}
	listeners := make([]*connection, 0, len(subs))
	for c := range subs {
		listeners = append(listeners, c)
	}
	h.mu.RUnlock()

	var stale []*connection
	for _, c := range listeners {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			continue
		}
		c.mu.Unlock()
		select {
		case c.send <- event:
		case <-c.done:
			stale = append(stale, c)
		case <-time.After(writeTimeout):
			stale = append(stale, c)
		}
	}
	for _, c := range stale {
		c.close()
	}
	return nil
}

func (h *Hub) remove(c *connection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.connections[c]; !ok {
		return
	}
	delete(h.connections, c)
	for auctionID := range c.auctions {
		if auctionConns, ok := h.auctions[auctionID]; ok {
			delete(auctionConns, c)
			if len(auctionConns) == 0 {
				delete(h.auctions, auctionID)
			}
		}
	}
	for notificationID := range c.notifications {
		if notificationConns, ok := h.notifications[notificationID]; ok {
			delete(notificationConns, c)
			if len(notificationConns) == 0 {
				delete(h.notifications, notificationID)
			}
		}
	}
	if userConns, ok := h.users[c.userID]; ok {
		delete(userConns, c)
		if len(userConns) == 0 {
			delete(h.users, c.userID)
		}
	}
}

func (c *connection) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	close(c.done)
	c.mu.Unlock()
	c.cancel()
	c.conn.Close(websocket.StatusGoingAway, "")
	c.hub.remove(c)
}

func (c *connection) writePump() {
	defer c.close()
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			ctx, cancel := context.WithTimeout(c.ctx, writeTimeout)
			err := c.conn.Write(ctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(c.ctx, pingTimeout)
			err := c.conn.Ping(ctx)
			cancel()
			if err != nil {
				return
			}
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *connection) readPump() {
	defer c.close()
	c.conn.SetReadLimit(4096)
	for {
		_, _, err := c.conn.Read(c.ctx)
		if err != nil {
			return
		}
	}
}

func (h *Hub) RunPublicEventConsumer(ctx context.Context, cfg kafka.ConsumerConfig) error {
	backoff := time.Second
	for {
		consumer := kafka.NewConsumer(cfg)
		err := consumer.Run(ctx, func(ctx context.Context, message kafka.Message) error {
			var envelope contracts.EventEnvelope[json.RawMessage]
			if err := json.Unmarshal(message.Payload, &envelope); err != nil {
				slog.WarnContext(ctx, "drop malformed public event", "error", err)
				return nil
			}
			var target struct {
				AuctionID string `json:"auction_id"`
			}
			if err := json.Unmarshal(envelope.Payload, &target); err != nil || target.AuctionID == "" {
				return nil
			}
			auctionID, err := uuid.Parse(target.AuctionID)
			if err != nil {
				return nil
			}
			return h.Publish(ctx, auctionID, message.Payload)
		})
		consumer.Close()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.Error("public event consumer stopped, reconnecting", "error", err, "backoff", backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
	}
}

func (h *Hub) RunNotificationConsumer(ctx context.Context, cfg kafka.ConsumerConfig) error {
	backoff := time.Second
	for {
		consumer := kafka.NewConsumer(cfg)
		err := consumer.Run(ctx, func(ctx context.Context, message kafka.Message) error {
			var envelope contracts.EventEnvelope[contracts.NotificationCreated]
			if err := json.Unmarshal(message.Payload, &envelope); err != nil {
				slog.WarnContext(ctx, "drop malformed notification event", "error", err)
				return nil
			}
			recipientID, err := uuid.Parse(envelope.Payload.RecipientID)
			if err != nil {
				return nil
			}
			return h.PublishNotification(ctx, recipientID, message.Payload)
		})
		consumer.Close()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.Error("notification consumer stopped, reconnecting", "error", err, "backoff", backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
	}
}
