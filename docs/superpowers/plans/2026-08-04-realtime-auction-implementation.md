# Realtime Auction Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go monorepo with independently runnable auction services, Kafka KRaft/PostgreSQL local infrastructure, asynchronous bid processing, WebSocket updates, settlement ledger, and a minimal smoke client.

**Architecture:** Use one Go module with independently compiled service binaries to avoid duplicated dependency management. Services own their domain tables and communicate through versioned Kafka envelopes; the gateway exposes REST/WebSocket and the auction service serializes commands by Kafka partition key `auction_id`.

**Tech Stack:** Go, `net/http`, chi router, pgx/v5, PostgreSQL, Kafka KRaft, `segmentio/kafka-go`, `google/uuid`, JWT, Argon2id, `nhooyr.io/websocket`, Docker Compose, Kafka UI, vanilla browser JavaScript.

## Global Constraints

- Initial capacity target is 10,000 bid commands per second with bursts up to 50,000 commands per second per cluster.
- Ordering is required per `auction_id`, not globally.
- Bid submission returns `202 Accepted` with `pending` status; final status is delivered through WebSocket and query API.
- Every bid command requires `Idempotency-Key` and a JWT access token.
- Accepted bid state and its outbox event are written in one PostgreSQL transaction.
- Kafka delivery is at least once; all consumers must be idempotent.
- No service reads another service's tables.
- English ascending auction is implemented first; proxy auto-bidding remains an explicit later extension.
- Fixed auction end is the default; anti-sniping is configurable per auction.
- Settlement is a ledger only; no wallet or payment gateway exists in this phase.
- No new runtime dependency is added when Go standard library or an already selected dependency is sufficient.
- Passwords and refresh tokens are never logged or stored in plaintext.

---

## File Map

### Root and infrastructure

- Create: `go.mod` - module and direct dependencies.
- Create: `go.work` - workspace entry for the root module.
- Create: `Makefile` - repeatable local commands.
- Create: `.env.example` - required image names and local ports without secrets.
- Create: `deployments/docker-compose.yml` - Kafka, PostgreSQL, Kafka UI, and service dependencies.
- Create: `deployments/kafka/init-topics.sh` - idempotent topic creation.
- Create: `deployments/postgres/init-databases.sql` - service database creation.
- Create: `Dockerfile` - multi-stage build for a selected service binary.
- Create: `.dockerignore` - excludes build and local secret files.
- Create: `cmd/migrate/main.go` - migration command for one service database.

### Shared packages

- Create: `packages/contracts/event.go` - event envelope and event type constants.
- Create: `packages/contracts/auth.go` - authenticated principal transport type.
- Create: `packages/contracts/auction.go` - bid command and auction event payloads.
- Create: `packages/contracts/transaction.go` - settlement payload.
- Create: `packages/config/config.go` - environment parsing and validation.
- Create: `packages/auth/jwt.go` - JWT issue and verification interfaces.
- Create: `packages/auth/password.go` - Argon2id hash and verify functions.
- Create: `packages/kafka/producer.go` - Kafka producer wrapper.
- Create: `packages/kafka/consumer.go` - consumer loop with retry and DLQ hooks.
- Create: `packages/postgres/database.go` - pgx pool creation and ping.
- Create: `packages/postgres/migrate.go` - ordered SQL migration runner.
- Create: `packages/postgres/outbox.go` - outbox polling and publishing.
- Create: `packages/observability/http.go` - request IDs, structured request logging, and metrics hooks.

### Services

