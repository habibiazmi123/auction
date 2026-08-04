package observability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// HealthCheck is a readiness dependency with a name and a check function.
type HealthCheck struct {
	Name  string
	Check func(context.Context) error
}

// WithHealth wraps a handler with /health/live and /health/ready endpoints.
// Live returns 200 without checking dependencies. Ready runs all checks and
// returns 503 with a stable problem code when any check fails.
func WithHealth(next http.Handler, checks []HealthCheck) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/health/live" {
			writeHealthJSON(w, http.StatusOK, map[string]string{"status": "live"})
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/health/ready" {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			for _, check := range checks {
				if err := check.Check(ctx); err != nil {
					writeHealthProblem(w, r, http.StatusServiceUnavailable, "service_unavailable", fmt.Sprintf("%s: %v", check.Name, err))
					return
				}
			}
			writeHealthJSON(w, http.StatusOK, map[string]string{"status": "ready"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

type healthProblem struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

func writeHealthJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeHealthProblem(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(healthProblem{Code: code, Message: message, RequestID: RequestID(r.Context())})
}

// RunServer starts an HTTP server and blocks until the context is cancelled.
// It performs a graceful shutdown within the supplied grace duration.
func RunServer(ctx context.Context, server *http.Server, grace time.Duration) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), grace)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		<-errCh
		return nil
	}
}

// Run starts all workers from the supplied context and waits for the first
// fatal error or context cancellation. On termination it cancels the shared
// context and waits for every worker to return before returning itself.
func Run(ctx context.Context, workers ...func(context.Context) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, len(workers))

	for _, worker := range workers {
		if worker == nil {
			continue
		}
		wg.Add(1)
		go func(w func(context.Context) error) {
			defer wg.Done()
			if err := w(ctx); err != nil && !errors.Is(err, context.Canceled) {
				errCh <- err
				cancel()
			}
		}(worker)
	}

	select {
	case <-ctx.Done():
	case <-errCh:
	}

	cancel()
	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}
