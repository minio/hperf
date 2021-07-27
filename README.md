# mesh-throughput

Mesh throughput tool performs N peers to N peers cross network benchmark measuring RX/TX bandwidth for each peers.

## Usecases
- Calculate baseline RX/TX
- Debug TOR Switch bottlenecks

```
./mesh-throughput IP1 IP2 IP3 ...
...
Bandwidth: 1.2 GB/s RX | 1.0 GB/s TX
Bandwidth: 1.2 GB/s RX | 1.1 GB/s TX
Bandwidth: 1.2 GB/s RX | 990 MB/s TX
Bandwidth: 1.2 GB/s RX | 944 MB/s TX
```

on all the servers IP1 IP2 IP3 ... respectively.

### LICENSE
Use of `mesh-throughput` tool is governed by the GNU AGPLv3 license that can be found in the [LICENSE](./LICENSE) file.
