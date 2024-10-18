# hperf

hperf is a tool for active measurements of the maximum achievable bandwidth between N peers, measuring RX/TX bandwidth for each peers.

## What is hperf for
Hperf was made to test networks in large infrastructure. It's highly scalable and cabaple of running parallel tests over a long period of time. 

## Common use cases
- Debugging link/nic MTU issues
- Optimizing throughput speed for specific payload/buffer sizes
- Finding servers that present latency on the application level when ping is showing no latency
- Testing overall network throughput
- Testing server to server connectivity 


## Core concepts
### The hperf binary
The binary can act as both client and server.

### Client
The client part of hperf is responsible for orchestrating the servers. Its only job is to send commands to the servers and receive incremental stats updates. It can be executed from any machine that can talk to the servers.

### Servers
Servers are the machines we are testing. To launch the hperf command in servers mode use the `server` command:

```bash
$ ./hperf server --help
```

This command will start an API and websocket on the given `--address` and save test results to `--storage-path`. 

WARNING: do not expose `--address` to the internet
<b>NOTE: if the `--address` is not the same as your external IP addres used for communications between servers then you need to set `--real-ip`, otherwise the server will report internal IPs in the stats and it will run the test against itself, causing invalid results.</b>

### The listen command
Hperf can run tests without a specific `client` needing to be constantly connected. Once the `client` has started a test, the `client` can 
easily exit without interrupting the test stopping.

Any `client` can hook into the list test at runtime using the `--id` of the test. There can even be multiple `clients`
listening to the same test.

Example:
```bash
$ ./hperf listen --hosts 10.10.1.{2...10} --id [TEST_ID]
```

## Getting started

### Download
[Download Binary Releases](https://github.com/minio/hperf/releases) for various platforms and place in a directory of
your choosing.

You can also install via source:
```
go install github.com/minio/hperf/cmd/hperf@latest
```

### Server
Run server with default settings:
NOTE: this will place all test result files in the same directory and use 0.0.0.0 as bind ip. We do not recommend this for larger tests. 
```bash
$ ./hperf server
```

Run the server with custom `--address`, `--real-ip` and `--storage-path`
```bash
$ ./hperf server --address 10.10.2.10:5000 --real-ip 150.150.20.2 --storage-path /tmp/hperf/
```

### Client 
If the `server` command was executed with a custom `--address`, the port can be specified in the `client` using `--port`.

The `--hosts` and `--id` flags are especially important to understand.

`--hosts` is where we determine which machines we will send the current command to. The hosts parameter supports
the same ellipsis pattern as minio and also a comma separate list of hosts as well as a file: input. The file expects a
host per file line.

```bash
./hperf [command] --hosts 1.1.1.1,2.2.2.2
./hperf [command] --hosts 1.1.1.{1...100}
./hperf [command] --hosts file:/home/user/hosts
```

`--id` is used to start, stop, listen to tests, or get results. 
NOTE: Be careful not to re-use the ID's if you care about fetching results at a later date.

```bash
# listen in on a running test
./hperf listen --hosts 1.1.1.{1...100} --id [my_test_id]

# stop a running test
./hperf stop --hosts 1.1.1.{1...100} --id [my_test_id]

# download test results
./hperf download --hosts 1.1.1.{1...100} --id [my_test_id] --file /tmp/test.out

# analyze test results
./hperf analyze --file /tmp/test.out
# analyze test results with full print output
./hperf analyze --file /tmp/test.out --print-stats --print-errors

# Generate a .csv file from a .json test file
./hperf csv --file /tmp/test.out
```

## Analysis
The analyze command will print statistics for the 10th and 90th percentiles and all datapoints in between. Additionally, you can use the `--print-stats` and `--print-erros` flags for a more verbose output.

The analysis will show:
 - 10th percentile: total, low, avarage, high
 - in between: total, low, avarage, high
 - 90th percentile: total, low, avarage, high

## Statistics
 - Payload Roundtrip (RMS high/low): 
   - Payload transfer time (Microseconds)
 - Time to first byte (TTFB high/low): 
   - This is the amount of time (Microseconds) it takes between a request being made and the first byte being requested by the receiver
 - Transferred bytes (TX high/low): 
   - Bandwidth throughput in KB/s, MB/s, GB/s, etc..
 - Transferred bytes (TX total): 
   - Total transferred bytes (not per second)
 - Request count (#TX): 
   - The number of HTTP/s requests made
 - Error Count (#ERR): 
   - Number of encountered errors
 - Dropped Packets (#Dropped): 
 - Memory (Mem high/low/used): 
 - CPU (CPU high/low/used): 

## Example: 20 second HTTP payload transfer test using multiple sockets
This test will use 12 concurrent workers to send http requests with a payload without any timeout between requests.
Much like a bandwidth test, but it will also test server behaviour when multiple sockets are being created and closed:
```
$ ./hperf requests --hosts file:./hosts --id http-test-1 --duration 20 --concurrency 12
```

## Example: 20 second HTTP payload transfer test using a stream
This will perform a 20 second bandwidth test with 12 concurrent HTTP streams:
```
$ ./hperf bandwidth --hosts file:./hosts --id http-test-2 --duration 20 --concurrency 12
```

## Example: 5 Minute latency test using a 1000 Byte buffer, with a delay of 50ms between requests
This test will send a single round trip request between servers to test base latency and reachability:
```
$ ./hperf latency --hosts file:./hosts --id http-test-2 --duration 360 --concurrency 1 --requestDelay 50
--bufferSize 1000 --payloadSize 1000
```

# Full test scenario with analysis and csv export
## On the server
```bash
$ ./hperf server --address 10.10.2.10:5000 --real-ip 150.150.20.2 --storage-path /tmp/hperf/
```

## The client













