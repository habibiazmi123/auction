package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/example/auction/packages/auth"
	"github.com/example/auction/packages/config"
	"github.com/example/auction/packages/kafka"
	"github.com/example/auction/packages/observability"
	kafkago "github.com/segmentio/kafka-go"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	tokens, err := auth.NewTokenService(auth.TokenConfig{Secret: cfg.JWTSecret})
	if err != nil {
		slog.Error("create token service", "error", err)
		os.Exit(1)
	}
	producer := kafka.NewProducer(kafka.ProducerConfig{Brokers: cfg.KafkaBrokers})
	defer producer.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hub := NewHub()

	publicEventConfig := kafka.ConsumerConfig{
		Brokers:     cfg.KafkaBrokers,
		Topic:       "auction.events.v1",
		GroupID:     "api-gateway-public-events",
		StartOffset: kafkago.FirstOffset,
	}
	notificationConfig := kafka.ConsumerConfig{
		Brokers:     cfg.KafkaBrokers,
		Topic:       "notification.events.v1",
		GroupID:     "api-gateway-notifications",
		StartOffset: kafkago.FirstOffset,
	}

	targets := ServiceTargets{
		User:         envOrDefault("USER_SERVICE_URL", "http://localhost:8081"),
		Product:      envOrDefault("PRODUCT_SERVICE_URL", "http://localhost:8082"),
		Auction:      envOrDefault("AUCTION_SERVICE_URL", "http://localhost:8083"),
		Transaction:  envOrDefault("TRANSACTION_SERVICE_URL", "http://localhost:8084"),
		Notification: envOrDefault("NOTIFICATION_SERVICE_URL", "http://localhost:8085"),
	}
	smokeClientDir := envOrDefault("SMOKE_CLIENT_DIR", "./apps/smoke-client")

	health := []observability.HealthCheck{
		{Name: "kafka", Check: func(ctx context.Context) error { return kafka.CheckConnectivity(ctx, cfg.KafkaBrokers) }},
	}
	server := &http.Server{
		Addr: fmt.Sprintf(":%d", gatewayPort(cfg)),
		Handler: observability.WithHealth(
			observability.Middleware(slog.Default())(NewHandler(NewBidIngress(producer), tokens, hub, targets, smokeClientDir, cfg.AllowedOrigins, &http.Client{Timeout: 5 * time.Second})),
			health,
		),
		ReadHeaderTimeout: 5 * time.Second,
	}
	slog.Info("api gateway listening", "addr", server.Addr)

	if err := observability.Run(ctx,
		func(ctx context.Context) error { return observability.RunServer(ctx, server, 10*time.Second) },
		func(ctx context.Context) error { return hub.RunPublicEventConsumer(ctx, publicEventConfig) },
		func(ctx context.Context) error { return hub.RunNotificationConsumer(ctx, notificationConfig) },
	); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("run api gateway", "error", err)
		os.Exit(1)
	}
}

func envOrDefault(key, defaultValue string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return defaultValue
}

func gatewayPort(cfg config.Config) int {
	if port := cfg.ServicePorts["API_GATEWAY"]; port != 0 {
		return port
	}
	return 8080
}
