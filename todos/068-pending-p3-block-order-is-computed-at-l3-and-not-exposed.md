---
status: pending
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

- [ ] An `Assertion` says where in its author's chain it was published
- [ ] The definition states how a key rotation counts
- [ ] `demo/cmd/dialog-mcp` uses it for assertions instead of its own index

## Work Log

### 2026-08-18 - Filed From the MCP Demo

**By:** Claude

Found while building phase 2 of the grounding demo, alongside `todos/067`.
Both are the same shape: L3 computes something to reach an answer and reports
the answer without the working.

## Notes

Source: `docs/plans/2026-08-18-grounding-demo.md`, phase 2.
