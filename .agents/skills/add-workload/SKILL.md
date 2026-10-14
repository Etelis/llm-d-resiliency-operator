---
name: add-workload
description: Add support for a workload type that runs an engine group's Pods (for example a new Kubernetes API). Use when IRO must manage a new kind of group, or when changing how Pods are reported, replaced or released.
---

# Add a workload

A workload plugin owns the Pods of one engine group. The controller never changes for a new workload type. Read
`internal/workload/workload.go` and `docs/components/workloads/index.md` first, then the closest existing workload
under `internal/workload/<name>/` and its page, and follow their structure.

## Before writing code

Find out how the workload type:

1. lists the Pods of one group, with a stable index per Pod,
2. replaces one Pod without recreating the group, if it can at all,
3. recreates the whole group, and what its restart policy does on a container restart,
4. tells that it is recreating the group, and that it gave up,
5. names the containers that run the engine.

## Write it

Under `internal/workload/<name>/`:

- `Discover` reports Pods as they are now, sorted by index, with `Identity` built from the UID and the engine
  containers' IDs and restart counts. Starting and terminating Pods are facts, not errors.
- `Reset`, `ResetAll`, `Release` touch only the Pods they name, by UID, and do nothing once those Pods are gone.
  A call the workload cannot do changes nothing and returns `ErrUnsupported`, for the whole list.
- `Reset` never recreates the rest of the group; `Release` never adds a Pod.
- No memory: every answer comes from the cluster.
- Report `Recreating` as soon as the workload will recreate the group, not only once it starts.

Register the factory in `cmd/main.go`, and add the RBAC the workload needs.

## Test it

With a fake Kubernetes client: Pods in every state, a repeated call that changes nothing, `ErrUnsupported` for a
Pod that cannot be replaced alone, a sidecar restart that keeps `Identity`, an engine restart that changes it.

## Document it

In the docs PR: a page `docs/components/workloads/<name>.md` (the workload, its journal storage, its RBAC). The
general workload page stays workload-neutral.
