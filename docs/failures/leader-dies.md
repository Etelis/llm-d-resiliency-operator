# The IRO leader dies mid-reconfig

The leader dies while a reconfig runs. The new leader finishes it from the journal.

- **IRO sees:** a reconfig in the journal, with or without saved acceptances
- **IRO does:** watches `Progress`; sends the same reconfig again only if no acceptance was saved and nothing reached the engine
- **Ends with:** the recovery finishes, never sent twice
- **Rules:** [reconfigs](../components/controller.md#reconfigs), [journal](../components/journal.md)

[example leader-change]: ../scenarios/leader-change.json
