package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/example/auction/packages/auth"
	"github.com/example/auction/packages/config"
	"github.com/example/auction/packages/observability"
	"github.com/example/auction/packages/postgres"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	credential, err := requiredInternalServiceCredential()
	if err != nil {
		slog.Error("load internal service credential", "error", err)
		os.Exit(1)
	}
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "localhost"
	}
	dsn := (&url.URL{
		Scheme:   "postgres",
		Host:     host + ":" + fmt.Sprint(cfg.PostgresPort),
		User:     url.UserPassword(cfg.PostgresUser, cfg.PostgresPassword),
		Path:     "/auction_db",
		RawQuery: "sslmode=disable",
	}).String()
	ctx := context.Background()
	pool, err := postgres.Open(ctx, dsn)
	if err != nil {
		slog.Error("open postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := postgres.ApplyMigrations(ctx, pool, os.DirFS("."), "migrations/auction"); err != nil {
		slog.Error("apply migrations", "error", err)
		os.Exit(1)
	}
	tokens, err := auth.NewTokenService(auth.TokenConfig{Secret: cfg.JWTSecret})
	if err != nil {
		slog.Error("create token service", "error", err)
		os.Exit(1)
	}
	productURL := strings.TrimSpace(os.Getenv("PRODUCT_SERVICE_URL"))
	if productURL == "" {
		productURL = "http://localhost:8082"
	}
	products := NewHTTPProductClient(productURL, credential, &http.Client{Timeout: 5 * time.Second})
	service := NewAuctionService(NewRepository(pool), products)
	server := &http.Server{Addr: fmt.Sprintf(":%d", servicePort(cfg)), Handler: observability.Middleware(slog.Default())(NewHandler(service, tokens))}
	slog.Info("auction service listening", "addr", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("serve auction service", "error", err)
		os.Exit(1)
	}
}

func requiredInternalServiceCredential() (string, error) {
	credential := strings.TrimSpace(os.Getenv("INTERNAL_SERVICE_CREDENTIAL"))
	if credential == "" {
		return "", fmt.Errorf("INTERNAL_SERVICE_CREDENTIAL is required")
	}
	return credential, nil
}

func servicePort(cfg config.Config) int {
	if port := cfg.ServicePorts["AUCTION_SERVICE"]; port != 0 {
		return port
	}
	return 8083
}
