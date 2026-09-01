# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

hperf is a network performance testing tool for active measurements of maximum achievable bandwidth and latency between N peers in large infrastructure. It's written in Go and developed by MinIO.

## Build and Development Commands

### Build
```bash
go build -o hperf ./cmd/hperf
```

### Install from source
```bash
go install github.com/minio/hperf/cmd/hperf@latest
```

### Run tests
```bash
go test -race ./...
```

CI runs the suite with `-race`, and runs the concurrency tests repeatedly at
`GOMAXPROCS=1` and `4`, because the locking bugs in this codebase pass a single
clean run and fail the tenth.

### Lint
```bash
golangci-lint run
```

`.golangci.yml` uses the v2 schema. It was previously pinned to golangci-lint
1.20.0 and enabled linters that no longer exist, so this command failed outright
and the gate never ran - if it starts erroring on a config version again, that is
what happened.

### Build Docker image
```bash
docker build -t hperf:latest .
```

## Architecture

### Core Components

**Binary Modes**: The hperf binary operates in two modes:
- **Server mode** (`server` command): Runs an HTTP/WebSocket API on configured address, accepts test commands from clients, performs tests with other servers, and saves results to disk
- **Client mode** (all other commands): Orchestrates servers by sending commands via WebSocket, receives incremental stats updates, and can detach/reattach to running tests

**Three-package structure**:
- `cmd/hperf/`: CLI commands and main entry point. Each command (bandwidth, latency, server, listen, download, analyze, etc.) is in a separate file
- `server/`: Server-side logic that runs performance tests between servers, manages WebSocket connections, collects system metrics (CPU, memory, dropped packets), and persists test results
- `client/`: Client-side logic that connects to servers via WebSocket, sends test configurations, collects/aggregates data points from all servers, and displays real-time results
- `shared/`: Common types and utilities including Config, DataPoint (DP), WebsocketSignal, host parsing with ellipsis patterns, and data serialization

### Communication Flow

1. Client connects to all servers via WebSocket (`wss://host:port/ws/host`)
2. Client sends `WebsocketSignal` with test configuration
3. Each server filters out itself from the host list to prevent self-testing
4. Servers run tests against other servers in the list (full mesh)
5. Servers stream `DataPoint` (DP) stats back to clients every second
6. Multiple clients can attach to the same test using `--id`
7. Tests persist on servers even if clients disconnect

### Test Types

- **RequestTest** (`latency` and `requests` commands): Sends fixed-size HTTP PUT requests with configurable delay between requests. Measures TTFB (Time To First Byte), RMS (Round-trip time), and tracks per-request latency. `latency` is a fixed probe (payload 1000, concurrency 1, 200 ms delay) and does not register those flags; `requests` registers them and honours them. Both share `runLatency`
- **StreamTest** (`bandwidth` command): Sends continuous HTTP streams with configurable concurrency. Measures throughput (TX bytes/sec) with multiple concurrent connections. Payload and buffer are pinned to 32000, so only `--concurrency` is tunable

Note a stream request never completes - it ends only when the test is cancelled -
so anything derived from request completion (RMS, and a completion-based request
count) is meaningless for a bandwidth test.

### Critical Implementation Details

**Server IP handling**: Servers need `--real-ip` flag when `--address` differs from external IP (or is a wildcard). Without this, servers report the bind address in stats and cannot recognize themselves in the host list (`isSelfHost` in server/server.go).

**Address handling**: All host entries pass through `shared.NormalizeHost` in `ParseHosts`, which strips brackets and canonicalizes IP literals, so one spelling reaches the wire, the self filters and the output. Use `shared.URLHostPort` when building a URL (it percent-encodes an IPv6 zone as RFC 6874 requires), `shared.HostOnly` to drop a port for display, and `shared.SameHost` to compare hosts - never substring matching, which used to make `--real-ip 10.0.0.1` swallow the peer `10.0.0.10`. The server binds with `fiber.NetworkTCP`, so a wildcard bind is dual-stack.

**Data persistence**: Test results are saved to `--storage-path` (default: current directory + `/hperf-tests/`). Each data point is JSON with a prefix byte (0=DataPoint, 1=ErrorPoint) followed by newline. Files are named `<testID>.<index>`. Test IDs are validated by `shared.ValidateTestID` before touching the filesystem: they are a filename component supplied by whichever client asked for the test, so an unvalidated ID could escape `--storage-path`, and an empty one made the cleanup glob match every saved test. Globs that select a test's files must be anchored with the separator (`id + ".*"`, never `id + "*"`).

**Server locking contract**: written out above `type test` in server/server.go and worth reading before touching that file. In short: `testLock` guards only the package-level `tests` slice; `t.M` guards `errors`, `errMap`, `DPS` and `cons`; `t.endedAt` carries liveness so callers can test it without `t.M`; `netPerfReader.m` guards only the TTFB/RMS watermarks and `hasStats` is atomic. The three locks are never held together, iterators snapshot under one lock and then work unlocked, and no blocking work (socket write, disk write, print) happens under `t.M` — `AddError` takes `t.M`, so persisting under it would deadlock as well as stall.

