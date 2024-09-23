# hperf

hperf is a tool for active measurements of the maximum achievable bandwidth between N peers, measuring RX/TX bandwidth for each peers.

## What is hperf for
Hperf was made to test networks in large infrastructure. It's highly scalable and cabaple of running parallel tests over
a long period of time. 

## Getting started

### Download
[Download Binary Releases](https://github.com/minio/hperf/releases) for various platforms.

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

## Common use cases
- Debugging link/nic MTU issues
- Optimizing throughput speed for specific payload/buffer sizes
- Finding servers that present latency on the application level when ping is showing no latency
- Testing overall network throughput
- Testing server to server reachability 

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

## Example test output which uses all stat types
```
$ ./hperf requests --hosts 10.10.10.1,10.10.10.2 --id http-test-1 --duration 5 --concurrency 12

Created  Local           Remote          PMSH PMSL TTFBH TTFBL TX        #TX    #ERR   #Dropped  MemUsed CPUUsed
14:42:09 10.10.10.1      10.10.10.2      19   0    6     0     2.70 GB/s 5129   0      0         40      73
14:42:09 10.10.10.2      10.10.10.1      18   0    7     0     2.70 GB/s 5111   0      0         40      63
14:42:10 10.10.10.2      10.10.10.1      16   0    8     0     2.69 GB/s 7799   0      0         40      63
14:42:10 10.10.10.1      10.10.10.2      17   0    6     0     2.73 GB/s 7862   0      0         40      73
14:42:11 10.10.10.1      10.10.10.2      19   0    9     0     2.68 GB/s 10553  0      0         40      89
14:42:11 10.10.10.2      10.10.10.1      20   0    6     0     2.67 GB/s 10472  0      0         40      88
14:42:12 10.10.10.1      10.10.10.2      17   0    5     0     2.69 GB/s 13238  0      0         40      89
14:42:12 10.10.10.2      10.10.10.1      17   0    7     0     2.69 GB/s 13175  0      0         40      88
```
