# Getting started

This page will walk through installing IRO and turning it on for an engine group.

Until then, the short version: run the manager with leader election, and choose the engine and the workload.

```sh
manager --leader-elect --enable-engine-ft --engine=vllm --workload=lws
```

The flags are listed in [the controller](components/controller.md#configuration), [vLLM](components/engines/vllm.md) and
[LeaderWorkerSet](components/workloads/lws.md).
