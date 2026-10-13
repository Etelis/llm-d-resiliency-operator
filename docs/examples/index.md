# Examples

End-to-end walkthroughs. Step through each with the buttons, or click it and use the arrow keys. For one page
per failure, see [Failures](../failures/index.md).

| Example | What happens |
|---|---|
| [A group starts](first-start.md) | IRO finds a new group and admits it once every member is up |
| [From a fault back to full](back-to-full.md) | a member dies, the group keeps serving, and is full again |

## Adding an example

An example is a JSON file in `docs/scenarios`, shown on any page by one line:

```markdown
[example my-scenario]: ../scenarios/my-scenario.json
```

GitHub hides that line; the site replaces it with the stepper, and with it any figure right above it. The file
lists the Pods once, then the steps; each step names only what changed.

```json
{
  "title": "m0 dies",
  "page": "failures/my-page",
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
| optional | `page` (where the title links), `note` and `journal` on a step, `in` and `out` on a call |