- Create: `apps/api-gateway/main.go` - gateway process.
- Create: `apps/api-gateway/handler.go` - public REST and WebSocket handlers.
- Create: `apps/api-gateway/hub.go` - bounded WebSocket subscription hub.
- Create: `apps/user-service/main.go` - user process.
- Create: `apps/user-service/domain.go` - user rules and errors.
- Create: `apps/user-service/repository.go` - user and refresh token SQL operations.
- Create: `apps/user-service/service.go` - register/login/refresh/logout use cases.
- Create: `apps/user-service/handler.go` - auth HTTP handlers.
- Create: `apps/product-service/main.go` - product process.
- Create: `apps/product-service/domain.go` - product rules.
- Create: `apps/product-service/repository.go` - product SQL operations.
- Create: `apps/product-service/handler.go` - product HTTP handlers.
- Create: `apps/auction-service/main.go` - auction process.
- Create: `apps/auction-service/domain.go` - auction and bid rules.
- Create: `apps/auction-service/repository.go` - auction, bid, and outbox SQL operations.
- Create: `apps/auction-service/handler.go` - auction query and bid ingress handlers.
- Create: `apps/auction-service/consumer.go` - ordered bid command consumer.
- Create: `apps/auction-service/closer.go` - due-auction closer.
- Create: `apps/transaction-service/main.go` - settlement process.
- Create: `apps/transaction-service/repository.go` - settlement SQL operations.
- Create: `apps/transaction-service/consumer.go` - close-event consumer.
- Create: `apps/transaction-service/handler.go` - settlement query handler.
- Create: `apps/notification-service/main.go` - notification process.
- Create: `apps/notification-service/repository.go` - notification SQL operations.
- Create: `apps/notification-service/consumer.go` - public event consumer.
- Create: `apps/notification-service/handler.go` - notification query handler.

### Migrations and tests

- Create: `migrations/user/001_users.sql`.
- Create: `migrations/product/001_products.sql`.
- Create: `migrations/product/002_outbox.sql`.
- Create: `migrations/auction/001_auctions.sql`.
- Create: `migrations/auction/002_bids_outbox.sql`.
- Create: `migrations/transaction/001_settlements.sql`.
- Create: `migrations/transaction/002_outbox.sql`.
- Create: `migrations/notification/001_notifications.sql`.
- Create: `migrations/notification/002_outbox.sql`.
- Create: `internal/testkit/postgres.go` - disposable database helpers.
- Create: `internal/testkit/kafka.go` - test topic and consumer helpers.
- Create: `apps/auction-service/domain_test.go`.
- Create: `apps/auction-service/integration_test.go`.
- Create: `apps/transaction-service/integration_test.go`.
- Create: `tests/e2e/auction_test.go`.
- Create: `tests/load/bid-producer.go`.

### Smoke client and operations

- Create: `apps/smoke-client/index.html`.
- Create: `apps/smoke-client/app.js`.
- Create: `docs/runbook.md`.
- Create: `.github/workflows/ci.yml`.

---

## Task 1: Bootstrap the Monorepo and Local Infrastructure

**Files:**
- Create: `go.mod`, `go.work`, `Makefile`, `.env.example`, `.dockerignore`.
- Create: `deployments/docker-compose.yml`, `deployments/kafka/init-topics.sh`, `deployments/postgres/init-databases.sql`.
- Create: `Dockerfile`.

**Interfaces:**
- Produces service environment variables consumed by every later task.
- Produces Kafka topics `auction.bid.commands.v1`, `auction.events.v1`, `product.events.v1`, `user.events.v1`, `transaction.events.v1`, and `notification.events.v1`.

- [ ] **Step 1: Add the module and workspace files.**

  Set the module path to `github.com/example/auction` and set the Go version to the version reported by `go version`. Add only the dependencies required by the implementation: chi, pgx/v5, kafka-go, google/uuid, JWT, Argon2id support, and nhooyr.io/websocket.

- [ ] **Step 2: Add environment validation inputs.**

  `.env.example` must define `KAFKA_IMAGE`, `POSTGRES_IMAGE`, `KAFKA_UI_IMAGE`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `KAFKA_BROKERS`, `JWT_SECRET`, and service ports. Compose must fail when image variables are absent instead of silently choosing an image tag.

- [ ] **Step 3: Add Compose services.**

  Configure Kafka in KRaft mode with persistent local volumes, PostgreSQL with a persistent volume and healthcheck, Kafka UI depending on Kafka health, and no host exposure beyond the documented local ports. Use `${KAFKA_IMAGE:?set KAFKA_IMAGE}` style interpolation.

