---
status: pending
priority: p2
issue_id: "044"
tags: [specification-gap, block-format, block-validation, key-rotation]
dependencies: ["042", "041"]
---

# Rule 6 Does Not Say Where a Rotation Block Sits, and Succession Now Depends on It

## Problem Statement

Validation rule 6 of `spec/02-block-format.md` reads:

> **Public/private reference rules.** Public blocks MUST only reference public
> blocks in their `refs` field. Private blocks MAY reference either public or
> private blocks.

There are three block types, not two. A rotation block's `type` is
`"rotation"`, so it is not a public block, and rule 6 — read literally —
forbids a public block from listing one in `refs`.

Issue #42 has just made that reading load-bearing. Verifiable succession now
requires the genesis block of a successor chain to list the rotation block's
digest in its `refs`, and that genesis block is, in the ordinary case, a public
block. Under the literal reading, **every conforming successor chain violates
rule 6**, and under the intended reading rule 6 means "MUST NOT reference a
private block" and the pair is fine. The specification does not say which.

A second question arrives with the same edit: the successor chain's genesis
block may itself be a **private** block, whose `refs` are inside `enc`. Its
back-reference to the rotation block is then invisible to every node without
the decryption key, so no such node can confirm the succession that
`spec/05-processing-model.md` § "Chain succession" asks it to act on — it MUST
mark the old key inactive and MUST add the new key to the set of known chains,
on the strength of evidence it cannot read.

## Findings

- `spec/02-block-format.md`, "Validation" rule 6, and the matching bullets in
  `spec/05-processing-model.md` § "Public/private reference rules": both are
  phrased as a public/private dichotomy, and predate the `type` field's third
  value (issue #15).
- `spec/02-block-format.md`, "Security Considerations": "Public blocks MUST NOT
  reference private blocks. This prevents public data from depending on content
  that non-recipient nodes cannot validate." This states the *reason* in the
  negative form, and a rotation block is fully visible to every node, so the
  reason does not reach it. That is evidence for the intended reading, not a
  statement of it.
- `spec/02-block-format.md`, "rotate_key" § "Verifiable succession" (added by
  issue #42): the back-reference is now a MUST.
- `go/block/validate.go`, `validateReferences`: implements rule 6 as "the
  referenced block MUST NOT be private", so a public block referencing a
  rotation block passes. That is the reading the successor case needs, but it
  was chosen by the implementation, not read out of the specification.

## Proposed Solutions

### Option 1: Restate rule 6 in the negative (Recommended)

- "A public block's `refs` MUST NOT name a private block. A private block's
  `refs` MAY name a block of any type." Rotation blocks, being visible to
  every node, may be referenced by anything.
- **Pros**: matches the stated reason, covers all three types with no
  enumeration to keep in step with the `type` field, and makes the successor
  chain's mandatory back-reference legal by construction.
- **Cons**: none identified.
- **Effort**: Trivial (spec), none (Go — this is already the behaviour)
- **Risk**: Low

### Option 2: Enumerate the permitted types

- "A public block MAY reference public and rotation blocks only."
- **Pros**: explicit.
- **Cons**: a fourth block type would need the sentence edited again.
- **Risk**: Low

### The private-successor-genesis question (independent)

State one of:

- **(a)** The genesis block of a successor chain MUST be a public or rotation
  block, so the back-reference is visible to every node; a private chain's
  successor is then linked in the clear.
- **(b)** It MAY be private, and nodes without the key simply cannot verify the
  succession — in which case `spec/05-processing-model.md` § "Chain
  succession" needs to say what such a node does with the rotation block it
  can read and the successor it cannot find.

## Recommended Action

Option 1, plus an explicit choice between (a) and (b). They are separable and
could be ratified independently.

## Technical Details

- **Affected Files**: `spec/02-block-format.md` ("Validation" rule 6,
  "Security Considerations"), `spec/05-processing-model.md`
  ("Public/private reference rules", "Chain succession")
- **Related Components**: reference validation, key rotation, private chains
- **Database Changes**: No

## Acceptance Criteria

- [ ] Rule 6 says whether a public block may reference a rotation block
- [ ] The rule's wording covers all three block types without enumerating two
- [ ] Whether a successor chain's genesis block may be private is stated
- [ ] `go/block` matches the ratified rule and its doc comment cites it

## Work Log

### 2026-08-13 - Filed While Applying Issue #42
**By:** Claude

Surfaced by making the succession back-reference a MUST: the specification now
requires a reference that its own rule 6 can be read to forbid. The Go
implementation already reads rule 6 as "not private" and so accepts the pair,
but that reading is the implementation's and not the specification's, which is
why this is filed rather than silently kept.

## Notes

Source: Go reference implementation, phase 3 (block), applying issue #42.
