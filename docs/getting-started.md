# Getting started

This page will walk through installing IRO and turning it on for an engine group.

> **Not built yet.** The manager flags below come with [#11](https://github.com/llm-d-incubation/llm-d-resiliency-operator/pull/11); on `main` the manager does not run engine fault
> recovery.

Until then, the short version: run the manager with leader election, and choose the engine and the workload.

```sh
manager --leader-elect --enable-engine-ft --engine=<engine> --workload=<workload>
```

The controller's flags are in [configuration](components/controller.md#configuration). Each engine and workload
page lists its own.
