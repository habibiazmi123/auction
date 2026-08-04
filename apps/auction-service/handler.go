package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/auction/packages/auth"
	"github.com/example/auction/packages/observability"
	"github.com/google/uuid"
)

type Handler struct {
	service AuctionService
	tokens  auth.TokenService
}

func NewHandler(service AuctionService, tokens auth.TokenService) *Handler {
	return &Handler{service: service, tokens: tokens}
}

type problemResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

func (h *Handler) Routes() http.Handler { return h }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/auctions" {
		switch r.Method {
		case http.MethodPost:
			h.create(w, r)
		case http.MethodGet:
			writeProblem(w, r, http.StatusNotFound, "not_found", "route not found")
		default:
			writeProblem(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/auctions/") {
		auctionID, ok := auctionIDFromPath(r.URL.Path)
		if !ok {
			writeProblem(w, r, http.StatusNotFound, "not_found", "auction not found")
			return
		}
		if r.Method != http.MethodGet {
			writeProblem(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		h.get(w, r, auctionID)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/bids/") {
		bidID, ok := bidIDFromPath(r.URL.Path)
		if !ok {
			writeProblem(w, r, http.StatusNotFound, "not_found", "bid not found")
			return
		}
		if r.Method != http.MethodGet {
			writeProblem(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		h.getBid(w, r, bidID)
		return
	}
	writeProblem(w, r, http.StatusNotFound, "not_found", "route not found")
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	if principal.Role != "seller" {
		writeProblem(w, r, http.StatusForbidden, "forbidden", "seller role is required")
		return
	}
	var input CreateAuctionInput
	if !decodeJSON(w, r, &input) {
		return
	}
	sellerID, err := uuid.Parse(principal.ID)
	if err != nil {
		writeProblem(w, r, http.StatusUnauthorized, "unauthorized", "access token is invalid")
		return
	}
	auction, err := h.service.Create(r.Context(), sellerID, input)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, auction)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, auctionID uuid.UUID) {
	view, err := h.service.Get(r.Context(), auctionID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type bidView struct {
	ID            string `json:"id"`
	AuctionID     string `json:"auction_id"`
	BidderID      string `json:"bidder_id"`
	AmountCents   int64  `json:"amount_cents"`
	Status        string `json:"status"`
	RejectionCode string `json:"rejection_code,omitempty"`
	CreatedAt     string `json:"created_at"`
}

func (h *Handler) getBid(w http.ResponseWriter, r *http.Request, bidID uuid.UUID) {
	principal, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	bid, err := h.service.GetBid(r.Context(), bidID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if principal.ID != bid.BidderID.String() && principal.Role != "admin" {
		writeProblem(w, r, http.StatusForbidden, "forbidden", "cannot view another bidder's bid")
		return
	}
	writeJSON(w, http.StatusOK, bidView{
		ID:            bid.ID.String(),
		AuctionID:     bid.AuctionID.String(),
		BidderID:      bid.BidderID.String(),
		AmountCents:   bid.AmountCents,
		Status:        bid.Status,
		RejectionCode: bid.RejectionCode,
		CreatedAt:     bid.CreatedAt.UTC().Format(time.RFC3339),
	})
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

func auctionIDFromPath(path string) (uuid.UUID, bool) {
	value := strings.TrimPrefix(path, "/v1/auctions/")
	if value == "" || strings.Contains(value, "/") {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(value)
	return id, err == nil
}

func bidIDFromPath(path string) (uuid.UUID, bool) {
	value := strings.TrimPrefix(path, "/v1/bids/")
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

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrAuctionNotFound):
		writeProblem(w, r, http.StatusNotFound, "not_found", "auction not found")
	case errors.Is(err, ErrProductNotFound):
		writeProblem(w, r, http.StatusNotFound, "not_found", "product not found")
	case errors.Is(err, ErrProductNotOwner):
		writeProblem(w, r, http.StatusForbidden, "forbidden", "seller does not own product")
	case errors.Is(err, ErrProductAuth), errors.Is(err, ErrProductDependency):
		writeProblem(w, r, http.StatusServiceUnavailable, "dependency_unavailable", "product service dependency unavailable")
	case errors.Is(err, ErrInvalidAuction):
		writeProblem(w, r, http.StatusBadRequest, "invalid_input", "request contains invalid fields")
	case errors.Is(err, ErrAuctionConflict), errors.Is(err, ErrProductUnavailable), errors.Is(err, ErrAuctionVersionConflict), errors.Is(err, ErrAuctionNotLive), errors.Is(err, ErrAuctionNotScheduled), errors.Is(err, ErrAuctionNotStarted), errors.Is(err, ErrAuctionEnded), errors.Is(err, ErrAuctionNotEnded), errors.Is(err, ErrBidTooLow), errors.Is(err, ErrSellerCannotBid):
		writeProblem(w, r, http.StatusConflict, "auction_state_conflict", "auction cannot be changed in its current state")
	default:
		writeProblem(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
	}
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
