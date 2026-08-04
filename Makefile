SHELL := /bin/sh
COMPOSE := docker compose --env-file .env -f deployments/docker-compose.yml

.PHONY: up down logs topics migrate test test-integration lint build

up:
	$(COMPOSE) up -d

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f

topics:
	$(COMPOSE) exec -T kafka bash /opt/bitnami/scripts/init-topics.sh

migrate:
	@for service in user product auction transaction notification; do \
		go run ./cmd/migrate -service "$${service}"; \
	done

test:
	go test ./...

test-integration:
	go test -tags=integration ./...

lint:
	golangci-lint run ./...

build:
	go build ./...