- [ ] **Step 4: Add database and topic initialization.**

  `init-databases.sql` creates `user_db`, `product_db`, `auction_db`, `transaction_db`, and `notification_db`. `init-topics.sh` uses Kafka's topic CLI with `--if-not-exists`; command topics and event topics use a configurable partition count, defaulting to 64 for local testing.

- [ ] **Step 5: Add Make targets.**

  Include exact targets `up`, `down`, `logs`, `topics`, `migrate`, `test`, `test-integration`, `lint`, and `build`. `up` must start Compose and `topics` must be safe to run repeatedly. `migrate` runs `go run ./cmd/migrate` once for each service database and migration directory.

- [ ] **Step 6: Verify the foundation.**

  Run:

  ```bash
  cp .env.example .env
  make up
  make topics
  docker compose -f deployments/docker-compose.yml ps
  ```

  Expected: PostgreSQL, Kafka, and Kafka UI report healthy; all six base topics exist; no application service is required for this task.

- [ ] **Step 7: Commit.**

  ```bash
  git add go.mod go.work Makefile .env.example .dockerignore Dockerfile deployments
  git commit -m "chore: bootstrap auction monorepo infrastructure"
  ```

## Task 2: Implement Shared Configuration, Contracts, Kafka, PostgreSQL, and Observability Primitives

**Files:**
- Create: `packages/config/config.go`, `packages/contracts/event.go`, `packages/contracts/auth.go`, `packages/contracts/auction.go`, `packages/contracts/transaction.go`.
- Create: `packages/auth/jwt.go`, `packages/auth/password.go`.
- Create: `packages/kafka/producer.go`, `packages/kafka/consumer.go`.
- Create: `packages/postgres/database.go`, `packages/postgres/outbox.go`.
- Create: `packages/observability/http.go`.
- Test: `packages/config/config_test.go`, `packages/auth/auth_test.go`, `packages/contracts/event_test.go`.

**Interfaces:**
- `contracts.EventEnvelope[T any]` contains `EventID`, `EventType`, `Version`, `OccurredAt`, `Producer`, `CorrelationID`, `CausationID`, and `Payload`.
- `kafka.Producer.Publish(ctx context.Context, topic, key string, value []byte) error` waits for broker acknowledgement.
- `auth.TokenService.Issue(ctx context.Context, principal Principal) (TokenPair, error)` and `Verify(ctx context.Context, token string) (Principal, error)`.
- `postgres.Open(ctx context.Context, dsn string) (*pgxpool.Pool, error)`.
- `postgres.OutboxRelay.Run(ctx context.Context) error` publishes pending rows and marks them sent.

- [ ] **Step 1: Write contract tests.**

  Test JSON round-trip of `EventEnvelope`, rejection of an empty event ID, and stable serialization of `BidCommand` and `SettlementCreated` payloads.

- [ ] **Step 2: Implement contracts and configuration.**

  Parse required environment variables once at process startup. Return a named error containing the missing key. Do not read environment variables from domain code.

- [ ] **Step 3: Implement the migration runner.**

  `postgres.ApplyMigrations(ctx, pool, fs.FS, dir)` reads numeric `.sql` files in lexical order, creates `schema_migrations`, executes each file in a transaction, and records the filename. Re-running the command skips recorded files and fails if a migration transaction fails. `cmd/migrate` selects one of the five service databases and its migration directory from a required `-service` flag.

- [ ] **Step 4: Write authentication tests.**

  Test Argon2id password verification, wrong-password rejection, expired JWT rejection, issuer/audience validation, and refresh-token rotation identity. Use a test secret generated in memory.

- [ ] **Step 5: Implement JWT and password services.**

  Access tokens carry `sub`, `role`, `iss`, `aud`, `iat`, and `exp`. Refresh tokens are opaque random values; only their hash is persisted by the user service. JWT verification returns a typed principal.

- [ ] **Step 6: Implement Kafka and PostgreSQL adapters.**

  Configure producer required acknowledgements and timeouts. The consumer wrapper must expose the message key, payload, topic, partition, and offset to handlers, and must not commit until the handler returns nil. PostgreSQL uses a bounded pgx pool and context-aware queries.

- [ ] **Step 7: Implement observability middleware.**

  Generate or accept `X-Request-ID`, propagate `X-Correlation-ID`, log method/path/status/latency as JSON, and redact authorization headers and request bodies by default.

