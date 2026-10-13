# Several members die at once

Two members die together. One reconfig removes both, and one call resets both Pods.

- **IRO sees:** several members terminal at once
- **IRO does:** one reconfig for all of them, then one `Reset` naming every failed Pod
- **Ends with:** full again, without a restart
- **Rules:** [C7](../components/controller.md#checks-on-every-plan), [restoring capacity](../components/controller.md#restoring-capacity)

[example two-members-die]: ../scenarios/two-members-die.json
