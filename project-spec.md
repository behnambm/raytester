# Xray Subscription Tester

Version: 2.0

---

# 1. Project Overview

A Go CLI binary that:

1. Downloads a subscription URL.
2. Detects Base64 subscriptions automatically.
3. Parses supported proxy protocols (VLESS, VMess, SS).
4. Deduplicates entries.
5. Tests connectivity via a real Xray process.
6. Measures end-to-end request latency.
7. Performs Geo-IP lookup to determine proxy country.
8. Filters results by latency threshold.
9. Sorts successful configurations by latency.
10. Prints working configurations to stdout.

---

# 2. Architecture

The project uses a `core/` + `cli/` architecture:

- `core/` — Reusable proxy testing library (importable by any consumer).
- `cli/` — One consumer of `core` that handles I/O (subscription download, parsing, logging).

This separation allows future consumers (web API, library caller) to import `core` directly.

---

# 3. Project Structure

```text
core/
├── interfaces.go      # Config, TestResult, Hooks, constants
├── tester.go          # Tester.Run(), worker pool orchestration
├── xray/
│   └── xray.go        # XrayInstance, config generation, process management
└── probe/
    └── probe.go       # Probe.Test() for latency, Probe.GeoLookup() for country

cli/
├── main.go            # CLI entry point, flag parsing, hooks setup
├── subscription/
│   └── download.go    # HTTP subscription download
├── parser/
│   └── parse.go       # Protocol detection and parsing orchestration
├── protocols/
│   ├── vless.go       # VLESS protocol parser
│   ├── vmess.go       # VMess protocol parser
│   └── ss.go          # Shadowsocks protocol parser
├── dedupe/
│   └── dedupe.go      # Semantic deduplication via SHA-256
└── logger/
    └── logger.go      # Info/Error loggers (stderr)
```

---

# 4. Core Library API

## 4.1 Types (core/interfaces.go)

```go
type ProxyConfig = xray.ProxyConfig

type Config struct {
    MaxLatency time.Duration
    Workers    int
    XrayPath   string
}

type TestResult struct {
    Config      ProxyConfig
    Latency     time.Duration
    Country     string        // 2-letter ISO code (e.g., "US")
    CountryName string        // Full name (e.g., "United States")
    Error       error
}

type Progress struct {
    Done  int
    Total int
}

type Hooks struct {
    OnTestStart    func(config ProxyConfig)
    OnTestComplete func(result TestResult)
    OnProgress     func(p Progress)
    OnComplete     func(results []TestResult)
}
```

## 4.2 Constants (core/interfaces.go)

```go
DefaultMaxLatency = 500ms
DefaultWorkers    = 20
DefaultXrayPath   = "xray"
MaxBodySize       = 20 MB
MaxConfigs        = 10,000
ReadinessTimeout  = 5s
```

## 4.3 Tester (core/tester.go)

```go
func NewTester(cfg Config, hooks Hooks) *Tester
func (t *Tester) Run(ctx context.Context, configs []ProxyConfig) []TestResult
```

`Run` manages a worker pool, processes all configs, collects results, sorts by latency (successful first), and invokes hooks.

## 4.4 Probe (core/probe/probe.go)

```go
func (p *Probe) Test(port int) (time.Duration, error)       // Latency test
func (p *Probe) GeoLookup(port int) (GeoResult, error)      // Geo-IP lookup
```

## 4.5 XrayInstance (core/xray/xray.go)

```go
func NewInstance(workerID int) *XrayInstance
func (xi *XrayInstance) WriteConfig(pc ProxyConfig) error
func (xi *XrayInstance) Start(xrayPath string) error
func (xi *XrayInstance) Stop() error
func (xi *XrayInstance) WaitReady(timeout time.Duration) bool
func (xi *XrayInstance) Cleanup()
```

---

# 5. ProxyConfig Fields

```go
type ProxyConfig struct {
    Protocol string   // "vless", "vmess", "ss"
    Address  string
    Port     int
    UUID     string
    AlterID  int
    Security string
    Network  string   // "tcp", "ws", "http", "grpc", "httpupgrade"
    Path     string
    Host     string
    TLS      bool
    SNI      string
    Method   string   // SS encryption method
    Password string   // SS password
    Raw      string   // Original config string (preserved)
}
```

---

# 6. CLI Usage

