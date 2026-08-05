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

	"github.com/habibiazmi123/auction/packages/auth"
)

type fakeHandlerService struct {
	register func(context.Context, string, string, string) (User, error)
	login    func(context.Context, string, string) (auth.TokenPair, error)
	refresh  func(context.Context, string) (auth.TokenPair, error)
	logout   func(context.Context, string) error
}

func (s fakeHandlerService) Register(ctx context.Context, email, password, role string) (User, error) {
	return s.register(ctx, email, password, role)
}

func (s fakeHandlerService) Login(ctx context.Context, email, password string) (auth.TokenPair, error) {
	return s.login(ctx, email, password)
}

func (s fakeHandlerService) Refresh(ctx context.Context, token string) (auth.TokenPair, error) {
	return s.refresh(ctx, token)
}

func (s fakeHandlerService) Logout(ctx context.Context, token string) error {
	return s.logout(ctx, token)
}

func TestRegisterHandlerReturnsCreatedUser(t *testing.T) {
	service := fakeHandlerService{register: func(_ context.Context, email, _, role string) (User, error) {
		return User{ID: "user-1", Email: email, Role: role}, nil
	}, login: func(context.Context, string, string) (auth.TokenPair, error) { return auth.TokenPair{}, nil }, refresh: func(context.Context, string) (auth.TokenPair, error) { return auth.TokenPair{}, nil }, logout: func(context.Context, string) error { return nil }}
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/register", strings.NewReader(`{"email":"buyer@example.com","password":"correct horse","role":"buyer"}`))
	recorder := httptest.NewRecorder()
	NewHandler(service).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusCreated)
	}
	var got User
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ID != "user-1" || got.Email != "buyer@example.com" {
		t.Fatalf("response user: %#v", got)
	}
}

func TestRegisterHandlerRejectsMalformedInput(t *testing.T) {
	service := fakeHandlerService{register: func(context.Context, string, string, string) (User, error) {
		t.Fatal("service called")
		return User{}, nil
	}, login: func(context.Context, string, string) (auth.TokenPair, error) { return auth.TokenPair{}, nil }, refresh: func(context.Context, string) (auth.TokenPair, error) { return auth.TokenPair{}, nil }, logout: func(context.Context, string) error { return nil }}
	recorder := httptest.NewRecorder()
	NewHandler(service).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/auth/register", strings.NewReader(`{"email":`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") {
		t.Fatalf("content type: got %q", got)
	}
}

func TestLoginRefreshAndLogoutHandlers(t *testing.T) {
	pair := auth.TokenPair{AccessToken: "access", RefreshToken: "refresh", AccessExpiresAt: time.Now().UTC().Add(time.Minute), RefreshExpiresAt: time.Now().UTC().Add(time.Hour)}
	logoutCalled := false
	service := fakeHandlerService{
		register: func(context.Context, string, string, string) (User, error) { return User{}, nil },
		login:    func(context.Context, string, string) (auth.TokenPair, error) { return pair, nil },
		refresh:  func(context.Context, string) (auth.TokenPair, error) { return pair, nil },
		logout:   func(context.Context, string) error { logoutCalled = true; return nil },
	}
	handler := NewHandler(service)
	for _, test := range []struct {
		name string
		path string
		body string
		want int
	}{
		{name: "login", path: "/v1/auth/login", body: `{"email":"buyer@example.com","password":"correct horse"}`, want: http.StatusOK},
		{name: "refresh", path: "/v1/auth/refresh", body: `{"refresh_token":"refresh"}`, want: http.StatusOK},
		{name: "logout", path: "/v1/auth/logout", body: `{"refresh_token":"refresh"}`, want: http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body)))
			if recorder.Code != test.want {
				t.Fatalf("status: got %d, want %d", recorder.Code, test.want)
			}
		})
	}
	if !logoutCalled {
		t.Fatal("logout service was not called")
	}
}

func TestLoginHandlerMapsInvalidCredentialsToUnauthorized(t *testing.T) {
	service := fakeHandlerService{
		register: func(context.Context, string, string, string) (User, error) { return User{}, nil },
		login: func(context.Context, string, string) (auth.TokenPair, error) {
			return auth.TokenPair{}, ErrInvalidCredentials
		},
		refresh: func(context.Context, string) (auth.TokenPair, error) { return auth.TokenPair{}, nil },
		logout:  func(context.Context, string) error { return errors.New("unused") },
	}
	recorder := httptest.NewRecorder()
	NewHandler(service).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"buyer@example.com","password":"wrong password"}`)))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}
