package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	"github.com/example/auction/packages/config"
	"github.com/example/auction/packages/kafka"
	"github.com/example/auction/packages/observability"
	postgrespkg "github.com/example/auction/packages/postgres"
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
		Path:     "/notification_db",
		RawQuery: "sslmode=disable",
	}).String()
	ctx := context.Background()
	pool, err := postgrespkg.Open(ctx, dsn)
	if err != nil {
		slog.Error("open postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := postgrespkg.ApplyMigrations(ctx, pool, os.DirFS("."), "migrations/notification"); err != nil {
		slog.Error("apply migrations", "error", err)
		os.Exit(1)
	}

	producer := kafka.NewProducer(kafka.ProducerConfig{Brokers: cfg.KafkaBrokers})
	defer producer.Close()

	relay := postgrespkg.NewOutboxRelay(pool, producer)
	go func() {
		if err := relay.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("outbox relay stopped", "error", err)
		}
	}()

	repository := NewNotificationRepository(pool)
	consumerConfig := kafka.ConsumerConfig{
		Brokers:     cfg.KafkaBrokers,
		Topic:       "auction.events.v1",
		GroupID:     "notification-service-public-events",
		StartOffset: kafkago.FirstOffset,
	}
	go func() {
		if err := RunPublicEventConsumer(ctx, consumerConfig, repository); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("public event consumer stopped", "error", err)
		}
	}()

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", servicePort(cfg)),
		Handler: observability.Middleware(slog.Default())(NewHandler(repository)),
	}
	slog.Info("notification service listening", "addr", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("serve notification service", "error", err)
		os.Exit(1)
	}
}

func servicePort(cfg config.Config) int {
	if port := cfg.ServicePorts["NOTIFICATION_SERVICE"]; port != 0 {
		return port
	}
	return 8085
}
