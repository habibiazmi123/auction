package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/example/auction/packages/auth"
	"github.com/example/auction/packages/config"
	"github.com/example/auction/packages/kafka"
	"github.com/example/auction/packages/observability"
	"github.com/example/auction/packages/postgres"
)

func main() {
	internalCredential, err := requiredInternalServiceCredential()
	if err != nil {
		slog.Error("load internal service credential", "error", err)
		os.Exit(1)
	}
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	relay := postgres.NewOutboxRelay(pool, producer)
	service := NewProductService(NewRepository(pool))

	health := []observability.HealthCheck{
		{Name: "postgres", Check: func(ctx context.Context) error { return pool.Ping(ctx) }},
		{Name: "kafka", Check: func(ctx context.Context) error { return kafka.CheckConnectivity(ctx, cfg.KafkaBrokers) }},
	}
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", servicePort(cfg)),
		Handler: observability.WithHealth(observability.Middleware(slog.Default())(NewHandler(service, tokens, internalCredential)), health),
	}
	slog.Info("product service listening", "addr", server.Addr)

	if err := observability.Run(ctx,
		func(ctx context.Context) error { return observability.RunServer(ctx, server, 10*time.Second) },
		func(ctx context.Context) error { return relay.Run(ctx) },
	); err != nil {
		slog.Error("run product service", "error", err)
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
	if port := cfg.ServicePorts["PRODUCT_SERVICE"]; port != 0 {
		return port
	}
	return 8082
}
