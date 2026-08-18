---
status: complete
priority: p3
issue_id: "081"
tags: [block-format, validation, reference-implementation, api]
dependencies: ["080"]
---

# Re-validating an Accepted Block Can Still Flip Rules 6 and 10

## Problem Statement

`todos/080` settled that rules 6 and 10 bind only for the `refs` entries a
validation resolved: an entry left *unchecked* is permanently outside the
block's validity, and a node MUST NOT invalidate a block it has accepted when
that entry turns out, later, to name a private or own-chain block.

Both implementations check the two rules against every `refs` entry the source
holds *at the moment of validation*. That is the reading `vectors/blocks.json`
pins and the specification's "once it holds" trigger states, and it is stable
for any single validation. It is not stable across two: validate a block while
one of its entries is unheld, and it is valid; hold that entry and validate the
same block again, and it is a rule 6 rejection. Nothing in the block changed.

`ts/` is safe by construction — `BlockStore.add` returns `duplicate` for a block
it has stored as valid and never re-validates it. `go/block` is a set of
functions over a `Source` and holds no verdicts, so the obligation lands on the
caller, and the package offers `ValidateChain` and `ValidateHistory`, which
re-validate a whole chain from a store. A Go node that re-validates its store on
startup — the natural reading of both functions — performs exactly the flip the
specification now forbids.

## Findings

- `go/block/validate.go`, `validateReferences`: the rules 6 and 10 pass fetches
  each `refs` entry and rejects on a private or own-chain target, before
  resolution decides whether any digest needs it. An entry the source does not
  hold is recorded in `Report.UncheckedRefs`.
- `go/block/chain.go`: `ValidateChain` and `ValidateHistory` walk a chain and
  validate every block against the current store. Neither has a way to know
  which entries an earlier verdict left unchecked.
- `ts/src/block.ts`, `BlockStore.add`: `if (existing !== undefined &&
  existing.valid) return { status: "duplicate" }`. The verdict is carried by the
  store, so the flip is unreachable through the public API.
- `spec/02-block-format.md`, "Validation", "A verdict moves in one direction":
  the MUST NOT is addressed to a node, and a node is what a caller of `go/block`
  builds. The package is conformant; a naive caller of it is not.
- The vectors pin the eager check:
  `invalid_in_chain/public_block_references_a_private_block`'s rejected block
  carries a single `create_atom`, so resolution needs nothing and the rejection
  comes from the pass alone. Making the check lazy would break it, which is why
  `todos/080` deliberately did not.

## Proposed Solutions

### Option 1: Say it in the doc comments and leave the API alone

Document on `Validate`, `ValidateChain` and `ValidateHistory` that a caller
re-validating a block it has already accepted MUST NOT downgrade the verdict on
a rule 6 or 10 rejection for an entry the earlier verdict reported unchecked.

- **Pros**: no API surface; `Report.UncheckedRefs` already gives the caller what
  it needs to tell the two apart.
- **Cons**: an obligation nothing enforces, in the one implementation whose
  users are most likely to write their own store.

### Option 2: Let a caller declare what an earlier verdict did not cover

Add a field to `Options` — a set of digests the caller's earlier verdict
reported unchecked — that the rules 6 and 10 pass skips.

- **Pros**: the rule becomes enforceable rather than advisory; the caller's
  record of `UncheckedRefs` feeds straight back in.
- **Cons**: an option that exists only for re-validation, and a caller that
  passes the wrong set weakens rule 6 silently.

### Option 3: Give go/block a verdict-carrying store, as ts has

Have `MemStore` (or a new type) validate on `Add` and record the verdict, so
that re-validating an accepted block is a lookup.

- **Pros**: the two implementations would model a node the same way, and the
  stability rule would hold by construction in both.
- **Cons**: much the largest change; `MemStore` is deliberately a `Source` and
  not a node, and several packages build on that.

## Recommended Action

Option 1 now, Option 3 if `go/block` ever grows a node-shaped store for other
reasons. Option 2 buys enforcement at the cost of an option whose only correct
use is one the doc comment can describe.

