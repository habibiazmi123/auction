package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	"github.com/example/auction/packages/auth"
	"github.com/example/auction/packages/config"
	"github.com/example/auction/packages/kafka"
	"github.com/example/auction/packages/observability"
	"github.com/example/auction/packages/postgres"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
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
		Path:     "/product_db",
		RawQuery: "sslmode=disable",
	}).String()
	ctx := context.Background()
	pool, err := postgres.Open(ctx, dsn)
	if err != nil {
		slog.Error("open postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := postgres.ApplyMigrations(ctx, pool, os.DirFS("."), "migrations/product"); err != nil {
		slog.Error("apply migrations", "error", err)
		os.Exit(1)
	}
	tokens, err := auth.NewTokenService(auth.TokenConfig{Secret: cfg.JWTSecret})
	if err != nil {
		slog.Error("create token service", "error", err)
		os.Exit(1)
	}
	producer := kafka.NewProducer(kafka.ProducerConfig{Brokers: cfg.KafkaBrokers})
	defer producer.Close()
	go func() {
		if err := postgres.NewOutboxRelay(pool, producer).Run(ctx); err != nil {
			slog.Error("run product outbox relay", "error", err)
		}
	}()
	service := NewProductService(NewRepository(pool))
	server := &http.Server{Addr: fmt.Sprintf(":%d", servicePort(cfg)), Handler: observability.Middleware(slog.Default())(NewHandler(service, tokens, os.Getenv("INTERNAL_SERVICE_CREDENTIAL")))}
	slog.Info("product service listening", "addr", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("serve product service", "error", err)
		os.Exit(1)
	}
}

func servicePort(cfg config.Config) int {
	if port := cfg.ServicePorts["PRODUCT_SERVICE"]; port != 0 {
		return port
	}
	return 8082
}
