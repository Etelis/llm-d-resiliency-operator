# Configuration

Everything is set with operator flags. Engine fault recovery is off unless
`--enable-vllm-ft` is set.

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
| `--enable-vllm-ft` | `false` | Turn it on. Needs `--leader-elect`. |
| `--vllm-lws` | none | LWS name. Required. |
| `--vllm-namespace` | `default` | LWS namespace. The journal lives here too. |
| `--vllm-api-port` | `8000` | Port of a Pod's first rank. Local rank `i` uses `8000 + i`. |
| `--vllm-observer-port` | `9257` | Observer port in each Pod |
| `--vllm-ft-store-port` | `29600` | Store port sent when the store master is removed |
| `--vllm-diagnosis-timeout` | `75s` | Longest time in `diagnosing` |
| `--vllm-apply-timeout` | `90s` | Longest time in `applying` |

## Fixed Values

These have no flag today.

| Value | Default |
| --- | --- |
| Tick | 1 s |
| Startup limit | 10 min |
| Retry window | 2 min |
| Reset budget | 3 per 30 min |
| Engine HTTP timeout | 10 s |
| Observer timeout | 1 s for exit evidence, 2 s for discovery |
| Leader election | 15 s lease, 10 s renew deadline, 2 s retry |
| Journal | ConfigMap `<lws>-ft-state`, key `state.json` |

What each timer bounds is in the
[state machine](design/engine-fault-state-machine.md#timers).

## Example

From [deploy/vllm-ft/operator.yaml](../deploy/vllm-ft/operator.yaml):

```yaml
args:
- --leader-elect
- --enable-vllm-ft
- --vllm-lws=wide-ep
- --vllm-namespace=default
```

Environment variables for the vLLM Pods are in [vLLM engine](engines/vllm.md#launchsh).