**Websocket peers**: never retain the `*websocket.Conn` that gofiber hands a handler. gofiber/contrib pools that wrapper and nils its embedded conn when the handler returns, so a stored wrapper later writes into an unrelated client's socket. Wrap it in `wsPeer` (server/server.go), which holds the inner non-pooled `fasthttp/websocket.Conn`, serializes writes on its own leaf mutex, and applies a write deadline so a stalled client cannot stall the test. Control frames deliberately bypass that mutex.

**Per-connection memory**: `fiber.Config.ReadBufferSize`/`WriteBufferSize` are allocated per concurrent connection, not once. A full mesh opens `(hosts-1) x concurrency` inbound connections per server, so these constants multiply by thousands — they are 64 KiB (`serverReadBufferSize`), measured at ~90 KiB RSS per connection. The only hard floor is that the largest inbound request header must fit or fasthttp answers 431; `--payload-size` is a body size and is streamed, never buffered whole.

**Concurrency model**: Each server maintains a semaphore channel per remote host (`concurrency chan int`) limiting concurrent requests. Workers pull from this channel, send requests, then return the slot (`startPerformanceReader`/`sendRequestToHost`). The channel starts full, so any direct call to `sendRequestToHost` must take a slot first or its deferred return blocks. Every response body is drained and closed on all paths — net/http cannot release a connection whose body is still open.

**Stats collection**: `pollHostStats` publishes an immutable `hostStats` snapshot through an `atomic.Pointer`, so `generateDataPoints` reads memory, CPU and drop counters without locking and without a nil dereference before the first poll. `cpu.Percent` blocks for a second by design, which is why it lives on its own goroutine.

**Dropped packets**: reported as a delta since the test started, covering receive *and* transmit drops, scoped to the interface resolved from `--real-ip`. `-1` means no usable counter, which is distinct from zero. The transmit column is the one that matters for a saturating sender.

**Throughput reporting**: the client aggregates incrementally in `client/aggregate.go`, updated per incoming data point under `responseLock` and read once per tick — never by rescanning the accumulated slice. `TX(max)`, `TX(min)` and `TX(avg)` summarize one population (per-flow, per-second rates), so `min <= avg <= max` holds. A single flow is ~1/(hosts-1) of a host's aggregate; that is the definition, not a bug, and it is why comparing `TX(max)` to a NIC counter mismatches by roughly the host count. `generateDataPoints` divides bytes by the *measured* elapsed window, so a slow sampling loop reduces the number of samples but does not bias the rate.

## Key Configuration Parameters

- `--hosts`: Supports ellipsis patterns (`10.10.1.{2...10}`), comma-separated lists, or file input (`file:/path/to/hosts`)
- `--id`: Test identifier for start/stop/listen/download operations. Auto-generated from Unix timestamp if not provided (for `bandwidth`, `latency` and `requests`). Letters, digits, `-`, `_` and `.` only, max 64 characters; validated server-side before it becomes a filename
- `--port`: Server port (default: 9010)
- `--concurrency`: Concurrent requests per host (default: 2 × GOMAXPROCS). Registered on `bandwidth` and `requests` only
- `--duration`: Test duration in seconds (default: 30)
- `--buffer-size`: Network buffer size in bytes (default: 32000). Registered on `requests` only
- `--payload-size`: HTTP payload size in bytes (default: 1000000). Registered on `requests` only
- `--request-delay`: Delay between requests in milliseconds (default: 0). Registered on `requests` only
- `--save`: Save test results on server for later retrieval (default: true)
- `--ip-family`: Address family used when resolving hostnames in `--hosts`: `auto`, `4` or `6` (default: auto)
- `--dns-server`: Resolve hostnames in `--hosts` through this DNS server

## Development Notes

- Go version: 1.26 (per go.mod)
- Uses Fiber v2 for HTTP/WebSocket server
- WebSocket library: gofiber/contrib/websocket (server) and fasthttp/websocket (client)
- System metrics: shirou/gopsutil for CPU/memory stats
- UI: charmbracelet/lipgloss for terminal styling
- The codebase filters servers from testing themselves: see `filterSelf` in client/client.go and `isSelfHost` in server/server.go
- `httpServer` in server/server.go is a package-level `fiber.New` singleton, so only one server can run per process. That is why there is no in-process multi-server test; end-to-end mesh testing needs separate processes or containers

## Helm Deployment

Helm chart located in `helm/hperf/` for Kubernetes deployments. Current version: 5.2.0. Includes StatefulSet, Service, ServiceAccount, and Job templates for bandwidth/latency tests. The chart version must be bumped whenever a template changes, per the note in `Chart.yaml`.
