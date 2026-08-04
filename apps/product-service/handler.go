package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/example/auction/packages/auth"
	"github.com/example/auction/packages/observability"
	"github.com/google/uuid"
)

type Handler struct {
	service            ProductService
	tokens             auth.TokenService
	internalCredential string
}

func NewHandler(service ProductService, tokens auth.TokenService, internalCredential string) *Handler {
	return &Handler{service: service, tokens: tokens, internalCredential: internalCredential}
}

type problemResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

type productListResponse struct {
	Items []Product `json:"items"`
	PageInfo
}

type auctionRequest struct {
	AuctionID uuid.UUID `json:"auction_id"`
}

func (h *Handler) Routes() http.Handler { return h }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/internal/products/") {
		h.internal(w, r)
		return
	}
	if r.URL.Path == "/v1/products" {
		switch r.Method {
		case http.MethodGet:
			h.list(w, r)
		case http.MethodPost:
			h.create(w, r)
		default:
			writeProblem(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/products/") {
		id, ok := productIDFromPath(r.URL.Path, "/v1/products/")
		if !ok {
			writeProblem(w, r, http.StatusNotFound, "not_found", "product not found")
			return
		}
		switch r.Method {
		case http.MethodGet:
			h.get(w, r, id)
		case http.MethodPut, http.MethodPatch:
			h.update(w, r, id)
		default:
			writeProblem(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
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
	var input CreateProductInput
	if !decodeJSON(w, r, &input) {
		return
	}
	product, err := h.service.Create(r.Context(), parsePrincipalID(principal), input)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, product)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	product, err := h.service.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, product)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	page, err := pageFromQuery(r.URL.Query())
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	products, pageInfo, err := h.service.List(r.Context(), page)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, productListResponse{Items: products, PageInfo: pageInfo})
}

func pageFromQuery(query map[string][]string) (Page, error) {
	page := Page{Number: 1, Size: DefaultPageSize}
	for key, target := range map[string]*int{"page": &page.Number, "page_size": &page.Size} {
		values, ok := query[key]
		if !ok {
			continue
		}
		if len(values) != 1 || values[0] == "" {
			return Page{}, fmt.Errorf("%s must be an integer", key)
		}
		parsed, err := strconv.Atoi(values[0])
		if err != nil {
			return Page{}, fmt.Errorf("%s must be an integer", key)
		}
		if parsed == 0 {
			return Page{}, fmt.Errorf("%s must be positive", key)
		}
		*target = parsed
	}
	return normalizePage(page)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, productID uuid.UUID) {
	principal, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	if principal.Role != "seller" {
		writeProblem(w, r, http.StatusForbidden, "forbidden", "seller role is required")
		return
	}
	var input UpdateProductInput
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := h.service.Update(r.Context(), parsePrincipalID(principal), productID, input); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) internal(w http.ResponseWriter, r *http.Request) {
	if !h.validInternalCredential(r) {
		writeProblem(w, r, http.StatusUnauthorized, "unauthorized", "internal service credential is required")
		return
	}
	service, ok := h.service.(internalProductService)
	if !ok {
		writeProblem(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/internal/products/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		writeProblem(w, r, http.StatusNotFound, "not_found", "route not found")
		return
	}
	productID, err := uuid.Parse(parts[0])
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_input", "product_id is invalid")
		return
	}
	switch parts[1] {
	case "ownership":
		if r.Method != http.MethodGet {
			writeProblem(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		userID, err := uuid.Parse(r.URL.Query().Get("user_id"))
		if err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_input", "user_id is invalid")
			return
		}
		product, err := service.Ownership(r.Context(), productID, userID)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, product)
	case "auction-lock", "auction-unlock":
		if r.Method != http.MethodPost {
			writeProblem(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		var request auctionRequest
		if !decodeJSON(w, r, &request) {
			return
		}
		if request.AuctionID == uuid.Nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_input", "auction_id is required")
			return
		}
		if parts[1] == "auction-lock" {
			err = service.Lock(r.Context(), productID, request.AuctionID)
		} else {
			err = service.Unlock(r.Context(), productID, request.AuctionID)
		}
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeProblem(w, r, http.StatusNotFound, "not_found", "route not found")
	}
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

func (h *Handler) validInternalCredential(r *http.Request) bool {
	credential := r.Header.Get("X-Internal-Service-Credential")
	if credential == "" {
		credential = r.Header.Get("X-Internal-Service-Token")
	}
	if credential == "" || h.internalCredential == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(credential), []byte(h.internalCredential)) == 1
}

func parsePrincipalID(principal auth.Principal) uuid.UUID {
	id, _ := uuid.Parse(principal.ID)
	return id
}

func productIDFromPath(path, prefix string) (uuid.UUID, bool) {
	value := strings.TrimPrefix(path, prefix)
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
	case errors.Is(err, ErrProductNotFound):
		writeProblem(w, r, http.StatusNotFound, "not_found", "product not found")
	case errors.Is(err, ErrProductNotOwner), errors.Is(err, ErrForbidden):
		writeProblem(w, r, http.StatusForbidden, "forbidden", "seller does not own product")
	case errors.Is(err, ErrUnauthorized):
		writeProblem(w, r, http.StatusUnauthorized, "unauthorized", "unauthorized")
	case errors.Is(err, ErrProductLocked):
		writeProblem(w, r, http.StatusConflict, "product_locked", "product is locked")
	case errors.Is(err, ErrProductUnavailable):
		writeProblem(w, r, http.StatusConflict, "product_unavailable", "product is not available")
	case errors.Is(err, ErrInvalidProduct), errors.Is(err, ErrInvalidPage):
		writeProblem(w, r, http.StatusBadRequest, "invalid_input", "request contains invalid fields")
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
