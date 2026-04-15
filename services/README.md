# OptiPilot Sample Services

Three lightweight Go HTTP services that simulate an e-commerce backend. They exist so OptiPilot has something realistic to scrape, forecast, and autoscale.

Each service is stdlib-only (no external Go deps), self-contained, and exposes a uniform `/metrics` shape so the controller can ingest all three through one code path.

## Services

| Service | Port | Role | Base latency | Load tiers |
|---|---|---|---|---|
| `api-gateway` | 8081 | Product catalog + search | 5–15ms | >200 conn → 50–200ms; >500 → 300–800ms |
| `order-service` | 8082 | Order create/read (DB-like) | 10–30ms | >150 → 80–250ms; >400 → 400–1200ms; 2% slow-query bump |
| `payment-service` | 8083 | Payment processing | 20–60ms | >100 → 100–400ms; >300 → 500–1500ms; 5% gateway 504 (2s) |

### Endpoints per service

**api-gateway**
- `GET /api/v1/products`
- `GET /api/v1/products/{id}`
- `GET /api/v1/search?q=<term>`

**order-service**
- `POST /api/v1/orders`
- `GET /api/v1/orders`
- `GET /api/v1/orders/{id}`

**payment-service**
- `POST /api/v1/payments/process`
- `GET /api/v1/payments/{id}/status`

**Shared on all three**
- `GET /health` — always 200 with service name
- `GET /ready` — 503 when active connections exceed 90% of `maxConnections` (1000)
- `GET /metrics` — JSON snapshot (see below)

### `/metrics` JSON

```json
{
  "service_name": "api-gateway",
  "rps": 145.2,
  "avg_latency_ms": 23.5,
  "p99_latency_ms": 187.3,
  "active_connections": 42,
  "cpu_usage_percent": 35.2,
  "memory_usage_mb": 128.4,
  "error_rate": 0.02,
  "total_requests": 584210,
  "uptime_seconds": 3600
}
```

- `rps`: requests in the last 60s, averaged per second, computed from 60 × 1-second buckets.
- `avg_latency_ms` / `p99_latency_ms`: from a ring buffer of the last 1000 request latencies.
- `active_connections`: atomic counter, incremented/decremented by the handler wrapper.
- `cpu_usage_percent`: simulated as `10 + (active / maxConnections) * 80`, capped at 100.
- `memory_usage_mb`: simulated as `64 + (total_requests / 10000) * 10`, capped at 512.
- `error_rate`: 5xx responses in the last 60s / total requests in the last 60s.

## Run locally (no Docker)

```bash
# Terminal 1
cd services/api-gateway && go run .

# Terminal 2
cd services/order-service && go run .

# Terminal 3
cd services/payment-service && go run .
```

Override the port with `PORT=9000 go run .`.

Smoke test:

```bash
curl -s localhost:8081/api/v1/products | jq
curl -s localhost:8081/metrics | jq
curl -s -XPOST localhost:8082/api/v1/orders \
  -H 'content-type: application/json' \
  -d '{"customer_id":"cust-1","items":[{"product_id":"p-001","quantity":2,"unit_price":249.99}]}' | jq
curl -s -XPOST localhost:8083/api/v1/payments/process \
  -H 'content-type: application/json' \
  -d '{"order_id":"abc","amount":499.98,"currency":"USD","method":"card"}' | jq
```

## Run with Docker Compose

From the repo root:

```bash
docker compose up --build        # foreground
docker compose up -d --build     # detached
docker compose logs -f api-gateway
docker compose down
```

All three services come up on `localhost:8081` / `:8082` / `:8083`.

## Build individual images

```bash
docker build -t optipilot/api-gateway:dev    services/api-gateway
docker build -t optipilot/order-service:dev  services/order-service
docker build -t optipilot/payment-service:dev services/payment-service
```

Images are multi-stage (`golang:1.23-alpine` → `alpine:3.19`) with `-ldflags="-s -w" -trimpath`. Expect ~13–15MB final size.

## Notes for the OptiPilot controller

- Scrape `/metrics` every 15s and feed it into `IngestMetrics` as a batched `ServiceMetric`.
- The 60s RPS window aligns with the ML service's prediction window — scrape cadence slower than 60s will under-sample the window.
- `/ready` flipping to 503 is the signal that the pod is saturated; use it as a hard trigger for reactive scaling while the predictive model catches up.
