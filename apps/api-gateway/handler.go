package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/example/auction/packages/auth"
	"github.com/example/auction/packages/contracts"
	"github.com/example/auction/packages/kafka"
	"github.com/example/auction/packages/observability"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

type BidIngress interface {
	Publish(context.Context, contracts.BidCommand) error
}

type bidIngress struct {
	producer *kafka.Producer
	topic    string
}

func NewBidIngress(producer *kafka.Producer) BidIngress {
	return &bidIngress{producer: producer, topic: "auction.bid.commands.v1"}
}

func (b *bidIngress) Publish(ctx context.Context, command contracts.BidCommand) error {
	payload, err := json.Marshal(command)
	if err != nil {
		return err
	}
	return b.producer.Publish(ctx, b.topic, command.AuctionID, payload)
}

type Handler struct {
	ingress             BidIngress
	tokens              auth.TokenService
	hub                 *Hub
	notificationURL     string
	notificationProxy   *httputil.ReverseProxy
	allowedOrigins      []string
	httpClient          *http.Client
}

func NewHandler(ingress BidIngress, tokens auth.TokenService, hub *Hub, notificationURL string, allowedOrigins []string, httpClient *http.Client) *Handler {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	var notificationProxy *httputil.ReverseProxy
	if notificationURL != "" {
		if target, err := url.Parse(notificationURL); err == nil {
			notificationProxy = httputil.NewSingleHostReverseProxy(target)
			notificationProxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
				writeProblem(w, r, http.StatusServiceUnavailable, "notification_unavailable", "notification service unavailable")
			}
			notificationProxy.Transport = httpClient.Transport
		}
	}
	return &Handler{
		ingress:           ingress,
		tokens:            tokens,
		hub:               hub,
		notificationURL:   notificationURL,
		notificationProxy: notificationProxy,
		allowedOrigins:    allowedOrigins,
		httpClient:        httpClient,
	}
}

type problemResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

type bidIngressRequest struct {
	AmountCents    int64  `json:"amount_cents"`
	IdempotencyKey string `json:"idempotency_key"`
}

