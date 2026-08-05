# Auction Smoke Runbook

This runbook describes how to start the local realtime-auction stack and use the browser smoke client to verify the public API and WebSocket event flow.

## Prerequisites

- Docker and Docker Compose
- Go 1.22+ (for migrations)
- A copy of `.env` in the repository root

## Start the stack

```bash
cp .env.example .env   # only needed once
make up
make topics
make migrate
```

`make up` starts all services defined in `deployments/docker-compose.yml`:
Postgres, Kafka, Kafka UI, API gateway, user, product, auction, transaction, and notification services.

## Open the smoke client

The API gateway serves the static smoke client at the root path:

```
http://localhost:8080/
```

Open it in two separate browser tabs to simulate multiple bidders.

## Verify an end-to-end auction

1. **Register a seller** in the first tab.
   - Email: `seller@example.com`, role: `seller`, password: any.
   - Click **Register**, then **Login**.

2. **Create a product**
   - Fill in name, description, and quantity.
   - Click **Create product**. The product ID appears in the Seller setup field.

3. **Create an auction**
   - Use the product ID from the previous step.
   - Set a start price, minimum increment, and an end time a few minutes in the future.
   - Click **Create auction**. The auction ID appears in the Buyer / live auction field.

4. **Register two buyers** in separate tabs.
   - In each tab, register and log in as `buyer` (e.g. `buyer1@example.com`, `buyer2@example.com`).
   - Paste the same auction ID into both tabs.
   - Click **Connect WebSocket** in both tabs.

5. **Submit bids**
   - In each tab, enter a bid amount that satisfies the minimum increment and click **Submit bid**.
   - The bid is returned as `202 pending` and its status updates when the `auction.bid.placed.v1` event arrives over the WebSocket.

6. **Close the auction**
   - Wait until the auction `ends_at` time passes. The auction service's closer marks it closed and emits `auction.closed.v1`.
   - Both tabs should display the closed event with the winner and final price.

7. **Confirm isolation**
   - Each tab should only receive events for the auction ID it subscribed to.
   - If you connect a tab to a different auction ID, it must not receive events from the first auction.

## Stop the stack

```bash
make down
```

## Troubleshooting

- **WebSocket refuses to connect**: ensure `ALLOWED_ORIGINS` includes the origin you are using (`http://localhost:8080` by default). The browser cannot set the `Authorization` header on a WebSocket handshake, so the smoke client sends the access token in the `access_token` query parameter. This is a smoke-client-only local workaround; do not pass access tokens in URLs in production.
- **401 errors**: log in again. The client automatically refreshes expired access tokens when the refresh token is still valid.
- **Pending bids never update**: if the WebSocket is disconnected, the client polls `GET /v1/bids/{bid_id}` every two seconds until the WebSocket reconnects or the bid status changes.