- [ ] **Step 8: Run focused tests.**

  ```bash
  go test ./packages/...
  go vet ./packages/...
  ```

  Expected: all tests pass and `go vet` produces no diagnostics.

- [ ] **Step 9: Commit.**

  ```bash
  git add packages
  git commit -m "feat: add shared auction service primitives"
  ```

## Task 3: Build User Authentication

**Files:**
- Create: `migrations/user/001_users.sql`.
- Create: `apps/user-service/main.go`, `domain.go`, `repository.go`, `service.go`, `handler.go`.
- Test: `apps/user-service/service_test.go`, `apps/user-service/handler_test.go`.

**Interfaces:**
- `UserService.Register(ctx context.Context, email, password, role string) (User, error)`.
- `UserService.Login(ctx context.Context, email, password string) (auth.TokenPair, error)`.
- `UserService.Refresh(ctx context.Context, refreshToken string) (auth.TokenPair, error)`.
- `UserRepository.Create`, `FindByEmail`, `SaveRefreshTokenHash`, and `RevokeRefreshToken`.

- [ ] **Step 1: Write service tests.**

  Cover duplicate email, invalid role, wrong password, successful login, refresh rotation, revoked refresh token, and expired refresh token. Assert that errors do not disclose whether an email exists during login.

- [ ] **Step 2: Add the migration.**

  Create `users`, `refresh_tokens`, and `schema_migrations`. Add unique normalized email, role check, Argon2id password hash, refresh token hash, expiry, revoked timestamp, and created/updated timestamps.

- [ ] **Step 3: Implement repository and use cases.**

  Normalize email with `strings.ToLower(strings.TrimSpace(email))`, validate password length at the boundary, hash passwords with Argon2id, and rotate refresh token inside a transaction that revokes the previous token.

- [ ] **Step 4: Implement HTTP handlers.**

  Return `201` for registration, `200` for login/refresh, `204` for logout, `400` for malformed input, `401` for invalid credentials/tokens, and `409` for duplicate registration. Use the common problem response.

- [ ] **Step 5: Verify.**

  ```bash
  go test ./apps/user-service -run 'Test(Register|Login|Refresh)' -v
  ```

  Expected: all authentication cases pass.

- [ ] **Step 6: Commit.**

  ```bash
  git add migrations/user apps/user-service
  git commit -m "feat: add JWT user authentication service"
  ```

## Task 4: Build Product Ownership and Product API

**Files:**
- Create: `migrations/product/001_products.sql`.
- Create: `apps/product-service/main.go`, `domain.go`, `repository.go`, `handler.go`.
- Test: `apps/product-service/domain_test.go`, `apps/product-service/handler_test.go`.

**Interfaces:**
- `ProductService.Create(ctx context.Context, sellerID uuid.UUID, input CreateProductInput) (Product, error)`.
- `ProductService.Get(ctx context.Context, id uuid.UUID) (Product, error)`.
- `ProductService.List(ctx context.Context, page Page) ([]Product, PageInfo, error)`.
- `ProductService.Update(ctx context.Context, sellerID, productID uuid.UUID, input UpdateProductInput) error`.

- [ ] **Step 1: Write domain and handler tests.**

  Test required name/description, positive quantity, seller-only create/update, ownership rejection, bounded page size, and problem responses.

- [ ] **Step 2: Add product migration.**

  Create `products` with seller ID, name, description, status (`draft`, `available`, `locked`), created/updated timestamps, and indexes on seller/status.

- [ ] **Step 3: Implement repository and HTTP API.**

  Use parameterized pgx queries. Create/update operations must return `409` when the product is locked and `403` when the authenticated seller is not the owner.

- [ ] **Step 4: Publish product events.**

  Add `migrations/product/002_outbox.sql` with the standard outbox schema. Write `product.created.v1` and `product.updated.v1` to the service outbox in the same transaction as the product change. Use product ID as event key.

