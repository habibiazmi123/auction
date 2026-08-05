package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/habibiazmi123/auction/packages/observability"
	"github.com/google/uuid"
)

type Handler struct {
	repository NotificationRepository
}

func NewHandler(repository NotificationRepository) *Handler {
	return &Handler{repository: repository}
}

type problemResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

type notificationResponse struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	AuctionID string `json:"auction_id,omitempty"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Read      bool   `json:"read"`
	CreatedAt string `json:"created_at"`
}

type notificationsResponse struct {
	Notifications []notificationResponse `json:"notifications"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/v1/notifications" {
		h.list(w, r)
		return
	}
	writeProblem(w, r, http.StatusNotFound, "not_found", "route not found")
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	recipientID := strings.TrimSpace(r.Header.Get("X-User-ID"))
	if recipientID == "" {
		writeProblem(w, r, http.StatusUnauthorized, "unauthorized", "user id is required")
		return
	}
	recipient, err := uuid.Parse(recipientID)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_input", "user id is invalid")
		return
	}
	notifications, err := h.repository.GetByRecipient(r.Context(), recipient, 50)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "internal_error", "failed to load notifications")
		return
	}
	response := notificationsResponse{Notifications: make([]notificationResponse, 0, len(notifications))}
	for _, n := range notifications {
		var payload struct {
			AuctionID string `json:"auction_id"`
			Title     string `json:"title"`
			Body      string `json:"body"`
		}
		_ = json.Unmarshal(n.Payload, &payload)
		response.Notifications = append(response.Notifications, notificationResponse{
			ID:        n.ID.String(),
			Type:      n.Type,
			AuctionID: payload.AuctionID,
			Title:     payload.Title,
			Body:      payload.Body,
			Read:      n.ReadAt != nil,
			CreatedAt: n.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, response)
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

var errNotificationNotFound = errors.New("notification not found")
