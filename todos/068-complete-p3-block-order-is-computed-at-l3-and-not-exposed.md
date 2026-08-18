---
status: complete
priority: p3
issue_id: "068"
tags: [layer-3, api, assertion-order, grounding-demo]
dependencies: ["067"]
---

# L3 Computes an Assertion's Block Order and Does Not Expose It

## Problem Statement

`Assertion.Block` is the digest of the block a truth meta-molecule was published
in — the provenance tag `spec/05-processing-model.md`, "Accumulation rules",
requires. `Assertion.Latest` says whether that assertion was its author's last
word. Between the two there is a fact the package computed and did not report:
*where* in the author's chain that block sits.

`accept.Build` needs an L1 block source precisely to work this out — `ErrNoSource`
says so — because "the later assertion (by block order) takes precedence"
(`spec/06-meta-bonds.md`, "Truth retraction") is a statement about position in a
chain. Having walked the chain to decide `Latest`, the package hands back the
digest and keeps the position.

An application that wants to *explain* a truth state rather than state it needs
that position. "gazetteer says it is untrue, in gazetteer block 4 of 4 (their
last word)" is an explanation a reader can check; "gazetteer says it is untrue,
in block c52a0528…" is not. `demo/cmd/dialog-mcp` therefore builds its own index
from block digest to (author, height) by walking `replay.Node.Chains` at
startup, which is only cheap because the demo holds the replayed chains. An
application holding a `View` and a `block.Source` — which is all `Build` asks for
— would have to walk every chain again to recover what `Build` already knew.

## Findings

- `go/accept/accept.go`: `Assertion` carries `Block cid.Digest` and `Latest bool`,
  and no ordinal.
- `go/accept/order.go`: the block-order computation, run during `Build`.
- `go/accept/accept.go`: `ErrNoSource` — "turning that into a position in the
  author's chain [...] means reading the chain", which is the position in
  question.
- `demo/cmd/dialog-mcp/server.go`: `blockRef` and the `blocks` map, the
  application-side reconstruction, and `Server.blockLabel`, which renders it.
- `spec/05-processing-model.md`, "Assertion order": defines the order; says
  nothing about reporting it.

## Proposed Solutions

### Option 1: Add the height to Assertion (Recommended)

Give `Assertion` a `Height int` field — the block's zero-based position in its
author's chain, counting through key rotations the way block order does.

- **Pros**: one field, already computed; makes every truth answer explainable;
  no new method and no new type.
- **Cons**: widens a value type that is copied a lot (immaterial); the
  definition has to say what a rotation does to the count, which is a question
  worth pinning anyway.
- **Effort**: Small
- **Risk**: Low — additive.

### Option 2: A view-level accessor

    func (v *View) BlockOrder(d cid.Digest) (author ed25519.PublicKey, height int, ok bool)

- **Pros**: serves callers who hold a block digest from somewhere other than an
  assertion — an authorship record from L2, for instance, which is the demo's
  other use of the same index.
- **Cons**: a second way to ask; the view would have to retain the order for
  every ingested block rather than for the assertions it read.
- **Risk**: Low

### Option 3: Leave it to the application

- **Pros**: nothing to change.
- **Cons**: every application that wants to explain a truth state re-walks the
  chains, and has to reproduce the rotation-following rule to get the same
  answer as L3. Two implementations of one order is how they diverge.
- **Risk**: Medium

## Recommended Action

Option 1, and Option 2 as well if a second caller appears: the demo wants the
height for L2 authorship records too (`dialog_provenance`), which an
`Assertion` field does not reach.

## Technical Details

- **Affected Files**: `go/accept/accept.go`, `go/accept/order.go`,
  `demo/cmd/dialog-mcp/server.go`
- **Related Components**: L3 assertion order, key rotation, the MCP grounding
  demo
- **Database Changes**: No

## Acceptance Criteria

- [x] An `Assertion` says where in its author's chain it was published
- [x] The definition states how a key rotation counts
- [x] `demo/cmd/dialog-mcp` uses it for assertions instead of its own index

## Work Log

### 2026-08-18 - Filed From the MCP Demo

**By:** Claude

Found while building phase 2 of the grounding demo, alongside `todos/067`.
Both are the same shape: L3 computes something to reach an answer and reports
the answer without the working.

### 2026-08-18 - Settled: Options 1 and 2 Together

**By:** Claude

Both, as the recommendation allowed: the second caller appeared in the same
breath, since `dialog_provenance` places L2 authorship records and `todos/067`'s
declaration backing places the block a meta-molecule was published in, and
neither is an `Assertion`.

`accept.ChainPosition` — author, lineage, height and length — is carried by
`Assertion.Position` and answered for any block by `View.BlockPosition`. Height
is zero-based and counts through a key rotation, because that is what block
order does: "every block of a successor chain comes after every block of the
chain it succeeds" (`spec/05-processing-model.md`, "Assertion order"), so a
successor's genesis block is not height 0, and a rotation block that published
nothing is still counted. The definition is pinned by a test.

Length needed a decision the todo did not anticipate. `Build` now places every
block that published an entity of the view — an O(view) walk that `blockOrder`
memoizes, so the walks are the ones standing and truth would have made anyway —
and Length is one past the height of the furthest block placed. That is "the
chain as far as this view can see it", which for a subscribed author is the
whole chain and makes "block 4 of 4" sayable, and for an unsubscribed author
whose block reached the index through a co-published entity is a smaller number.
The demo does not print a total in that second case: the view knows the block's
height and not the chain's length, and "of 1" would report the view's ignorance
as a fact about gazetteer.

The alternative — placing every block L2 names, regardless of subscription —
was rejected: it would make `Build`'s cost and its error surface scale with all
of L2 rather than with the view, and it would break the pinned contract that a
`Build` with nothing to order reads nothing from the source
(`TestBuildNeedsTheBlocksItOrdersBy`).

`demo/cmd/dialog-mcp` dropped its `blocks` index and its `blockRef` type;
assertions and declarations render the position they carry, and everything else
asks the view.

## Notes

Source: `docs/plans/2026-08-18-grounding-demo.md`, phase 2.
