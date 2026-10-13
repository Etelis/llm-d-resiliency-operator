# The failed Pod cannot come back

Resetting the failed Pod does not bring its members back. IRO tries once.

- **IRO sees:** `Reset` is unsupported, or the new member cannot join
- **IRO does:** marks the member rejected and stops trying
- **Ends with:** serving below full until the next group, or a reset with `--ft-capacity-restore=reset-all`
- **Rules:** [restoring capacity](../components/controller.md#getting-back-to-full)

[example capacity-not-restored]: ../scenarios/capacity-not-restored.json
