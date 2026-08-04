package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/example/auction/packages/auth"
	"github.com/example/auction/packages/observability"
)

type Handler struct{ service UserService }

func NewHandler(service UserService) *Handler { return &Handler{service: service} }

func (h *Handler) Routes() http.Handler { return h }

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type registerRequest struct {
	credentialsRequest
	Role string `json:"role"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	AccessExpiresAt  string `json:"access_expires_at"`
	RefreshExpiresAt string `json:"refresh_expires_at"`
}

type problemResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeProblem(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	switch r.URL.Path {
	case "/v1/auth/register":
		h.register(w, r)
	case "/v1/auth/login":
		h.login(w, r)
	case "/v1/auth/refresh":
		h.refresh(w, r)
	case "/v1/auth/logout":
		h.logout(w, r)
	default:
		writeProblem(w, r, http.StatusNotFound, "not_found", "route not found")
	}
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var request registerRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	user, err := h.service.Register(r.Context(), request.Email, request.Password, request.Role)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var request credentialsRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	pair, err := h.service.Login(r.Context(), request.Email, request.Password)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newTokenResponse(pair))
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var request refreshRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.RefreshToken == "" {
		writeProblem(w, r, http.StatusBadRequest, "invalid_input", "refresh_token is required")
		return
	}
	pair, err := h.service.Refresh(r.Context(), request.RefreshToken)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newTokenResponse(pair))
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	var request refreshRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.RefreshToken == "" {
		writeProblem(w, r, http.StatusBadRequest, "invalid_input", "refresh_token is required")
		return
	}
	service, ok := h.service.(logoutService)
	if !ok {
		writeProblem(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	if err := service.Logout(r.Context(), request.RefreshToken); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

func newTokenResponse(pair auth.TokenPair) tokenResponse {
	return tokenResponse{
		AccessToken:      pair.AccessToken,
		RefreshToken:     pair.RefreshToken,
		AccessExpiresAt:  pair.AccessExpiresAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		RefreshExpiresAt: pair.RefreshExpiresAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
	}
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrDuplicateEmail):
		writeProblem(w, r, http.StatusConflict, "duplicate_email", "email is already registered")
	case errors.Is(err, ErrInvalidCredentials):
		writeProblem(w, r, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
	case errors.Is(err, ErrInvalidRefreshToken), errors.Is(err, ErrRevokedRefreshToken), errors.Is(err, ErrExpiredRefreshToken):
		writeProblem(w, r, http.StatusUnauthorized, "invalid_refresh_token", "invalid refresh token")
	case errors.Is(err, ErrInvalidEmail), errors.Is(err, ErrInvalidRole), errors.Is(err, ErrInvalidPassword):
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