- [ ] **Step 5: Add internal ownership and lock endpoints.**

  Implement `GET /internal/products/{product_id}/ownership?user_id={seller_id}`, `POST /internal/products/{product_id}/auction-lock`, and `POST /internal/products/{product_id}/auction-unlock` with an internal service credential. The ownership response includes the product snapshot; the lock and unlock endpoints are idempotent by `auction_id`; a locked product cannot be edited.

- [ ] **Step 6: Verify and commit.**

  ```bash
  go test ./apps/product-service -v
  git add migrations/product apps/product-service
  git commit -m "feat: add seller product service"
  ```

## Task 5: Implement Auction Lifecycle and Domain Rules

**Files:**
- Create: `migrations/auction/001_auctions.sql`.
- Create: `apps/auction-service/domain.go`, `repository.go`, `handler.go`, `main.go`.
- Test: `apps/auction-service/domain_test.go`, `apps/auction-service/handler_test.go`.

**Interfaces:**
- `AuctionService.Create(ctx context.Context, sellerID uuid.UUID, input CreateAuctionInput) (Auction, error)`.
- `AuctionService.Get(ctx context.Context, auctionID uuid.UUID) (AuctionView, error)`.
- `AuctionRules.ValidateBid(auction Auction, bidderID uuid.UUID, amount int64, now time.Time) error`.
- `AuctionRules.ApplyBid(auction Auction, bidderID uuid.UUID, amount int64, now time.Time) (Auction, error)`.

- [ ] **Step 1: Write domain tests first.**

  Cover draft-to-scheduled-to-live transitions, seller exclusion, auction window, minimum increment, lower/equal bid rejection, anti-sniping extension, fixed-end behavior, and close idempotency.

- [ ] **Step 2: Add the auction migration.**

  Create `auctions` with product/seller IDs, status, starting/current price, current winner, increment, start/end times, anti-sniping settings, and version. Add indexes on status/end time and unique product/live-auction protection.

- [ ] **Step 3: Implement pure domain rules.**

  Keep time and persistence out of `domain.go`. Use integer minor units for money, reject negative values and overflow, and return typed errors such as `ErrAuctionNotLive`, `ErrBidTooLow`, and `ErrSellerCannotBid`.

- [ ] **Step 4: Implement lifecycle API.**

  Before creating an auction, call `GET /internal/products/{product_id}/ownership` and then the idempotent `POST /internal/products/{product_id}/auction-lock`. The auction service stores the returned seller ID and product snapshot; it never queries product tables. If auction persistence fails after the lock, issue the matching unlock command. Return `201` on create, `409` on invalid product/auction state, and `404` for unknown IDs.

- [ ] **Step 5: Verify and commit.**

  ```bash
  go test ./apps/auction-service -run TestAuction -v
  git add migrations/auction/001_auctions.sql apps/auction-service
  git commit -m "feat: add auction lifecycle domain"
  ```

## Task 6: Implement Asynchronous Bid Ingress, Ordered Processing, Outbox, and Close Scheduler

**Files:**
- Create: `migrations/auction/002_bids_outbox.sql`.
- Modify: `apps/api-gateway/handler.go`.
- Create: `apps/auction-service/consumer.go`, `closer.go`.
- Modify: `apps/auction-service/repository.go`, `handler.go`, `main.go`.
- Create: `apps/auction-service/integration_test.go`.

**Interfaces:**
- `BidIngress.Publish(ctx context.Context, command contracts.BidCommand) error`.
- `BidProcessor.Handle(ctx context.Context, command contracts.BidCommand) error`.
- `AuctionRepository.ApplyBidTx(ctx context.Context, command contracts.BidCommand) (BidResult, error)`.
- `AuctionCloser.CloseDue(ctx context.Context, now time.Time) (int, error)`.

- [ ] **Step 1: Write domain-level bid result tests.**

  Test accepted bid, rejected bid persisted with rejection code, duplicate idempotency key returning the original result, same key with different payload returning conflict, anti-sniping extension, and bid received after `ends_at` being rejected.

- [ ] **Step 2: Add bid and outbox migrations.**

  Create append-only `bids`, unique `(auction_id, bidder_id, idempotency_key)`, indexed auction/time, and `outbox_events` with event ID, aggregate ID, topic, key, payload, attempts, next-at, sent-at, and last error. Add a processed-command constraint where needed.

