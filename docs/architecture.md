# Architecture

IRO (inference resiliency operator) is one controller-runtime binary. Engine
fault recovery runs inside it, on the elected replica.

```mermaid
flowchart TB
    subgraph iro["IRO operator Pod"]
        mgr["manager<br/>leader election, metrics :8080, probes :8081"]
        ef["Engine fault recovery<br/>opt-in, leader only"]
        mgr --> ef
    end
    ef -->|"engine adapter"| ranks["engine ranks"]
    ef -->|"workload adapter"| lws["LWS, Pods, journal"]
    router["Router (EPP)"] --> ranks
```

| Part | Job | Doc |
| --- | --- | --- |
| Recovery controller | The state machine, one tick per second | [State machine](design/engine-fault-state-machine.md) |
| Engine adapter | Talks to the engine ranks | [Adapters](design/adapters.md), [vLLM](engines/vllm.md) |
| Workload adapter | Finds the group, recreates it, keeps the journal | [Adapters](design/adapters.md), [LWS](workloads/lws.md) |
| Journal | Saved state for restarts | [Journal and failover](design/journal-and-failover.md) |
| Router (EPP) | Routes traffic on each rank's own health | [vLLM routing](engines/vllm.md#routing) |

---

## Startup

```mermaid
sequenceDiagram
    participant M as main
    participant G as manager
    participant L as Lease
    M->>M: parse flags
    M->>G: create manager
    M->>G: register engine fault runner, if enabled
    M->>G: start
    G->>L: acquire, if --leader-elect
    L-->>G: leader
    G->>G: start the runner
```

The runner only runs on the leader, and engine fault recovery won't start
without leader election.

---

## Code Layout

| Package | Role |
| --- | --- |
| `cmd` | entry point, flags, manager |
| `internal/controller/enginefault` | recovery controller, decisions, runner, setup |
| `internal/engine` | engine contract and shared types |
| `internal/engine/vllm` | vLLM adapter |
| `internal/workload/lws` | LWS adapter |
| `runtime/vllm` | Pod-side bridge for vLLM |
| `deploy/kubernetes`, `deploy/vllm-ft` | base manifests and the engine fault overlay |

---

## Deployment

- The base manifests are in [deploy/kubernetes](../deploy/kubernetes).
- The overlay in [deploy/vllm-ft](../deploy/vllm-ft/README.md) adds a
  namespaced Role for one LWS and the flags that turn engine fault recovery
  on.
- One IRO instance handles one engine group today.
