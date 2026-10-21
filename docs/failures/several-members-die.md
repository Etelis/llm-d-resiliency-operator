# Several members die at once

Two members die together. One reconfig removes both, and one call resets both Pods.

- **IRO sees:** several members terminal at once
- **IRO does:** one reconfig for all of them, then one `Reset` naming every failed Pod
- **Ends with:** full again without a restart if the engine can grow; otherwise serving with fewer members
- **Rules:** [C7](../components/controller.md#safety-checks), [restoring capacity](../components/controller.md#getting-back-to-full)

[example two-members-die]: ../scenarios/two-members-die.json
