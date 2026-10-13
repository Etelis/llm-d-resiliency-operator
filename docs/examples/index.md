---
title: Examples
sidebar_position: 1
---

# Examples

Each example walks one fault from start to end. Step through it with the buttons or the arrow keys: every step
shows the group's Pods and members, the controller's phase, and each call IRO makes with its input and output.

All examples use the same group: four Pods, `pod-0` to `pod-3`, one engine member each, `m0` to `m3`.

| Example | What happens |
|---|---|
| [A member dies](member-dies.mdx) | One member dies. The rest keep serving, and the group is full again once the failed Pod is reset. |
| [Two members die](two-members-die.mdx) | Two members die at once. One reconfig removes both, and one reset brings both Pods back. |
| [A transient fault](transient-fault.mdx) | A step fails but nobody dies. IRO retries in place and no member is lost. |
| [The leader dies mid-reconfig](leader-change.mdx) | The IRO leader dies while a reconfig runs. The new leader finishes it from the journal without sending it twice. |
| [IRO cannot see the group](controller-blind.mdx) | IRO loses sight of every member. It waits, and resets only after --ft-unseen-timeout. |
| [A reconfig fails](reconfig-fails.mdx) | The engine cannot complete a shrink. IRO resets the whole group. |

## Adding an example

An example is a JSON file next to these pages, plus a three-line page that shows it. The file lists the Pods
once, then the steps; each step names only what changed.

```json
{
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
| optional | `note` and `journal` on a step, `in` and `out` on a call |
