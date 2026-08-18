---
status: pending
priority: p3
issue_id: "084"
tags: [processing-model, specification-gap, conformance]
dependencies: ["082", "083"]
---

# Revalidation on Arrival Is a MAY in the Specification

## Problem Statement

`todos/083` made both implementations wake a held block when the block it is
waiting for *arrives*, whatever verdict the arriving block gets. They now agree.

Nothing in the specification requires it. `spec/05-processing-model.md`, "Block
reception", says a node holding a stored but unvalidated block **MAY** validate
it again when the missing block arrives, and says nothing about when. A third
implementation that revalidates only on acceptance — which is what `ts/` did
until `todos/083` — is conformant by the letter and reaches different verdicts
for the same blocks offered in the same order.

That is the situation `todos/083` set out to end, and it ended it in the two
implementations rather than in the specification.

## Findings

- `spec/05-processing-model.md`, "Block reception": the third outcome is
  normative, the timing of its resolution is not.
- `spec/02-block-format.md`, validation rule 4 (as amended by `todos/082`):
  resolution reads blocks and not verdicts. This is what makes an *arrival*
  sufficient — the rule is already there, and only the obligation to act on it
  is missing.
- The verdicts that diverge are not obscure. A block whose `refs` name a block
  held undecided is accepted by a node that wakes on arrival and left undecided
  forever by a node that wakes on acceptance, if the undecided block's own
  ancestry never turns up.
- Both implementations pin the divergent order in a test now
  (`go/block/verdict_test.go`, `TestPendingReferenceIsRevalidatedOnArrival`;
  `ts/test/block.test.ts`, "rule 4: the same blocks in the other order reach the
  same verdicts (todos/083)"), so a specification sentence would have two
  implementations already satisfying it.

## Proposed Solutions

### Option 1: A SHOULD in "Block reception"

"A node that holds a block as stored but unvalidated SHOULD validate it again
when a block it was waiting for arrives, whatever verdict that block itself
received."

- **Pros**: matches what both implementations do; a node that cannot afford it
  (a node with no index of what waits on what) is still conformant.
- **Cons**: a SHOULD still admits two nodes disagreeing about the same blocks.

### Option 2: A MUST

- **Pros**: two conformant nodes holding the same blocks reach the same
  verdicts, which is what the validation rules exist for.
- **Cons**: it obliges every node to keep the waiting index, which is real
  storage; and a node that discards undecided blocks rather than holding them —
  which `spec/05` permits — would have to be excluded from the rule explicitly.

### Option 3: Say the timing is implementation-scoped, and say what follows

Write down that a verdict is relative to what a node has *offered itself*, so
that the divergence is documented rather than surprising.

- **Pros**: honest, no new obligation.
- **Cons**: it concedes that two nodes with the same blocks disagree, which
  `todos/083` rejected once already.

## Recommended Action

Option 1, with the reason stated in the same sentence: an arrival settles a rule
4 dependency because resolution reads blocks and not verdicts, and only rule 3
waits for a verdict. Option 2 is the same text with a stronger keyword, and can
be taken later if a third implementation appears and diverges.

## Technical Details

- **Affected Files**: `spec/05-processing-model.md` ("Block reception"), and
  possibly `spec/02-block-format.md`'s rule 4 note
- **Related Components**: L1 validation, stored but unvalidated blocks,
  demand-driven resolution
- **Database Changes**: No

## Acceptance Criteria

- [ ] The specification says when a held block is revalidated, or says that it
      does not say
- [ ] Both implementations are checked against whatever it says

## Work Log

### 2026-08-19 - Filed While Applying 083

**By:** Claude

Both implementations now wake waiters on arrival. The specification still only
permits it, so the agreement is a convention between two codebases rather than a
conformance requirement.

## Notes

Source: applying `todos/083`.
