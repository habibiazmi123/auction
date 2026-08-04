package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/example/auction/packages/auth"
	"github.com/example/auction/packages/config"
	"github.com/example/auction/packages/kafka"
	"github.com/example/auction/packages/observability"
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
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", gatewayPort(cfg)),
		Handler: observability.Middleware(slog.Default())(NewHandler(NewBidIngress(producer), tokens)),
	}
	slog.Info("api gateway listening", "addr", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("serve api gateway", "error", err)
		os.Exit(1)
	}
	_ = ctx
}

func gatewayPort(cfg config.Config) int {
	if port := cfg.ServicePorts["API_GATEWAY"]; port != 0 {
		return port
	}
	return 8080
}
