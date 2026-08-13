---
status: pending
priority: p3
issue_id: "043"
tags: [specification-gap, block-validation, processing-model]
dependencies: []
---

# "An Existing, Valid Block" Does Not Say How Far Validity Recurses

## Problem Statement

Validation rule 3 of `spec/02-block-format.md` reads:

> **Chain integrity.** If `prev` is not null, it MUST reference an existing,
> valid block with the same `pub` key.

Taken literally, validating block *n* requires validating block *n−1*, which
requires block *n−2*, and so on to the genesis block — every block's validation
is the validation of its whole chain, and validating a chain of *n* blocks
costs O(n²). Taken as a statement about the node's state — "the predecessor is
a block this node has already accepted" — validation is O(1) in the chain
length and the recursion never happens, because a stored block was validated
when it arrived.

The second reading is almost certainly the intent: `spec/05-processing-model.md`
describes reception as validate-then-store, so anything in the store has been
validated once. But the specification never says so, and the difference is
visible:

- **Cost.** An implementation that reads rule 3 literally re-validates
  ancestors, including their signatures, for every block it receives.
- **Partial chains.** A node that has the predecessor's bytes but has not
  validated it — say it was received out of order — cannot tell from the text
  whether it may use it to satisfy rule 3.
- **Missing ancestors.** If a node holds blocks *n* and *n−1* but not *n−2*,
  is block *n* valid? Rule 3 is satisfied at every hop it can see, but the
  chain is not anchored to a genesis block, and reachability (rule 4) may or
  may not need the missing ancestor.

## Findings

- `spec/02-block-format.md`, "Validation" rule 3, quoted above; "existing" and
  "valid" are both undefined terms here.
- `spec/05-processing-model.md`, "Block reception": validate, then store, then
  make available to L2 — the invariant that makes the cheap reading sound, but
  stated as a procedure rather than as the definition rule 3 leans on.
- Rule 4's second bullet — "Any ancestor block in the author's own chain
  (reachable via `prev`)" — makes ancestor blocks a resolution input, so an
  incomplete chain has consequences beyond rule 3.
- Nothing states whether a chain must be anchored at a genesis block for its
  tip to be valid.

## Proposed Solutions

### Option 1: Define rule 3 against stored, already-validated blocks (Recommended)

- "The block referenced by `prev` MUST be one the node has already accepted as
  valid. Validity is not re-derived: a stored block was validated when it was
  received (see 05-processing-model.md, 'Block reception'), so rule 3 is a
  lookup, not a recursion."
- Add whether a chain must reach a genesis block for its tip to be accepted,
  and what a node does with a block whose ancestors it is still fetching
  (reject, or hold as pending — the latter needs a name).
- **Pros**: O(1) per block, matches how any real node works, closes the
  out-of-order question.
- **Cons**: makes validity relative to a node's history, so two nodes can
  disagree about a block until both have the same ancestors. That is already
  true of rule 4.
- **Effort**: Small
- **Risk**: Low

### Option 2: Keep the recursive reading and say so

- **Pros**: validity is absolute, not node-relative.
- **Cons**: quadratic cost, and it still needs the "what if the ancestor is
  missing" answer.
- **Risk**: Medium

## Recommended Action

Option 1, with an explicit sentence about incomplete chains, since that is the
case implementations actually hit.

## Technical Details

- **Affected Files**: `spec/02-block-format.md` ("Validation" rule 3),
  `spec/05-processing-model.md` ("Block reception")
- **Related Components**: block validation, chain walking, reachability
- **Database Changes**: No

## Acceptance Criteria

- [ ] Rule 3 says what "valid" means for the predecessor and whether it
      recurses
- [ ] The treatment of a block whose ancestors are missing is stated
- [ ] `go/block` matches the ratified reading

## Work Log

### 2026-08-13 - Filed While Implementing go/block
**By:** Claude

`Validate` takes the cheap reading: rule 3 checks that the predecessor exists
in the source, carries the same `pub`, and is not a rotation block, and leaves
whole-chain validation to `ValidateChain`, which validates from the genesis
block forward. An ancestor the source does not hold surfaces only if a
reference actually needs it, and then as a rule 4 failure naming the gap.

## Notes

Source: Go reference implementation, phase 3 (block).
