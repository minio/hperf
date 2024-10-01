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
The client part of hperf is responsible for orchestrating the servers. It's only job is to send commands to the
servers and receive incremental stats update. It can be executed from any machine that can talk to the servers.

### Servers
Servers are the machines we are testing. To launch the hperf command in servers mode, simply use the `server` command:
NOTE: `server` is the only command you can execute on the servers. All other commands are executed from the client.
```bash
$ ./hperf server --help
```
This command will start an API and websocket on the given `--address` and save test results to `--storage-path`. 


## Getting started

### Download
[Download Binary Releases](https://github.com/minio/hperf/releases) for various platforms.

You can also install via source

```
go install github.com/minio/hperf/cmd/hperf@latest
```

### Server
1. Download hperf and place it in a directory of your choosing
   - This can be automated with deployment tools, hperf is just a single binary

2. Run hperf help to see a list of available server commands flags and example
```bash
$ ./hperf server --help
```

3. Run the server with your preferred settings

### Client
1. Download hperf 

2. Run hperf help to see available commands, flags and examples
   - The `--hosts` and `--id` flags are especially important to understand
```bash
$ ./hperf --help
$ ./hperf [command] --help
```

```
--hosts: Hosts is where we determine which machines we will send the current command to. The hosts parameter supports
the same ellipsis pattern as minio and also a comma seperate list of hosts as well as a file: input. The file expects a
host per file line.

Examples:
./hperf [command] --hosts 1.1.1.1,2.2.2.2
./hperf [command] --hosts 1.1.1.{1...100}
./hperf [command] --hosts file:/home/user/hosts

Additionally, if the `server` command was executed with a custom address + port, the port can be specified using `--port`.

--id: is the ID used when starting tests, listening to tests, or fetching test results. Be carefull not to re-use the
ID's if you care about fetching results at a later date.
```

## Available Statistics
 - Payload Roundtrip (PMS high/low): 
   - Payload transfer time (Milliseconds)
 - Time to first byte (TTFB high/low): 
   - This is the amount of time (Milliseconds) it takes between a request being made and the first byte being requested by the receiver
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
This test will use 12 concurrent workers(concurrency) to send http requests with a payload without any timeouts inbetween requests.
Much like a bandwidth test, but it will also test server behaviour when multiple sockets are being created and closed.
```
$ ./hperf requests --hosts file:./hosts --id http-test-1 --duration 20 --concurrency 12
```

## Example: 20 second HTTP payload transfer test using a stream
This will perform a 20 second bandwidth test with 12 concurrent HTTP streams.
```
$ ./hperf bandwidth --hosts file:./hosts --id http-test-2 --duration 20 --concurrency 12
```

## Example: 5 Minute latency test using a 2000 Byte buffer, with a delay of 50ms between requests
This test will send a single round trip request between servers to test base latency and reachability 
```
$ ./hperf latency --hosts file:./hosts --id http-test-2 --duration 360 --concurrency 1 --requestDelay 50
--bufferSize 2000 --payloadSize 2000
```


