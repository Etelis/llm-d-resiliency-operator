# Journal

The journal keeps the controller's state for one engine group, so a new leader can pick up where the
last one stopped. It answers "what was IRO doing?", not "which Pods exist" (that is the
[workload](workload.md)).

It stores bytes and never looks inside them. The schema is the controller's
[`Record`](controller.md#objects). Today it is part of `Workload` (`Load`, `Save`); it gets its own
contract so it can be replaced or faked on its own.

## Interface

```go
type Journal interface {
    Load(ctx context.Context) ([]byte, error)
    Save(ctx context.Context, data []byte) error
}
```

## Objects

```go
// ErrConflict means someone else wrote the record since this process last loaded or saved it.
var ErrConflict = errors.New("journal conflict")
```

The record itself is opaque `[]byte`. Its content is defined by the controller's [`Record`](controller.md#objects).

## Functions

### Load

Reads the saved record.

| | |
|---|---|
| **Input** | none |
| **Output** | the record bytes, or `nil, nil` when there is no record yet |
| **Errors** | the store cannot be read |
| **Side effects** | remembers the version it read, for the next `Save` |

**Example.**

```go
Load(ctx) // first run of IRO for this LWS: returns nil, nil. The controller starts a new group.
Load(ctx) // after a leader change: returns {"phase":"recovering","round":{"id":"iro-ft-7c1e",...},...}
```

### Save

Writes the record, unless someone else wrote it first.

| | |
|---|---|
| **Input** | the record bytes |
| **Output** | `nil` when written. `ErrConflict` when another writer changed the record since the last `Load` or `Save`. Another error otherwise. |
| **Side effects** | remembers the new version |

**Example**, two IRO processes that both think they lead:

```go
// process A and process B both loaded version 41.
A.Save(ctx, recordA) // nil. The record is now version 42.
B.Save(ctx, recordB) // ErrConflict. B's state is not saved, so B does not act on it.
```

## Rules

| Rule | Why |
|---|---|
| The bytes are opaque. The controller owns the schema. | the journal never interprets recovery state |
| `Save` is compare-and-swap against the version this process last loaded or saved | two leaders can never both win |
| A change that leaves the record alone, such as a new annotation, is not a conflict | metadata edits must not stop recovery |
| A record deleted while running is recreated from the state being saved | IRO keeps working without a restart |
| The record lives and dies with the workload object | a new workload with the same name starts clean |

## Considerations

- **A failed `Save` stops the step.** The controller never acts on state it could not save. Its phase in memory stays at the last saved one.
- **A bad record does not stop the manager.** If `Load` fails or the bytes do not parse, the runner logs and tries again on the next poll.
- **Why not part of the workload.** The workload keeps no memory; the journal is all memory. A set with several
  engine groups needs one record per group, and tests can fake the journal alone. The implementation can still
  come from the workload module, because it needs the workload object for the owner reference.
