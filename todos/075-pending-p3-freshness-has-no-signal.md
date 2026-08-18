---
status: pending
priority: p3
issue_id: "075"
tags: [transport, specification-gap, security, block-format, subscriptions]
dependencies: []
---

# Freshness Has No Signal

## Problem Statement

Nothing in Dialog distinguishes "this is the author's tip" from "this is the tip
I am willing to show you". A block is self-authenticating — signature, author
key, chain link and content address are all in the bytes — so a source cannot
lie about a block's *contents*. It can lie by omission, and withholding the last
*n* blocks of a chain is the cheapest such lie: every block the client does
receive verifies, the chain is intact, and the result is a silently stale view
of an author.

No block carries trustworthy time. `spec/02-block-format.md` states that
timestamps are self-reported and untrusted, so a client cannot even bound how
old the tip it holds might be. And a signed tip attestation — the obvious fix —
would be a new signed object, which v1 does not have room for: the block type
set is closed, and an attestation is not a block.

The design document classes this with completeness as one of the two things a
transport cannot give: "**freshness** (is this the tip?) — unverifiable at all".

This is a deliberately-open design question for the `spec/07-transport.md`
drafting phase. Nothing here is decided.

## Findings

- `spec/02-block-format.md`, Security Considerations: "Timestamps are
  self-reported and untrusted. Implementations MUST NOT use timestamps for
  validation decisions." `todos/016` (timestamp monotonicity) covers what little
  ordering they carry.
- `spec/02-block-format.md`, the `type` field: the set is closed — `"public"`,
  `"private"`, `"rotation"` — and every signed artifact in the protocol is a
  block.
  There is no signed object a tip attestation could be without extending that
  set.
- `docs/design/2026-08-18-transport-design.md` §1 R0: the two trust properties a
  transport cannot supply are completeness ("unverifiable from one source",
  tracked in `todos/070`) and freshness ("unverifiable at all").
- `docs/design/2026-08-18-transport-design.md` §4.2: within a range,
  completeness is free — a client walks `prev` from the first block it receives
  and sees any skip immediately. Between the last block and the real tip it is
  not: "a server that withholds the tip is indistinguishable from an author who
  stopped publishing."
- `docs/design/2026-08-18-transport-design.md` §2.8: AT Protocol's `getRecord`
  returns a Merkle path proving existence *or non-existence* against a signed
  root, and the tlog-tiles family serves an append-only log as static
  CDN-cacheable files with client-computed proofs. Dialog has no commitment
  structure that could support such a proof today — worth knowing the technique
  exists before concluding the gap is unfixable.
- Multi-source sync (`todos/070`) helps here too without solving it: two sources
  disagreeing about a tip proves one is behind, but two sources agreeing proves
  nothing.

## Proposed Solutions

The design document lists two directions and picks neither.

### Option 1: Accept it as unfixable in v1 and record it

State in the security considerations that a node cannot verify it holds an
author's current tip, that a stale view is indistinguishable from an author who
stopped publishing, and that no transport removes this.

- **Pros**: true, costs nothing, and stops a reader assuming a guarantee; pairs
  naturally with the completeness statement `todos/070` may add.
- **Cons**: a real property of the protocol is left with only a warning.

### Option 2: Defer to a version that can add a signed object

Record the gap and note that a signed tip attestation — author key, tip digest,
a time — would close the "is this the tip" question at the cost of a new signed
object and a new signing context, and is therefore post-v1.

- **Pros**: keeps the door open and names the shape of the fix; the design
  document's §2.8 pointers say where the prior art is.
- **Cons**: an attestation carries a self-reported time like everything else, so
  it bounds staleness only against a signer the client already trusts to be
  online; it also gives an author a way to be *forced* to attest, which has its
  own privacy shape.

### Option 3: Mitigations without a new object

Note what is available cheaply: multi-source tip comparison, an author
publishing a heartbeat block, and the observation that a client that has ever
seen a later tip can detect a source serving an earlier one.

- **Pros**: usable today with no format change; the last point is a real
  monotonicity check a client can keep locally per source.
- **Cons**: none of them detects a first contact with a source that has been
  withholding from the start.

## Recommended Action

None yet — deliberately. Filed open for the `spec/07-transport.md` drafting
phase. Options 1 and 3 are compatible and cost nothing; Option 2 is the only one
that changes the protocol and the only one that would actually close the gap.

## Technical Details

- **Affected Files**: `spec/07-transport.md` (unwritten),
  `spec/05-processing-model.md` (Security Considerations), potentially
  `spec/02-block-format.md` and `spec/04-cryptography.md` if a signed object is
  ever added
- **Related Components**: tip discovery, subscriptions, security considerations
- **Database Changes**: No

## Acceptance Criteria

- [ ] The security considerations state that tip freshness is unverifiable and
      what that means for a subscriber
- [ ] The specification says whether a signed tip attestation is deferred or
      ruled out
- [ ] Any cheap mitigations a client can apply locally are written down

## Work Log

### 2026-08-18 - Filed From the Transport Design Document

**By:** Claude

Carried over as-is from
[`docs/design/2026-08-18-transport-design.md`](../docs/design/2026-08-18-transport-design.md)
§5, Q8. No decision taken.

## Notes

Source: `docs/design/2026-08-18-transport-design.md` §5, Q8 (and §1 R0, §4.2,
§2.8).