- [ ] **Step 3: Implement gateway bid ingress.**

  Validate UUIDs, positive minor-unit amount, body size, JWT role, and non-empty idempotency key. Generate `bid_id`, publish `BidCommand` keyed by `auction_id`, wait for Kafka acknowledgement, and return:

  ```json
  {"bid_id":"...","auction_id":"...","status":"pending"}
  ```

  Return `400` before Kafka for malformed input and `503` when Kafka cannot acknowledge.

- [ ] **Step 4: Implement the ordered consumer.**

  Consume `auction.bid.commands.v1` in a consumer group. For each command, begin a transaction, lock the auction row, check idempotency, apply domain rules, insert the bid result, update auction state when accepted, insert the corresponding event into outbox, commit, and only then commit the Kafka offset.

- [ ] **Step 5: Implement outbox relay.**

  Poll unsent rows with `FOR UPDATE SKIP LOCKED`, publish to the event topic using the aggregate key, mark sent on acknowledgement, and exponentially delay failures. Rows exceeding the configured attempt count are published to the matching DLQ and marked terminal.

- [ ] **Step 6: Implement due-auction closer.**

  Every second, claim live auctions whose `ends_at <= now()` with `FOR UPDATE SKIP LOCKED`. Set status to `closed`, insert one `auction.closed.v1` outbox event, and commit. A second worker must observe no claim for the same auction.

- [ ] **Step 7: Add integration tests.**

  With PostgreSQL and Kafka from Compose, submit two same-auction bids concurrently and assert deterministic final price/winner, duplicate delivery produces one bid result, outbox retry republishes, and close retry emits one close event.

- [ ] **Step 8: Verify load-path basics and commit.**

  ```bash
  make up
  make topics
  go test ./apps/auction-service -run 'Test(Bid|Outbox|Close)' -count=1 -v
  git add migrations/auction/002_bids_outbox.sql apps/api-gateway apps/auction-service
  git commit -m "feat: add ordered asynchronous bid processing"
  ```

## Task 7: Add Gateway, Settlement, Notifications, and WebSocket Fanout

**Files:**
- Create: `apps/api-gateway/main.go`, `apps/api-gateway/hub.go`.
- Create: `migrations/transaction/001_settlements.sql`, `apps/transaction-service/main.go`, `repository.go`, `consumer.go`, `handler.go`.
- Create: `migrations/transaction/002_outbox.sql`, `migrations/notification/001_notifications.sql`, `migrations/notification/002_outbox.sql`.
- Create: `apps/notification-service/main.go`, `repository.go`, `consumer.go`, `handler.go`.
- Create: `apps/api-gateway/gateway_test.go`, `apps/transaction-service/integration_test.go`, `apps/notification-service/integration_test.go`.

**Interfaces:**
- `Hub.Subscribe(ctx context.Context, auctionID, userID uuid.UUID, conn *websocket.Conn) error`.
- `Hub.Publish(ctx context.Context, auctionID uuid.UUID, event []byte) error`.
- `SettlementRepository.CreateFromCloseTx(ctx context.Context, event contracts.AuctionClosed) (Settlement, error)`.
- `NotificationRepository.CreateIfAbsent(ctx context.Context, eventID string, input NotificationInput) error`.

- [ ] **Step 1: Add settlement migration and test.**

  Create `settlements` with unique `auction_id`, winner, seller, final price, status (`pending`, `paid`, `failed`), event ID, and timestamps. Test duplicate close events return the existing settlement without a second row.

- [ ] **Step 2: Implement transaction consumer.**

  Consume `auction.closed.v1`, insert settlement with `ON CONFLICT (auction_id)`, write `transaction.settlement.created.v1` to the transaction outbox in the same transaction, and commit the Kafka offset after success. `migrations/transaction/002_outbox.sql` uses the standard outbox schema.

- [ ] **Step 3: Add notification migration and consumer.**

  Create notifications with unique source event ID, recipient, type, payload, read timestamp, and created timestamp. Add `migrations/notification/002_outbox.sql`; consume accepted/rejected/closed/settlement events, ignore duplicate source IDs, and write `notification.created.v1` to the outbox in the same transaction.

