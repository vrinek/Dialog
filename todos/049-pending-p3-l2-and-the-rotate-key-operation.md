---
status: pending
priority: p3
issue_id: "049"
tags: [specification-gap, processing-model, key-rotation, layer-2]
dependencies: ["001", "044"]
---

# L2's Accumulation Rules Have No Answer for a rotate_key Operation

## Problem Statement

`spec/05-processing-model.md` § "Accumulation rules" is written as a loop over
operations:

> For each valid block in L1 [...] the node MUST:
>
> 1. Extract each operation from the block's `ops` list [...]
> 2. Compute the CID of the resulting entity (atom, bond, or molecule) [...]
> 3. Add the entity to the L2 graph, tagged with [...]

There are four operation types, and the fourth creates no entity. A rotation
block's `ops` list holds exactly one `rotate_key` and nothing else
(`spec/02-block-format.md`, "Rotation block"), so step 1 extracts an operation
for which step 2 has no object and step 3 has nothing to add. The specification
never says so. Three questions follow:

**1. Is a rotation block an input to L2 at all?** The accumulation rules say
"for each valid block in L1", without excluding a type. Reading them literally,
a rotation block is processed and contributes nothing; reading them by their
parenthesis — "(atom, bond, or molecule)" — a rotation block is not L2's
business in the first place. Both readings produce the same graph, which is why
this is p3, but they differ on what a node is required to *do*.

**2. Does anything about a key succession belong in L2?** The duties a rotation
imposes — mark the old key inactive, add the new key to the set of known chains,
auto-subscribe, decide author identity — are all listed under § "Layer 1 —
Block storage and validation", "Chain succession". Nothing in § "Layer 2" or
§ "Layer 3" mentions keys or successions. So an entity published by an author's
successor key carries a *different* authorship tag from the same entity
published before the rotation, and no layer is told to relate the two: "Author
identity (mapping multiple keys to a single author) is implementation-scoped".
That is a deliberate choice for the mapping, but it leaves L3's filtering rules
("check if any of its authors [...] is in the user's subscription list") with a
key set that silently changes under the user.

**3. May L2 record that it processed a block that contributed nothing?** An
implementation that wants re-processing its store to be a no-op has to remember
which blocks it has seen, rotation blocks included, or it will re-walk them
forever. Nothing forbids this and nothing describes it.

## Findings

- `spec/05-processing-model.md`, "Accumulation rules": steps 1–3, the
  "(atom, bond, or molecule)" parenthesis, and "L2 is append-only".
- `spec/05-processing-model.md`, "Chain succession (key rotation)": four steps,
  all of them L1's, and "Author identity [...] is implementation-scoped".
- `spec/02-block-format.md`, "Rotation block": "It MUST contain exactly one
  `rotate_key` operation and no other operations."
- `spec/02-block-format.md`, "rotate_key": the operation names the successor
  key; it defines no entity and has no CID of its own.
- `go/block`, `Operation.Creates`: returns `ok == false` for `RotateKey`, which
  is the implementation's reading of the parenthesis.
- `go/graph`, `Graph.Ingest`: accepts a rotation block, records it so that
  re-ingestion is a no-op, and adds no entity and no authorship record. An
  author whose only ingested block is a rotation block is therefore absent from
  `Graph.Authors`.
- Related: todo 001 (key rotation as a layer violation) and todo 044 (rule 6 and
  the rotation block type) circle the same seam from L1's side.

## Proposed Solutions

### Option 1: Say what a rotation block contributes to L2 (Recommended)

- One sentence in § "Accumulation rules": a `rotate_key` operation creates no
  entity and contributes nothing to the graph; the node's response to a rotation
  block is entirely the L1 procedure of § "Chain succession".
- A second sentence permitting a node to record which blocks it has processed,
  so that re-processing a store is idempotent — which the section's own "same
  CID = same entity" argument already implies for the other three operations.
- **Pros**: costs two sentences, removes the dangling step 2, and matches what
  an implementation has to do anyway.
- **Cons**: does not address the author-identity question, which is deliberately
  out of scope.
- **Effort**: Small (spec), none (Go)
- **Risk**: Low

### Option 2: Also say what L3 does across a rotation

- State whether a subscription to an author follows the succession into L3's
  filtering rules, given that L1 SHOULD auto-subscribe to the successor chain.
- **Pros**: closes the question that actually bites a user — data published
  after a rotation vanishing from their view.
- **Cons**: brushes against "author identity is implementation-scoped"; needs
  care to stay a SHOULD about subscriptions rather than a MUST about identity.
- **Risk**: Medium

### Option 3: Say nothing

- **Cons**: every implementation invents the same three answers, and the one
  that reads step 1 literally and step 2 strictly has no defined behaviour for a
  block type the protocol requires it to accept.
- **Risk**: Low

## Recommended Action

Option 1 for v1. Option 2 is worth a separate look together with todo 001, since
both are about where key succession stops being L1's business.

## Technical Details

- **Affected Files**: `spec/05-processing-model.md` ("Accumulation rules")
- **Related Components**: L2 accumulation, key rotation, `go/graph`
- **Database Changes**: No

## Acceptance Criteria

- [ ] The accumulation rules say what a `rotate_key` operation contributes to L2
- [ ] Whether a node may record processed blocks (for idempotent re-processing)
      is stated or explicitly left to the implementation

## Work Log

### 2026-08-15 - Filed While Implementing go/graph

**By:** Claude

Found writing the operation loop of `Graph.Ingest`. `Operation.Creates` already
reported that `rotate_key` creates nothing, so the code wrote itself; what could
not be read off the specification was whether ingesting a rotation block is a
thing a node does at all, and whether the graph is allowed to remember that it
did. The implementation accepts the block, records it and adds no entity, and
its doc comment says that acting on the rotation is L1's job — a reading, not a
rule.

## Notes

Source: Go reference implementation, phase 6 (L2 ontology graph).
