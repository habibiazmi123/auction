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
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "localhost"
	}
	dsn := (&url.URL{
		Scheme:   "postgres",
		Host:     host + ":" + fmt.Sprint(cfg.PostgresPort),
		User:     url.UserPassword(cfg.PostgresUser, cfg.PostgresPassword),
		Path:     "/user_db",
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
	if err := postgres.ApplyMigrations(ctx, pool, os.DirFS("."), "migrations/user"); err != nil {
		slog.Error("apply migrations", "error", err)
		os.Exit(1)
	}
	tokens, err := auth.NewTokenService(auth.TokenConfig{Secret: cfg.JWTSecret})
	if err != nil {
		slog.Error("create token service", "error", err)
		os.Exit(1)
	}
	service := NewUserService(NewRepository(pool), auth.NewPasswordService(), tokens)

	health := []observability.HealthCheck{
		{Name: "postgres", Check: func(ctx context.Context) error { return pool.Ping(ctx) }},
	}
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", servicePort(cfg)),
		Handler: observability.WithHealth(observability.Middleware(slog.Default())(NewHandler(service)), health),
	}
	slog.Info("user service listening", "addr", server.Addr)

	if err := observability.Run(ctx, func(ctx context.Context) error {
		return observability.RunServer(ctx, server, 10*time.Second)
	}); err != nil {
		slog.Error("run user service", "error", err)
		os.Exit(1)
	}
}

func servicePort(cfg config.Config) int {
	if port := cfg.ServicePorts["USER_SERVICE"]; port != 0 {
		return port
	}
	return 8081
}
