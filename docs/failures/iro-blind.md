# IRO cannot see the group

IRO cannot read any member. The group may be fine; IRO may be the one cut off.

- **IRO sees:** `Observe` reads no admitted member
- **IRO does:** waits with deadlines paused, and resets only after `--ft-unseen-timeout`
- **Ends with:** nothing, if reads come back in time; otherwise a new group
- **Rules:** [unknown members](../components/controller.md#members-it-cannot-read)

[example controller-blind]: ../scenarios/controller-blind.json
