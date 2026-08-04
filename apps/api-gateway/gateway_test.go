package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/auction/packages/auth"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

func newTestHub() *Hub {
	return NewHub()
}

func testTokens(t *testing.T) auth.TokenService {
	t.Helper()
	tokens, err := auth.NewTokenService(auth.TokenConfig{Secret: "test-secret-for-gateway-tests-only"})
	if err != nil {
		t.Fatal(err)
	}
	return tokens
}

func issueTestToken(t *testing.T, tokens auth.TokenService, userID, role string) string {
	t.Helper()
	pair, err := tokens.Issue(context.Background(), auth.Principal{ID: userID, Role: role})
	if err != nil {
		t.Fatal(err)
	}
	return pair.AccessToken
}

func TestGatewayWebSocketRejectsWithoutJWT(t *testing.T) {
	tokens := testTokens(t)
	hub := newTestHub()
	handler := NewHandler(&fakeBidIngress{}, tokens, hub, "", nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := websocket.Dial(ctx, server.URL+"/v1/auctions/"+uuid.NewString()+"/live", nil)
	if err == nil {
		t.Fatal("expected websocket without auth to fail")
	}
}

func TestGatewayWebSocketAuthenticatedHandshake(t *testing.T) {
	tokens := testTokens(t)
	hub := newTestHub()
	handler := NewHandler(&fakeBidIngress{}, tokens, hub, "", nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	userID := uuid.NewString()
	auctionID := uuid.NewString()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	client, _, err := websocket.Dial(ctx, server.URL+"/v1/auctions/"+auctionID+"/live", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + issueTestToken(t, tokens, userID, "buyer")}},
	})
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer client.Close(websocket.StatusNormalClosure, "")

	time.Sleep(100 * time.Millisecond)
	if len(hub.users[userID]) != 1 {
		t.Fatalf("expected user to have one connection, got %d", len(hub.users[userID]))
	}
}

func TestGatewayEventFanoutIsolatesByAuction(t *testing.T) {
	origInterval := pingInterval
	origTimeout := pingTimeout
	pingInterval = 10 * time.Second
	pingTimeout = 5 * time.Second
	defer func() {
		pingInterval = origInterval
		pingTimeout = origTimeout
	}()

	tokens := testTokens(t)
	hub := newTestHub()
	handler := NewHandler(&fakeBidIngress{}, tokens, hub, "", nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	userID := uuid.NewString()
	auctionA := uuid.NewString()
	auctionB := uuid.NewString()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	clientA, _, err := websocket.Dial(ctx, server.URL+"/v1/auctions/"+auctionA+"/live", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + issueTestToken(t, tokens, userID, "buyer")}},
	})
	if err != nil {
		t.Fatalf("dial websocket A: %v", err)
	}
	defer clientA.Close(websocket.StatusNormalClosure, "")

	clientB, _, err := websocket.Dial(ctx, server.URL+"/v1/auctions/"+auctionB+"/live", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + issueTestToken(t, tokens, userID, "buyer")}},
	})
	if err != nil {
		t.Fatalf("dial websocket B: %v", err)
	}
	defer clientB.Close(websocket.StatusNormalClosure, "")

	time.Sleep(100 * time.Millisecond)

	msgA := []byte(`{"event_id":"evt-a","auction_id":"` + auctionA + `"}`)
	hub.Publish(ctx, uuid.MustParse(auctionA), msgA)

	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	typ, data, err := clientA.Read(readCtx)
	if err != nil {
		t.Fatalf("read from A: %v", err)
	}
	if typ != websocket.MessageText || !strings.Contains(string(data), "evt-a") {
		t.Fatalf("expected text event a, got %s: %s", typ, string(data))
	}

	readCtxB, readCancelB := context.WithTimeout(ctx, 500*time.Millisecond)
	defer readCancelB()
	_, _, err = clientB.Read(readCtxB)
	if err == nil {
		t.Fatal("expected B to receive no event for auction A")
	}
}

