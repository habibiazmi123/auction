# Frontend — Next.js Auction Platform

**Date:** 2026-08-05
**Status:** Approved

## Overview

Full production frontend for the Go auction microservices platform. Built with Next.js 15 (App Router), shadcn/ui, Tailwind CSS, and ReUI. Deep Neumorphism theme applied globally. Lives at `apps/frontend/` inside the existing monorepo.

## Architecture

- **Framework:** Next.js 15, App Router, React Server Components as default
- **Rendering:** Server components for data-fetching pages (auction listings, product catalog, settlements). Client components only where interactivity is required (bidding, WebSocket, auth forms)
- **Auth:** Server-side sessions via httpOnly cookies. Next.js middleware checks cookies on protected routes, auto-refreshes expired tokens, redirects to `/login` on failure
- **API Proxy:** Server components call the Go API Gateway directly via `lib/api.ts` — no BFF layer. Client components call the gateway through the same domain (no CORS)
- **WebSocket:** Singleton WebSocket manager in `lib/ws.ts`, consumed only inside `<AuctionRoom>` (a client component)
- **Styling:** Neumorphism theme via Tailwind plugin + CSS custom properties, applied to shadcn/ui base, inherited by ReUI components via `className`

## Directory Structure

```
apps/frontend/
├── src/
│   ├── app/
│   │   ├── layout.tsx                # Root layout, theme provider, fonts
│   │   ├── page.tsx                  # Landing / auction feed
│   │   ├── (auth)/
│   │   │   ├── layout.tsx            # Auth layout (no navbar)
│   │   │   ├── login/page.tsx
│   │   │   ├── register/page.tsx
│   │   │   └── logout/page.tsx
│   │   ├── (dashboard)/
│   │   │   ├── layout.tsx            # Dashboard layout (navbar + sidebar)
│   │   │   ├── auctions/
│   │   │   │   ├── page.tsx          # Server: auction list via data-grid
│   │   │   │   └── [id]/page.tsx     # Client: auction room
│   │   │   ├── products/
│   │   │   │   ├── page.tsx          # Server: product list
│   │   │   │   ├── new/page.tsx      # Client: create product form
│   │   │   │   └── [id]/page.tsx     # Server: product detail
│   │   │   ├── settlements/page.tsx  # Server: settlement history
│   │   │   └── notifications/page.tsx # Server: notification list
│   │   └── api/auth/                 # Auth proxy routes
│   │       ├── login/route.ts
│   │       ├── register/route.ts
│   │       ├── refresh/route.ts
│   │       └── logout/route.ts
│   ├── components/
│   │   ├── ui/                       # shadcn primitives
│   │   ├── reui/                     # ReUI data-grid, etc.
│   │   ├── layout/
│   │   │   ├── navbar.tsx
│   │   │   ├── sidebar.tsx
│   │   │   └── shell.tsx
│   │   ├── auction/
│   │   │   ├── bid-panel.tsx         # Client: bid input + submit
│   │   │   ├── auction-timer.tsx     # Client: live countdown
│   │   │   ├── bid-history.tsx       # Client: real-time bid list
│   │   │   └── live-indicator.tsx    # Client: WebSocket status
│   │   └── auth/
│   │       └── auth-form.tsx         # Client: login/register form
│   ├── lib/
│   │   ├── api.ts                    # Server-side fetch to Go gateway
│   │   ├── auth.ts                   # Cookie session helpers
│   │   ├── ws.ts                     # WebSocket client singleton
│   │   └── types.ts                  # Shared DTOs (mirrors Go contracts)
│   └── middleware.ts                 # Auth guard for (dashboard) routes
├── tailwind.config.ts
├── components.json
├── next.config.ts
├── package.json
└── tsconfig.json
```

## Data Flow

### Auth

```
[Login form] → POST /api/auth/login → Go gateway /v1/auth/login
→ {access_token, refresh_token}
→ Route handler sets httpOnly cookies:
  - access_token  (15 min, sameSite=strict, path=/)
  - refresh_token (7 days, sameSite=strict, path=/api/auth/refresh)
→ Redirect to /auctions

middleware.ts (runs on (dashboard) routes):
  1. Read access_token cookie
  2. If missing or expired → call /api/auth/refresh
  3. If refresh succeeds → set new cookies, continue
  4. If refresh fails → redirect /login
```

### Server Component Fetching

```
Server Component
  → lib/api.ts
  → reads cookies() from next/headers
  → fetches http://api-gateway:8080/v1/...  (Docker network, internal)
  → maps response to typed DTOs from lib/types.ts
  → renders with data
```

### Auction Room (Client Component)

```
<AuctionRoom auctionId={id}>
  Initial data: server-fetched auction details (current price, endsAt, etc.)
  Children:
    <AuctionTimer endsAt={endsAt}/>       — no API needed, client-side countdown
    <BidPanel auctionId={id}/>           — POST /v1/auctions/{id}/bids
    <BidHistory auctionId={id}/>         — WebSocket /v1/auctions/{id}/live
    <LiveIndicator connected={wsState}/> — WebSocket status display
```

### WebSocket

```
lib/ws.ts — singleton manager
  - connect(auctionId, token)
  - onBidPlaced(callback)
  - onAuctionClosed(callback)
  - disconnect()
  - auto-reconnect with exponential backoff (max 30s)
  - reconnects on auth token refresh (listens to cookie change events)
```

## Component Plan (ReUI)

| Page | ReUI Component | Notes |
|------|---------------|-------|
| `/auctions` | `data-grid` | Filters: status, price range. Columns: product name, current bid, time remaining, status |
| `/products` | `data-grid` | Filters: status. Columns: name, quantity, status, created |
| `/notifications` | `data-grid` | Columns: type, message, read status, timestamp |

