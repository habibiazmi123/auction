package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/example/auction/packages/postgres"
)

var services = map[string]string{
	"user":         "user_db",
	"product":      "product_db",
	"auction":      "auction_db",
	"transaction":  "transaction_db",
	"notification": "notification_db",
}

var migrationDirs = map[string]string{
	"user": "migrations/user",
}

func main() {
	service := flag.String("service", "", "service to migrate")
	flag.Parse()
	database, ok := services[*service]
	if !ok {
		fmt.Fprintln(os.Stderr, "-service must be one of: user, product, auction, transaction, notification")
		os.Exit(2)
	}

	user, password, port := requiredEnv("POSTGRES_USER"), requiredEnv("POSTGRES_PASSWORD"), os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5432"
	}
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "localhost"
	}
	dsn := (&url.URL{Scheme: "postgres", Host: host + ":" + port, User: url.UserPassword(user, password), Path: "/" + database, RawQuery: "sslmode=disable"}).String()
	pool, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer pool.Close()

	migrationDir := filepath.Join("services", *service, "migrations")
	if dir, ok := migrationDirs[*service]; ok {
		migrationDir = dir
	}
	if err := postgres.ApplyMigrations(context.Background(), pool, os.DirFS("."), migrationDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func requiredEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		fmt.Fprintf(os.Stderr, "required environment variable %s is missing\n", key)
		os.Exit(1)
	}
	return value
}
