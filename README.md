# hperf

hperf is a tool for active measurements of the maximum achievable bandwidth between N peers, measuring RX/TX bandwidth for each peers.

## What is hperf for
Hperf was made to test networks in large infrastructure. It's highly scalable and capable of running parallel tests over a long period of time. 

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
Hperf can run tests without a specific `client` needing to be constantly connected. Once the `client` has started a test, the `client` can exit without affecting the test.

Any `client` can hook into the list test at runtime using the `--id` of the test.
There can even be multiple `clients` listening to the same test.

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



# Full test scenario using (requests, download, analysis and csv export)
## On the servers
```bash
$ ./hperf server --address 0.0.0.0:6000 --real-ip 10.10.10.2 --storage-path /tmp/hperf/
```

## The client

### Run test
```bash
 ./hperf latency --hosts 10.10.10.{2...3} --port 6000 --duration 10 --id latency-test-1

 Test ID: latency-test-1

#ERR   #TX        TX(high)   TX(low)    TX(total)       RMS(high) RMS(low)  TTFB(high) TTFB(low) #Dropped  Mem(high) Mem(low)  CPU(high) CPU(low)
0      8          4.00 KB/s  4.00 KB/s  8.00 KB         1         0         0         0          937405    1         1         0         0
0      26         5.00 KB/s  4.00 KB/s  18.00 KB        1         0         0         0          1874810   1         1         0         0
0      73         5.00 KB/s  4.00 KB/s  33.00 KB        1         0         0         0          3317563   1         1         0         0
0      92         5.00 KB/s  4.00 KB/s  38.00 KB        1         0         0         0          3749634   1         1         0         0
0      140        5.00 KB/s  4.00 KB/s  48.00 KB        1         0         0         0          4687048   1         1         0         0
0      198        5.00 KB/s  4.00 KB/s  58.00 KB        1         0         0         0          5624466   1         1         0         0
0      266        5.00 KB/s  4.00 KB/s  68.00 KB        1         0         0         0          6561889   1         1         0         0
0      344        5.00 KB/s  4.00 KB/s  78.00 KB        1         0         0         0          7499312   1         1         0         0
0      432        5.00 KB/s  4.00 KB/s  88.00 KB        9         0         0         0          8436740   1         1         0         0
0      530        5.00 KB/s  4.00 KB/s  98.00 KB        9         0         0         0          9374172   1         1         0         0

 Testing finished ..
 Analyzing data ..


 _____ P99 data points _____

Created  Local           Remote          RMS(high) RMS(low)  TTFB(high) TTFB(low) TX         #TX        #ERR   #Dropped  Mem(used) CPU(used)
10:30:54 10.10.10.3      10.10.10.3      9         0         0          0         5.00 KB/s  44         0      432076    1         0

 Sorting: RMSH
 Time: Milliseconds

P10  count      sum        min        avg        max
     18         30         0          1          9
P50  count      sum        min        avg        max
     10         25         0          2          9
P90  count      sum        min        avg        max
     2          18         9          9          9
P99  count      sum        min        avg        max
     1          9          9          9          9

```

### Explaining the stats above.
The first section includes the combined highs/lows and counters for ALL servers beings tested. 
Each line represents a 1 second stat point.
Here is a breakdown of the individual stats:

 - `#ERR`: number of errors ( all servers )
 - `#TX`: total number of HTTP requests made ( all servers )
 - `TX(high/low)`: highest and lowest transfer rate seen ( single server )
 - `RMS(high/low)`: longest and fastest round trip latency ( single server )
 - `TTFB(high/low)`: The time it took to read the first byte ( single server ) 
 - `#Dropped`: highest count of dropped packets ( single server )
 - `Mem(high/low)`: highest and lowest memory usage ( single server )
 - `CPU(high/low)`: highest and lowest cpu usage ( single server )

The next section is a print-out for the `p99` data points.
p99 represents the 1% of the worst data points and all statistics are related to
that single data point between `Local` and `Remote`. 

Finally we have the p10 to p99 breakdown.
 - `Sorting`: the data point being sorted/used for the data breakdown
 - `Time:`: the time unit being used. Default is milliseconds but can be changed to microseconds `--micro`
 - `count`: the total number of data points in this category
 - `sum`: the sum of all valuesa in thie category
 - `min`: the single lowest value in this category
 - `avg`: the avarage for all values in this category
 - `max`: the highest value in this category

### Download test
```bash
./hperf download --hosts 10.10.10.{2...3} --port 6000 --id latency-test-1 --file latency-test-1
```

### Analyze test
NOTE: this analysis will display the same output as the final step when running the test above.
```bash
./hperf analyze --file latency-test-1 --print-stats --print-errors
```

### Export csv
```bash
./hperf csv --file latency-test-1
```

# Random Example tests

## Example: Basic latency testing
This will run a 20 second latency test and analyze+print the results when done
```
$ ./hperf latency --hosts file:./hosts --port [PORT] --duration 20 --print-all
```

## Example: Basic bandwidth testing
This will run a 20 second bandwidth test and print the results when done
```
$ ./hperf bandwidth --hosts file:./hosts --port [PORT] --duration 20 --concurrency 10 --print-all
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
