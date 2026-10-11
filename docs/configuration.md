# Configuration

Everything is set with operator flags. Engine fault recovery is off unless
`--enable-engine-ft` is set.

## Operator

| Flag | Default | Purpose |
| --- | --- | --- |
| `--metrics-bind-address` | `:8080` | Prometheus metrics |
| `--health-probe-bind-address` | `:8081` | `/healthz` and `/readyz` |
| `--leader-elect` | `false` | Lease-based leader election. Required for engine fault recovery. |
| `-v` and other klog flags | | Log verbosity |

## Engine Fault Recovery

| Flag | Default | Purpose |
| --- | --- | --- |
| `--enable-engine-ft` | `false` | Turn it on. Needs `--leader-elect`. |
| `--engine` | none | Engine adapter to use, by name. Required. |
| `--lws-name` | none | LWS name. Required. |
| `--lws-namespace` | `default` | LWS namespace. The journal lives here too. |
| `--engine-api-port` | `8000` | Port of a Pod's first rank. Local rank `i` uses `8000 + i`. |
| `--engine-observer-port` | `9257` | Observer port in each Pod |
| `--ft-store-port` | `29600` | Store port sent when the store master is removed |
| `--ft-diagnosis-timeout` | `75s` | Longest time in `diagnosing` |
| `--ft-apply-timeout` | `90s` | Longest time in `applying` |

`--engine` must name an adapter registered in `cmd/main.go`. None is registered
yet, so turning the feature on fails with `unknown engine` until one is added.

## Fixed Values

These have no flag today.

| Value | Default |
| --- | --- |
| Tick | 1 s |
| Startup limit | 10 min |
| Retry window | 2 min |
| Reset budget | 3 per 30 min |
| Observer timeout during discovery | 2 s |
| Leader election | 15 s lease, 10 s renew deadline, 2 s retry |
| Journal | ConfigMap `<lws>-ft-state`, key `state.json` |

What each timer bounds is in the
[state machine](design/engine-fault-state-machine.md#timers).

## Example

```yaml
args:
- --leader-elect
- --enable-engine-ft
- --engine=<adapter name>
- --lws-name=wide-ep
- --lws-namespace=default
```
