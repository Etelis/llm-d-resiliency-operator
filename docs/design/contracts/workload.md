# Workload

The workload owns the Pods of one engine group. It does four things:

- **reports** the group's Pods as they are right now,
- **resets one Pod**: replaces it with a new Pod in the same place, while the rest of the group keeps running,
- **resets all**: recreates the whole group,
- **releases** Pods: removes them and frees their GPUs. It never adds Pods.

It remembers nothing: no earlier calls, no earlier groups. Deciding whether this is still the same group
is the [controller's](controller.md#reading-discover) job. It knows nothing about engines: no ranks, ports
or processes. The first implementation, LWS, comes with its own PR and doc.

## Interface

```go
type Workload interface {
    Discover(ctx context.Context) (Group, error)
    Reset(ctx context.Context, pod PodRef) error
    ResetAll(ctx context.Context, pods []PodRef) error
    Release(ctx context.Context, pods []PodRef) error
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

**Example**, while `lws-0-3` is reset in the [running example](README.md#running-example):

```yaml
Recreating: false
RecreateStopped: false
Pods:
- {Name: lws-0,   UID: a1, Index: 0, Identity: idA,  Node: gpu-node-1, State: Ready}
- {Name: lws-0-1, UID: b1, Index: 1, Identity: idB,  Node: gpu-node-2, State: Ready}
- {Name: lws-0-2, UID: c1, Index: 2, Identity: idC,  Node: gpu-node-3, State: Ready}
- {Name: lws-0-3, UID: d2, Index: 3, Identity: idD2, Node: gpu-node-9, State: Starting}
```

`lws-0-3` has a new UID: it is a new Pod in the same place. `Discover` does not say that the old one
was reset on purpose. The controller knows that from its journal.

### Reset

Replaces one Pod with a new Pod in the same place. The rest of the group keeps running.

| | |
|---|---|
| **Input** | `pod PodRef`: the Pod to replace, as the controller last saw it |
| **Output** | `nil` once the replacement is triggered, or once that Pod (by UID) no longer exists. `ErrUnsupported` when the workload cannot replace one Pod without recreating the group, with nothing changed. Another error otherwise. |
| **Side effects** | the old Pod goes. A new Pod with the same `Index` and a new UID starts. |

**Example.**

```go
Reset(ctx, PodRef{"lws-0-3", "d1"})
// a workload that supports it: lws-0-3 (d1) is deleted and comes back as lws-0-3 (d2). Returns nil.
// the same call again: no Pod with UID d1 exists. Returns nil, changes nothing.
// a workload that cannot: returns ErrUnsupported, changes nothing.
```

### ResetAll

Recreates the group that the named Pods belong to.

| | |
|---|---|
| **Input** | `pods []PodRef`: the Pods of the group to recreate, as the controller last saw them |
| **Output** | `nil` once the recreation is triggered, or once none of the named Pods exist. An error otherwise. |
| **Side effects** | the whole group is replaced by new Pods, when the workload's own restart policy allows it |

**Example.**

```go
ResetAll(ctx, []PodRef{{"lws-0", "a1"}, {"lws-0-1", "b1"}, {"lws-0-2", "c1"}, {"lws-0-3", "d1"}})
// first call: LWS deletes lws-0 (UID a1). LWS recreates the group. Returns nil.
// after the new group exists: lws-0 now has UID f1, so no named Pod exists. Returns nil, changes nothing.
```

The second call is why the controller can repeat `ResetAll` until the old Pods are gone: it can never
touch the new group.

### Release

Removes the named Pods and frees their GPUs. Nothing is added in their place.

| | |
|---|---|
| **Input** | `pods []PodRef`: the Pods to remove |
| **Output** | `nil` once the removal is triggered, or once none of the named Pods exist. `ErrUnsupported` when the workload cannot remove Pods from a running group, with nothing changed. Another error otherwise. |
| **Side effects** | the named Pods go, and the group gets smaller |

**Example.**

```go
Release(ctx, []PodRef{{"lws-0-3", "d1"}})
// a workload that supports it: lws-0-3 starts terminating and is not replaced. Returns nil.
// the same call again: no Pod with UID d1 exists. Returns nil, changes nothing.
// a workload that cannot: returns ErrUnsupported, changes nothing.
```

## Rules

| Rule | Why |
|---|---|
| No memory. Every answer comes from the cluster as it is now. | a restarted IRO or a new leader gets the same answers |
| `Discover` fails only when it cannot read the workload, or its spec is not supported | the controller should see starting and terminating Pods, not lose sight of the group |
| `Pods` are sorted by `Index`, and a Pod keeps its `Index` for life | stable addressing for the engine driver |
| `Pod.Identity` changes when any container restarts | a restarted process is never mistaken for the old one |
| `Reset`, `ResetAll` and `Release` touch only the Pods they name, by UID | a late call can never hit a new Pod |
| All three can be repeated: once the named Pods are gone, they do nothing | the controller repeats them until `Discover` shows the result |
| `Reset` never recreates the rest of the group. If it would have to, it returns `ErrUnsupported`. | the healthy members keep serving |
| `Release` never adds a Pod | capacity comes back only through a reset |
| A workload that cannot do something changes nothing and returns `ErrUnsupported` | the controller keeps the Pod as it is, and the group serves with fewer members |
| No engine knowledge: ranks, ports, processes, endpoints | that belongs to the [engine driver](engine.md#driver) |

## Considerations

- **Who paces resets.** The workload paces recreations and decides when to stop, including ones IRO asks for.
  IRO has no reset budget of its own. A workload that stopped says so in `RecreateStopped`.
- **Same place, maybe not the same node.** `Reset` keeps the Pod's `Index`, but the scheduler may put the new
  Pod on the same node. If the node is the problem, the new Pod fails too, the join fails, and the group
  serves with fewer members until the next group. Moving work off a bad node is outside this contract.
- **Leaders.** Some workloads cannot replace or remove their leader without recreating the group. They return
  `ErrUnsupported` for that Pod, and the controller keeps it as it is.
- **Report `Recreating` as early as possible.** A workload that will recreate the group because of what it
  sees (for example, any container restart, when its policy recreates the group for that) reports `Recreating` right away,
  not only once the leader is deleted. Otherwise the controller may plan a round for a group that is about to go.
- **Recreating vs. stopped.** `Recreating` is temporary: the controller waits. `RecreateStopped` is
  the workload saying it gave up: the controller waits too, and telemetry reports it.

## Later: disaggregated sets

A prefill/decode set holds several engine groups with different roles. Proposal: each engine group
is one recovery unit with its own `Group`, journal record and controller loop, and the workload lists
the units. This is open decision 1 in the [overview](README.md#open-decisions).
