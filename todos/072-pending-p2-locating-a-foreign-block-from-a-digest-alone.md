---
status: pending
priority: p2
issue_id: "072"
tags: [transport, specification-gap, refs, content-addressing, foreign-chains]
dependencies: []
---

# Locating a Foreign Block From a Digest Alone

## Problem Statement

A `refs` entry is a bare 32-byte digest. `spec/03-encoding.md`, "Internal
references", fixes that: every reference inside a Dialog structure is the raw
SHA-256 digest, carrying no author, no chain hint and no locator. Resolution,
however, is a *fetch*: `spec/05-processing-model.md`, "Resolution procedure",
step 4, says "fetch blocks listed in H's `refs`", and step 5 recurses into those
blocks' own `refs`.

So the one operation the specification asks a transport to perform is
content-addressed lookup in a space with no routing information — materially
harder than "fetch author P's chain", and on the validation path, bounded by the
scan limit of 256 distinct foreign blocks per block validated. A node that
cannot locate a referenced block cannot validate the block that references it
(rule 4), so this is not a convenience gap: it decides validity.

The design document calls this "the requirement that comes nearest to justifying
a change to the block format", which is why it should be answered before a
profile ships rather than after.

This is a deliberately-open design question for the `spec/07-transport.md`
drafting phase. Nothing here is decided.

## Findings

- `spec/03-encoding.md`, "Internal references": `refs` entries are 32-byte
  digests; the CDDL's `bstr .size 32` excludes anything larger, so no locator
  fits in the field as specified.
- `spec/05-processing-model.md`, "Resolution procedure", steps 4 and 5:
  demand-driven fetch and recursion into referenced blocks' `refs`.
- `spec/05-processing-model.md`, "Scan limit": 256 distinct foreign blocks by
  default; up to that many lookups sit on one block's validation path, which is
  a latency budget as much as a safety bound (`todos/060` settled the number and
  the counting unit).
- `spec/02-block-format.md`, validation rule 4: unreachable digests make the
  referencing block invalid, so a lookup failure and a forged reference are
  indistinguishable to the validator.
- `docs/design/2026-08-18-transport-design.md` §1 R4, first bullet: the digest
  is "the key and nothing else", named as the single strongest argument for
  either a content-addressed substrate or an out-of-band author hint.
- `docs/design/2026-08-18-transport-design.md` §1 R7: a private block's contents
  cannot be indexed, so any digest→block index a server builds covers its public
  and rotation blocks only. A reference into a private block is resolvable only
  by a node that holds the block already.
- `docs/design/2026-08-18-transport-design.md` §2.1 and §2.7: a DHT answers this
  question directly and is the option with the worst subscription-privacy
  properties on the survey, which is why the answer is not simply "use a DHT".
- `todos/009` (reachability and foreign loading model) settled what reachability
  means; where the bytes come from was out of its scope.

## Proposed Solutions

The design document lists three directions and picks none.

### Option 1: A server-side digest index

A server indexes the digests of the public and rotation blocks it holds and
answers `GET /blocks/{digest}` for any of them, whoever authored them.

- **Pros**: no format change; matches the sketch's `/blocks/{CID}` endpoint;
  cacheable forever, since blocks are immutable.
- **Cons**: only works within one server's held set — a reference to a block no
  server you use holds is unresolvable; covers no private block; and the index
  is exactly the structure that makes a server's holdings enumerable.

### Option 2: An out-of-band author hint carried by the transport

The reference stays a bare digest on the wire inside blocks; the transport
carries an optional hint beside it — the author key, or a server URL — so a
client knows where to ask.

- **Pros**: keeps block bytes and every existing digest unchanged; a hint is
  advisory and unverifiable, which is harmless because the fetched block is
  self-authenticating and its digest is checked.
- **Cons**: needs somewhere to put the hint that is not the block, so the
  transport grows a parallel structure; a hint is absent exactly when a block
  travels as a file, which is the case R8 protects.

### Option 3: Accept that resolution works within a server's held set

State plainly that demand-driven resolution succeeds when the referenced block
is within reach of the node's configured sources and fails otherwise, and that
"fails" means the referencing block stays unvalidated rather than invalid.

- **Pros**: honest and needs nothing built; matches how the demo already works.
- **Cons**: rule 4 currently makes an unresolvable reference *invalid*, not
  pending, so this option interacts with validation and cannot be a transport
  note alone.

### Option 4 (named but not proposed): carry an author hint in the block

The design document flags that this is the requirement nearest to justifying a
block-format change, without proposing one. It would move every digest in every
existing chain and is recorded here only so the trade-off is visible.

## Recommended Action

None yet — deliberately. Filed open for the `spec/07-transport.md` drafting
phase, and flagged as the one open question whose answer could reach back into
`spec/02-block-format.md` and `spec/03-encoding.md` — so it should be decided
before a profile ships, not after.

## Technical Details

- **Affected Files**: `spec/07-transport.md` (unwritten),
  `spec/05-processing-model.md` (resolution procedure), possibly
  `spec/02-block-format.md` (rule 4) and `spec/03-encoding.md` (internal
  references)
- **Related Components**: resolution, foreign chain loading, scan limit
- **Database Changes**: No

## Acceptance Criteria

- [ ] The specification says how a node is expected to locate a block named only
      by a digest
- [ ] The answer covers the private-block case, where no index is possible
- [ ] The interaction with rule 4 (unresolvable versus invalid) is stated
      explicitly

## Work Log

### 2026-08-18 - Filed From the Transport Design Document

**By:** Claude

Carried over as-is from
[`docs/design/2026-08-18-transport-design.md`](../docs/design/2026-08-18-transport-design.md)
§5, Q5. No decision taken.

## Notes

Source: `docs/design/2026-08-18-transport-design.md` §5, Q5 (and §1 R4, R7).
