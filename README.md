# Ray Tester

Proxy subscription testing tool. Downloads a subscription URL → parses VLESS/VMess/SS configs → deduplicates → tests each via a real Xray process → measures latency and Geo-IP → outputs working configs.

Two interfaces: **CLI** binary for one-shot runs, **API server** with a web UI for manual and scheduled testing.

## Requirements

- **Go** 1.26+
- **Xray** binary available at runtime (from `PATH` or `--xray-path`)

## Quick Start

```bash
# CLI — one-shot test
go run cli/main.go --url https://your-subscription-url

# API server — web UI at http://localhost:4433
make run
```

## CLI

```bash
go build -o raytest cli/main.go

./raytest \
  --url https://example.com/sub \
  --max-latency 500ms \
  --workers 20 \
  --xray-path /usr/local/bin/xray
```

| Flag | Default | Description |
|------|---------|-------------|
| `--url` | *required* | Subscription URL |
| `--max-latency` | `500ms` | Maximum allowed proxy latency |
| `--workers` | `20` | Number of parallel workers |
| `--xray-path` | `xray` | Path to xray binary |

Working configs are printed to **stdout** (pipeable). Logs go to **stderr**.

```bash
./raytest --url https://... > working-configs.txt
```

## API Server

```bash
make run                          # default http://localhost:4433
make run ADDR=:8080               # custom port
make run ADDR=:3000 FRONTEND=./dist
```

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | `:4433` | HTTP listen address |
| `--frontend` | `frontend` | Static files directory |
| `--data-dir` | `~/.raytester/scheduler` | Scheduler storage path |
| `--max-latency` | `500ms` | Default max latency |
| `--workers` | `20` | Default worker count |
| `--xray-path` | `xray` | Path to xray binary |

Data dir can also be set via `RAYTESTER_DATA_DIR` env var.

### Web UI

Open `http://localhost:4433` in a browser.

**Manual Test tab** — enter a subscription URL, configure latency/workers, start a test. Results stream in real-time via WebSocket with filtering by protocol, country, and sort options. Copy configs or generate QR codes.

**Scheduled Tasks tab** — create recurring tests with cron expressions. Quick presets: 30min, 1h, 3h, 6h, 12h, daily. Each task tracks metrics (total runs, success/fail counts, last run, avg duration). View results in a modal with the same table format.

### API Endpoints

#### Manual Tests

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/test` | Start a new test |
| `GET` | `/api/tests` | List all sessions |
| `GET` | `/api/test/{id}/stats` | Session statistics |
| `GET` | `/api/test/{id}/results` | Working configs |
| `POST` | `/api/test/{id}/stop` | Stop a running test |
| `DELETE` | `/api/test/{id}` | Delete a session |
| `GET` | `/api/config` | Server default config |
| `GET` | `/api/ws?id={id}` | WebSocket (real-time updates) |

#### Scheduled Tasks

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/scheduler/tasks` | Create task |
| `GET` | `/api/scheduler/tasks` | List all tasks |
| `GET` | `/api/scheduler/tasks/{id}` | Task details |
| `PUT` | `/api/scheduler/tasks/{id}` | Update task |
| `DELETE` | `/api/scheduler/tasks/{id}` | Delete task |
| `POST` | `/api/scheduler/tasks/{id}/run` | Trigger immediate run |
| `GET` | `/api/scheduler/tasks/{id}/results` | Latest results |
| `GET` | `/api/scheduler/tasks/{id}/metrics` | Run metrics |

## Project Structure

```
raytester/
├── cli/                  # CLI entry point
│   ├── main.go
│   ├── subscription/     # URL download + base64 decode
│   ├── parser/           # Config line parser
│   ├── protocols/        # VLESS / VMess / SS parsers
│   ├── dedupe/           # SHA-256 fingerprint dedup
│   └── logger/           # Stderr loggers
├── core/                 # Shared library
│   ├── interfaces.go     # Types, constants, hooks
│   ├── tester.go         # Worker-pool orchestrator
│   ├── xray/             # Xray process management
│   └── probe/            # SOCKS5 latency + geo probes
├── api/                  # REST + WebSocket server
│   ├── server.go         # HTTP server, session management
│   ├── handlers.go       # Manual test endpoints
│   ├── scheduler_handlers.go  # Scheduled task endpoints
│   ├── types.go          # Request/response types
│   ├── websocket.go      # WebSocket hub + client
│   ├── scheduler/        # Cron-based task scheduler
│   └── storage/          # Persistence (file, future DB/S3)
├── cmd/api/              # API server entry point
├── frontend/             # Web UI (Alpine.js + Tailwind)
└── Makefile
```

## Architecture

- **`core/`** is the library layer — never imports `cli/` or `api/`
- Worker ports: `30000 + workerID` (default 20 workers = ports 30001–30020)
- Xray temp configs: `/tmp/xray-subscription-tester/worker-{ID}.json`
- Best-effort error handling throughout — bad configs never crash the run
- Results sorted: successes first, then by latency ascending

## Scheduled Tasks

Tasks are stored as JSON files in `~/.raytester/scheduler/` (configurable via `--data-dir`). Each task has:

- **metadata**: name, URL, cron expression, config overrides
- **results**: latest run's working configs (overwritten each run)
- **metrics**: total runs, success/failure counts, last/avg duration, result count

The `Storage` interface is pluggable — swap the file implementation for a database or S3 backend without touching the scheduler.

Each task runs in total isolation: separate goroutine, `recover()` from panics, 10-minute context timeout. A failing task never affects others or the scheduler itself.

## Build

```bash
make build    # both binaries
make clean    # remove binaries
go build -o raytest cli/main.go
go build -o raytester-api cmd/api/main.go
```
