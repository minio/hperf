# hperf

**Enterprise-grade network performance testing for large-scale infrastructure**

hperf is a powerful tool for measuring maximum achievable bandwidth and latency between multiple servers in your infrastructure. Designed for scalability, it can run parallel tests across hundreds of nodes over extended periods, making it ideal for validating network performance in production environments.

## Why hperf?

Modern infrastructure demands reliable, high-performance networking. hperf helps you:

- **Validate network investments** - Measure actual throughput and latency between servers
- **Diagnose performance issues** - Identify bottlenecks in MTU configuration, NIC tuning, or network paths
- **Ensure SLA compliance** - Verify network performance meets business requirements
- **Optimize at scale** - Test payload sizes and buffer configurations for your workload
- **Continuous monitoring** - Run long-duration tests to detect intermittent issues

### Common Use Cases

- Debugging link/NIC MTU misconfigurations
- Optimizing throughput for specific payload and buffer sizes
- Finding servers with application-level latency when ping shows no issues
- Benchmarking overall network throughput in your infrastructure
- Validating server-to-server connectivity and reachability

## Architecture Overview

### Distributed Client-Server Model

hperf uses a simple but powerful architecture:

**Servers**: Deploy the hperf server on each node you want to test. Servers communicate with each other to perform the actual performance measurements.

**Client**: Run the client from any machine that can reach your servers. The client orchestrates tests and displays results in real-time.

**Stateless Operation**: Tests run independently on servers. Clients can disconnect and reconnect to running tests at any time, making hperf ideal for long-running tests and monitoring scenarios.

### How It Works

1. Start hperf in server mode on all nodes you want to test
2. Run a client command specifying the test type and target servers
3. The client instructs each server to test connectivity with all other servers (full mesh)
4. Servers report real-time statistics back to the client
5. Results are aggregated and displayed, with optional persistence for later analysis

**Important**: The `--real-ip` flag should be set on servers when the bind address differs from the external IP used for inter-server communication. This ensures accurate reporting and prevents servers from testing against themselves.

## Getting Started

### Installation

#### Binary Release (Recommended)