```bash
raytest \
  --url https://example.com/sub.txt \
  --max-latency 500ms \
  --workers 20 \
  --xray-path xray
```

## Arguments

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--url` | Yes | — | Subscription URL |
| `--max-latency` | No | 500ms | Maximum allowed latency |
| `--workers` | No | 20 | Worker count |
| `--xray-path` | No | xray | Path to xray binary |

---

# 7. High-Level Flow

```text
Download subscription
  ↓
Base64 detection
  ↓
Parse protocols (VLESS, VMess, SS)
  ↓
Deduplicate (SHA-256 fingerprint, ignore fragment)
  ↓
Limit to 10,000 configs
  ↓
Worker pool (N workers, each with own Xray instance)
  ↓
For each config:
  1. Generate Xray JSON config
  2. Start Xray process
  3. Wait for SOCKS port readiness (5s timeout)
  4. Latency test via proxy → http://www.gstatic.com/generate_204
  5. If latency <= threshold: Geo-IP lookup via proxy → https://get.geojs.io/v1/ip/country.json
  6. Stop Xray process
  ↓
Sort results (successful first, then by latency ascending)
  ↓
Print working configs to stdout
```

---

# 8. Probe Design

## 8.1 Latency Test

- Endpoint: `http://www.gstatic.com/generate_204`
- Expected: HTTP 204
- Measurement: Full request duration (TCP + TLS + headers + body)
- Timeout: 10s

## 8.2 Geo-IP Lookup

- Endpoint: `https://get.geojs.io/v1/ip/country.json`
- Response format: `{"country":"US","name":"United States"}`
- Performed only after successful latency test
- Optional: failures result in empty country fields (non-fatal)
- Proxy must be alive during lookup (Stop() called after)

---

# 9. Worker Architecture

Each worker:

```text
Worker ID → Port (30000 + ID)
         → Config file (/tmp/xray-subscription-tester/worker-{ID}.json)
         → Xray process
```

Workers are independent. Each processes configs from a shared job queue.

---

# 10. Xray Configuration

One SOCKS inbound (no auth, UDP enabled).

One outbound per protocol:

- VLESS: vnext with stream settings (network, TLS, reality)
- VMess: vnext with stream settings (network, TLS)
- SS: server with method and password

---

# 11. Output Format

## stdout

Only working config lines (designed for piping):

```bash
raytest --url ... > working.txt
```

## stderr

Human-readable logs:

```
[INFO] Downloading subscription from ...
[INFO] Parsed 5056 configs
[INFO] After dedupe: 3193 configs
[INFO] Working: ss://... (581ms) [US - United States]
[INFO] Done. 8/3193 working configs
```

---

# 12. Sorting

Results sorted ascending by latency.

Successful configs appear before failed ones.

Example:

```text
319ms → first
428ms
503ms
548ms
581ms
656ms
766ms
1.6s → last
```

---

# 13. Error Handling

Never fail entire run on:

- Bad config
- Bad worker
- Bad subscription entry
- Geo-IP lookup failure

Always continue. Best-effort processing.

---

# 14. Supported Protocols

| Protocol | Status |
|----------|--------|
| VLESS | Supported |
| VMess | Supported |
| Shadowsocks (SS) | Supported |
| Trojan | Ignored |
| Hysteria | Ignored |
| Hysteria2 | Ignored |
| TUIC | Ignored |

---

# 15. Dependencies

- `github.com/schollz/progressbar/v3` — Progress bar
- External: `xray` binary (resolved from PATH or `--xray-path`)

---

# 16. Build & Run

```bash
go build -o raytest cli/main.go
./raytest --url https://example.com/sub.txt
```

Or:

```bash
go run cli/main.go --url https://example.com/sub.txt
```

---

# 17. Definition of Done

1. Builds on macOS.
2. Supports VLESS, VMess, SS.
3. Detects Base64 subscriptions.
4. Deduplicates entries.
5. Limits to 10,000 configs.
6. Uses external Xray binary.
7. Uses worker pool.
8. Measures real latency.
9. Performs Geo-IP lookup (country code + name).
10. Filters by latency threshold.
11. Sorts by latency.
12. Prints working configs with country info.
13. Cleans temporary files.
14. Cleans child processes.
15. Graceful shutdown on SIGINT/SIGTERM.
