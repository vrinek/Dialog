---
status: pending
priority: p3
issue_id: "071"
tags: [transport, specification-gap, processing-model, private-blocks, chain-integrity]
dependencies: []
---

# A Serving Node's Obligations Versus a Storing Node's

## Problem Statement

`spec/05-processing-model.md`, "Public/private reference rules", permits a node
to discard what it cannot read:

> Non-recipient nodes (those without the decryption key) MAY safely drop private
> blocks they cannot decrypt

"Safely" is true of a node's own store: a private block it cannot open
contributes nothing to its L2, and dropping it costs that node only the ability
to follow the chain past it. It is not true of a node that *serves* the chain to
somebody else. A chain with a private block removed is a chain with a hole, and
at the client every block after the hole fails validation rule 3 — "If `prev` is
not null, it MUST reference a block the node holds and has accepted as valid"
(`spec/02-block-format.md`). One node exercising a MAY silently truncates every
downstream node's view of that author at the first private block.

The specification has one concept of "node" and therefore cannot express the
difference. It also gives a client no way to distinguish "the chain ends here"
from "this source is not giving me everything", which is the same distinction
seen from the other end.

This is a deliberately-open design question for the `spec/07-transport.md`
drafting phase. Nothing here is decided.

## Findings

- `spec/05-processing-model.md`, "Public/private reference rules", third bullet:
  the permission, stated without a scope.
- `spec/02-block-format.md`, validation rule 3: a block whose predecessor is
  absent is stored but unvalidated — so a hole is not an error the client can
  attribute, it is silence.
- `spec/02-block-format.md`, "Private block": `v`, `type`, `pub`, `sig` and
  `prev` stay in plaintext, so a node that cannot decrypt a block can still
  store it, chain it and hand it on verbatim. Retention costs it storage and
  nothing else — the block is opaque bytes with a verifiable signature.
- `docs/design/2026-08-18-transport-design.md` §1 R7: "a serving profile should
  say plainly that a server retains and serves opaque blocks it cannot read, and
  that a client must be able to tell 'the chain ends here' from 'this server is
  not giving me everything'".
- `docs/design/2026-08-18-transport-design.md` §1 R7, first bullet: a relay
  cannot index a private block's contents, so a digest-keyed "who defines entity
  X" index can only ever cover public and rotation blocks — a separate
  consequence of the same opacity, and one that bears on `todos/072`.
- `todos/051` (foreign private chains and subscription) covers the neighbouring
  question of what a node does with a private chain it follows but cannot read.

## Proposed Solutions

The design document asks whether storage policy and serving policy should be
separated, and proposes no wording.

### Option 1: Separate the two policies in `spec/05-processing-model.md`

Scope the existing MAY to a node's own store, and add that a node acting as a
source for others SHOULD (or MUST) retain and serve blocks it cannot decrypt,
because the permission to drop is a permission about reading, not about
relaying.

- **Pros**: fixes the rule where it is stated, so it holds for every transport;
  costs a server nothing but disk.
- **Cons**: introduces a role ("serving node") the processing model does not
  currently have, and roles tend to multiply.

### Option 2: Put it in the transport profile only

Leave `spec/05-processing-model.md` alone and make retention a requirement on a
conforming server in `spec/07-transport.md`.

- **Pros**: keeps the role where it belongs — a server is a transport concept —
  and keeps the processing model about a single node.
- **Cons**: a non-conforming or profile-less server is unconstrained, and the
  hazard is not specific to any one transport.

### Option 3: Make the hole detectable instead of forbidding it

Say nothing about retention and instead give the client a signal: a source that
declines to serve a block it holds, or does not hold one, answers in a way that
is distinguishable from "no such block". The design sketch's "404 means 'I do
not have it', never 'it does not exist'" (§4.2) is the beginning of this.

- **Pros**: attacks the client-side half, which no retention rule can fix — a
  server that never had the block is indistinguishable from one that dropped it
  unless the answer says so.
- **Cons**: a signal a lying server can simply not send; works against
  carelessness, not malice.

## Recommended Action

None yet — deliberately. Filed open for the `spec/07-transport.md` drafting
phase. Options 1/2 and Option 3 address different halves and may both be
needed.

## Technical Details

- **Affected Files**: `spec/05-processing-model.md` ("Public/private reference
  rules"), `spec/07-transport.md` (unwritten)
- **Related Components**: L1 storage, private block handling, chain serving
- **Database Changes**: No

## Acceptance Criteria

- [ ] The specification says whether the permission to drop undecryptable
      private blocks applies to a node that serves the chain to others
- [ ] A client can distinguish a chain that ends from a source that is
      withholding, or the specification says plainly that it cannot
- [ ] The wording does not require a server to be able to read what it serves

## Work Log

### 2026-08-18 - Filed From the Transport Design Document

**By:** Claude

Carried over as-is from
[`docs/design/2026-08-18-transport-design.md`](../docs/design/2026-08-18-transport-design.md)
§5, Q4. No decision taken.

## Notes

Source: `docs/design/2026-08-18-transport-design.md` §5, Q4 (and §1 R7).
