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
go test ./...
```

### Lint
```bash
golangci-lint run
```

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

- **RequestTest** (latency command): Sends fixed-size HTTP PUT requests with configurable delay between requests. Measures TTFB (Time To First Byte), RMS (Round-trip time), and tracks per-request latency
- **StreamTest** (bandwidth command): Sends continuous HTTP streams with configurable concurrency. Measures throughput (TX bytes/sec) with multiple concurrent connections

### Critical Implementation Details

**Server IP handling**: Servers need `--real-ip` flag when `--address` differs from external IP. Without this, servers report internal IPs in stats and may test against themselves (server/server.go:388-390).

**Data persistence**: Test results are saved to `--storage-path` (default: current directory + `/hperf-tests/`). Each data point is JSON with a prefix byte (0=DataPoint, 1=ErrorPoint) followed by newline. Files are named by test ID.

**Concurrency model**: Each server maintains a semaphore channel per remote host (`concurrency chan int`) limiting concurrent requests. Workers pull from this channel, send requests, then return the slot (server/server.go:700-723).

**Stats collection**: Separate goroutine collects system stats (memory, CPU, dropped packets from `/proc/net/dev`) every second (server/server.go:260-287). Stats are locked with mutexes when updating high/low watermarks.

## Key Configuration Parameters

- `--hosts`: Supports ellipsis patterns (`10.10.1.{2...10}`), comma-separated lists, or file input (`file:/path/to/hosts`)
- `--id`: Test identifier for start/stop/listen/download operations. Auto-generated from Unix timestamp if not provided
- `--port`: Server port (default: 9010)
- `--concurrency`: Concurrent requests per host (default: 2 × GOMAXPROCS)
- `--duration`: Test duration in seconds (default: 30)
- `--buffer-size`: Network buffer size in bytes (default: 32000)
- `--payload-size`: HTTP payload size in bytes (default: 1000000)
- `--request-delay`: Delay between requests in milliseconds (default: 0)
- `--save`: Save test results on server for later retrieval (default: true)

## Development Notes

- Go version: 1.24 (per go.mod)
- Uses Fiber v2 for HTTP/WebSocket server
- WebSocket library: gofiber/contrib/websocket (server) and fasthttp/websocket (client)
- System metrics: shirou/gopsutil for CPU/memory stats
- UI: charmbracelet/lipgloss for terminal styling
- The codebase filters servers from testing themselves: see client/client.go:78-87 and server/server.go:386-399

## Helm Deployment

Helm chart located in `helm/hperf/` for Kubernetes deployments. Current version: 5.0.6. Includes StatefulSet, Service, ServiceAccount, and Job templates for bandwidth/latency tests.
