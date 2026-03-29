# App Package — CLAUDE.md

Service wiring and lifecycle management for Document Processing.

## Main Components

- **app.go** — `App` struct: wires all components (infra clients, engines, application orchestrators, ingress, egress) in `New(ctx, cfg)`. Manages startup (`Run()`) and graceful shutdown (`Shutdown()`) with ordered teardown.
- **dmhandler.go** — `DMResponseHandler`: composite handler that dispatches DM responses (artifact notifications) to appropriate awaiter (e.g., DMConfirmationAwaiter for processing/comparison pipeline confirmations).

## Lifecycle

**Startup (New + Run):**
1. Initialize all infrastructure clients (broker, KV, object storage, OCR, observability)
2. Create domain engines (validator, text extraction, structure, semantic tree, comparison)
3. Create application orchestrators (processing pipeline, comparison pipeline, lifecycle manager)
4. Start broker consumer subscriptions (command handling + DM response handling)
5. Start HTTP health server (port 8080: `/healthz`, `/readyz`) and metrics server (port 9090: `/metrics`)
6. Block until context cancelled

**Shutdown (triggered by signal or context cancel):**
1. Stop readiness probe (`/readyz` → not ready)
2. Close broker connection (drains in-flight handlers)
3. Stop HTTP servers (health + metrics)
4. Close KV store
5. Flush observability (traces, logs)

## Entry Point

`cmd/dp-worker/main.go` creates App via `app.New()` and calls `app.Run()` with signal context.

## HTTP Endpoints

- **Health (port 8080):** `/healthz` (liveness), `/readyz` (readiness)
- **Metrics (port 9090):** `/metrics` (Prometheus format)
