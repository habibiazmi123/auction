package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/example/auction/packages/auth"
	"github.com/example/auction/packages/contracts"
	"github.com/example/auction/packages/kafka"
	"github.com/example/auction/packages/observability"
	"github.com/google/uuid"
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
	ingress BidIngress
	tokens  auth.TokenService
}

func NewHandler(ingress BidIngress, tokens auth.TokenService) *Handler {
	return &Handler{ingress: ingress, tokens: tokens}
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
