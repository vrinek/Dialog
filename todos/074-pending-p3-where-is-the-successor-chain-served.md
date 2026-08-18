---
status: pending
priority: p3
issue_id: "074"
tags: [transport, specification-gap, key-rotation, subscriptions]
dependencies: []
---

# Where Is the Successor Chain Served?

## Problem Statement

After a rotation, a node learns the successor key and nothing else. The rotation
block carries `new_pub`; `spec/05-processing-model.md`, "Chain succession",
says the node "MUST add the new key [...] to the set of known chains" and
SHOULD auto-subscribe if the user follows the old key's author. The evidence
link runs backwards — the successor's genesis names the rotation block's digest
in its `refs` (`spec/02-block-format.md`, "Verifiable succession") — so
confirming a succession means *fetching the chain of `new_pub`* and checking its
genesis.

Fetching that chain requires knowing where it is served, and nothing anywhere
says. If the answer is "the same place the old chain was", that is a hosting
convention, not a protocol fact, and it fails exactly when it matters most: an
author who rotates keys because their old key or their old host was compromised,
and who moves both at the same moment, disappears from every follower. The
specification's succession machinery is sound and simply never reaches the new
chain.

This is a deliberately-open design question for the `spec/07-transport.md`
drafting phase. Nothing here is decided.

## Findings

- `spec/02-block-format.md`, "rotate_key" / "Verifiable succession": the
  successor genesis MUST list the rotation block's digest in its `refs`; nothing
  in the rotation block points forward at a block that did not exist when it was
  signed.
- `spec/05-processing-model.md`, "Chain succession", steps 2 and 3: add the new
  key to the known chains, auto-subscribe if subscribed to the old — both stated
  as if the chain were already in hand.
- `docs/design/2026-08-18-transport-design.md` §1 R5: the lookup reduces to R1
  (fetch the chain of `new_pub`) plus one predicate, with two wrinkles — the
  ambiguity case, and this one: "a node that has just learned `new_pub` from a
  rotation block has no idea *where* that chain is served".
- `docs/design/2026-08-18-transport-design.md` §4.5: peer and server discovery
  are explicitly out of the v1 sketch — "you learn a server's URL the way you
  learn a podcast feed's: somebody tells you" — which is what makes the
  successor case a real gap rather than an instance of a solved problem.
- The ambiguous-succession half (several genesis blocks claiming one rotation
  block) is `todos/070`'s shape at chain position zero and is tracked there.
- `todos/042` (key succession linkage is only a SHOULD) is the neighbouring
  question about the strength of the link, not about where the successor lives.

## Proposed Solutions

The design document lists two directions and picks neither.

### Option 1: State hosting continuity as a convention

Say in the profile that a node SHOULD look for the successor chain at the same
sources it used for the predecessor, and that an author who rotates SHOULD
publish the successor chain there.

- **Pros**: matches what implementations would do anyway; needs no new field, no
  new block type and no discovery mechanism; makes the assumption explicit
  rather than silent, which is the design document's actual complaint.
- **Cons**: a SHOULD on where an author publishes is a rule about deployments,
  which the specification otherwise avoids; and it fails the compromised-host
  case, which is one of the reasons rotation exists.

### Option 2: Define how a successor's location is discovered

Give the successor's location a defined path — a locator hint carried by the
transport beside the rotation block, a well-known lookup keyed by the successor
key, or a profile-level directory.

- **Pros**: survives an author moving hosts, which Option 1 does not.
- **Cons**: this is peer discovery under another name, deliberately out of scope
  for v1; a locator is unverifiable, though harmlessly so, since what it points
  at is checked against the rotation block's digest.

### Option 3: Say plainly that succession discovery is out of band

Record that a node learns where a successor chain is served the same way it
learned where any chain is served, and that a rotation carries no locator by
design.

- **Pros**: honest and consistent with "transport is out of scope"; the
  succession *evidence* remains verifiable however the blocks arrive.
- **Cons**: leaves auto-subscription (a SHOULD in `spec/05-processing-model.md`)
  depending on a step the specification does not describe.

## Recommended Action

None yet — deliberately. Filed open for the `spec/07-transport.md` drafting
phase. Note that whatever is chosen, the successor's *validity* never depends on
it: the genesis names the rotation block and the evidence is checked in the
bytes. Only reachability is at stake.

## Technical Details

- **Affected Files**: `spec/07-transport.md` (unwritten),
  `spec/05-processing-model.md` ("Chain succession")
- **Related Components**: key rotation, chain succession, subscriptions
- **Database Changes**: No

## Acceptance Criteria

- [ ] The specification says how a node is expected to reach the successor
      chain after a rotation, or states that it is out of band
- [ ] The compromised-host case (author rotates key and host together) is
      addressed or explicitly acknowledged as unhandled
- [ ] Auto-subscription's dependency on that step is stated

## Work Log

### 2026-08-18 - Filed From the Transport Design Document

**By:** Claude

Carried over as-is from
[`docs/design/2026-08-18-transport-design.md`](../docs/design/2026-08-18-transport-design.md)
§5, Q7. No decision taken.

## Notes

Source: `docs/design/2026-08-18-transport-design.md` §5, Q7 (and §1 R5, §4.5).
