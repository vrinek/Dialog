---
status: pending
priority: p2
issue_id: "069"
tags: [transport, specification-gap, processing-model, block-format, security]
dependencies: []
---

# Is a Block's Plaintext Head Separately Serveable?

## Problem Statement

`spec/05-processing-model.md`, "Scan limit", exempts from the count "a block a
node fetches only to read its type or its author, for validation rules 6 and 10,
without reading its operations". That sentence describes a real operation — a
*head fetch*: get a block's `v`, `type`, `pub` and `prev` without its `ops`.

A Dialog signature covers the whole block (`spec/04-cryptography.md`, "Signature
input": the dCBOR of every field except `sig`). A head on its own therefore
carries no evidence for itself. A server that serves heads separately can claim
any `type` and any `pub` it likes for a digest it holds, and a client that acts
on the claim will apply rule 6 (`rotate_key` may only appear in a rotation
block) or rule 10 (reference hygiene) to fabricated values — rejecting a block
that is valid, or admitting a scan it should have refused. That is a
withholding-class attack (`docs/design/2026-08-18-transport-design.md`, R0) that
does not require the server to forge anything cryptographic.

The specification currently neither offers a head fetch nor forbids one; it
describes it in passing while defining a different rule.

This is a deliberately-open design question for the `spec/07-transport.md`
drafting phase. Nothing here is decided; the todo exists so the question is
tracked and answered before a profile ships an endpoint that presumes one
answer.

## Findings

- `spec/05-processing-model.md`, "Scan limit": the exemption quoted above, which
  is the only place in the specification that names a partial-block fetch.
- `spec/04-cryptography.md`, "Signature input": the signature is over the dCBOR
  encoding of the block map without `sig`, prefixed by `"dialog-v1-block"`.
  There is no per-field commitment, no Merkle structure over the fields, and no
  way to verify a subset of the map.
- `spec/02-block-format.md`, validation rules 6 and 10: both read another
  block's `type` and `pub`. Rule 6 decides whether a `rotate_key` operation is
  in the right kind of block; rule 10 decides whether a public block may
  reference a private one.
- `spec/02-block-format.md`, "Private block": `v`, `type`, `pub`, `sig` and
  `prev` stay plaintext in a private block, so a "head" is a shape the format
  already has — but the private block's head still comes with its own `sig`,
  which covers the whole encoded block including `enc`.
- `docs/design/2026-08-18-transport-design.md` §4.1: the sketch deliberately
  omits a head-only endpoint and defers the decision here; §1 R4 states the
  tension in full.

## Proposed Solutions

The design document lists three directions and picks none.

### Option 1: Forbid partial serving

A transport serves whole blocks or nothing. The scan-limit exemption stays as
written, describing a *local* optimization: a node that already holds a block
may read only its head, but no node ever obtains a head from elsewhere.

- **Pros**: nothing unverifiable ever crosses the wire; no new spec machinery.
- **Cons**: the round-trip cost the exemption was meant to relieve stays, and a
  client that only needs a type pays for a full block; batch fetch has to
  absorb it.

### Option 2: A head is advisory and MUST NOT decide validity

Permit head fetch, and state normatively that a head obtained from a transport
is a hint — usable for planning fetches, never as the input to rules 6 or 10. A
node that would reject a block on the strength of a head MUST fetch the whole
block first.

- **Pros**: keeps the optimization where it actually pays (deciding what to
  fetch next) and closes the attack, since no decision rests on the hint.
- **Cons**: a rule about what a node may *conclude* from data, which is a new
  kind of rule for this specification and hard to make conformance-testable.

### Option 3: Accept that the exemption describes an unserveable optimization

Leave the wire alone and add a note to the scan-limit text saying the exempted
fetch is one no safe transport can offer remotely.

- **Pros**: smallest change; honest about what the sentence means today.
- **Cons**: leaves a sentence in the specification whose operation nothing can
  perform, which is what raised the question.

## Recommended Action

None yet — deliberately. The question is filed open for `spec/07-transport.md`.
Whoever drafts it should note that Options 2 and 3 differ only in whether the
hint is allowed to exist, and that Option 1 and Option 3 reach the same wire.

## Technical Details

- **Affected Files**: `spec/07-transport.md` (unwritten),
  `spec/05-processing-model.md` ("Scan limit"), possibly
  `spec/02-block-format.md` (rules 6 and 10)
- **Related Components**: L1 validation, resolution procedure, any transport
  profile's fetch endpoints
- **Database Changes**: No

## Acceptance Criteria

- [ ] `spec/07-transport.md` (or `spec/05-processing-model.md`) states whether a
      block's head may be served separately from its block
- [ ] If it may, the specification says what a node may and may not conclude
      from an unverified head
- [ ] The scan-limit exemption's wording matches the answer

## Work Log

### 2026-08-18 - Filed From the Transport Design Document

**By:** Claude

Carried over as-is from
[`docs/design/2026-08-18-transport-design.md`](../docs/design/2026-08-18-transport-design.md)
§5, Q2. No decision taken.

## Notes

Source: `docs/design/2026-08-18-transport-design.md` §5, Q2 (and §1 R4, §4.1).
