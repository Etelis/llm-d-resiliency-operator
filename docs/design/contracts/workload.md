# Workload

The workload owns the Pods of one engine group. It does three things:

- **reports** the group's Pods as they are right now,
- **recreates** the group when asked,
- **resizes** the group when asked.

It remembers nothing: no earlier calls, no earlier groups. Deciding whether this is still the same group
is the [controller's](controller.md#reading-discover) job. It knows nothing about engines: no ranks, ports
or processes. The first implementation is [LWS](#lws).

## Interface

```go
type Workload interface {
    Discover(ctx context.Context) (Group, error)
    Reset(ctx context.Context, pods []PodRef) error
    Resize(ctx context.Context, req ResizeRequest) error
}
```

## Objects

```go
// Group is the group as it is now.
type Group struct {
    Pods            []Pod // every Pod, sorted by Index, including starting and terminating ones
    Recreating      bool  // the workload is replacing the whole group right now
    RecreateStopped bool  // the workload gave up recreating the group, e.g. its restart budget is used up
}

// Pod is one Pod of the group.
type Pod struct {
    Name     string
    UID      string
    IP       string
    Index    int      // position in the group, fixed for the Pod's life
    Identity string   // UID + container IDs + restart counts. Changes when any container restarts.
    Node     string   // the node it runs on, empty until scheduled
    State    PodState
}

// Ref names the Pod exactly.
func (pod *Pod) Ref() PodRef { return PodRef{Name: pod.Name, UID: pod.UID} }

type PodState string

const (
    PodStarting    PodState = "Starting"    // scheduling, pulling, or containers not all running yet
    PodReady       PodState = "Ready"       // every container runs and the Pod has an IP
    PodTerminating PodState = "Terminating" // being deleted
)

// PodRef names one Pod. The UID makes it exact: a new Pod with the same name is a different Pod.
type PodRef struct {
    Name string
    UID  string
}

// ResizeRequest is the shape the controller wants.
type ResizeRequest struct {
    Pods   int      // how many Pods the group should hold
    Remove []PodRef // Pods that must go. No other Pod may be removed.
}

// ErrUnsupported means the workload cannot do this at all. Nothing was changed.
var ErrUnsupported = errors.New("unsupported")
```

## Functions

### Discover

Reports the group as it is now.

| | |
|---|---|
| **Input** | none |
| **Output** | [`Group`](#objects), or an error |
| **Errors** | only when the workload cannot be read, or its spec is not supported. Starting, terminating and missing Pods are facts, not errors. |
| **Side effects** | none |

**Example**, during the resize in the [running example](README.md#running-example):

```yaml
Recreating: false
RecreateStopped: false
Pods:
- {Name: lws-0,   UID: a1, Index: 0, Identity: idA, Node: gpu-node-1, State: Ready}
- {Name: lws-0-1, UID: b1, Index: 1, Identity: idB, Node: gpu-node-2, State: Ready}
- {Name: lws-0-2, UID: c1, Index: 2, Identity: idC, Node: gpu-node-3, State: Ready}
- {Name: lws-0-3, UID: d1, Index: 3, Identity: idD, Node: gpu-node-7, State: Terminating}
- {Name: lws-0-4, UID: e1, Index: 4, Identity: idE, Node: gpu-node-9, State: Starting}
```

`Discover` does not say that `lws-0-3` was asked to go or that `lws-0-4` is new. The controller
knows both from its journal.

### Reset

Recreates the group that the named Pods belong to.

| | |
|---|---|
| **Input** | `pods []PodRef`: the Pods of the group to recreate, as the controller last saw them |
| **Output** | `nil` once the recreation is triggered, or once none of the named Pods exist. An error otherwise. |
| **Side effects** | the whole group is replaced by new Pods, when the workload's own restart policy allows it |

**Example.**

```go
Reset(ctx, []PodRef{{"lws-0", "a1"}, {"lws-0-1", "b1"}, {"lws-0-2", "c1"}, {"lws-0-3", "d1"}})
// first call: LWS deletes lws-0 (UID a1). LWS recreates the group. Returns nil.
// after the new group exists: lws-0 now has UID f1, so no named Pod exists. Returns nil, changes nothing.
```

The second call is why the controller can repeat `Reset` until the old Pods are gone: it can never
touch the new group.

### Resize

Makes the group hold `req.Pods` Pods, removing exactly `req.Remove`, without recreating it.

| | |
|---|---|
| **Input** | [`ResizeRequest`](#objects) |
| **Output** | `nil` when the change is accepted (it may take a long time to show in `Discover`). `ErrUnsupported` when the workload cannot resize, with nothing changed. Another error otherwise. |
| **Side effects** | named Pods are removed, new Pods are added, in the order the workload picks |

**Example.**

```go
Resize(ctx, ResizeRequest{Pods: 4, Remove: []PodRef{{"lws-0-3", "d1"}}})
// a workload that supports it: lws-0-3 starts terminating, lws-0-4 is created. Returns nil.
// the same call again: lws-0-3 is already going and the group already counts 4 Pods. Returns nil, changes nothing.
// LWS today: returns ErrUnsupported, changes nothing.
```

## Rules

| Rule | Why |
|---|---|
| No memory. Every answer comes from the cluster as it is now. | a restarted IRO or a new leader gets the same answers |
| `Discover` fails only when it cannot read the workload, or its spec is not supported | the controller should see starting and terminating Pods, not lose sight of the group |
| `Pods` are sorted by `Index`, and a Pod keeps its `Index` for life | stable addressing for the engine driver |
| `Pod.Identity` changes when any container restarts | a restarted process is never mistaken for the old one |
| `Reset` touches only the Pods it names, by UID, and repeating it is harmless | a late reset can never hit a new group |
| `Resize` removes only the Pods in `Remove`, by UID, and never picks another Pod to reach the size | the dead Pod goes, a healthy one never does |
| A Pod in `Remove` that cannot go without recreating the group, such as a leader, stays and still counts toward `Pods` | its capacity waits for the next reset |
| `Resize` states an end state, not a delta | repeating it is harmless |
| A workload that cannot resize changes nothing and returns `ErrUnsupported` | the controller stops asking, and restores capacity by reset or stays below target, per `--ft-capacity-restore` |
| No engine knowledge: ranks, ports, processes, endpoints | that belongs to the [engine driver](engine.md#driver) |

## Considerations

- **Who paces resets.** The workload paces recreations and decides when to stop, including ones IRO asks for.
  IRO has no reset budget of its own. A workload that stopped says so in `RecreateStopped`.
- **Order of a resize.** The workload picks remove-first, add-first or replace. It knows its own
  scheduling limits. Remove-first frees the GPUs and works on a full cluster.
- **Leaders.** Some workloads cannot remove their leader without recreating the group. That Pod stays,
  and its capacity comes back only with the next reset.
- **Report `Recreating` as early as possible.** A workload that will recreate the group because of what it
  sees (for LWS, any container restart under `RecreateGroupOnPodRestart`) reports `Recreating` right away,
  not only once the leader is deleted. Otherwise the controller may plan a round for a group that is about to go.
- **Recreating vs. stopped.** `Recreating` is temporary: the controller waits. `RecreateStopped` is
  the workload saying it gave up: the controller waits too, and shows it in its status.

## LWS

| Contract | LWS |
|---|---|
| Group | one LWS with `replicas: 1`, Pods with label `group-index=0` |
| Required spec | `groupIdentity: Ordinal` or unset, `size >= 1`. Today also `RecreateGroupOnPodRestart` (open decision 2). |
| `Pod.Index` | label `leaderworkerset.sigs.k8s.io/worker-index`. Names are `<lws>-0` and `<lws>-0-<i>`. |
| `Pod.Node` | `spec.nodeName` |
| `Pod.State` | `Terminating` when the Pod is being deleted. `Ready` when every container runs and the Pod has an IP. Otherwise `Starting`. |
| `Recreating` | the leader Pod is missing or terminating, or carries `leaderworkerset.sigs.k8s.io/group-recreate-pending` (recreation held back by `restartBackoff`), or, under `RecreateGroupOnPodRestart`, any Pod has a restarted or failed container: LWS is about to recreate the group |
| `RecreateStopped` | the leader Pod carries `leaderworkerset.sigs.k8s.io/group-restart-budget-exhausted` (`maxGroupRestarts` used up) |
| `Discover` fails | the LWS cannot be read, or its spec is not supported |
| `Reset` | delete the leader among the named Pods, with a UID precondition. `NotFound` or `Conflict` count as done. LWS recreates the group. |
| Reset pacing, giving up | `restartBackoff` and `maxGroupRestarts`, on LWS `main` and not in v0.11.1 |
| `Resize` | returns `ErrUnsupported` until LWS can remove and add Pods in a running group |
| Journal | see [journal.md](journal.md#lws) |
| Flags | `--lws-namespace`, `--lws-name` |

**Today** `lws.Discover` does more than report. It fails while Pods start, terminate or differ in count
from `size`, and it hashes every Pod and container identity into a group ID. Both go: Pod states are
reported instead, and the controller compares Pods with its journal.

Leaving LWS for the [vLLM driver](engine.md#vllm):

| Today in `lws.Discover` | Why it is vLLM |
|---|---|
| calls the rank observer `GET :9257/status` | a vLLM process observer that matches vLLM process names |
| one API endpoint per rank at `8000 + local index` | vLLM's one API server per DP rank |
| requires `rank == worker * ranksPerPod + local` | vLLM's DP start-rank layout |
| fills `Endpoint.DiagnosticsURL` | read only by the vLLM driver |
| flags `--engine-api-port`, `--engine-observer-port` | vLLM settings |

## Later: disaggregated sets

A prefill/decode set holds several engine groups with different roles. Proposal: each engine group
is one recovery unit with its own `Group`, journal record, capacity target and controller loop, and
the workload lists the units. This is open decision 1 in the [overview](README.md#open-decisions).
