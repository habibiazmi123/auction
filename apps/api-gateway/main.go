package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
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

	ctx := context.Background()
	hub := NewHub()

	publicEventConfig := kafka.ConsumerConfig{
		Brokers:     cfg.KafkaBrokers,
		Topic:       "auction.events.v1",
		GroupID:     "api-gateway-public-events",
		StartOffset: kafkago.FirstOffset,
	}
	go func() {
		if err := hub.RunPublicEventConsumer(ctx, publicEventConfig); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("public event consumer stopped", "error", err)
		}
	}()

	notificationConfig := kafka.ConsumerConfig{
		Brokers:     cfg.KafkaBrokers,
		Topic:       "notification.events.v1",
		GroupID:     "api-gateway-notifications",
		StartOffset: kafkago.FirstOffset,
	}
	go func() {
		if err := hub.RunNotificationConsumer(ctx, notificationConfig); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("notification consumer stopped", "error", err)
		}
	}()

	notificationURL := strings.TrimSpace(os.Getenv("NOTIFICATION_SERVICE_URL"))
	if notificationURL == "" {
		notificationURL = "http://localhost:8085"
	}

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", gatewayPort(cfg)),
		Handler:           observability.Middleware(slog.Default())(NewHandler(NewBidIngress(producer), tokens, hub, notificationURL, &http.Client{Timeout: 5 * time.Second})),
		ReadHeaderTimeout: 5 * time.Second,
	}
	slog.Info("api gateway listening", "addr", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("serve api gateway", "error", err)
		os.Exit(1)
	}
}

func gatewayPort(cfg config.Config) int {
	if port := cfg.ServicePorts["API_GATEWAY"]; port != 0 {
		return port
	}
	return 8080
}
