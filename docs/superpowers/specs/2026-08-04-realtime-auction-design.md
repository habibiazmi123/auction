# Realtime Auction Backend Design

Date: 2026-08-04
Status: Approved for implementation planning

## Goal

Build a Go monorepo containing a production-oriented realtime auction backend
and a small frontend smoke client. The first backend slice must support user
authentication, product ownership, auction creation, asynchronous bid
commands, realtime bid status, auction closing, and settlement ledger creation.

The initial capacity target is 10,000 bid commands per second with bursts up
to 50,000 commands per second per cluster. Ordering is required per auction,
not globally.

## Decisions

- Architecture: hybrid event-driven microservices.
- Services: API gateway, user, product, auction, transaction, notification.
- Language: Go.
- Messaging: Kafka in KRaft mode.
- Database: PostgreSQL, with logical database ownership per service.
- Public API: versioned REST.
- Realtime delivery: WebSocket.
- Authentication: JWT access and refresh tokens.
- Auction: English ascending in the first slice; proxy bidding is a later
  extension with a reserved model boundary.
- End time: fixed by default; anti-sniping is configurable per auction.
- Transaction scope: settlement ledger only. Wallets and payment providers are
  explicitly out of scope.
- Local infrastructure: Docker Compose using existing images, PostgreSQL,
  Kafka, and Kafka UI.
- Frontend scope: login, product/auction listing, auction detail, bid command,
  and WebSocket status stream.

## Monorepo Layout

```text
auction/
  apps/
    api-gateway/
    user-service/
    product-service/
    auction-service/
    transaction-service/
    notification-service/
    smoke-client/
  packages/
    contracts/
    config/
    observability/
    auth/
    postgres/
    kafka/
  migrations/
    user/
    product/
    auction/
    transaction/
  deployments/
    docker-compose.yml
    kafka/
    postgres/
    kafka-ui/
  docs/
  Makefile
  go.work
```

`packages/contracts` contains transport types and versioned event envelopes,
not shared domain logic. Each service owns its domain, application use cases,
adapters, and entrypoint. Local Compose may use one PostgreSQL instance with
separate databases; no service may read another service's tables.

## Service Responsibilities

### API Gateway

- Expose public REST and WebSocket endpoints.
- Verify JWTs and attach user identity to requests.
- Enforce request size, pagination, subscription, and rate limits.
- Publish authenticated bid commands to Kafka.
- Maintain correlation and request IDs.
- Fan out public events to WebSocket subscribers.

### User Service

- Register, login, refresh, logout, and user profile operations.
- Store password hashes, refresh-token hashes, expiry, and revocation state.
- Issue short-lived access tokens and rotating refresh tokens.

### Product Service

- Manage seller-owned products.
- Validate product ownership.
- Prevent unsafe product changes while a related auction is live.

### Auction Service

- Manage auction lifecycle and bid acceptance.
- Process commands in order per `auction_id`.
- Apply increment, seller exclusion, time-window, and anti-sniping rules.
- Persist accepted/rejected bid state and outbox events.
- Close auctions exactly once.
- Leave a model boundary for future proxy bids.

### Transaction Service

- Consume auction close events.
- Create an idempotent settlement ledger containing winner, final price,
  seller obligation, and buyer obligation.
- Expose settlement status queries.
- Do not move money in this phase.

### Notification Service

- Consume public domain events.
- Persist user notifications.
- Publish notification events for gateway fanout.

## Event Flow

```text
Client
  -> API Gateway: JWT and Idempotency-Key
  -> Kafka: auction.bid.commands.v1, key = auction_id
  -> Auction Service consumer group
  -> PostgreSQL: bid + auction state + outbox in one transaction
  -> Outbox relay
  -> Kafka: auction.bid.accepted.v1 or rejected.v1 or closed.v1
  -> Notification Service: notification record + notification.created.v1
  -> WebSocket Gateway: fanout to auction subscribers
```

The bid endpoint returns `202 Accepted` with a `bid_id` and `pending` status.
The client receives the final `accepted` or `rejected` result through the
WebSocket and can query bid status.

