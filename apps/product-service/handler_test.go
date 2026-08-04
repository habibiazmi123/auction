package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/auction/packages/auth"
	"github.com/google/uuid"
)

type fakeProductService struct {
	create    func(context.Context, uuid.UUID, CreateProductInput) (Product, error)
	get       func(context.Context, uuid.UUID) (Product, error)
	list      func(context.Context, Page) ([]Product, PageInfo, error)
	update    func(context.Context, uuid.UUID, uuid.UUID, UpdateProductInput) error
	ownership func(context.Context, uuid.UUID, uuid.UUID) (Product, error)
	lock      func(context.Context, uuid.UUID, uuid.UUID) error
	unlock    func(context.Context, uuid.UUID, uuid.UUID) error
}

func (s fakeProductService) Create(ctx context.Context, sellerID uuid.UUID, input CreateProductInput) (Product, error) {
	return s.create(ctx, sellerID, input)
}

func (s fakeProductService) Get(ctx context.Context, id uuid.UUID) (Product, error) {
	return s.get(ctx, id)
}

func (s fakeProductService) List(ctx context.Context, page Page) ([]Product, PageInfo, error) {
	return s.list(ctx, page)
}

func (s fakeProductService) Update(ctx context.Context, sellerID, productID uuid.UUID, input UpdateProductInput) error {
	return s.update(ctx, sellerID, productID, input)
}

func (s fakeProductService) Ownership(ctx context.Context, productID, userID uuid.UUID) (Product, error) {
	return s.ownership(ctx, productID, userID)
}

func (s fakeProductService) Lock(ctx context.Context, productID, auctionID uuid.UUID) error {
	return s.lock(ctx, productID, auctionID)
}

func (s fakeProductService) Unlock(ctx context.Context, productID, auctionID uuid.UUID) error {
	return s.unlock(ctx, productID, auctionID)
}

func TestProductHandlerRequiresSellerForCreate(t *testing.T) {
	tokens, err := auth.NewTokenService(auth.TokenConfig{Secret: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	service := fakeProductService{
		create: func(context.Context, uuid.UUID, CreateProductInput) (Product, error) {
			called = true
			return Product{}, nil
		},
	}
	handler := NewHandler(service, tokens, "internal-secret")
	accessToken := issueTestToken(t, tokens, "buyer", uuid.New())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/products", strings.NewReader(`{"name":"camera","description":"used","quantity":1}`))
	request.Header.Set("Authorization", "Bearer "+accessToken)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden || called {
		t.Fatalf("status=%d called=%v", recorder.Code, called)
	}
}

func TestProductHandlerMapsValidationToProblem(t *testing.T) {
	service := fakeProductService{
		create: func(context.Context, uuid.UUID, CreateProductInput) (Product, error) {
			return Product{}, ErrInvalidProduct
		},
	}
	tokens, _ := auth.NewTokenService(auth.TokenConfig{Secret: "test-secret"})
	accessToken := issueTestToken(t, tokens, "seller", uuid.New())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/products", strings.NewReader(`{"name":"camera","description":"used","quantity":1}`))
	request.Header.Set("Authorization", "Bearer "+accessToken)
	NewHandler(service, tokens, "internal-secret").ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "application/problem+json") {
		t.Fatalf("status=%d content-type=%q", recorder.Code, recorder.Header().Get("Content-Type"))
	}
	var problem problemResponse
	if err := json.NewDecoder(recorder.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != "invalid_input" {
		t.Fatalf("problem=%#v", problem)
	}
}

func TestProductHandlerRejectsOutOfRangePagination(t *testing.T) {
	called := false
	service := fakeProductService{
		list: func(context.Context, Page) ([]Product, PageInfo, error) {
			called = true
			return nil, PageInfo{}, nil
		},
	}
	for _, query := range []string{"?page=0", "?page_size=101", "?page=9223372036854775807&page_size=100"} {
		t.Run(query, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/v1/products"+query, nil)
			NewHandler(service, nil, "internal-secret").ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "application/problem+json") {
				t.Fatalf("status=%d content-type=%q", recorder.Code, recorder.Header().Get("Content-Type"))
			}
		})
	}
	if called {
		t.Fatal("list service was called for invalid pagination")
	}
}

