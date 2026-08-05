package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/habibiazmi123/auction/packages/config"
	"github.com/habibiazmi123/auction/packages/kafka"
	"github.com/habibiazmi123/auction/packages/observability"
	postgrespkg "github.com/habibiazmi123/auction/packages/postgres"
	kafkago "github.com/segmentio/kafka-go"
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
		Path:     "/transaction_db",
		RawQuery: "sslmode=disable",
	}).String()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgrespkg.Open(ctx, dsn)
	if err != nil {
		slog.Error("open postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := postgrespkg.ApplyMigrations(ctx, pool, os.DirFS("."), "migrations/transaction"); err != nil {
		slog.Error("apply migrations", "error", err)
		os.Exit(1)
	}

	producer := kafka.NewProducer(kafka.ProducerConfig{Brokers: cfg.KafkaBrokers})
	defer producer.Close()

	relay := postgrespkg.NewOutboxRelay(pool, producer)
	repository := NewSettlementRepository(pool)
	consumerConfig := kafka.ConsumerConfig{
		Brokers:     cfg.KafkaBrokers,
		Topic:       "auction.events.v1",
		GroupID:     "transaction-service-close-events",
		StartOffset: kafkago.FirstOffset,
	}

	health := []observability.HealthCheck{
		{Name: "postgres", Check: func(ctx context.Context) error { return pool.Ping(ctx) }},
		{Name: "kafka", Check: func(ctx context.Context) error { return kafka.CheckConnectivity(ctx, cfg.KafkaBrokers) }},
	}
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", servicePort(cfg)),
		Handler: observability.WithHealth(observability.Middleware(slog.Default())(NewHandler(repository)), health),
	}
	slog.Info("transaction service listening", "addr", server.Addr)

	if err := observability.Run(ctx,
		func(ctx context.Context) error { return observability.RunServer(ctx, server, 10*time.Second) },
		func(ctx context.Context) error { return relay.Run(ctx) },
		func(ctx context.Context) error { return RunCloseEventConsumer(ctx, consumerConfig, repository) },
	); err != nil {
		slog.Error("run transaction service", "error", err)
		os.Exit(1)
	}
}

func servicePort(cfg config.Config) int {
	if port := cfg.ServicePorts["TRANSACTION_SERVICE"]; port != 0 {
		return port
	}
	return 8084
}