type bidIngressResponse struct {
	BidID     string `json:"bid_id"`
	AuctionID string `json:"auction_id"`
	Status    string `json:"status"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/auctions/") && strings.HasSuffix(r.URL.Path, "/bids") {
		h.createBid(w, r)
		return
	}
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/auctions/") && strings.HasSuffix(r.URL.Path, "/live") {
		h.subscribeAuction(w, r)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/notifications/live" {
		h.subscribeNotifications(w, r)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/notifications" {
		h.proxyNotifications(w, r)
		return
	}
	writeProblem(w, r, http.StatusNotFound, "not_found", "route not found")
}

func (h *Handler) createBid(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	if principal.Role != "buyer" {
		writeProblem(w, r, http.StatusForbidden, "forbidden", "buyer role is required")
		return
	}
	auctionID, ok := auctionIDFromBidPath(r.URL.Path)
	if !ok {
		writeProblem(w, r, http.StatusBadRequest, "invalid_input", "auction_id is invalid")
		return
	}
	var input bidIngressRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.AmountCents <= 0 {
		writeProblem(w, r, http.StatusBadRequest, "invalid_input", "amount_cents must be positive")
		return
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		writeProblem(w, r, http.StatusBadRequest, "invalid_input", "idempotency_key is required")
		return
	}

	bidID := uuid.NewString()
	command := contracts.BidCommand{
		BidID:          bidID,
		CommandID:      uuid.NewString(),
		AuctionID:      auctionID.String(),
		BidderID:       principal.ID,
		AmountCents:    input.AmountCents,
		IdempotencyKey: input.IdempotencyKey,
	}
	if err := h.ingress.Publish(r.Context(), command); err != nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "kafka_unavailable", "unable to accept bid at this time")
		return
	}
	writeJSON(w, http.StatusAccepted, bidIngressResponse{BidID: bidID, AuctionID: auctionID.String(), Status: "pending"})
}

func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	if h.tokens == nil {
		writeProblem(w, r, http.StatusUnauthorized, "unauthorized", "access token is required")
		return auth.Principal{}, false
	}
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		writeProblem(w, r, http.StatusUnauthorized, "unauthorized", "access token is required")
		return auth.Principal{}, false
	}
	principal, err := h.tokens.Verify(r.Context(), parts[1])
	if err != nil {
		writeProblem(w, r, http.StatusUnauthorized, "unauthorized", "access token is invalid")
		return auth.Principal{}, false
	}
	return principal, true
}

func auctionIDFromBidPath(path string) (uuid.UUID, bool) {
	value := strings.TrimPrefix(strings.TrimSuffix(path, "/bids"), "/v1/auctions/")
	if value == "" || strings.Contains(value, "/") {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(value)
	return id, err == nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_input", "request body is malformed")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeProblem(w, r, http.StatusBadRequest, "invalid_input", "request body is malformed")
		return false
	}
	return true
}

func (h *Handler) subscribeAuction(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	auctionID, ok := auctionIDFromLivePath(r.URL.Path)
	if !ok {
		writeProblem(w, r, http.StatusBadRequest, "invalid_input", "auction_id is invalid")
		return
	}

	userID, err := uuid.Parse(principal.ID)
	if err != nil {
		writeProblem(w, r, http.StatusUnauthorized, "unauthorized", "access token is invalid")
		return
	}

	if !h.hub.CanSubscribe(auctionID, userID) {
		writeProblem(w, r, http.StatusServiceUnavailable, "subscription_rejected", "unable to subscribe to auction")
		return
	}

	conn, err := h.acceptWebSocket(w, r, "auction-live")
	if err != nil {
		return
	}

	if err := h.hub.Subscribe(context.Background(), auctionID, userID, conn); err != nil {
		conn.Close(websocket.StatusGoingAway, "subscription rejected")
		return
	}
}

func (h *Handler) subscribeNotifications(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	userID, err := uuid.Parse(principal.ID)
	if err != nil {
		writeProblem(w, r, http.StatusUnauthorized, "unauthorized", "access token is invalid")
		return
	}
	if h.hub == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "subscription_rejected", "notification hub unavailable")
		return
	}
	if !h.hub.CanSubscribeNotification(userID) {
		writeProblem(w, r, http.StatusServiceUnavailable, "subscription_rejected", "unable to subscribe to notifications")
		return
	}
	conn, err := h.acceptWebSocket(w, r, "notification-live")
	if err != nil {
		return
	}
	if err := h.hub.SubscribeNotification(context.Background(), userID, conn); err != nil {
		conn.Close(websocket.StatusGoingAway, "subscription rejected")
		return
	}
}

func (h *Handler) acceptWebSocket(w http.ResponseWriter, r *http.Request, subprotocol string) (*websocket.Conn, error) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:       []string{subprotocol},
		OriginPatterns:     h.allowedOrigins,
		InsecureSkipVerify: false,
	})
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "websocket_error", "failed to accept websocket")
		return nil, err
	}
	return conn, nil
}

func (h *Handler) proxyNotifications(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	if h.notificationProxy == nil {
		writeProblem(w, r, http.StatusInternalServerError, "internal_error", "notification proxy misconfigured")
		return
	}
	r.Header.Set("X-User-ID", principal.ID)
	h.notificationProxy.ServeHTTP(w, r)
}

func auctionIDFromLivePath(path string) (uuid.UUID, bool) {
	value := strings.TrimPrefix(strings.TrimSuffix(path, "/live"), "/v1/auctions/")
	if value == "" || strings.Contains(value, "/") {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(value)
	return id, err == nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problemResponse{Code: code, Message: message, RequestID: observability.RequestID(r.Context())})
}
