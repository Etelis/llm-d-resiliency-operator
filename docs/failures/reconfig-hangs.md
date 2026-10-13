# A reconfig never finishes

A reconfig keeps running.

- **IRO sees:** `Progress` says `Running` past `--ft-apply-timeout`
- **IRO does:** resets the whole group
- **Ends with:** a new group
- **Rules:** [reconfigs](../components/controller.md#reconfigs)

[example reconfig-hangs]: ../scenarios/reconfig-hangs.json
