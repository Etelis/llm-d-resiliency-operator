# IRO Documentation

| I want to… | Read |
| --- | --- |
| understand what IRO is made of | [Components and contracts](design/contracts/README.md) |
| see the phases and what the controller does in each | [Controller](design/contracts/controller.md) |
| add a new engine | [Engine](design/contracts/engine.md) |
| add a new workload type | [Workload](design/contracts/workload.md) |
| understand restarts and leader changes | [Journal](design/contracts/journal.md) |
| find logs, metrics and Events | [Telemetry](design/contracts/telemetry.md) |
| see the LWS side and its RBAC | [LeaderWorkerSet](design/lws.md) |
| deploy vLLM for IRO | [vLLM](design/vllm.md) |
| look up a flag | [Controller](design/contracts/controller.md#configuration), [LeaderWorkerSet](design/lws.md), [vLLM](design/vllm.md#flags) |
| check pinned dependency versions | [Upstream versions](upstream-versions.md) |

```text
docs/
├── README.md                 this page
├── design/
│   ├── contracts/
│   │   ├── README.md         start here
│   │   ├── controller.md
│   │   ├── engine.md
│   │   ├── workload.md
│   │   ├── journal.md
│   │   └── telemetry.md
│   ├── lws.md                LeaderWorkerSet workload and journal
│   └── vllm.md               vLLM engine
└── upstream-versions.md
```
