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
    Load(ctx context.Context) (data []byte, version string, err error)
    Save(ctx context.Context, data []byte, version string) (string, error)
}
```

## Objects

```go
// ErrConflict means the record is not at the version Save was given: another writer changed or deleted it,
// or, for "", it already exists.
var ErrConflict = errors.New("journal conflict")
```

The record itself is opaque `[]byte`. Its content is defined by the controller's [`Record`](controller.md#objects).

## Functions

### Load

Reads the saved record.

| | |
|---|---|
| **Input** | none |
| **Output** | the record bytes and their version, or `nil, ""` when there is no record yet |
| **Errors** | the store cannot be read |
| **Side effects** | none |

**Example.**

```go
Load(ctx) // first run of IRO for this LWS: nil, "", nil. The controller starts a new group.
Load(ctx) // after a leader change: {"phase":"recovering","reconfig":{"id":"iro-ft-7c1e",...},...}, "v42", nil
```

### Save

Writes the record, unless someone else wrote it first.

| | |
|---|---|
| **Input** | the record bytes, and the version they replace: the one from the last `Load` or `Save`, or `""` when `Load` found no record |
| **Output** | the new version. `ErrConflict` when the record is not at that version: changed or deleted by someone else, or, for `""`, already there. Another error otherwise. |
| **Side effects** | the write only |

**Example**, two IRO processes that both think they lead:

```go
// process A and process B both loaded version 41.
A.Save(ctx, recordA, "41") // "42", nil.
B.Save(ctx, recordB, "41") // ErrConflict. B's state is not saved, so B does not act on it. B loads again.
B.Save(ctx, recordB, "")   // ErrConflict: the record exists. "" only creates.
```

## Rules

| Rule | Why |
|---|---|
| The bytes are opaque. The controller owns the schema. | the journal never interprets recovery state |
| `Save` is compare-and-swap on the version it is given. The journal keeps no version of its own. | two leaders can never both win, and a writer that lost one race is not stuck: it loads the current version and goes on |
| A change that leaves the record alone, such as a new annotation, is not a conflict | metadata edits must not stop recovery |
| `Save` never recreates a deleted record. With a version it returns `ErrConflict`; `""` only creates. | a writer that cannot prove its state is current must not write it back. Deleting the record is how a person makes IRO forget the group. |
| The record lives and dies with the workload object | a new workload with the same name starts clean |

## Considerations

- **A failed `Save` stops the step.** The controller never acts on state it could not save. Its phase in memory stays at the last saved one.
- **After `ErrConflict` the controller loads again.** It drops its state and continues from the saved record,
  as after a leader change, deadlines included. If the record was deleted, it starts over as on IRO's first run.
- **A bad record does not stop the manager.** If `Load` fails or the bytes do not parse, the runner logs and tries again on the next poll.
- **Why not part of the workload.** The workload keeps no memory; the journal is all memory. A set with several
  engine groups needs one record per group, and tests can fake the journal alone. The implementation can still
  come from the workload module, because it needs the workload object for the owner reference.
