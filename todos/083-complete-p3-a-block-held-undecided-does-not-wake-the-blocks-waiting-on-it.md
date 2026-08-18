---
status: complete
priority: p3
issue_id: "083"
tags: [reference-implementation, validation, processing-model]
dependencies: ["081", "082"]
---

# A Block Held Undecided Does Not Wake the Blocks Waiting on It

## Problem Statement

`todos/082` settled that reference resolution reads blocks and not verdicts: a
definition may be taken from a block held as *stored but unvalidated*. A block
whose rule 4 resolution was waiting for block P therefore becomes decidable the
moment P **arrives**, whatever verdict P itself gets — P only has to be
readable.

`ts/`'s `BlockStore` re-validates the blocks waiting for P only when P is
*accepted*. `go/block`'s new `ValidatingStore` re-validates them on any
arrival. The two stores therefore reach different verdicts for the same set of
blocks offered in the same order, which is the thing having two implementations
is meant to catch.

## Findings

- `ts/src/block.ts`, `BlockStore.add`: `retryPending(selfDigest)` is called
  only on the accepted path. The unvalidated path calls `hold` and returns, so
  nothing waiting on that block is retried.
- `go/block/verdict.go`, `ValidatingStore.Add`: `settle(adm.Digest)` runs for
  both verdicts, with the reasoning written out — an arrival settles a rule 4
  dependency by being readable, and only rule 3 waits for the block to be
  *accepted*, which it goes on doing if it was not.
- The divergent case is small and reachable: A's genesis block is missing, so
  A's second block — which defines a bond — is held undecided; B's block names
  it in `refs`. Offer B first, then A's second block. Go accepts B; TypeScript
  leaves B stored but unvalidated until A's genesis block turns up, which may be
  never.
- `go/block/verdict_test.go`, `TestPendingReferenceIsRevalidatedOnArrival` pins
  the Go behaviour. `ts/test/block.test.ts` has no case for the order.
- Neither behaviour is wrong by the letter of the specification: nothing says
  *when* a node must revalidate a block it is holding, only that it MAY. But
  "MAY revalidate" plus a definite arrival is a poor place for two conformant
  nodes to hold different views of the same blocks, and the weaker one wastes
  the decision `todos/082` just made.

## Proposed Solutions

### Option 1: TypeScript retries on any arrival

Call `retryPending` on the unvalidated path too. A block waiting on P for rule 3
will simply fail rule 3 again and be re-filed under P; a block waiting on P for
rule 4 is decided.

- **Pros**: the two implementations agree; the cost is one wasted validation per
  rule 3 waiter per arrival.
- **Cons**: a rule 3 waiter is re-validated once for nothing.

### Option 2: File what each waiter is waiting *for*

Record whether a waiter needs the block to arrive (rule 4) or to be accepted
(rule 3), and wake only the ones an arrival can settle.

- **Pros**: no wasted work; the index says what it means.
- **Cons**: more state, for a saving nobody has measured.

### Option 3: Say in the specification that revalidation timing is
implementation-scoped, and leave both

- **Pros**: honest; no code changes.
- **Cons**: two nodes holding the same blocks disagree about which reach L3,
  which is what the validation rules exist to prevent.

## Recommended Action

Option 1, with a test in each implementation offering the blocks in the order
that separates them. Option 2 is the same behaviour with bookkeeping that can be
added later if the wasted validations ever matter.

## Technical Details

- **Affected Files**: `ts/src/block.ts`, `ts/test/block.test.ts`
- **Related Components**: L1 validation, stored but unvalidated blocks,
  demand-driven resolution
- **Database Changes**: No

## Acceptance Criteria

- [x] Offering the same blocks in the same order gives the same verdicts in both
      implementations
- [x] Each implementation has a test for the order that separates them today

## Work Log

### 2026-08-19 - Filed While Applying 081 and 082

**By:** Claude

Found while writing `ValidatingStore.settle`: deciding when to re-offer a held
block is exactly where the two decisions meet, and the answer `todos/082` gives
makes an arrival — not an acceptance — the event that matters.

### 2026-08-19 - Ratified and Applied

**By:** Claude

**Option 1**, as recommended. An arrival wakes the blocks waiting on it whatever
verdict the arriving block got.

- `ts/src/block.ts`, `BlockStore.add`: the unvalidated path now calls
  `retryPending(selfDigest)` too, with the reasoning at the call site — rule 4
  needed the block *readable*, which a stored-but-unvalidated block is, and rule
  3 is the one rule that wants it *accepted*, so a rule 3 waiter woken by an
  undecided arrival fails the same rule again and is re-filed under the same
  block. `hold`'s doc comment already said "re-validated by the same arrival",
  which was false before this change and is true after it.
- `ts/test/block.test.ts`: two tests. "rule 4: the same blocks in the other order
  reach the same verdicts (todos/083)" is the divergence scenario — Alice's
  genesis never offered, Alice's second block carrying the bond, Bob's block
  naming it in `refs`, offered Bob-first; it fails without the fix and passes
  with it. "rule 3: an undecided arrival re-files the waiter it cannot settle
  (todos/083)" pins the other branch, which the fix is what makes reachable.
- `go/block/verdict_test.go`, `TestPendingReferenceIsRevalidatedOnArrival`: the
  behaviour was already right; the test now names the scenario as this todo's
  and points at its TypeScript twin, so the two are findable from each other.

**Vectors: no byte moved.** This is a verdict-timing question and touches no
encoding.

**Left open:** nothing in `spec/05-processing-model.md` *requires* revalidation
on arrival — it says a node MAY revalidate. Both implementations now agree by
construction rather than by the specification, which is a conformance gap for
the third implementation. Filed as `todos/084`.

## Notes

Source: applying `todos/081` and `todos/082`.
