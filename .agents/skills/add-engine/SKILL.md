---
name: add-engine
description: Add support for an inference engine, as a new engine plugin (driver and policy). Use when IRO must recover a new engine, or when changing how an engine is read or reconfigured.
---

# Add an engine

An engine plugin is everything IRO knows about one inference engine. The controller never changes for a new
engine. Read `internal/engine/engine.go` and `docs/components/engines/index.md` first, then the closest existing
engine under `internal/engine/<name>/` and its page, and follow their structure.

## Before writing code

Find out, from the engine's own code or docs, how it:

1. reports each member's state: serving, dead for good, still deciding, unreadable,
2. tells a member to retry, to drop dead peers, or to admit new ones,
3. reports which recovery request a member is working on, and whether it finished,
4. can prove that no member received a request (needed for `NotStarted`; if it cannot, never return it),
5. names its members within a Pod.

If the engine cannot report a member as dead for good, IRO can never remove it in place: only reset the group.

## Write it

Under `internal/engine/<name>/`:

- **Driver**: `Observe`, `Probe`, `Apply`, `Verify`. Every call bounded by a timeout. No memory between calls:
  anything needed later goes in `Observation.Engine`. A member it cannot read is `Unknown`, never guessed
  `Terminal`. `Apply` refuses a reconfig naming a member missing from the observation, and the same reconfig ID is
  safe to send twice.
- **Policy**: `Plan`, `Progress`. Pure: no I/O, no clock. `Wait` only while the engine is still diagnosing.
  `Add` only if `Capabilities().Grow`.
- **Options**: the engine's own flags, registered through its factory.

Register the factory in `cmd/main.go`.

## Test it

- Driver tests against a fake engine API: each member state, a refused apply, a resend of the same ID.
- Policy tests from recorded observations: every plan kind and every progress state.
- A flow test that drives a full recovery through the controller with the fake engine.

## Document it

In the docs PR: a page `docs/components/engines/<name>.md` (what the engine gives, driver, policy, flags, how to
deploy it for IRO, known engine issues). The general engine page stays engine-neutral.
