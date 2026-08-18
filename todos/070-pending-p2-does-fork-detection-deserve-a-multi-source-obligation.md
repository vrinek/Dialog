---
status: pending
priority: p2
issue_id: "070"
tags: [transport, specification-gap, block-validation, security, subscriptions]
dependencies: []
---

# Does Fork Detection Deserve a Multi-Source Obligation?

## Problem Statement

`spec/02-block-format.md`, validation rule 9, is normative:

> If a node receives a block whose `prev` value matches the `prev` of another
> block already stored from the same `pub` key, the node MUST detect this as a
> chain fork.

The rule fires only on a node that *holds both blocks*. A node that syncs a
chain from one server, and is served one side of a fork, satisfies rule 9
vacuously and forever — and so does a node whose only source is an honest server
that happens to have seen one branch. The same shape appears again for
succession: "If a node holds more than one genesis block referencing the same
rotation block, the succession is ambiguous and the node MUST surface the
conflict" (`spec/02-block-format.md`, "rotate_key").

So a MUST that reads like a guarantee is in fact a property of the node's reach,
and the specification says nothing about reach. A server serving a fork has
every incentive to serve one side of it, which makes the gap adversarial rather
than merely theoretical.

This is a deliberately-open design question for the `spec/07-transport.md`
drafting phase. Nothing here is decided.

## Findings

- `spec/02-block-format.md`, validation rule 9, and "rotate_key" (ambiguous
  succession): both conditioned on holding two blocks.
- `spec/05-processing-model.md`, "Fork handling": what a node does once it has
  detected a fork is local policy; *whether* it detects one is not addressed.
- `docs/design/2026-08-18-transport-design.md` §1 R3: "a transport that cannot
  express 'are there other blocks with this `prev`?' reduces a normative MUST to
  a formality", and the observation that multi-source sync is strictly stronger
  than a sibling query, because the query is answered by the party with the
  motive to lie.
- `docs/design/2026-08-18-transport-design.md` §4.4: the sketch's multi-source
  rule, stated as a client discipline needing no wire support — every response
  is self-verifying and stateless, so "ask two servers" is the same code twice.
- `docs/design/2026-08-18-transport-design.md` §3, "What to build first": a
  client that syncs a chain from two sources and surfaces a fork when they
  disagree is named as the deliverable that would show whether rule 9 is a real
  rule.
- Related but distinct: `todos/006` (fork detection missing) concerns the rule's
  own statement, not the reach it depends on.

## Proposed Solutions

The design document lists two directions and picks neither.

### Option 1: Add a multi-source SHOULD

State in `spec/05-processing-model.md` (or in the transport profile) that a node
SHOULD obtain each subscribed chain from more than one source, and that a source
is anything independent — two servers, a server and a file, a friend's copy.

- **Pros**: makes rule 9 reachable in practice; needs no wire feature; also
  blunts the subscription-privacy leak, since a node splitting chains across
  servers hands no single party its whole interest set (see `todos/073`).
- **Cons**: a SHOULD about a node's deployment, not its data handling, which is
  a new kind of requirement here; unenforceable and untestable by vectors; a
  single-server user is now nominally non-conforming for a reason they cannot
  fix alone.

### Option 2: Say plainly that fork detection is best-effort

Add to the security considerations that fork detection is bounded by what a node
receives, that a single-source node cannot detect withholding, and that rule 9
is therefore a rule about what a node does with what it holds, not a guarantee
about what it will see.

- **Pros**: honest, and matches what the protocol can actually promise; no new
  obligation on deployments.
- **Cons**: leaves the practical gap unaddressed; a reader who wants fork
  detection to work gets no guidance on how to get it.

### Option 3: Both

The two are not exclusive: state the limit in the security considerations and
recommend multi-source sync as the mitigation.

- **Pros**: the honest statement and the actionable advice are different
  sentences and both are true.
- **Cons**: more text; the SHOULD's testability problem remains.

## Recommended Action

None yet — deliberately. Filed open for the `spec/07-transport.md` drafting
phase. Note that the current silence is the one option that is definitely wrong:
it reads as a guarantee the protocol does not make.

## Technical Details

- **Affected Files**: `spec/07-transport.md` (unwritten),
  `spec/05-processing-model.md` (chain management, security considerations),
  `spec/02-block-format.md` (rule 9's surrounding prose)
- **Related Components**: L1 chain sync, fork handling, key succession
- **Database Changes**: No

## Acceptance Criteria

- [ ] The specification states whether a node is expected to sync a chain from
      more than one source
- [ ] The security considerations say what fork detection can and cannot
      promise on a single-source node
- [ ] The same treatment covers ambiguous succession, which has the same shape

## Work Log

### 2026-08-18 - Filed From the Transport Design Document

**By:** Claude

Carried over as-is from
[`docs/design/2026-08-18-transport-design.md`](../docs/design/2026-08-18-transport-design.md)
§5, Q3. No decision taken.

## Notes

Source: `docs/design/2026-08-18-transport-design.md` §5, Q3 (and §1 R3, §4.4).