Kafka topics initially include:

```text
auction.bid.commands.v1
auction.events.v1
product.events.v1
user.events.v1
transaction.events.v1
notification.events.v1
<topic>.retry.v1
<topic>.dlq.v1
```

Every command and event is keyed by `auction_id` when auction ordering is
required. The common envelope contains `event_id`, `event_type`, `version`,
`occurred_at`, `producer`, `correlation_id`, `causation_id`, and payload.

Delivery is at least once. Consumers are idempotent by `event_id` or the
domain key appropriate to the operation. Failed messages retry and then move
to a DLQ; offsets are committed only after side effects succeed.

## Auction Consistency

Auction data includes:

- `auctions`: lifecycle, product/seller identity, prices, winner, timestamps,
  anti-sniping settings, and optimistic version.
- `bids`: append-only bid records with command identity and result metadata.
- `proxy_bids`: reserved for the later proxy implementation.
- `outbox_events`: events pending publication.

The ordered command processor validates that the auction is live, the bidder
is not the seller, the amount meets the increment, and the command is not a
duplicate. It then writes the bid, updates the current winner and version,
extends `ends_at` when anti-sniping applies, and writes the outbox event in one
PostgreSQL transaction.

A scheduler claims due auctions with row locking. Closing also runs through a
transaction and is protected by a unique close/result constraint, so retries
cannot close or settle an auction twice.

Strong consistency applies within one auction for current price, winner, bid
acceptance, extension, and close. Settlement, notification, and product
projections are eventually consistent through Kafka.

## Public API

```text
POST /v1/auth/register
POST /v1/auth/login
POST /v1/auth/refresh
POST /v1/auth/logout

POST /v1/products
GET  /v1/products
GET  /v1/products/{product_id}

POST /v1/auctions
GET  /v1/auctions
GET  /v1/auctions/{auction_id}
POST /v1/auctions/{auction_id}/bids
GET  /v1/bids/{bid_id}

GET  /v1/settlements/{settlement_id}
GET  /v1/notifications

WS   /v1/ws?auction_id={auction_id}
```

Bid commands require `Authorization: Bearer <JWT>` and `Idempotency-Key`.
`202` means the command entered Kafka, not that the bid won. Bid status is
`pending`, `accepted`, or `rejected` with a stable rejection code.

Errors use `application/problem+json` with `code`, `message`, `request_id`,
and optional `details`. Expected statuses include `401`, `403`, `409`, `429`,
and `503`.

Minimum roles are `buyer`, `seller`, and `admin`. Admin overrides do not bypass
bid ordering in the first phase.

WebSocket handshakes validate JWTs, enforce subscription limits, use heartbeat
and reconnect semantics, and never accept auction decisions from clients.

## Operations and Testing

Each service exposes live and ready health checks. Readiness verifies its
PostgreSQL and Kafka dependencies. Logs are structured JSON and include
request, correlation, and causation IDs.

Metrics must cover consumer lag, command latency, accepted/rejected counts,
outbox backlog, retry count, DLQ count, database contention, and WebSocket
connections. OpenTelemetry interfaces are used without selecting a vendor in
the first phase.

Required tests:

- Unit tests for bid rules, increments, anti-sniping, close races, JWT claims,
  and settlement idempotency.
- Integration tests for migrations, PostgreSQL transactions, Kafka delivery,
  outbox relay, retries, and DLQ behavior.
- Contract tests for REST errors/statuses and versioned event envelopes.
- End-to-end test from registration through product, auction, bid, WebSocket,
  close, and settlement.
- Load tests at 10,000 commands per second and stress at 50,000 commands per
  second, measuring latency, lag, contention, duplicates, and loss.
- Failure tests for consumer restart, rebalance, duplicate delivery, database
  restart, and WebSocket reconnect.

## Explicitly Out of Scope

- Payment gateway integration.
- Wallet or funds reservation.
- Full proxy auto-bidding implementation.
- Admin UI.
- Multi-region deployment.
- Global event ordering.
- Production frontend beyond the smoke client.
