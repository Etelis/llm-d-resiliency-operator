# Getting started

This page will walk through installing IRO and turning it on for an engine group.

Until then, the short version: run the manager with leader election, and choose the engine and the workload.

```sh
manager --leader-elect --enable-engine-ft --engine=<engine> --workload=<workload>
```

The controller's flags are in [configuration](components/controller.md#configuration). Each engine and workload
page lists its own.
