# hperf

hperf is a tool for active measurements of the maximum achievable bandwidth between N peers, measuring RX/TX bandwidth for each peers.

## Download

[Download Binary Releases](https://github.com/minio/hperf/releases) for various platforms.

## Usecases
- Calculate baseline RX/TX
- Debug TOR Switch bottlenecks

## Usage
```
./hperf IP1 IP2 IP3 ...
...
Bandwidth: 1.2 GB/s RX | 1.0 GB/s TX
Bandwidth: 1.2 GB/s RX | 1.1 GB/s TX
Bandwidth: 1.2 GB/s RX | 990 MB/s TX
Bandwidth: 1.2 GB/s RX | 944 MB/s TX
```

on all the servers IP1 IP2 IP3 ... respectively.

Default ports are `9999` and `10000` make sure your firewalls allow these ports. You may optionally configure `./hperf` to use custom ports as well, for example setting port `5001` would require opening up port `5002` as well.


```
NPERF_PORT=5001 ./hperf IP1 IP2 IP3 ...
```

## On k8s

### Using helm
```
helm install https://github.com/minio/hperf/raw/main/helm-releases/hperf-v4.0.0.tgz --generate-name --namespace <my-namespace>
```

### Using `yaml`

```
export NAMESPACE=<my-namespace>
kubectl apply -f https://github.com/minio/hperf/raw/main/hperf.yaml --namespace $NAMESPACE
```

### Observe the output
```
kubectl logs --namespace <my-namespace> --max-log-requests <replica-count> -l "app=hperf" -f
```

### LICENSE
Use of `hperf` tool is governed by the GNU AGPLv3 license that can be found in the [LICENSE](./LICENSE) file.
