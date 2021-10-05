# mperf

mperf is a tool for active measurements of the maximum achievable bandwidth between N peers, measuring RX/TX bandwidth for each peers.

## Download

[Download Binary Releases](https://github.com/minio/mperf/releases) for various platforms.

## Usecases
- Calculate baseline RX/TX
- Debug TOR Switch bottlenecks

## Usage
```
./mperf IP1 IP2 IP3 ...
...
Bandwidth: 1.2 GB/s RX | 1.0 GB/s TX
Bandwidth: 1.2 GB/s RX | 1.1 GB/s TX
Bandwidth: 1.2 GB/s RX | 990 MB/s TX
Bandwidth: 1.2 GB/s RX | 944 MB/s TX
```

on all the servers IP1 IP2 IP3 ... respectively.

### LICENSE
Use of `mperf` tool is governed by the GNU AGPLv3 license that can be found in the [LICENSE](./LICENSE) file.
