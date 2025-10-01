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

## Understanding Test Results

### Real-Time Output

During test execution, hperf displays aggregated statistics across all servers:

| Metric           | Description                                            |
|------------------|--------------------------------------------------------|
| `#ERR`           | Total error count across all servers                   |
| `#TX`            | Total HTTP requests made across all servers            |
| `TX(high/low)`   | Highest and lowest transfer rate (single server)       |
| `RMS(high/low)`  | Longest and fastest round-trip latency (single server) |
| `TTFB(high/low)` | Slowest and fastest time-to-first-byte (single server) |
| `#Dropped`       | Highest count of dropped packets (single server)       |
| `Mem(high/low)`  | Highest and lowest memory usage (single server)        |
| `CPU(high/low)`  | Highest and lowest CPU usage (single server)           |

### Post-Test Analysis

After a test completes, hperf automatically analyzes results and displays percentile breakdowns:

- **P99 data points**: Shows the worst 1% of measurements - critical for understanding tail latency
- **Percentile statistics**: P10, P50, P90, P99 breakdowns showing count, sum, min, average, and max values
- Results can be sorted by any metric using `--sort` flag (e.g., `--sort RMSH` for worst round-trip times)

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
Useful for detecting intermittent network issues:
```bash
./hperf latency --hosts file:./hosts --port 6000 --duration 300 \
  --concurrency 1 --request-delay 50 --buffer-size 1000 --payload-size 1000
```

#### Maximum Throughput Test
Push the network to its limits:
```bash
./hperf bandwidth --hosts file:./hosts --port 6000 --duration 60 \
  --concurrency 16 --payload-size 10000000
```

#### Custom Payload Optimization
Find optimal buffer/payload sizes for your workload:
```bash
./hperf bandwidth --hosts file:./hosts --port 6000 --duration 30 \
  --concurrency 8 --buffer-size 65536 --payload-size 5000000
```

## Configuration Reference

### Common Flags

| Flag              | Default        | Description                                                  |
|-------------------|----------------|--------------------------------------------------------------|
| `--hosts`         | (required)     | Target servers (comma-separated, ellipsis pattern, or file:) |
| `--port`          | 9010           | Server port                                                  |
| `--id`            | auto-generated | Test identifier (timestamp if not specified)                 |
| `--duration`      | 30             | Test duration in seconds                                     |
| `--concurrency`   | 2×CPUs         | Concurrent requests per server                               |
| `--payload-size`  | 1000000        | Payload size in bytes                                        |
| `--buffer-size`   | 32000          | Network buffer size in bytes                                 |
| `--request-delay` | 0              | Delay between requests in milliseconds                       |
| `--save`          | true           | Save test results on servers                                 |
| `--insecure`      | false          | Use HTTP instead of HTTPS                                    |
| `--debug`         | false          | Enable debug output                                          |

### Environment Variables

All flags can be set via environment variables with `HPERF_` prefix:
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
2. **Set realistic test IDs**: Use descriptive IDs like `prod-latency-2024-01-15` for easier result management
3. **Configure external IPs**: Always set `--real-ip` when servers have multiple interfaces
4. **Plan for scale**: Long tests with many servers generate significant data - monitor disk usage
5. **Network isolation**: Run tests on a dedicated management network when possible
6. **Automate analysis**: Use `--file` with `analyze` and `csv` commands to integrate with monitoring systems

### For Development and Testing

1. **Start small**: Test with 2-3 servers before scaling to production
2. **Use debug mode**: Add `--debug` to understand communication flow
3. **Experiment with parameters**: Test different `--concurrency`, `--payload-size`, and `--buffer-size` values
4. **Save results**: Always use `--save` during testing to enable later analysis

## Troubleshooting

### Servers testing themselves
**Symptom**: Unusually high throughput or low latency results
**Solution**: Ensure `--real-ip` matches the external IP used for inter-server communication

### No data points received
**Symptom**: Client shows no statistics during test
**Solution**: Check firewall rules, verify servers can reach each other on the specified port, enable `--debug`

### Connection timeouts
**Symptom**: Client can't connect to servers
**Solution**: Verify servers are running, check `--address` and `--port` match client configuration, test network connectivity

### High error counts
**Symptom**: `#ERR` column shows many errors
**Solution**: Check server logs with `--debug`, verify network stability, reduce `--concurrency` or increase `--request-delay`

## License

hperf is licensed under the GNU Affero General Public License v3.0. See [LICENSE](LICENSE) for details.

## Contributing

Contributions are welcome! This project is maintained by [MinIO, Inc.](https://min.io)

## Support

- **Issues**: Report bugs and request features on [GitHub Issues](https://github.com/minio/hperf/issues)
- **Commercial Support**: Contact MinIO for enterprise support and consulting

