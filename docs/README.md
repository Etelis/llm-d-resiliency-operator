# Architecture

IRO keeps a multi-node inference engine serving through faults. It watches one engine group, the Pods that
run the engine's members (for vLLM, its DP ranks). When a member fails, IRO removes it in place so the rest keep
serving, then brings the group back to full capacity by resetting the failed Pod. Only when that cannot work
does it restart the whole group.

![IRO manager, its plugins and what they talk to](design/figures/architecture.svg)

The controller knows no engine and no workload type. It talks to them through two plugins: the
[workload](design/contracts/workload.md) (LeaderWorkerSet today) and the [engine](design/contracts/engine.md)
(vLLM today). Its state lives in a versioned [journal](design/contracts/journal.md), so a new leader picks up a
recovery where the old one stopped.

## A fault, end to end

![One member dies and the group goes 4, 3, 4 while serving](design/figures/recovery-timeline.svg)

The controller is always in one of three phases. A fault moves it from `serving` to `recovering`; a reconfig that
removes the dead member brings it back. A `ResetAll` is the only way back to `starting`.

![Controller phases: starting, serving, recovering](design/figures/phases.svg)

## Components

| Component | What it does | Read |
|---|---|---|
| Controller | phases, deadlines, checks on every plan, resets, restoring capacity | [controller.md](design/contracts/controller.md) |
| Workload | reports the group's Pods, resets or releases them | [workload.md](design/contracts/workload.md), [LWS](design/lws.md) |
| Engine | reports members and their state, plans and sends reconfigs | [engine.md](design/contracts/engine.md), [vLLM](design/vllm.md) |
| Journal | durable controller state, one writer at a time | [journal.md](design/contracts/journal.md) |
| Telemetry | logs, metrics, Events, incident records | [telemetry.md](design/contracts/telemetry.md) |

The boundaries between them, and why they are drawn there, are in [Components and contracts](design/contracts/README.md).
