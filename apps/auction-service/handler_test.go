package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/auction/packages/auth"
	"github.com/google/uuid"
)

type fakeAuctionService struct {
	create func(context.Context, uuid.UUID, CreateAuctionInput) (Auction, error)
	get    func(context.Context, uuid.UUID) (AuctionView, error)
}

func (s fakeAuctionService) Create(ctx context.Context, sellerID uuid.UUID, input CreateAuctionInput) (Auction, error) {
	return s.create(ctx, sellerID, input)
}

func (s fakeAuctionService) Get(ctx context.Context, auctionID uuid.UUID) (AuctionView, error) {
	return s.get(ctx, auctionID)
}

func TestAuctionHandlerCreatesAuctionForSeller(t *testing.T) {
	tokens, err := auth.NewTokenService(auth.TokenConfig{Secret: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	sellerID := uuid.New()
	service := fakeAuctionService{create: func(_ context.Context, gotSeller uuid.UUID, input CreateAuctionInput) (Auction, error) {
		if gotSeller != sellerID || input.ProductID == uuid.Nil {
			t.Fatalf("create arguments: seller=%s input=%#v", gotSeller, input)
		}
		return Auction{ID: uuid.New(), SellerID: sellerID, Status: AuctionStatusScheduled}, nil
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/auctions", strings.NewReader(`{"product_id":"`+uuid.NewString()+`","starting_price_cents":100,"minimum_increment_cents":10,"starts_at":"2026-08-04T13:00:00Z","ends_at":"2026-08-04T14:00:00Z"}`))
	request.Header.Set("Authorization", "Bearer "+issueAuctionTestToken(t, tokens, "seller", sellerID))
	NewHandler(service, tokens).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status=%d content-type=%q", recorder.Code, recorder.Header().Get("Content-Type"))
	}
}

func TestAuctionHandlerRejectsBuyerCreate(t *testing.T) {
	tokens, _ := auth.NewTokenService(auth.TokenConfig{Secret: "test-secret"})
	called := false
	service := fakeAuctionService{create: func(context.Context, uuid.UUID, CreateAuctionInput) (Auction, error) {
		called = true
		return Auction{}, nil
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/auctions", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+issueAuctionTestToken(t, tokens, "buyer", uuid.New()))
	NewHandler(service, tokens).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden || called {
		t.Fatalf("status=%d called=%v", recorder.Code, called)
	}
}

func TestAuctionHandlerGetsAuctionAndMapsNotFound(t *testing.T) {
	auctionID := uuid.New()
	service := fakeAuctionService{get: func(_ context.Context, gotID uuid.UUID) (AuctionView, error) {
		if gotID != auctionID {
			t.Fatal("wrong auction ID")
		}
		return AuctionView{Auction: Auction{ID: auctionID, Status: AuctionStatusLive}}, nil
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/auctions/"+auctionID.String(), nil)
	NewHandler(service, nil).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("get status=%d", recorder.Code)
	}
	var view AuctionView
	if err := json.NewDecoder(recorder.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if view.ID != auctionID {
		t.Fatalf("view=%#v", view)
	}

	service = fakeAuctionService{get: func(context.Context, uuid.UUID) (AuctionView, error) { return AuctionView{}, ErrAuctionNotFound }}
	recorder = httptest.NewRecorder()
	NewHandler(service, nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/auctions/"+auctionID.String(), nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("not found status=%d", recorder.Code)
	}
}

func TestAuctionHandlerMapsInvalidCreate(t *testing.T) {
	tokens, _ := auth.NewTokenService(auth.TokenConfig{Secret: "test-secret"})
	service := fakeAuctionService{create: func(context.Context, uuid.UUID, CreateAuctionInput) (Auction, error) {
		return Auction{}, ErrInvalidAuction
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/auctions", strings.NewReader(`{"product_id":"`+uuid.NewString()+`"}`))
	request.Header.Set("Authorization", "Bearer "+issueAuctionTestToken(t, tokens, "seller", uuid.New()))
	NewHandler(service, tokens).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", recorder.Code)
	}
}

func issueAuctionTestToken(t *testing.T, service *auth.JWTService, role string, id uuid.UUID) string {
	t.Helper()
	pair, err := service.Issue(context.Background(), auth.Principal{ID: id.String(), Role: role})
	if err != nil {
		t.Fatal(err)
	}
	return pair.AccessToken
}
