# A member hangs

A member stops making progress without dying. Nothing proves it is gone, so nothing can be removed.

- **IRO sees:** paused members, none terminal; the policy says `Wait`
- **IRO does:** waits for the engine, then resets the group when `--ft-diagnosis-timeout` passes
- **Ends with:** a new group
- **Rules:** [C1](../components/controller.md#checks-on-every-plan), [reconfigs](../components/controller.md#reconfigs)

[example member-hangs]: ../scenarios/member-hangs.json