func TestProductHandlerInternalEndpointsRequireCredential(t *testing.T) {
	productID, userID, auctionID := uuid.New(), uuid.New(), uuid.New()
	service := fakeProductService{
		ownership: func(context.Context, uuid.UUID, uuid.UUID) (Product, error) {
			return Product{ID: productID, SellerID: userID, Status: ProductStatusAvailable, CreatedAt: time.Now()}, nil
		},
		lock:   func(context.Context, uuid.UUID, uuid.UUID) error { return nil },
		unlock: func(context.Context, uuid.UUID, uuid.UUID) error { return nil },
	}
	for _, test := range []struct {
		name   string
		path   string
		method string
	}{
		{name: "ownership", path: "/internal/products/" + productID.String() + "/ownership?user_id=" + userID.String(), method: http.MethodGet},
		{name: "lock", path: "/internal/products/" + productID.String() + "/auction-lock", method: http.MethodPost},
		{name: "unlock", path: "/internal/products/" + productID.String() + "/auction-unlock", method: http.MethodPost},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(`{"auction_id":"`+auctionID.String()+`"}`))
			NewHandler(service, nil, "internal-secret").ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestProductHandlerInternalOwnershipReturnsSnapshot(t *testing.T) {
	productID, userID := uuid.New(), uuid.New()
	want := Product{ID: productID, SellerID: userID, Name: "camera", Description: "used", Quantity: 1, Status: ProductStatusAvailable}
	service := fakeProductService{
		ownership: func(_ context.Context, gotProductID, gotUserID uuid.UUID) (Product, error) {
			if gotProductID != productID || gotUserID != userID {
				t.Fatal("wrong ownership arguments")
			}
			return want, nil
		},
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/internal/products/"+productID.String()+"/ownership?user_id="+userID.String(), nil)
	request.Header.Set("X-Internal-Service-Credential", "internal-secret")
	NewHandler(service, nil, "internal-secret").ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d", recorder.Code)
	}
	var got Product
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.SellerID != want.SellerID || got.Name != want.Name {
		t.Fatalf("snapshot: got %#v want %#v", got, want)
	}
}

func TestProductHandlerMapsOwnershipAndLockErrors(t *testing.T) {
	productID, auctionID := uuid.New(), uuid.New()
	service := fakeProductService{
		ownership: func(context.Context, uuid.UUID, uuid.UUID) (Product, error) { return Product{}, ErrProductNotOwner },
		lock:      func(context.Context, uuid.UUID, uuid.UUID) error { return ErrProductLocked },
		unlock:    func(context.Context, uuid.UUID, uuid.UUID) error { return errors.New("unexpected") },
	}
	handler := NewHandler(service, nil, "internal-secret")
	for _, test := range []struct {
		name string
		path string
		want int
	}{
		{name: "ownership", path: "/internal/products/" + productID.String() + "/ownership?user_id=" + uuid.NewString(), want: http.StatusForbidden},
		{name: "lock", path: "/internal/products/" + productID.String() + "/auction-lock", want: http.StatusConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(map[string]string{"ownership": http.MethodGet, "lock": http.MethodPost}[test.name], test.path, strings.NewReader(`{"auction_id":"`+auctionID.String()+`"}`))
			request.Header.Set("X-Internal-Service-Credential", "internal-secret")
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status: got %d, want %d", recorder.Code, test.want)
			}
		})
	}
}

func issueTestToken(t *testing.T, service *auth.JWTService, role string, id uuid.UUID) string {
	t.Helper()
	pair, err := service.Issue(context.Background(), auth.Principal{ID: id.String(), Role: role})
	if err != nil {
		t.Fatal(err)
	}
	return pair.AccessToken
}
