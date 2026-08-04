package observability

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPMiddlewarePropagatesIDsAndLogsOnlyMetadata(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestID(r.Context()) != "request-1" || CorrelationID(r.Context()) != "correlation-1" {
			t.Fatal("request IDs were not propagated into context")
		}
		w.WriteHeader(http.StatusCreated)
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/bids", bytes.NewBufferString(`{"secret":"do-not-log"}`))
	req.Header.Set("X-Request-ID", "request-1")
	req.Header.Set("X-Correlation-ID", "correlation-1")
	req.Header.Set("Authorization", "Bearer secret")
	res := httptest.NewRecorder()

	Middleware(logger)(next).ServeHTTP(res, req)
	if got := res.Header().Get("X-Request-ID"); got != "request-1" {
		t.Fatalf("request ID header: got %q", got)
	}
	if got := res.Header().Get("X-Correlation-ID"); got != "correlation-1" {
		t.Fatalf("correlation ID header: got %q", got)
	}
	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("decode log: %v", err)
	}
	if entry["method"] != http.MethodPost || entry["path"] != "/v1/bids" || entry["status"] != float64(http.StatusCreated) {
		t.Fatalf("unexpected log metadata: %#v", entry)
	}
	if bytes.Contains(logs.Bytes(), []byte("Bearer secret")) || bytes.Contains(logs.Bytes(), []byte("do-not-log")) {
		t.Fatal("sensitive request data was logged")
	}
}

func TestHTTPMiddlewareGeneratesRequestID(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestID(r.Context()) == "" {
			t.Fatal("request ID was not generated")
		}
		if CorrelationID(r.Context()) == "" {
			t.Fatal("correlation ID was not generated")
		}
	})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	res := httptest.NewRecorder()
	Middleware(slog.Default())(next).ServeHTTP(res, req)
	if res.Header().Get("X-Request-ID") == "" || res.Header().Get("X-Correlation-ID") == "" {
		t.Fatal("generated IDs were not returned")
	}
}

func TestHTTPMiddlewarePreservesOptionalResponseWriterInterfaces(t *testing.T) {
	writer := &optionalResponseWriter{ResponseRecorder: httptest.NewRecorder()}
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, ok := w.(http.Hijacker); !ok {
			t.Fatal("http.Hijacker was not preserved")
		}
		if _, _, err := w.(http.Hijacker).Hijack(); err != nil {
			t.Fatalf("hijack: %v", err)
		}
		w.(http.Flusher).Flush()
		if err := w.(http.Pusher).Push("/asset", nil); err != nil {
			t.Fatalf("push: %v", err)
		}
		if _, err := w.(io.ReaderFrom).ReadFrom(strings.NewReader("body")); err != nil {
			t.Fatalf("read from: %v", err)
		}
	})

	Middleware(slog.Default())(handler).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
	if !writer.hijacked || !writer.flushed || !writer.pushed {
		t.Fatalf("optional interfaces were not delegated: %#v", writer)
	}
}

type optionalResponseWriter struct {
	*httptest.ResponseRecorder
	hijacked bool
	flushed  bool
	pushed   bool
}

func (w *optionalResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijacked = true
	return nil, nil, nil
}

func (w *optionalResponseWriter) Flush() {
	w.flushed = true
	w.ResponseRecorder.Flush()
}

func (w *optionalResponseWriter) Push(string, *http.PushOptions) error {
	w.pushed = true
	return nil
}

func (w *optionalResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(w.ResponseRecorder, r)
}
