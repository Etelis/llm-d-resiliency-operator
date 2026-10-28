# Failures

Every failure IRO handles, one page each. Each page shows the failure step by step: the group's Pods and
members, the controller's phase, and selected calls with their input and output. Steps can show parts of a
poll or skip repeated polls; the [poll-by-poll example](member-dies.md#poll-by-poll) labels each poll explicitly.

## How IRO recovers

![Controller phases: starting, serving, recovering](../figures/phases.svg)

1. **Remove in place.** When the engine proves members are gone, one reconfig removes them. The survivors resume serving.
2. **Restore capacity.** Once the group serves, IRO follows the capacity policy. With the default `reset-pod`,
   it resets failed Pods and admits their new members when the engine can grow; otherwise it keeps the Pods.
3. **Reset as the last resort.** When nothing can be removed safely, or a recovery itself fails, IRO resets the
   whole group. It never gives up on its own: the workload's restart policy paces resets.
4. **Act only on what it sees.** IRO removes a member only when the engine reports it dead, and resets a group only
   when it can list its Pods.

A fault always comes first: capacity work waits until the group serves again.

### Engine members

| Failure | IRO sees | Ends with |
|---|---|---|
| [A member dies](member-dies.md) | the engine reports the member terminal; the others pause | serving with fewer members; full again if the engine can grow |
| [Several members die at once](several-members-die.md) | several members terminal at once | serving with fewer members; full again if the engine can grow |
| [Another member dies after a shrink](another-member-dies.md) | a new terminal member while others are already excluded | serving with fewer members; full again if the engine can grow |
| [A transient fault](transient-fault.md) | every member paused, none terminal | the same members, serving |
| [A member hangs](member-hangs.md) | members stop making progress, none terminal; the policy says `Wait` | a new group |
| [A member cannot be read](member-unreadable.md) | an unknown member next to readable ones | a new group |

### Pods

| Failure | IRO sees | Ends with |
|---|---|---|
| [A Pod is lost](pod-lost.md) | `Discover` no longer lists the Pod; its members are added as terminal, `pod lost` | full again if the engine can grow; otherwise fewer members or a new group |
| [The failed Pod cannot come back](capacity-not-restored.md) | `Reset` is unsupported, or the new member cannot join | serving below full until the next group, or a reset with `--ft-capacity-restore=reset-all` |

### Recovery itself

| Failure | IRO sees | Ends with |
|---|---|---|
| [A reconfig fails](reconfig-fails.md) | `Progress` says `Failed` with scope `Group` | a new group |
| [A reconfig never finishes](reconfig-hangs.md) | `Progress` says `Running` past `--ft-apply-timeout` | a new group |
| [The group cannot generate after a recovery](verify-fails.md) | `Verify` keeps failing after a reconfig | a new group |
| [The engine cannot recover in place](unrecoverable.md) | the policy says `Unrecoverable` | a new group |

### IRO itself

| Failure | IRO sees | Ends with |
|---|---|---|
| [The IRO leader dies mid-reconfig](leader-dies.md) | a reconfig in the journal, with or without saved acceptances | the recovery finishes, never sent twice |
| [IRO cannot see the group](iro-blind.md) | `Observe` reads no admitted member | nothing, if reads come back in time; otherwise a new group |
| [The journal is lost](journal-lost.md) | an empty journal | a new group |

The workload can also give up recreating the group (`RecreateStopped`). IRO then waits and acts on
nothing until the workload recreates the group; see [resetting the group](../components/controller.md#resetting-the-group).

## Adding a failure

A failure's stepper is a JSON file in `docs/scenarios`, shown on any page by one line:

```markdown
[example my-failure]: ../scenarios/my-failure.json
```

GitHub hides that line; the site replaces it with the stepper, and with it any figure right above it. The file
lists the Pods once, then the steps; each step names only what changed.

```json
{
  "title": "m0 dies",
  "page": "failures/my-failure",
  "pods": [{"name": "pod-0", "uid": "a1", "members": {"m0": "serving"}}],
  "steps": [
    {"title": "m0 dies", "phase": "recovering",
     "pods": {"pod-0": {"m0": "dead"}},
     "calls": [{"to": "policy", "call": "Plan(...)", "out": "Unrecoverable"}]}
  ]
}
```

| Field | Values |
|---|---|
| `phase` | `starting`, `serving`, `recovering` |
| member state | `serving`, `paused`, `dead`, `excluded`, `starting`, `joining`, `unknown` |
| Pod change | member states, a new `uid`, or `"gone": true` |
| `calls[].to` | `workload`, `driver`, `policy`, `journal`, `telemetry` |
| optional | `page` (where the title links), `note`, `journal` and `internal` (engine state) on a step, `in` and `out` on a call |
| `engines` | wraps `pods` and `steps` per engine; the stepper shows one tab per engine |
| `requests` | on a driver call: HTTP requests made inside it, collapsed by default and toggled by clicking the call heading; a grouped row sends one request to each listed target |
| detailed member | object with `state`, `membership`, `status`, `health`, and optional `ft_state` and `last_ft_request_id`; a step replaces the whole object for that member |
| `membership` | `admitted`, `candidate`, `excluded`, `rejected`, or `untracked`; saved membership is separate from the observed state |
| `decision` | controller logic shown separately from interface calls |

For detailed members, `state` describes IRO's observation: `serving` means Serving, `dead` means Terminal,
`unknown` means Unknown, and `paused` means all three flags are false. `starting` means no member has been
observed. `status` and `health` show the engine response fields behind that observation; omitted fields mean
no response was read. HTTP examples show relevant response fields, and IDs and times are illustrative.
Policy calls consume the observation and make no HTTP requests. Pod UIDs used by workload calls are distinct
from the engine member identities saved in the journal.

Run `cd website && npm test` for frame and request checks. `make test` also checks these examples against the
engine driver and policy.
