---
status: pending
priority: p3
issue_id: "041"
tags: [specification-gap, block-format, refs, content-addressing]
dependencies: ["022"]
---

# The refs List Has No Rules About Duplicates, Order, or Self-Reference

## Problem Statement

`spec/02-block-format.md` defines `refs` as `[* bstr .size 32]` and says the
entries are the blocks that define the entities this block needs. Nothing else
about the list is specified. Four questions follow, and each has a visible
consequence because `refs` is inside the signed, hashed bytes:

1. **Duplicates.** `[X, X]` is legal under the CDDL. It denotes the same
   dependency twice and produces a different block digest from `[X]` — two
   distinct blocks, same meaning, same operations.
2. **Order.** `[X, Y]` and `[Y, X]` likewise differ in digest and not in
   meaning. dCBOR canonicalizes map keys, not array elements, so the encoder
   cannot remove the choice; only a rule can.
3. **Self- and own-chain references.** May a block list a block in its own
   author's chain, which is already reachable through `prev`? May it list
   itself, or a block that does not exist? None of these are forbidden.
4. **Checkability of rule 6.** "Public blocks MUST only reference public
   blocks" requires knowing each referenced block's type, but
   `spec/05-processing-model.md` fetches referenced blocks *on demand* — only
   those needed to resolve an unresolved digest. A public block listing a
   private block it never actually needs is therefore a violation nobody is
   obliged to notice, and whether an implementation must fetch every ref just
   to check its type is unstated.

Questions 1 and 2 matter most for the conformance vectors this implementation
is meant to produce: without a rule, two implementations building "the same"
block from the same inputs can legitimately produce different CIDs.

## Findings

- `spec/02-block-format.md`, the `refs` field row: "Zero or more SHA-256
  digests of CID-providing blocks ... MAY be empty." No further constraint.
- `spec/03-encoding.md`, "Deterministic CBOR": rules 2 and 3 canonicalize map
  keys and forbid duplicate keys; arrays have no such rules, by design.
- `spec/05-processing-model.md`, "Resolution procedure" steps 4 and 5: fetch
  blocks listed in refs *for unresolved digests*, recursing as needed — the
  demand-driven behaviour that leaves unneeded refs unfetched.
- `spec/02-block-format.md` rule 6 and `spec/05-processing-model.md`
  "Public/private reference rules" state the constraint without saying at what
  point it is evaluated.

## Proposed Solutions

### Option 1: Make refs a canonical set (Recommended)

- Add to the `refs` field description: "Entries MUST be unique, and MUST be
  sorted in ascending bytewise order of the digest. A block whose `refs` list
  repeats an entry or is unsorted MUST be rejected."
- Optionally state that a ref pointing into the block's own chain is permitted
  but redundant, and that a block MUST NOT list its own digest (which it cannot
  know before signing anyway).
- **Pros**: one encoding per dependency set, which is what content addressing
  wants everywhere else in Dialog; makes vectors reproducible; cheap to check.
- **Cons**: authors lose the ability to signal priority by ordering, which
  nothing in the protocol currently reads.
- **Effort**: Small (spec), Small (implementations)
- **Risk**: Low

### Option 2: Leave the list free, state that order and duplicates are not
significant

- **Pros**: no new rejection.
- **Cons**: keeps distinct CIDs for equivalent blocks, which is the situation
  `spec/03-encoding.md` exists to prevent.
- **Risk**: Medium

### Rule 6 checkability (independent of the above)

State one of:

- **(a)** Rule 6 is checked for every entry of `refs`, so an implementation
  fetches each referenced block at least far enough to read its `type`; or
- **(b)** Rule 6 is checked only for blocks actually fetched during resolution,
  and an unfetched private ref in a public block goes unnoticed.

(a) is the stricter reading of "MUST only reference"; (b) is what demand-driven
resolution actually does.

## Recommended Action

Option 1 for the list shape, plus an explicit choice between (a) and (b) for
rule 6's evaluation point. They are separable and could be ratified
independently.

## Technical Details

- **Affected Files**: `spec/02-block-format.md` (`refs` field description,
  "Validation" rule 6), `spec/05-processing-model.md` ("Resolution procedure",
  "Public/private reference rules")
- **Related Components**: block encoding, reference resolution, conformance
  vectors
- **Database Changes**: No

## Acceptance Criteria

- [ ] The specification states whether `refs` may repeat an entry and whether
      its order is constrained
- [ ] The evaluation point of rule 6 is stated
- [ ] `go/block` matches both rulings

## Work Log

### 2026-08-13 - Filed While Implementing go/block
**By:** Claude

The decoder accepts any `refs` list, duplicates and all, because rejecting
would refuse blocks the CDDL admits. For rule 6 the implementation takes
reading (a) for refs the source happens to hold — it fetches each direct ref,
rejects a private one, and warns when the block is unavailable rather than
failing — which is the strictest behaviour that never rejects a block for being
unreachable.

## Notes

Source: Go reference implementation, phase 3 (block).
