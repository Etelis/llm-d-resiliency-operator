# IRO Documentation

| I want to… | Read |
| --- | --- |
| understand what IRO is made of | [Architecture](architecture.md) |
| understand engine fault recovery | [Design overview](design/engine-fault-recovery.md) |
| see every state and transition | [State machine](design/engine-fault-state-machine.md) |
| know why a rank was retried, removed or reset | [Decisions](design/engine-fault-decisions.md) |
| add a new engine or workload type | [Adapters](design/adapters.md) |
| understand restarts and leader election | [Journal and failover](design/journal-and-failover.md) |
| see the LWS side and its RBAC | [LeaderWorkerSet workload](workloads/lws.md) |
| debug a stuck group | [Operations](operations.md) |
| look up a flag | [Configuration](configuration.md) |
| check pinned dependency versions | [Upstream versions](upstream-versions.md) |

```text
docs/
├── README.md                          this page
├── architecture.md
├── design/
│   ├── engine-fault-recovery.md       start here for engine faults
│   ├── engine-fault-state-machine.md
│   ├── engine-fault-decisions.md
│   ├── adapters.md
│   └── journal-and-failover.md
├── workloads/
│   └── lws.md
├── diagrams/                          figures used by the docs
├── operations.md
├── configuration.md
└── upstream-versions.md
```
