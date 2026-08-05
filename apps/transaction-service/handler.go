package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/habibiazmi123/auction/packages/observability"
	"github.com/google/uuid"
)

type Handler struct {
	repository SettlementRepository
}

func NewHandler(repository SettlementRepository) *Handler {
	return &Handler{repository: repository}
}

type problemResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

type settlementResponse struct {
	ID            string `json:"id"`
	AuctionID     string `json:"auction_id"`
	SellerID      string `json:"seller_id"`
	BuyerID       string `json:"buyer_id"`
	BidID         string `json:"bid_id"`
	AmountCents   int64  `json:"amount_cents"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/auctions/") && strings.HasSuffix(r.URL.Path, "/settlement") {
		h.getByAuctionID(w, r)
		return
	}
	writeProblem(w, r, http.StatusNotFound, "not_found", "route not found")
}

func (h *Handler) getByAuctionID(w http.ResponseWriter, r *http.Request) {
	auctionID, ok := auctionIDFromPath(r.URL.Path)
	if !ok {
		writeProblem(w, r, http.StatusBadRequest, "invalid_input", "auction_id is invalid")
		return
	}
	settlement, err := h.repository.GetByAuctionID(r.Context(), auctionID)
	if err != nil {
		if errors.Is(err, ErrSettlementNotFound) {
			writeProblem(w, r, http.StatusNotFound, "not_found", "settlement not found")
			return
		}
		writeProblem(w, r, http.StatusInternalServerError, "internal_error", "failed to load settlement")
		return
	}
	writeJSON(w, http.StatusOK, settlementResponse{
		ID:          settlement.ID.String(),
		AuctionID:   settlement.AuctionID.String(),
		SellerID:    settlement.SellerID.String(),
		BuyerID:     settlement.BuyerID.String(),
		BidID:       settlement.BidID.String(),
		AmountCents: settlement.AmountCents,
		Status:      string(settlement.Status),
		CreatedAt:   settlement.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   settlement.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

func auctionIDFromPath(path string) (uuid.UUID, bool) {
	value := strings.TrimPrefix(strings.TrimSuffix(path, "/settlement"), "/v1/auctions/")
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
