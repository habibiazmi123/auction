package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/habibiazmi123/auction/packages/auth"
	"github.com/habibiazmi123/auction/packages/config"
	"github.com/habibiazmi123/auction/packages/contracts"
	"github.com/habibiazmi123/auction/packages/kafka"
	"github.com/habibiazmi123/auction/packages/observability"
	"github.com/habibiazmi123/auction/packages/postgres"
	kafkago "github.com/segmentio/kafka-go"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
	repository := NewRepository(pool)
	service := NewAuctionService(repository, products)

	producer := kafka.NewProducer(kafka.ProducerConfig{Brokers: cfg.KafkaBrokers})
	defer producer.Close()

	relay := NewOutboxRelay(pool, producer)
	processor := NewBidProcessor(repository)
	consumer := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:     cfg.KafkaBrokers,
		Topic:       "auction.bid.commands.v1",
		GroupID:     "auction-service-bid-processors",
		StartOffset: kafkago.FirstOffset,
	})
	defer consumer.Close()

	closer := NewAuctionCloser(repository)
	starter := NewAuctionStarter(repository)

	health := []observability.HealthCheck{
		{Name: "postgres", Check: func(ctx context.Context) error { return pool.Ping(ctx) }},
		{Name: "kafka", Check: func(ctx context.Context) error { return kafka.CheckConnectivity(ctx, cfg.KafkaBrokers) }},
	}
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", servicePort(cfg)),
		Handler: observability.WithHealth(observability.Middleware(slog.Default())(NewHandler(service, tokens)), health),
	}
	slog.Info("auction service listening", "addr", server.Addr)

	if err := observability.Run(ctx,
		func(ctx context.Context) error { return observability.RunServer(ctx, server, 10*time.Second) },
		func(ctx context.Context) error { return relay.Run(ctx) },
		func(ctx context.Context) error {
			return consumer.Run(ctx, func(ctx context.Context, message kafka.Message) error {
				var command contracts.BidCommand
				if err := json.Unmarshal(message.Payload, &command); err != nil {
					slog.WarnContext(ctx, "drop malformed bid command", "error", err)
					return nil
				}
				return processor.Handle(ctx, command)
			})
		},
		func(ctx context.Context) error { return runCloser(ctx, closer) },
		func(ctx context.Context) error { return runStarter(ctx, starter) },
	); err != nil {
		slog.Error("run auction service", "error", err)
		os.Exit(1)
	}
}

func runCloser(ctx context.Context, closer AuctionCloser) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := closer.CloseDue(ctx, time.Now().UTC()); err != nil {
				slog.Error("close due run failed", "error", err)
			}
		}
	}
}

func runStarter(ctx context.Context, starter AuctionStarter) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := starter.StartDue(ctx, time.Now().UTC()); err != nil {
				slog.Error("start due run failed", "error", err)
			}
		}
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