## Technical Details

- **Affected Files**: `go/block/validate.go`, `go/block/chain.go`,
  `go/block/store.go`
- **Related Components**: L1 validation, verdict stability, demand-driven
  resolution
- **Database Changes**: No

## Acceptance Criteria

- [x] A Go caller re-validating a chain over a store that has grown cannot
      downgrade an accepted block by accident, or is told in the doc comments
      exactly what it must not do — both: it is impossible over a
      `ValidatingStore`, and `ValidateChain` states the obligation for a
      verdict-free source
- [x] Whatever is chosen, `vectors/blocks.json` stays byte-identical and
      `public_block_references_a_private_block` is still rejected

## Work Log

### 2026-08-19 - Filed While Applying 080

**By:** Claude

Found while checking that `go/block` matches the ratified decision. It does, for
one validation; what it cannot do is remember that an earlier verdict left an
entry unchecked, and `ValidateChain` is a documented way to ask for a second
one.

### 2026-08-19 - Ratified and Applied

**By:** Claude

**Decision (project lead):** Option 3 — `go/block` gets a verdict-carrying
store, mirroring `ts/`'s model. The store records each block's verdict on
admission, an accepted verdict is never recomputed, and a block held as stored
but unvalidated is revalidated when what it awaits arrives. `Validate` stays the
pure function it is; the verdict-carrying layer sits above it.

**Design chosen: a wrapper, not a change to `MemStore`.** `ValidatingStore`
(`go/block/verdict.go`) holds a `MemStore` and adds the verdict index, so
`Source`, `Siblings` and `Referrers` are unchanged and every existing caller
keeps working — `graph`, `accept`, `privacy`, `internal/vectors` and the demo's
`replay` needed no edit at all. `MemStore` stays what it was documented to be: a
`Source`, not a node.

**Changes:**

- `go/block/verdict.go` (new): `Verdict`, the `Verdicts` interface, `Admission`
  and `ValidatingStore`. `Add` validates, records, stores, and files an
  undecided block under the block that would settle it; `settle` re-offers the
  waiters when that block is admitted. A rejected block is not stored. An
  accepted verdict is returned with `Admission.Duplicate` rather than
  recomputed.
- `go/block/validate.go`: `PendingError` names the one block a verdict waits
  for, so a store can file the waiter without parsing a message (its `Error` is
  the wrapped error's, so no text moved and `errors.Is` still finds
  `ErrNotFound` / `ErrUndecryptable`); `Awaiting` is the convenience. Rule 3
  now asks a `Verdicts` source whether the predecessor was *accepted*, not
  merely held — `ErrUnaccepted`, which `IsUnvalidated` answers true to. A source
  without verdicts is unaffected.
- `go/block/chain.go`: `ValidateChain` reads an accepted verdict from a
  `Verdicts` source instead of recomputing it, which closes the flip this issue
  is about; its doc comment states the obligation for a source that carries no
  verdicts. `ValidateHistory` inherits it.
- `go/block/verdict_test.go` (new): the regression test is
  `TestAcceptedVerdictSurvivesTheStoreGrowing` — a block accepted with an
  unchecked `refs` entry stays accepted when that entry arrives and turns out
  private, and `ValidateChain` over the grown store returns the accepted verdict
  though a bare `Validate` on the same store now reports rule 6. Also: verdicts
  recorded and re-read, an unvalidated block refused as a predecessor, a rule 4
  dependency revalidated on arrival, an invalid block not stored, and fork
  flagging.

**Deadlock note.** `admit` must not hold the store's lock across `Validate`,
which reads the store back for rule 3's verdict and for resolution. It does not;
`record` refuses to write over an accepted verdict, which is what the released
lock leaves to guard.

**Vectors: no byte moved.** `genvectors` reproduces `vectors/` byte for byte,
and `public_block_references_a_private_block` is still rejected.

## Notes

Source: applying `todos/080`; see also `todos/041`, which settled the evaluation
point of rules 6 and 10, and `todos/082`, whose decision this store had to
respect: it hands unvalidated blocks to reference resolution.
