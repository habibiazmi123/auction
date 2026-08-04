package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/auction/packages/auth"
	"github.com/example/auction/packages/contracts"
	"github.com/google/uuid"
)

type fakeBidIngress struct {
	commands []contracts.BidCommand
	err      error
}

func (f *fakeBidIngress) Publish(ctx context.Context, command contracts.BidCommand) error {
	f.commands = append(f.commands, command)
	return f.err
}

type fakeTokenService struct {
	role string
}

func (f *fakeTokenService) Issue(ctx context.Context, principal auth.Principal) (auth.TokenPair, error) {
	return auth.TokenPair{AccessToken: principal.ID + ":" + principal.Role}, nil
}

func (f *fakeTokenService) Verify(ctx context.Context, token string) (auth.Principal, error) {
	parts := strings.Split(token, ":")
	if len(parts) != 2 {
		return auth.Principal{}, errors.New("invalid token")
	}
	if f.role != "" && parts[1] != f.role {
		return auth.Principal{ID: parts[0], Role: parts[1]}, nil
	}
	return auth.Principal{ID: parts[0], Role: parts[1]}, nil
}

func issueGatewayToken(t *testing.T, role string, id string) string {
	t.Helper()
	pair, err := (&fakeTokenService{}).Issue(context.Background(), auth.Principal{ID: id, Role: role})
	if err != nil {
		t.Fatal(err)
	}
	return pair.AccessToken
}

func newTestHandler(ingress BidIngress, tokens auth.TokenService) *Handler {
	return NewHandler(ingress, tokens, nil, "", nil)
}

func TestCreateBidRequiresUUIDAuctionID(t *testing.T) {
	ingress := &fakeBidIngress{}
	handler := newTestHandler(ingress, &fakeTokenService{})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/auctions/not-a-uuid/bids", strings.NewReader(`{"amount_cents":100,"idempotency_key":"k"}`))
	request.Header.Set("Authorization", "Bearer "+issueGatewayToken(t, "buyer", uuid.NewString()))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid auction id, got %d", recorder.Code)
	}
	if len(ingress.commands) != 0 {
		t.Fatalf("expected no commands published, got %d", len(ingress.commands))
	}
}

func TestCreateBidRequiresPositiveAmount(t *testing.T) {
	ingress := &fakeBidIngress{}
	handler := newTestHandler(ingress, &fakeTokenService{})
	auctionID := uuid.NewString()

	for _, amount := range []int64{0, -1} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/auctions/"+auctionID+"/bids", strings.NewReader(`{"amount_cents":`+stringAmount(amount)+`,"idempotency_key":"k"}`))
		request.Header.Set("Authorization", "Bearer "+issueGatewayToken(t, "buyer", uuid.NewString()))
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("amount=%d expected 400, got %d", amount, recorder.Code)
		}
	}
	if len(ingress.commands) != 0 {
		t.Fatalf("expected no commands published, got %d", len(ingress.commands))
	}
}

func stringAmount(amount int64) string {
	b, _ := json.Marshal(amount)
	return string(b)
}

func TestCreateBidRequiresIdempotencyKey(t *testing.T) {
	ingress := &fakeBidIngress{}
	handler := newTestHandler(ingress, &fakeTokenService{})
	auctionID := uuid.NewString()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/auctions/"+auctionID+"/bids", strings.NewReader(`{"amount_cents":100,"idempotency_key":""}`))
	request.Header.Set("Authorization", "Bearer "+issueGatewayToken(t, "buyer", uuid.NewString()))
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty idempotency key, got %d", recorder.Code)
	}
	if len(ingress.commands) != 0 {
		t.Fatalf("expected no commands published, got %d", len(ingress.commands))
	}
}

func TestCreateBidRequiresBuyerRole(t *testing.T) {
	ingress := &fakeBidIngress{}
	handler := newTestHandler(ingress, &fakeTokenService{})
	auctionID := uuid.NewString()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/auctions/"+auctionID+"/bids", strings.NewReader(`{"amount_cents":100,"idempotency_key":"k"}`))
	request.Header.Set("Authorization", "Bearer "+issueGatewayToken(t, "seller", uuid.NewString()))
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-buyer role, got %d", recorder.Code)
	}
	if len(ingress.commands) != 0 {
		t.Fatalf("expected no commands published, got %d", len(ingress.commands))
	}
}

func TestCreateBidReturnsPendingOnSuccess(t *testing.T) {
	ingress := &fakeBidIngress{}
	handler := newTestHandler(ingress, &fakeTokenService{})
	auctionID := uuid.NewString()
	buyerID := uuid.NewString()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/auctions/"+auctionID+"/bids", strings.NewReader(`{"amount_cents":100,"idempotency_key":"k"}`))
	request.Header.Set("Authorization", "Bearer "+issueGatewayToken(t, "buyer", buyerID))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", recorder.Code)
	}
	var response bidIngressResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Status != "pending" || response.AuctionID != auctionID || response.BidID == "" {
		t.Fatalf("unexpected response: %#v", response)
	}
	if len(ingress.commands) != 1 {
		t.Fatalf("expected one command published, got %d", len(ingress.commands))
	}
	if ingress.commands[0].AuctionID != auctionID || ingress.commands[0].BidderID != buyerID || ingress.commands[0].AmountCents != 100 || ingress.commands[0].IdempotencyKey != "k" {
		t.Fatalf("unexpected command: %#v", ingress.commands[0])
	}
}

func TestCreateBidReturns503WhenKafkaUnavailable(t *testing.T) {
	ingress := &fakeBidIngress{err: errors.New("kafka unavailable")}
	handler := newTestHandler(ingress, &fakeTokenService{})
	auctionID := uuid.NewString()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/auctions/"+auctionID+"/bids", strings.NewReader(`{"amount_cents":100,"idempotency_key":"k"}`))
	request.Header.Set("Authorization", "Bearer "+issueGatewayToken(t, "buyer", uuid.NewString()))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", recorder.Code)
	}
}

func TestCreateBidRequiresAuthentication(t *testing.T) {
	ingress := &fakeBidIngress{}
	handler := newTestHandler(ingress, &fakeTokenService{})
	auctionID := uuid.NewString()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/auctions/"+auctionID+"/bids", strings.NewReader(`{"amount_cents":100,"idempotency_key":"k"}`))
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Code)
	}
	if len(ingress.commands) != 0 {
		t.Fatalf("expected no commands published, got %d", len(ingress.commands))
	}
}

func TestCreateBidRejectsMalformedBody(t *testing.T) {
	ingress := &fakeBidIngress{}
	handler := newTestHandler(ingress, &fakeTokenService{})
	auctionID := uuid.NewString()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/auctions/"+auctionID+"/bids", strings.NewReader(`{not json}`))
	request.Header.Set("Authorization", "Bearer "+issueGatewayToken(t, "buyer", uuid.NewString()))
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed body, got %d", recorder.Code)
	}
}
