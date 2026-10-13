# A Pod is lost

A Pod disappears: its node failed or it was deleted. Its members are lost.

- **IRO sees:** `Discover` no longer lists the Pod; its members are added as terminal, `pod lost`
- **IRO does:** removes them in one reconfig, then admits the replacement Pod's members
- **Ends with:** full again, or a new group if the workload recreates the whole group
- **Rules:** [reading Discover](../components/controller.md#reading-discover), [connecting Pods and members](../components/controller.md#connecting-pods-and-members)

[example pod-lost]: ../scenarios/pod-lost.json
