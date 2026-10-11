# Architecture

IRO (inference resiliency operator) is one controller-runtime binary with two
recovery paths.

```mermaid
flowchart TB
    subgraph iro["IRO operator Pod"]
        mgr["manager<br/>leader election, metrics :8080, probes :8081"]
        rr["RecoveryRequest controller"]
        ef["Engine fault recovery<br/>opt-in, leader only"]
        mgr --> rr
        mgr --> ef
    end
    det["fault detector"] -->|creates| crd["RecoveryRequest"]
    rr -->|watches| crd
    rr -->|"relabels serving Pods"| pods["inference Pods"]
    ef -->|"engine adapter"| ranks["engine ranks"]
    ef -->|"workload adapter"| lws["LWS, Pods, journal"]
    router["Router (EPP)"] --> ranks
```

| Path | Trigger | Scope | IRO acts on | State kept in | Docs |
| --- | --- | --- | --- | --- | --- |
| RecoveryRequest | a request from a fault detector | node or device | Pod serving labels | the request's status | [CRDs](crds.md) |
| Engine fault recovery | ranks report faults | one engine group | the engine's FT API, the group's leader Pod | journal ConfigMap | [design](design/engine-fault-recovery.md) |

---

## Startup

```mermaid
sequenceDiagram
    participant M as main
    participant G as manager
    participant L as Lease
    M->>M: parse flags
    M->>G: create manager
    M->>G: register RecoveryRequest controller
    M->>G: register engine fault runner, if enabled
    M->>G: start
    G->>L: acquire, if --leader-elect
    L-->>G: leader
    G->>G: start controllers and runner
```

With leader election on, both paths run only on the leader. Engine fault
recovery won't start without it.

---

## Code Layout

| Package | Role |
| --- | --- |
| `cmd` | entry point, flags, manager |
| `apis/inference/v1alpha1` | RecoveryRequest types |
| `internal/controller/recoveryrequest` | RecoveryRequest controller |
| `internal/controller/enginefault` | recovery controller, decisions, runner, setup |
| `internal/engine` | engine contract and shared types |
| `internal/engine/vllm` | vLLM adapter |
| `internal/workload/lws` | LWS adapter |
| `runtime/vllm` | Pod-side bridge for vLLM |
| `deploy/kubernetes`, `deploy/vllm-ft` | base manifests and the engine fault overlay |

---

## Deployment

- One Deployment with its ServiceAccount and a ClusterRole for RecoveryRequest
  and nodes ([deploy/kubernetes](../deploy/kubernetes)).
- The engine fault overlay ([deploy/vllm-ft](../deploy/vllm-ft/README.md)) adds
  a namespaced Role for one LWS and the flags to turn it on.
- One IRO instance handles one engine group today.