Download pre-built binaries for your platform from [GitHub Releases](https://github.com/minio/hperf/releases).

#### Build from Source

```bash
go install github.com/minio/hperf/cmd/hperf@latest
```

### Quick Start

#### 1. Start Servers

On each server you want to test:

```bash
# Basic setup - uses current directory for results
./hperf server

# Production setup - specify bind address, external IP, and storage path
./hperf server --address 10.10.2.10:5000 --real-ip 150.150.20.2 --storage-path /var/lib/hperf/
```

**Security Note**: The server API is unauthenticated. Do not expose the server port to untrusted networks.

#### 2. Run a Test

##### Latency Test
Measure round-trip latency and time-to-first-byte between servers:

```bash
./hperf latency --hosts 10.10.10.{2...10} --port 5000 --duration 20 --id latency-test-1
```

##### Bandwidth Test
Measure maximum throughput using concurrent streams:

```bash
./hperf bandwidth --hosts 10.10.10.{2...10} --port 5000 --duration 20 --concurrency 10 --id bandwidth-test-1
```

### Host Specification Patterns

hperf supports flexible host specification:

```bash
# Comma-separated list
./hperf latency --hosts 1.1.1.1,2.2.2.2,3.3.3.3

# Ellipsis pattern (MinIO-style)
./hperf latency --hosts 1.1.1.{1...100}

# File input (one host per line)
./hperf latency --hosts file:/home/user/hosts.txt
```

### IPv6

IPv6 works everywhere IPv4 does. Addresses are accepted with or without
brackets and are canonicalized internally, so `2001:db8::1`, `[2001:db8::1]`
and `2001:0db8:0000:0000:0000:0000:0000:0001` all refer to the same host:

```bash
# IPv6 literals, ellipsis patterns and scoped link-local addresses
./hperf latency --hosts 2001:db8::1,2001:db8::2
./hperf latency --hosts 2001:db8::{1...10}
./hperf latency --hosts fe80::1%eth0,fe80::2%eth0
```

Servers need a listener on an IPv6 address:

```bash
# Dual-stack: accepts IPv4 and IPv6 on every interface
./hperf server --address '[::]:9010'

# A single IPv6 address, with the same address reported in results
./hperf server --address '[2001:db8::1]:9010' --real-ip 2001:db8::1
```

Note that a wildcard bind (`0.0.0.0:9010` or `[::]:9010`, including the
default) listens for both address families. Bind a specific address if you
need to restrict the server to one family. The server API is unauthenticated,
so this matters when the port is reachable from untrusted networks.

When `--hosts` contains hostnames, `--ip-family` picks the address family they
resolve to (`auto`, `4` or `6`), and `--dns-server` resolves them through a
specific DNS server:

```bash
./hperf latency --hosts node{1...4}.example.com --ip-family 6
```

## Understanding Test Results

### Real-Time Output

During test execution, hperf displays aggregated statistics across all servers:

| Metric           | Description                                                     |
|------------------|-----------------------------------------------------------------|
| `#ERR`           | Total error count across all servers                            |
| `#TX`            | Total HTTP requests completed across all servers                |
| `TX(max/min)`    | Highest and lowest transfer rate of any single flow             |
| `TX(avg)`        | Mean transfer rate across every flow, over the same population   |
| `TX(total)`      | Total bytes transferred by the whole mesh                       |
| `RMS(high/low)`  | Longest and fastest round-trip latency (single server)          |
| `TTFB(high/low)` | Slowest and fastest time-to-first-byte (single server)          |
| `#Dropped`       | Packets dropped during this test, or `-1` if unavailable        |
| `Mem(high/low)`  | Highest and lowest memory usage (single server)                 |
| `CPU(high/low)`  | Highest and lowest CPU usage (single server)                    |

A "flow" is one server's traffic to one peer, sampled once a second. `TX(max)`,
`TX(min)` and `TX(avg)` summarize that same population, so `TX(min)` <=
`TX(avg)` <= `TX(max)` always holds. None of the three is the aggregate
throughput of a host or of the cluster: in a full mesh of N hosts each host
carries N-1 flows, so a single flow's rate is roughly 1/(N-1) of what one host's
NIC counters will show. Use `TX(total)` over the test duration, or the per-host
table below, when comparing against `ethtool` or switch counters.

`#Dropped` counts receive plus transmit drops on the interface carrying the
test, measured from the moment the test started. It is `-1` when no counter
could be read, which is not the same as zero. On Linux the interface is derived
from `--real-ip`, falling back to summing every non-loopback interface.

### Per-Host Throughput

When a test finishes, hperf prints one row per host with that host's average,
slowest and fastest flow rates, sorted slowest first, so a single lagging node
is visible instead of being averaged away. Hosts averaging under half the
fleet-wide average are highlighted.

### Post-Test Analysis

Latency tests (`latency` and `requests`) analyze their results automatically when
they finish, and `analyze` reproduces the same breakdown from a saved file:

- **P99 data points**: Shows the worst 1% of measurements - critical for understanding tail latency
- **Percentile statistics**: P10, P50, P90, P99 breakdowns showing count, sum, min, average, and max values
- Sort with `--sort RMSH` (round-trip, the default) or `--sort TTFBH` (time to
  first byte). Those are the only two sort keys

Bandwidth tests do not produce a percentile breakdown; use the live table, the
per-host table above, and `--print-all` or `analyze --print-stats` for the
individual data points.

## Advanced Workflows

### Managing Long-Running Tests

Tests continue running on servers even if the client disconnects. This enables:

#### Listen to a Running Test
```bash
./hperf listen --hosts 10.10.10.{2...10} --id latency-test-1
```

Multiple clients can monitor the same test simultaneously.

#### Stop a Test
```bash
./hperf stop --hosts 10.10.10.{2...10} --id latency-test-1
```

#### List and Delete Saved Tests
```bash
# What is stored on the servers
./hperf list --hosts 10.10.10.{2...10}

# Remove one test, or every test when --id is omitted
./hperf delete --hosts 10.10.10.{2...10} --id latency-test-1
```

### Analyzing Historical Results

#### Download Test Results
```bash
./hperf download --hosts 10.10.10.{2...10} --id latency-test-1 --file latency-test-1.json
```

#### Analyze Saved Results
```bash
# Basic analysis
./hperf analyze --file latency-test-1.json

# Detailed analysis with all data points and errors
./hperf analyze --file latency-test-1.json --print-stats --print-errors

# Filter by specific host
./hperf analyze --file latency-test-1.json --host-filter 10.10.10.5
```

#### Export to CSV
```bash
./hperf csv --file latency-test-1.json
```

This creates `latency-test-1.json.csv` with all data points for analysis in spreadsheet tools.

### Test Examples

#### High-Frequency Latency Test
Useful for detecting intermittent network issues. Use `requests` rather than
`latency` when you want to control the request shape - `latency` is a fixed
probe and does not accept these flags:
```bash
./hperf requests --hosts file:./hosts --port 6000 --duration 300 \
  --concurrency 1 --request-delay 50 --buffer-size 1000 --payload-size 1000
```

#### Maximum Throughput Test
Push the network to its limits:
```bash
./hperf bandwidth --hosts file:./hosts --port 6000 --duration 60 \
  --concurrency 16
```

`bandwidth` deliberately fixes its payload and buffer at 32000 bytes, so
concurrency is the only knob it exposes.

#### Custom Payload Optimization
Find optimal buffer/payload sizes for your workload. This is what `requests`
is for:
```bash
./hperf requests --hosts file:./hosts --port 6000 --duration 30 \
  --concurrency 8 --buffer-size 65536 --payload-size 5000000
```

## Configuration Reference

### Common Flags

Flags are registered per command, so not every flag is accepted everywhere. The
"Commands" column below says where each one applies.

| Flag              | Default        | Commands                | Description                                                  |
|-------------------|----------------|-------------------------|--------------------------------------------------------------|
| `--hosts`         | (required)     | all client commands     | Target servers (comma-separated, ellipsis pattern, or file:) |
| `--port`          | 9010           | all client commands     | Server port                                                  |
| `--id`            | auto-generated | all client commands     | Test identifier (timestamp if not specified)                 |
| `--duration`      | 30             | bandwidth, latency, requests | Test duration in seconds                                |
| `--concurrency`   | 2×CPUs         | bandwidth, requests     | Concurrent requests per server                               |
| `--payload-size`  | 1000000        | requests                | Payload size in bytes                                        |
| `--buffer-size`   | 32000          | requests                | Network buffer size in bytes                                 |
| `--request-delay` | 0              | requests                | Delay between requests in milliseconds                       |
| `--save`          | true           | bandwidth, latency, requests | Save test results on servers                            |
| `--dns-server`    | (system)       | all client commands     | DNS server used to resolve hostnames in `--hosts`            |
| `--ip-family`     | auto           | all client commands     | Address family for hostname resolution: `auto`, `4` or `6`   |
| `--sort`          | RMSH           | analyze                 | Sort data points: `RMSH` or `TTFBH`                          |
| `--insecure`      | true           | global (before command)  | Use HTTP instead of HTTPS - **on by default**                |
| `--debug`         | false          | global (before command)  | Enable debug output                                          |

`--insecure` and `--debug` are application-level flags and must appear *before*
the subcommand: `./hperf --debug bandwidth --hosts ...`. Note that `--insecure`
defaults to **true**, so hperf speaks plain HTTP unless you turn it off.

`bandwidth` and `latency` pin their own payload, buffer and delay settings and do
not accept those flags; `requests` is the tunable form of the latency test.

### Environment Variables

Most flags can be set via environment variables with the `HPERF_` prefix -
`--hosts`, `--port`, `--insecure`, `--concurrency`, `--request-delay`,
`--duration`, `--buffer-size`, `--payload-size`, `--restart-on-error`, `--save`,
`--dns-server`, `--ip-family` and `--debug`. Output and file flags (`--id`,
`--file`, `--sort`, `--micro`, `--print-*`, `--host-filter`) are flag-only.

```bash
export HPERF_HOSTS="10.10.1.{1...10}"
export HPERF_PORT="6000"
export HPERF_DURATION="60"
```

## Deployment

### Kubernetes/Helm

Deploy hperf across your Kubernetes cluster using Helm:

```bash
helm install hperf ./helm/hperf
```

The chart includes:
- StatefulSet for server deployment
- Job templates for automated bandwidth and latency tests
- ServiceAccount and RBAC configuration

See `helm/hperf/values.yaml` for configuration options.

### Docker

```bash
docker run -p 9010:9010 minio/hperf:latest server --address 0.0.0.0:9010
```

## Best Practices

### For Enterprise Deployments

1. **Use dedicated storage**: Specify `--storage-path` to a dedicated volume for test results
2. **Set realistic test IDs**: Use descriptive IDs like `prod-latency-2024-01-15` for easier result management. IDs may contain letters, digits, `-`, `_` and `.`, up to 64 characters
3. **Configure external IPs**: Always set `--real-ip` when servers have multiple interfaces
4. **Plan for scale**: Long tests with many servers generate significant data - monitor disk usage
5. **Network isolation**: Run tests on a dedicated management network when possible
6. **Automate analysis**: Use `--file` with `analyze` and `csv` commands to integrate with monitoring systems
7. **Size server memory for the mesh**: a full mesh opens `(hosts - 1) x concurrency` inbound connections per
   server, each costing roughly 90 KiB of RSS. Budget about
   `(hosts - 1) x concurrency x 90 KiB` plus ~50 MiB of baseline - for example
   ~750 MiB at 64 hosts with `--concurrency 128`, or ~3 GiB at 256 hosts. Lower
   `--concurrency` if that does not fit; note that changing it changes the
   workload, so keep it pinned when comparing runs over time

### For Development and Testing

1. **Start small**: Test with 2-3 servers before scaling to production
2. **Use debug mode**: Add `--debug` to understand communication flow
3. **Experiment with parameters**: Test different `--concurrency`, `--payload-size`, and `--buffer-size` values
4. **Save results**: Always use `--save` during testing to enable later analysis

## Troubleshooting

### Servers testing themselves
**Symptom**: Unusually high throughput or low latency results
**Solution**: Ensure `--real-ip` matches the external IP used for inter-server communication

### Server exits with "unable to listen on ..."
**Symptom**: The server stops right after start
**Solution**: The bind address is not usable on this host. `--address '[::]:9010'` is the dual-stack wildcard; an IPv6 literal has to be bracketed (`'[2001:db8::1]:9010'`)

### No data points received
**Symptom**: Client shows no statistics during test
**Solution**: Check firewall rules, verify servers can reach each other on the specified port, enable `--debug`

### Connection timeouts
**Symptom**: Client can't connect to servers
**Solution**: Verify servers are running, check `--address` and `--port` match client configuration, test network connectivity

### High error counts
**Symptom**: `#ERR` column shows many errors
**Solution**: Check server logs with `--debug`, verify network stability, reduce `--concurrency` or increase `--request-delay`

### Some hosts were excluded from the test
**Symptom**: A warning naming hosts that did not answer, and a smaller mesh than configured
**Solution**: The run continues with the hosts that connected rather than aborting. Check that the named hosts are running and reachable on `--port`; their absence lowers `TX(total)` proportionally

### Reported throughput looks far lower than NIC counters
**Symptom**: `TX(max)` is roughly 1/(hosts-1) of what `ethtool` reports
**Solution**: This is expected. `TX(max/min/avg)` describe a single flow between one pair of hosts, not a host's or the cluster's aggregate. Compare `TX(total)` over the test duration, or use the per-host table printed when the test finishes

## License

hperf is licensed under the GNU Affero General Public License v3.0. See [LICENSE](LICENSE) for details.

## Contributing

Contributions are welcome! This project is maintained by [MinIO, Inc.](https://min.io)

## Support

- **Issues**: Report bugs and request features on [GitHub Issues](https://github.com/minio/hperf/issues)
- **Commercial Support**: Contact MinIO for enterprise support and consulting

