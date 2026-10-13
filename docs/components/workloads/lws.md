# LWS workload

How LeaderWorkerSet implements the [workload](index.md) and [journal](../journal.md)
contracts. Code: `internal/workload/lws` and `internal/journal/configmap`.

> **Not built yet.** This workload and its journal come with [#11](https://github.com/llm-d-incubation/llm-d-resiliency-operator/pull/11); they are not on `main`.

## Workload

| Contract | LWS |
|---|---|
| Group | one LWS with `replicas: 1`, Pods with label `group-index=0` |
| Required spec | `size >= 1`, `groupIdentity: Ordinal` or unset, `restartPolicy` unset, `RecreateGroupOnPodRestart` or `None` |
| `Pod.Index` | label `leaderworkerset.sigs.k8s.io/worker-index`. Names are `<lws>-0` and `<lws>-0-<i>`. |
| `Pod.Identity` | a hash of the Pod UID and, per engine container, its ID and restart count. Engine containers are `--lws-engine-containers`, by default every container in `spec.containers`. Init containers, native sidecars included, count only when named. Changing the flag changes every identity, which IRO reads as a new group. |
| `Pod.Node` | `spec.nodeName` |
| `Pod.State` | `Terminating` when the Pod is being deleted. `Ready` when every container runs and the Pod has an IP. Otherwise `Starting`. |
| `Recreating` | the leader Pod is missing or terminating, carries `leaderworkerset.sigs.k8s.io/group-recreate-pending`, or is newer than a worker (LWS replaced it while old workers remain). Under `RecreateGroupOnPodRestart`, also any Pod terminating or with a restarted or exited container. |
| `RecreateStopped` | the leader Pod carries `leaderworkerset.sigs.k8s.io/group-restart-budget-exhausted` |
| `Discover` fails | the LWS cannot be read, its spec is not supported, or a Pod's name does not match its worker index |
| `Reset` | under `RecreateGroupOnPodRestart`: `ErrUnsupported`, because deleting any Pod recreates the whole group. Under `None`: delete each named worker with a UID precondition, and LWS recreates each one alone. A list that names the leader: `ErrUnsupported` before anything is deleted, because deleting it takes its workers with it. |
| `ResetAll` | delete the leader with a UID precondition and foreground propagation, so the old leader stays `Terminating` until its workers are gone. `NotFound` or `Conflict` count as done. |
| `Release` | `ErrUnsupported`: LWS cannot remove a chosen Pod from a running group |
| Flags | `--lws-namespace` (default `default`), `--lws-name`, `--lws-engine-containers` (each Pod must have at least one of them, or `Discover` fails; leader and workers may name theirs differently) |

## Journal

| Contract | ConfigMap |
|---|---|
| Storage | ConfigMap `<lws-name>-ft-state`, key `state.json` |
| Version | `<resourceVersion>/<digest of state.json>` |
| Compare-and-swap | read the ConfigMap, and update it only if its `resourceVersion` or its `state.json` matches the version. Anything else, including a Kubernetes conflict, is `ErrConflict`. |
| Metadata-only change | `state.json` is unchanged, so the update goes ahead on the new `resourceVersion` |
| Deleted | `ErrConflict`. The empty version creates the ConfigMap, and `AlreadyExists` is `ErrConflict`. |
| Lifetime | owner reference to the LWS |

## RBAC

`leaderworkersets` get; `pods` list and delete; `configmaps` get, create and update; `leases` for
`--leader-elect`; `events` create and patch. See `deploy/kubernetes/rbac.yaml`.

## Open decisions

1. **Restart policy.** Resetting one Pod needs `restartPolicy: None`. Under `RecreateGroupOnPodRestart`,
   `Reset` answers `ErrUnsupported`, and any container restart, including one from a probe, recreates the
   group: IRO waits and adopts it. Under `None`, LWS replaces only a deleted Pod, and kubelet restarts a
   crashed container in place. Either way IRO removes the lost member in place. A deleted Pod's replacement
   brings members that stay candidates until the engine can admit them (an engine that cannot grow never does),
   holding their GPU. An
   engine container restarted in place keeps the Pod's UID: its new members are never candidates, and the Pod
   is reset like any failed Pod (with an engine that cannot grow, kept as it is). Neither policy tolerates a probe that fails while a
   member is paused for recovery: see the engine's doc.
2. **Restart limits for IRO resets.** `restartBackoff` and `maxGroupRestarts` are on LWS `main`, not in
   v0.11.1. Check that a recreation IRO causes, by deleting the leader, is paced and counted like any other.
