---
status: pending
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

- [ ] A Go caller re-validating a chain over a store that has grown cannot
      downgrade an accepted block by accident, or is told in the doc comments
      exactly what it must not do
- [ ] Whatever is chosen, `vectors/blocks.json` stays byte-identical and
      `public_block_references_a_private_block` is still rejected

## Work Log

### 2026-08-19 - Filed While Applying 080

**By:** Claude

Found while checking that `go/block` matches the ratified decision. It does, for
one validation; what it cannot do is remember that an earlier verdict left an
entry unchecked, and `ValidateChain` is a documented way to ask for a second
one.

## Notes

Source: applying `todos/080`; see also `todos/041`, which settled the evaluation
point of rules 6 and 10.