- [ ] **Step 4: Implement WebSocket hub.**

  Maintain bounded per-auction subscriber sets, one writer per connection, read deadlines, ping/pong heartbeat, and removal on write failure. Reject subscriptions above the configured per-user and per-connection limits.

- [ ] **Step 5: Connect gateway event consumers.**

  Consume public auction and notification events, decode only versioned contracts, and fan out by `auction_id` or recipient. Proxy `GET /v1/notifications` to notification service using the authenticated user ID. Reconnect Kafka consumers with backoff without dropping already committed messages.

- [ ] **Step 6: Add WebSocket tests.**

  Assert authenticated handshake, subscription rejection without JWT, event fanout to the correct auction only, heartbeat timeout cleanup, and reconnect behavior.

- [ ] **Step 7: Verify and commit.**

  ```bash
  go test ./apps/api-gateway ./apps/transaction-service ./apps/notification-service -v
  git add apps/api-gateway apps/transaction-service apps/notification-service migrations/transaction migrations/notification
  git commit -m "feat: add settlement notifications and websocket updates"
  ```

## Task 8: Wire Service Entrypoints, Health Checks, and Compose Runtime

**Files:**
- Modify: `apps/*/main.go`.
- Modify: `packages/observability/http.go`, `deployments/docker-compose.yml`, `Dockerfile`.
- Create: `apps/api-gateway/health_test.go`.

**Interfaces:**
- Every service exposes `GET /health/live` and `GET /health/ready`.
- Every process handles `SIGTERM`, stops accepting requests, drains consumers, closes WebSocket connections, and exits within the configured grace period.

- [ ] **Step 1: Add common health handlers.**

  `/health/live` returns `200` without dependencies. `/health/ready` returns `200` only when the service's PostgreSQL pool and required Kafka connection are healthy; otherwise return `503` with a stable problem code.

- [ ] **Step 2: Wire graceful startup and shutdown.**

  Use `signal.NotifyContext`, start HTTP and consumer goroutines from one process context, collect the first fatal error, cancel all workers, and wait for them before returning from `main`.

- [ ] **Step 3: Add Compose service definitions.**

  Build each Go service with a `SERVICE` build argument, pass only its database DSN and Kafka topics, expose gateway port, and keep internal service ports on the Compose network. Configure the gateway with service base URLs and an `SMOKE_CLIENT_DIR` mount so it can serve the static smoke client at `/`.

- [ ] **Step 4: Wire gateway proxy routes.**

  Route auth, product, auction, settlement, and notification paths to their owning service through one `http.Client` with a per-request timeout. Forward the authenticated principal and correlation ID as internal headers; reject external requests that attempt to set those headers.

- [ ] **Step 5: Verify runtime.**

  ```bash
  make build
  make up
  curl -fsS http://localhost:8080/health/live
  curl -fsS http://localhost:8080/health/ready
  docker compose -f deployments/docker-compose.yml ps
  ```

  Expected: both health endpoints return `200`, all configured services remain running after one Kafka consumer restart, and `make down` removes containers without deleting persistent volumes.

- [ ] **Step 6: Commit.**

  ```bash
  git add apps deployments Dockerfile packages/observability
  git commit -m "chore: wire service runtime and health checks"
  ```

## Task 9: Add the Frontend Smoke Client

**Files:**
- Create: `apps/smoke-client/index.html`, `apps/smoke-client/app.js`.
- Modify: `deployments/docker-compose.yml`, `docs/runbook.md`.

**Interfaces:**
- The client uses only the public `/v1` REST and `/v1/ws` APIs.
- The client must display bid status transitions without reading Kafka.

- [ ] **Step 1: Add static HTML controls.**

  Include login/register fields, product and auction IDs, bid amount, submit button, status output, and an event log. Use labels, keyboard focus, disabled pending state, and text content insertion rather than HTML injection.

- [ ] **Step 2: Implement browser API calls.**

  Store access token in memory, use refresh endpoint on `401`, send `Idempotency-Key` for every bid, render `202` as pending, and poll `GET /v1/bids/{bid_id}` only when WebSocket is disconnected.