No ReUI block covers the auction room (WebSocket + real-time bid panel is too custom). Build with shadcn primitives: Card, Button, Input, Badge.

## Neumorphism Theme

### Color Palette

| Token | Value | Usage |
|-------|-------|-------|
| `--bg` | `#e8ecf1` | Page background |
| `--surface` | `#e0e5ec` | Cards, panels, navbar |
| `--shadow-dark` | `#a3b1c6` | Outer shadow dark edge |
| `--shadow-light` | `#ffffff` | Outer shadow light edge |
| `--primary` | `#6c8cd9` | Buttons, links, active states |
| `--accent` | `#e07b5a` | Bids, alerts, CTAs |
| `--text` | `#3a3f47` | Body text |
| `--text-muted` | `#8b93a0` | Secondary text |

### Tailwind Plugin

Custom `neumorphic` plugin generates:

- `.nm-surface` — raised surface (outer shadow)
- `.nm-inset` — pressed/depressed surface (inner shadow)
- `.nm-btn` — interactive button with pressed state
- `.nm-input` — inset input field
- `.nm-btn-primary` — primary colored with soft press
- `.nm-card` — surface + rounded corners (18px radius)

### CSS Variable Override for shadcn/ui

```css
:root {
  --background: var(--bg);
  --foreground: var(--text);
  --card: var(--surface);
  --card-foreground: var(--text);
  --primary: var(--primary);
  --primary-foreground: #ffffff;
  --muted: var(--surface);
  --muted-foreground: var(--text-muted);
  --radius: 0.75rem;
  /* ... */
}
```

shadcn components use these variables, so they naturally inherit the neumorphic palette. Additional `.nm-surface` class applied to Card wrappers for the shadow effect.

### Key Elements

| Element | Class | Effect |
|---------|-------|--------|
| Page background | `bg-[var(--bg)]` | Solid warm grey |
| Cards | `nm-card` | Outer shadow, 18px radius |
| Navbar | `nm-surface`, fixed top | Full-width raised bar |
| Sidebar | `nm-surface` | Full-height raised panel |
| Buttons | `nm-btn nm-btn-primary` | Outer shadow → inset on active |
| Inputs | `nm-input` | Inset shadow, 12px radius |
| Modals | `nm-surface`, backdrop blur | Raised dialog |
| Active bid | `.nm-inset` + pulse animation | Pulsing inset/outset transition |

## Pages Detail

### `/` — Landing
- If authenticated: redirect to `/auctions`
- If not: hero section with neumorphic CTA buttons (Register / Login)

### `/login`, `/register`
- `<AuthForm>` client component
- Centered neumorphic card, inset inputs, raised button
- On success: redirect to `/auctions`
- On error: inline error message (inset card, accent border)

### `/auctions` — Auction List
- `<DataGrid>` from ReUI
- Server-fetched: `GET /v1/auctions` (see Backend Dependencies)
- Each row links to `/auctions/[id]`
- Status badges: `live` (accent pulsing), `scheduled` (muted), `closed` (gray)

### `/auctions/[id]` — Auction Room
- Server component shell renders auction metadata (product name, description, starting price)
- Client island: `<BidPanel>`, `<BidHistory>`, `<AuctionTimer>`, `<LiveIndicator>`
- Bid history: scrolling list, newest at top, current user's bids highlighted
- Bid panel: inset input for bid amount, raised submit button
- Timer: large display, flash on <60s remaining
- On auction closed: disable bid panel, show winner announcement

### `/products` — Product Catalog
- ReUI `<DataGrid>`, server-fetched
- Seller sees "New Product" button
- Each row links to detail

### `/products/new` — Create Product
- Client form: neumorphic inputs + submit
- POST `/v1/products`
- On success: redirect to `/products`

### `/products/[id]` — Product Detail
- Server-fetched: product info
- If seller owns product: "Create Auction" button → prefills auction form

### `/settlements` — Settlement History
- ReUI `<DataGrid>`, server-fetched
- Shows completed transactions

### `/notifications` — Notifications
- ReUI `<DataGrid>`, server-fetched
- Read/unread state with badge

## Error Handling

- Server component fetch errors: render error card with retry button
- Client-side API errors: toast notification (shadcn Sonner)
- WebSocket disconnect: `<LiveIndicator>` shows disconnected state, auto-reconnect
- Bid rejection: inline error in `<BidPanel>` (e.g. "bid too low", "auction ended")
- 401 on server: middleware catches → redirect /login
- 401 on client (stale token): auto-refresh via cookie → retry original request

## Testing

- Unit: Vitest for utility functions (api.ts, auth.ts)
- Component: React Testing Library for client components (BidPanel, AuthForm)
- Integration: Playwright for critical flows (login → browse → bid)
- WebSocket: mock WebSocket server in Playwright tests

## Docker / Deployment

- Add `frontend` service to `docker-compose.yml`
- `next start` on port 3000
- API Gateway URL configured via `API_GATEWAY_URL=http://api-gateway:8080` (Docker network)
- Multi-stage Dockerfile: build with `npm run build`, serve with `next start`

## Backend Dependencies (Missing Endpoints)

The following API endpoints do not exist yet and must be added to the Go services before the corresponding pages can render real data. Until added, these pages use mock data as stopgaps.

| Endpoint | Service | Needed For | Priority |
|----------|---------|-----------|----------|
| `GET /v1/auctions` | auction-service (via gateway) | `/auctions` list page | High |
| `GET /v1/settlements` | transaction-service (via gateway) | `/settlements` list page | Low |

All other pages use existing endpoints exclusively.

## Out of Scope

- Admin panel / admin role
- User profile management (edit email, change password)
- Product image uploads
- i18n
- PWA / offline support
