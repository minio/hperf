# hperf

hperf is a tool for active measurements of the maximum achievable bandwidth between N peers, measuring RX/TX bandwidth for each peers.

## Download

[Download Binary Releases](https://github.com/minio/hperf/releases) for various platforms.

## Usecases
- Calculate baseline RX/TX
- Debug TOR Switch bottlenecks
- Calculate roundtrip MS for http requests

## Usage
Various configurations have been added for controling everything from payload size to http read/write buffers. All flags
can be seen via `-help`.

Hperf can be used without any configuration, just run hperf on all the servers IP1 IP2 IP3 ... respectively.
```
./hperf -stream=false -hosts 10.10.1.{1...10}
┌────────────┬────────────┬───────┬──────────┬───────┬──────────┬─────────────────┬───────────────────┬──────┐
│ Local      │ Remote     │ #RX   │ RX       │ #TX   │ TX       │ TX(ms) high/low │ TTFB(ms) high/low │ #Err │
├────────────┼────────────┼───────┼──────────┼───────┼──────────┼─────────────────┼───────────────────┼──────┤
│ 10.10.10.1 │ 10.10.10.6 │ 14927 │ 1.3 GB/s │ 10681 │ 1.2 GB/s │         312 / 2 │            13 / 0 │    0 │
│ 10.10.10.1 │ 10.10.10.2 │ 10880 │ 1.3 GB/s │ 18187 │ 1.2 GB/s │         260 / 2 │            13 / 0 │    0 │
│ 10.10.10.1 │ 10.10.10.3 │ 16804 │ 1.3 GB/s │ 17141 │ 1.2 GB/s │         299 / 2 │            13 / 0 │    0 │
│ 10.10.10.1 │ 10.10.10.4 │ 18670 │ 1.4 GB/s │ 18920 │ 1.3 GB/s │         321 / 2 │            10 / 0 │    0 │
│ 10.10.10.1 │ 10.10.10.5 │ 30070 │ 1.2 GB/s │ 29626 │ 1.3 GB/s │         636 / 2 │            10 / 0 │    0 │
│ 10.10.10.1 │ 10.10.10.7 │ 24031 │ 1.3 GB/s │ 27004 │ 1.3 GB/s │         600 / 2 │            16 / 0 │    0 │
│ 10.10.10.1 │ 10.10.10.8 │ 20844 │ 1.2 GB/s │ 21870 │ 1.2 GB/s │         297 / 1 │            13 / 0 │    0 │
└────────────┴────────────┴───────┴──────────┴───────┴──────────┴─────────────────┴───────────────────┴──────┘
```

Default ports are `9999` make sure your firewalls allow this port. You may optionally configure `./hperf` to use a custom port as well `-port MYPORT`

```
./hperf -port MYPORT -stream=false -hosts 10.10.1.{1...10}
┌────────────┬────────────┬───────┬──────────┬───────┬──────────┬─────────────────┬───────────────────┬──────┐
│ Local      │ Remote     │ #RX   │ RX       │ #TX   │ TX       │ TX(ms) high/low │ TTFB(ms) high/low │ #Err │
├────────────┼────────────┼───────┼──────────┼───────┼──────────┼─────────────────┼───────────────────┼──────┤
│ 10.10.10.1 │ 10.10.10.6 │ 14927 │ 1.3 GB/s │ 10681 │ 1.2 GB/s │         312 / 2 │            13 / 0 │    0 │
│ 10.10.10.1 │ 10.10.10.2 │ 10880 │ 1.3 GB/s │ 18187 │ 1.2 GB/s │         260 / 2 │            13 / 0 │    0 │
│ 10.10.10.1 │ 10.10.10.3 │ 16804 │ 1.3 GB/s │ 17141 │ 1.2 GB/s │         299 / 2 │            13 / 0 │    0 │
│ 10.10.10.1 │ 10.10.10.4 │ 18670 │ 1.4 GB/s │ 18920 │ 1.3 GB/s │         321 / 2 │            10 / 0 │    0 │
│ 10.10.10.1 │ 10.10.10.5 │ 30070 │ 1.2 GB/s │ 29626 │ 1.3 GB/s │         636 / 2 │            10 / 0 │    0 │
│ 10.10.10.1 │ 10.10.10.7 │ 24031 │ 1.3 GB/s │ 27004 │ 1.3 GB/s │         600 / 2 │            16 / 0 │    0 │
│ 10.10.10.1 │ 10.10.10.8 │ 20844 │ 1.2 GB/s │ 21870 │ 1.2 GB/s │         297 / 1 │            13 / 0 │    0 │
└────────────┴────────────┴───────┴──────────┴───────┴──────────┴─────────────────┴───────────────────┴──────┘
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
