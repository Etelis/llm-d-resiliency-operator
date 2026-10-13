# Telemetry

Telemetry turns what the controller does into things people can see:

- **logs**: one structured line per event,
- **metrics**: Prometheus, for dashboards and alerts,
- **Kubernetes Events** on the workload object, one series per incident,
- **incident records**: what failed, what IRO decided, how long it took, how it ended,
- **readiness**: whether the leader's loop is still making progress.

It is read by people, Prometheus and `kubectl`, never by IRO. It never blocks or fails recovery.
It is not the [journal](journal.md): the journal is the controller's current state and must be saved
before IRO acts; telemetry is history and is best-effort.

This covers issues [#25](https://github.com/llm-d-incubation/llm-d-resiliency-operator/issues/25)
(Events and metrics) and [#30](https://github.com/llm-d-incubation/llm-d-resiliency-operator/issues/30)
(incident records). Tracing is not part of it yet; see [Considerations](#considerations).

## Interface

```go
type Recorder interface {
    Stepped(record *Record, err error)
    PhaseChanged(from, to Phase, reason string)
    IncidentOpened(incident *Incident)
    ReconfigStarted(reconfig *Reconfig, plan *Plan)
    ReconfigFinished(reconfig *Reconfig, progress Progress)
    PodResetRequested(pods []PodRef, err error)
    ResetAllRequested(reason ResetReason, pods []PodRef)
    ReleaseRequested(pods []PodRef, err error)
    SaveFailed(err error)
    IncidentClosed(incident *Incident)
    Ready() error
}
```

The code is in `internal/controller/enginefault/recorder.go`: the recorder reports controller state, so it
lives next to it. The controller calls the recorder after each thing happens. The recorder decides which signals to emit.
`Ready` is the only call that returns an error, and only the manager's readiness check uses it.

`Record` and `Phase` come from the [controller](controller.md#objects); `Reconfig`, `Plan`, `Progress` and
`MemberID` from the [engine](engine.md#objects); `PodRef` from the [workload](workload.md#objects).

## Objects

```go
// Incident is one fault, from the moment IRO sees it until the group serves again or is reset.
// It opens when the engine reports a fault, when some admitted members cannot be read, or when the workload
// starts or stops recreating a serving group.
type Incident struct {
    ID        string        // e.g. "inc-7c1e"
    Opened    time.Time
    Closed    time.Time     // zero while open
    Cause     string        // the reason the controller gave when it opened the incident
    Affected  []MemberID    // Terminal members when it opened (if none, those not Serving),
                            // plus every member a reconfig removes
    Pods      []PodRef      // their Pods
    Nodes     []string      // their nodes
    Reconfigs    []ReconfigResult // every reconfig sent for it
    Outcome   Outcome       // set when closed
    Durations Durations
}

type ReconfigResult struct {
    Reconfig    Reconfig
    Progress Progress
}

type Outcome string

const (
    Recovered Outcome = "Recovered" // back to serving with the same members
    Shrunk    Outcome = "Shrunk"    // back to serving with fewer members
    Reset     Outcome = "Reset"     // IRO asked the workload to recreate the group
    Recreated Outcome = "Recreated" // the workload recreated the group on its own, e.g. after a container failed
)

// Durations splits an incident into stages.
type Durations struct {
    Diagnose time.Duration // opened -> first reconfig sent, or reset requested
    Apply    time.Duration // reconfigs sent -> their Progress finished
    Verify   time.Duration // verification started -> passed
    Total    time.Duration // opened -> closed
}

// ResetReason is a fixed set, so it can be a metric label.
type ResetReason string // unrecoverable | reconfig_failed | diagnosis_deadline | apply_deadline | flap | verify_failed | exclusion_broken | capacity
```

The controller keeps the **open** incident in its [`Record`](controller.md#objects), so a new leader
continues the same incident and its timings. Closed incidents leave the record and go to telemetry.

## Functions

All examples use the [running example](README.md#running-example). The group label is `ns1/lws`.

### Stepped

Called at the end of every `Step`.

| | |
|---|---|
| **Input** | the controller's [`Record`](controller.md#objects) after the step, and the step's error |
| **Emits** | refreshes the gauges (phase, members, capacity, and how long the controller has not seen the group, from `UnseenSince`). Remembers the time, for `Ready`. Logs the error, if any. A `GroupUnseen` Warning Event when `UnseenSince` is first set. |

**Example.** After poll 6: `llm_d_iro_phase{group="ns1/lws",phase="serving"} 1`,
`llm_d_iro_members{state="admitted"} 3`, `llm_d_iro_members{state="excluded"} 1`.

### PhaseChanged

| | |
|---|---|
| **Input** | the old phase, the new phase, the reason |
| **Emits** | a log line, a `PhaseChanged` Event, `llm_d_iro_phase_transitions_total{from,to}` |

**Example.** `PhaseChanged(serving, recovering, "dp3 is dead; survivors paused")`

### IncidentOpened

| | |
|---|---|
| **Input** | the new [`Incident`](#objects), with `ID`, `Opened`, `Cause`, `Affected`, `Pods`, `Nodes` |
| **Emits** | a log line and an `IncidentOpened` Warning Event. Every later Event of this incident names its ID. |

**Example.** `IncidentOpened({ID: inc-7c1e, Cause: "dp3 is dead", Affected: [dp3@idD], Pods: [lws-0-3], Nodes: [gpu-node-7]})`
→ Event: `inc-7c1e: dp3@idD on lws-0-3 (node gpu-node-7) is dead; recovering`

### ReconfigStarted

| | |
|---|---|
| **Input** | the [`Reconfig`](engine.md#objects) just sent and the [`Plan`](engine.md#objects) it came from |
| **Emits** | a log line and a `ReconfigStarted` Event, again when the reconfig is sent again (reason `resent: no member got it`) |

**Example.** → Event: `inc-7c1e: reconfig iro-ft-7c1e started: shrink, remove dp3@idD`

### ReconfigFinished

| | |
|---|---|
| **Input** | the reconfig and its final [`Progress`](engine.md#objects) |
| **Emits** | a log line, a `ReconfigSucceeded` or `ReconfigFailed` Event, `llm_d_iro_reconfigs_total{change,outcome}`. `NotStarted` is a reconfig dropped because no member got it and it no longer passed the checks: outcome `dropped`. A reconfig ended by a reset or a new group finishes `Running`, with that as its reason. |

**Example.** `ReconfigFinished(iro-ft-7c1e, {Succeeded})` → `llm_d_iro_reconfigs_total{change="shrink",outcome="succeeded"}` +1

### PodResetRequested

| | |
|---|---|
| **Input** | the Pods named in `Reset`, and the workload's answer |
| **Emits** | a log line, a `PodReset` Event and `llm_d_iro_pod_resets_total` (one per Pod), once per call (not every poll). A call answered with `ErrUnsupported` emits only the log line, and the Pods the workload cannot handle stay as they are. A list the workload cannot handle as a whole is not recorded; each Pod is then asked for, and recorded, alone. |

**Example.** `PodResetRequested([{lws-0-2, c1}, {lws-0-3, d1}], nil)` → Event: `resetting lws-0-2, lws-0-3 to bring dp2, dp3 back`

### ResetAllRequested

| | |
|---|---|
| **Input** | a fixed [`ResetReason`](#objects) and the Pods named in `ResetAll` |
| **Emits** | a log line, a `GroupReset` Warning Event, `llm_d_iro_resets_total{reason}` |

**Example.** `ResetAllRequested(reconfig_failed, [lws-0 ... lws-0-3])` → `llm_d_iro_resets_total{reason="reconfig_failed"}` +1

### ReleaseRequested

| | |
|---|---|
| **Input** | the Pods named in `Release`, and the workload's answer |
| **Emits** | a log line and a `PodsReleased` Event, once per request (not every poll). As for `PodResetRequested`, `ErrUnsupported` emits only the log line. |

**Example.** `ReleaseRequested([{lws-0-3, d1}], nil)` → Event: `releasing lws-0-3; serving with 3 members`

### SaveFailed

| | |
|---|---|
| **Input** | the journal error |
| **Emits** | a log line, `llm_d_iro_journal_save_failures_total` |

**Example.** `SaveFailed(ErrConflict)` → `llm_d_iro_journal_save_failures_total{group="ns1/lws"}` +1

### IncidentClosed

| | |
|---|---|
| **Input** | the closed [`Incident`](#objects), with `Outcome` and `Durations` |
| **Emits** | a log line, an `IncidentClosed` Event, `llm_d_iro_incidents_total{outcome}`, one `llm_d_iro_incident_duration_seconds{stage}` observation per stage, and an entry in the [incident history](#incident-history) |

**Example.** → Event: `inc-7c1e: shrunk in 41s (diagnose 4s, apply 22s, verify 15s)`

### Ready

| | |
|---|---|
| **Input** | none |
| **Output** | `nil` when this process is not the leader, or when the leader completed a `Step` recently. An error otherwise. |

**Example.** The last `Stepped` was 45s ago with a 1s poll → `error: no completed step for 45s`. Kubernetes
marks the Pod not ready, so a stuck leader is visible. This replaces today's plain ping.

## Reference

### Metrics

All metrics carry `group` (`<namespace>/<workload name>`). Label values come from fixed sets only.

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `llm_d_iro_leader` | gauge | | 1 on the leader, 0 elsewhere |
| `llm_d_iro_phase` | gauge | `phase` | 1 for the current phase |
| `llm_d_iro_phase_transitions_total` | counter | `from`, `to` | phase changes |
| `llm_d_iro_members` | gauge | `state` | members by state: candidate, admitted, excluded, rejected |
| `llm_d_iro_pod_resets_total` | counter | | Pods IRO reset to bring members back, from calls that returned nil |
| `llm_d_iro_reconfigs_total` | counter | `change`, `outcome` | reconfigs by shape (retry, shrink, grow, replace) and result (succeeded, failed_joiners, failed_group, deadline, dropped) |
| `llm_d_iro_unseen_seconds` | gauge | | how long the controller has not seen the group (`Discover` failed, or no admitted member could be read); 0 when it sees it |
| `llm_d_iro_resets_total` | counter | `reason` | resets IRO asked for, by [`ResetReason`](#objects). Recreations the workload did on its own are not counted here; they close incidents as `Recreated`. |
| `llm_d_iro_incidents_total` | counter | `outcome` | closed incidents by outcome |
| `llm_d_iro_incident_duration_seconds` | histogram | `stage` | diagnose, apply, verify, total |
| `llm_d_iro_journal_save_failures_total` | counter | | failed journal saves |
| `llm_d_iro_last_verified_timestamp_seconds` | gauge | | when the last reconfig's members passed `Verify` |

Alerts this allows: recovering for more than N minutes, below full for more than N minutes,
the group unseen for more than N minutes, journal saves failing, no leader.

### Events

On the workload object (the LWS). Every Event of an incident starts with its ID.

| Reason | Type | When |
|---|---|---|
| `IncidentOpened` | Warning | a fault opens an incident |
| `PhaseChanged` | Normal | any phase change |
| `ReconfigStarted` | Normal | a reconfig is sent |
| `ReconfigSucceeded` / `ReconfigFailed` | Normal / Warning | a reconfig finishes |
| `PodReset` | Normal | IRO resets Pods to bring their members back |
| `PodsReleased` | Normal | IRO releases Pods and serves below full |
| `GroupReset` | Warning | IRO asks the workload to recreate the group |
| `RecreateStopped` | Warning | the workload reports it gave up recreating the group |
| `GroupUnseen` | Warning | the controller can no longer see the group: `Discover` fails, or no admitted member can be read |
| `IncidentClosed` | Normal | the group serves again, or was reset |

### Incident history

The last 20 closed incidents, newest first, in ConfigMap `<lws-name>-ft-incidents`, owned by the LWS.
Written best-effort: a failed write is logged and dropped. It is separate from the journal, so the
journal stays small and its saves stay critical-path only.

## Rules

| Rule | Why |
|---|---|
| Never returns an error to the controller, except `Ready` | telemetry must not change what IRO does |
| Never blocks. Metrics are in memory; Events and history writes are asynchronous. | a slow API server must not delay a reconfig or a reset |
| IRO never reads telemetry back | decisions come from the workload, the engine and the journal only |
| Metric labels come from fixed sets: no member IDs, Pod names or free text | bounded cardinality. IDs and names go in Events and logs. |
| Names follow llm-d: `llm_d_iro_*` | consistent with `llm_d_epp_*` and the rest of llm-d |
| Each incident's Events and history entry carry its ID | one incident can be followed end to end |

## Considerations

- **Why a `Recorder` interface.** Recovery logic says what happened, once. The recorder fans it out to
  logs, metrics and Events. Tests assert the exact calls without Prometheus or a cluster.
- **Why not the journal.** The journal must succeed before IRO acts and keeps only the current state.
  Telemetry is history and must never block. Putting history in the journal would make every save bigger
  and every telemetry hiccup a recovery problem.
- **Detection time.** Issue #30 lists a detect stage. Measuring it needs the time the engine saw the fault,
  and `Member` has no timestamp yet. Until it does, `Diagnose` starts when IRO opens the incident.
- **Fault kinds.** Issue #30 lists kinds such as rank death, collective stall and frontend loss. Today the
  incident's `Cause` is the policy's free-text reason. A fixed `Plan.Fault` kind from the engine would let
  incidents be counted by kind.
- **Tracing.** Not now. An OpenTelemetry trace per incident, following llm-d's tracing conventions, is
  proposed on [#30](https://github.com/llm-d-incubation/llm-d-resiliency-operator/issues/30). It would
  use the same incident boundaries.

## Implementation

| Signal | How |
|---|---|
| Logs | the controller-runtime logger from the context, with `group`, `incident`, `reconfig` and `phase` keys |
| Metrics | the controller-runtime metrics registry, served on the manager's `/metrics` |
| Events | the manager's `EventRecorder`, on the LWS |
| Incident history | ConfigMap `<lws-name>-ft-incidents`, owned by the LWS |
| Readiness | a `readyz` check that calls `Ready` |

**Today** IRO logs errors only. It has no IRO metrics, no Events, and its readiness check is a plain ping.
