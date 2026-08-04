package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/auction/packages/observability"
)

func TestHealthLiveReturnsOK(t *testing.T) {
	handler := observability.WithHealth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "live" {
		t.Fatalf("unexpected status: %v", body["status"])
	}
}

func TestHealthReadyReturnsOKWhenHealthy(t *testing.T) {
	checks := []observability.HealthCheck{
		{Name: "postgres", Check: func(context.Context) error { return nil }},
	}
	handler := observability.WithHealth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), checks)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ready" {
		t.Fatalf("unexpected status: %v", body["status"])
	}
}

func TestHealthReadyReturns503WhenUnhealthy(t *testing.T) {
	checks := []observability.HealthCheck{
		{Name: "postgres", Check: func(context.Context) error { return errors.New("connection refused") }},
	}
	handler := observability.WithHealth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), checks)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", recorder.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["code"] != "service_unavailable" {
		t.Fatalf("unexpected code: %v", body["code"])
	}
}