func TestGatewayHeartbeatCleansUpDeadConnection(t *testing.T) {
	origInterval := pingInterval
	origTimeout := pingTimeout
	pingInterval = 100 * time.Millisecond
	pingTimeout = 50 * time.Millisecond
	defer func() {
		pingInterval = origInterval
		pingTimeout = origTimeout
	}()

	tokens := testTokens(t)
	hub := newTestHub()
	handler := NewHandler(&fakeBidIngress{}, tokens, hub, "", nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	userID := uuid.NewString()
	auctionID := uuid.NewString()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client, _, err := websocket.Dial(ctx, server.URL+"/v1/auctions/"+auctionID+"/live", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + issueTestToken(t, tokens, userID, "buyer")}},
	})
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	client.Close(websocket.StatusNormalClosure, "")

	time.Sleep(300 * time.Millisecond)
	hub.mu.RLock()
	count := len(hub.users[userID])
	hub.mu.RUnlock()
	if count != 0 {
		t.Fatalf("expected dead connection to be cleaned up, got %d", count)
	}
}

func TestGatewayReconnectResubscribes(t *testing.T) {
	origInterval := pingInterval
	origTimeout := pingTimeout
	pingInterval = 10 * time.Second
	pingTimeout = 5 * time.Second
	defer func() {
		pingInterval = origInterval
		pingTimeout = origTimeout
	}()

	tokens := testTokens(t)
	hub := newTestHub()
	handler := NewHandler(&fakeBidIngress{}, tokens, hub, "", nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	userID := uuid.NewString()
	auctionID := uuid.NewString()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client1, _, err := websocket.Dial(ctx, server.URL+"/v1/auctions/"+auctionID+"/live", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + issueTestToken(t, tokens, userID, "buyer")}},
	})
	if err != nil {
		t.Fatalf("dial websocket 1: %v", err)
	}
	client1.Close(websocket.StatusNormalClosure, "")

	client2, _, err := websocket.Dial(ctx, server.URL+"/v1/auctions/"+auctionID+"/live", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + issueTestToken(t, tokens, userID, "buyer")}},
	})
	if err != nil {
		t.Fatalf("dial websocket 2: %v", err)
	}
	defer client2.Close(websocket.StatusNormalClosure, "")

	time.Sleep(100 * time.Millisecond)

	hub.mu.RLock()
	count := len(hub.users[userID])
	hub.mu.RUnlock()
	if count != 1 {
		t.Fatalf("expected exactly one connection after reconnect, got %d", count)
	}

	msg := []byte(`{"event_id":"evt-reconnect","auction_id":"` + auctionID + `"}`)
	hub.Publish(ctx, uuid.MustParse(auctionID), msg)

	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	typ, data, err := client2.Read(readCtx)
	if err != nil {
		t.Fatalf("read after reconnect: %v", err)
	}
	if typ != websocket.MessageText || !strings.Contains(string(data), "evt-reconnect") {
		t.Fatalf("expected text reconnect event, got %s: %s", typ, string(data))
	}
}

func TestGatewayAuctionLimitRejectsExcessSubscriptions(t *testing.T) {
	tokens := testTokens(t)
	hub := newTestHub()
	hub.maxPerAuction = 1
	handler := NewHandler(&fakeBidIngress{}, tokens, hub, "", nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	auctionID := uuid.NewString()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	userA := uuid.NewString()
	client1, _, err := websocket.Dial(ctx, server.URL+"/v1/auctions/"+auctionID+"/live", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + issueTestToken(t, tokens, userA, "buyer")}},
	})
	if err != nil {
		t.Fatalf("dial websocket 1: %v", err)
	}
	defer client1.Close(websocket.StatusNormalClosure, "")

	time.Sleep(50 * time.Millisecond)

	userB := uuid.NewString()
	_, _, err = websocket.Dial(ctx, server.URL+"/v1/auctions/"+auctionID+"/live", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + issueTestToken(t, tokens, userB, "buyer")}},
	})
	if err == nil {
		t.Fatal("expected second connection to be rejected due to auction limit")
	}

	hub.mu.RLock()
	count := len(hub.auctions[uuid.MustParse(auctionID)])
	hub.mu.RUnlock()
	if count != 1 {
		t.Fatalf("expected exactly one auction subscriber, got %d", count)
	}
}

func TestGatewayProxyNotificationsForwardsUserID(t *testing.T) {
	tokens := testTokens(t)
	var receivedUserID string
	notificationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUserID = r.Header.Get("X-User-ID")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"notifications":[]}`))
	}))
	defer notificationServer.Close()

	handler := NewHandler(&fakeBidIngress{}, tokens, nil, notificationServer.URL, nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	userID := uuid.NewString()
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/notifications", nil)
	req.Header.Set("Authorization", "Bearer "+issueTestToken(t, tokens, userID, "buyer"))

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("proxy notifications: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if receivedUserID != userID {
		t.Fatalf("expected X-User-ID=%s, got %s", userID, receivedUserID)
	}
}
