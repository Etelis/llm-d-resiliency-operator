# The journal is lost

IRO's saved state is deleted while the group serves or recovers.

- **IRO sees:** an empty journal
- **IRO does:** saves a reset intent, discovers the Pods and resets the group
- **Ends with:** a new group
- **Rules:** [journal](../components/journal.md)

[example journal-lost]: ../scenarios/journal-lost.json
