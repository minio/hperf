# hperf

hperf is a tool for active measurements of the maximum achievable bandwidth between N peers, measuring RX/TX bandwidth for each peers.

## What is hperf for
Hperf was made to test networks in large infrastructure. It's highly scalable and cabaple of running parallel tests over
a long period of time. 

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
The client part of hperf is responsible for orchestrating the servers. Its only job is to send commands to the
servers and receive incremental stats updates. It can be executed from any machine that can talk to the servers.

### Servers
Servers are the machines we are testing. To launch the hperf command in servers mode, simply use the `server` command:
NOTE: `server` is the only command you can execute on the servers. All other commands are executed from the client.
```bash
$ ./hperf server --help
```
This command will start an API and websocket on the given `--address` and save test results to `--storage-path`. 

WARNING: do not expose `--address` to the internet

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
NOTE: this will place all test result files in the same directory.
```bash
$ ./hperf server
```
Run the server with custom `--address` and `--storage-path`
```bash
$ ./hperf server --address 10.10.2.10:5000 --storage-path /tmp/hperf/
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
# get test results
./hperf stat --hosts 1.1.1.{1...100} --id [my_test_id]
# save test results
./hperf stat --hosts 1.1.1.{1...100} --id [my_test_id] --output /tmp/test.out

# analyze test results
./hperf analyze --file /tmp/test.out

# listen in on a running test
./hperf listen --hosts 1.1.1.{1...100} --id [my_test_id]

# stop a running test
./hperf stop --hosts 1.1.1.{1...100} --id [my_test_id]
```

## Analysis
The analyze command will print statistics for the 10th and 90th percentiles and all datapoints in between. 
The format used is:
 - 10th percentile: total, low, avarage, high
 - in between: total, low, avarage, high
 - 90th percentile: total, low, avarage, high

## Available Statistics
 - Payload Roundtrip (RMS high/low): 
   - Payload transfer time (Microseconds)
 - Time to first byte (TTFB high/low): 
   - This is the amount of time (Microseconds) it takes between a request being made and the first byte being requested by the receiver
 - Transferred bytes (TX): 
   - Bandwidth throughput in KB/s, MB/s, GB/s, etc..
 - Request count (#TX): 
   - The number of HTTP/s requests made
 - Error Count (#ERR): 
   - Number of encountered errors
 - Dropped Packets (#Dropped): 
   - Total dropped packets on the server (total for all time)
 - Memory (MemUsed): 
   - Total memory in use (total for all time)
 - CPU (CPUUsed): 
   - Total memory in use (total for all time)

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

## Example: 5 Minute latency test using a 2000 Byte buffer, with a delay of 50ms between requests
This test will send a single round trip request between servers to test base latency and reachability:
```
$ ./hperf latency --hosts file:./hosts --id http-test-2 --duration 360 --concurrency 1 --requestDelay 50
--bufferSize 2000 --payloadSize 2000
```


