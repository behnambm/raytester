# AGENTS.md — Ray Tester

Go module: `raytest` (Go 1.26.4)

## What is this?

A proxy subscription testing tool. Downloads a subscription URL → parses VLESS/VMess/SS configs → deduplicates → tests each via a real Xray process → measures latency and Geo-IP → outputs working configs.

## Project Structure

| Directory | Purpose |
|-----------|---------|
| `core/` | Reusable proxy testing library (`interfaces.go`, `tester.go`, `xray/`, `probe/`) |
| `cli/` | CLI consumer of `core` (main.go + parsing, subscription, dedupe, protocols, logger) |
| `api/` | REST + WebSocket server wrapping `core` — multi-session (handlers, types, websocket hub, sessions map) |
| `cmd/api/` | Entry point for the API server binary |
| `frontend/` | Web UI for the API server (single-page: Alpine.js + Tailwind + QRCode) |

Two entry points:
- **CLI**: `cli/main.go` → `go build -o raytest cli/main.go`
- **API server**: `cmd/api/main.go` → `go build -o raytester-api cmd/api/main.go` (default port `:4433`)

## Build & Run

```bash
# CLI
go build -o raytest cli/main.go
./raytest --url <subscription-url>

# API server (serves frontend/ as static files)
go run cmd/api/main.go --addr :4433 --frontend frontend

```

No Makefile or test suite exists.

## Architecture Rules

- **`core/` must not import `cli/` or `api/`** — it's the library layer.
- `api/` depends on `core/` + `cli/subscription`, `cli/parser`, `cli/dedupe`.
- `ProxyConfig` is a type alias for `xray.ProxyConfig` defined in `core/interfaces.go`.
- The `core.Tester` is the central orchestrator; all consumers (CLI, API) use it.

## Key Conventions

- **Best-effort error handling**: never fail the entire run on a bad config, bad worker, or failed geo lookup. Always continue.
- **Worker ports**: each worker gets port `30000 + workerID`.
- **Xray temp configs**: `/tmp/xray-subscription-tester/worker-{ID}.json` — must be cleaned up.
- **Sorting**: successful results first, then by latency ascending.
- **Output separation**: stdout = machine-readable (working config lines, pipeable); stderr = human-readable logs.
- **Tests**: there are no automated tests (`go test ./...` will find nothing).

## External Dependency

An `xray` binary must be available at runtime (resolved from `PATH` or `--xray-path`). This project does NOT bundle Xray — it shell-execs it per worker.

## Known Gotchas

- **Port conflicts**: the hardcoded `30000 + workerID` port range can conflict with other services. Default worker count is 20, meaning ports 30001–30020.
- **Xray cleanup**: `XrayInstance.Cleanup()` removes temp config files; `Stop()` kills the Xray process. Both must be called, or stale processes and files accumulate.
- **WebSocket reconnect**: the web frontend auto-reconnects on disconnect (3s delay).
- **CORS**: API server allows all origins (`*`).
- **Max configs**: hard limit of 10,000 configs tested per run.

## Docs

- `project-spec.md` — full specification and API reference. Read before making significant changes to `core/`.