- [ ] **Step 3: Implement WebSocket reconnect.**

  Connect with the current access token, subscribe to one auction, display accepted/rejected/closed events, use exponential reconnect capped at 10 seconds, and stop reconnecting when the user logs out.

- [ ] **Step 4: Verify manually.**

  Register a seller and buyer, create product and auction, submit two bids from separate browser tabs, close the auction, and confirm both tabs receive only events for their subscribed auction.

- [ ] **Step 5: Commit.**

  ```bash
  git add apps/smoke-client deployments/docker-compose.yml docs/runbook.md
  git commit -m "feat: add auction smoke client"
  ```

## Task 10: Add End-to-End, Contract, Load, Failure, and CI Gates

**Files:**
- Create: `internal/testkit/postgres.go`, `internal/testkit/kafka.go`.
- Create: `tests/e2e/auction_test.go`, `tests/load/bid-producer.go`.
- Create: `.github/workflows/ci.yml`, `docs/runbook.md`.
- Modify: `Makefile`.

**Interfaces:**
- E2E setup starts Compose dependencies, applies migrations, and uses public gateway URLs.
- Load producer accepts `-brokers`, `-topic`, `-auctions`, `-rate`, and `-duration` flags and reports sent count, error count, and p50/p95/p99 publish latency.

- [ ] **Step 1: Add contract tests.**

  Validate every public problem response, bid `202` response, event envelope version, and required field using fixed JSON fixtures. A changed event field must fail the test until its version changes.

- [ ] **Step 2: Add the end-to-end flow.**

  Exercise registration, login, product creation, auction creation, WebSocket subscription, bid submission, accepted/rejected status, auction close, and settlement query. Assert the settlement contains exactly one ledger row for the auction.

- [ ] **Step 3: Add the load producer.**

  Produce commands keyed by `auction_id`, distribute traffic across configured auctions, use a bounded worker count, and fail fast when broker publish errors exceed one percent. Do not claim the 10k/50k target until the measured report is recorded.

- [ ] **Step 4: Add failure checks.**

  Restart one consumer during bid traffic, publish a duplicate command, stop PostgreSQL briefly, reconnect a WebSocket, and verify no duplicate settlement, no lost accepted bid, and eventual consumer recovery.

- [ ] **Step 5: Add CI.**

  Run formatting check, `go vet ./...`, unit tests, integration tests with PostgreSQL/Kafka services, migration check, and image build. Fail CI on race detector failures for domain and service packages.

- [ ] **Step 6: Document operational commands.**

  `docs/runbook.md` must document startup, topic inspection in Kafka UI, health checks, consumer lag diagnosis, retry/DLQ inspection, migration execution, graceful shutdown, and local secret replacement.

- [ ] **Step 7: Verify all gates and commit.**

  ```bash
  make lint
  make test
  make test-integration
  go test -race ./apps/... ./packages/...
  git add internal tests .github Makefile docs/runbook.md
  git commit -m "test: add auction reliability and CI gates"
  ```

## Plan Self-Review

- Spec coverage: service boundaries are covered by Tasks 3-8; Kafka ordering,
  idempotency, outbox, retry, and DLQ are covered by Tasks 2 and 6; settlement
  and notification are covered by Task 7; REST/WebSocket and smoke client are
  covered by Tasks 7-9; observability and health are covered by Tasks 2 and 8;
  integration, load, and failure checks are covered by Task 10.
- Scope check: payment, wallet, multi-region, admin UI, and full proxy bidding
  remain explicitly excluded.
- Placeholder scan: the plan contains no `TBD`, `TODO`, or unspecified
  implementation steps.
- Type consistency: `BidCommand`, `EventEnvelope`, `TokenPair`, `Principal`,
  `BidIngress`, `BidProcessor`, `AuctionCloser`, and settlement repository
  interfaces are introduced before their consumers.
- Deliberate ceiling: a single PostgreSQL instance and local Compose are for
  development only; production scaling requires separate databases, broker
  sizing, and a measured load-test result before rollout.
