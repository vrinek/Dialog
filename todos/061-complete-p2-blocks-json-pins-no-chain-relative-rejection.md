---
status: complete
priority: p2
issue_id: "061"
tags: [conformance-vectors, block-format, validation, interoperability]
dependencies: []
---

# `blocks.json` Pins No Chain-Relative Rejection

## Problem Statement

`vectors/blocks.json` has 23 invalid cases, and every one of them is a rejection
a decoder can make from a single block's bytes: a bad version, a bad type, a
closed-map violation, a wrong field size, a broken signature, the rotation-block
constraints, a duplicate `refs` entry. Not one of them is a rejection that
depends on *what else the node holds*.

That is exactly the half of `spec/02-block-format.md`, "Validation" that is hard
to implement. Rules 3, 4, 5 and 6, the own-chain half of rule 10 and the
succession rules of "rotate_key" are all relations between a block and a store,
and the interop contract is silent on every one of them:

- **Rule 3** — a `prev` naming a block of another author's chain, or a block
  appended to a chain a rotation block already ended.
- **Rule 4** — a `create_molecule` whose bond digest is reachable from nowhere;
  one whose digest is defined by a *later* operation of the same block; one
  whose scalar filler's `unit` resolves nowhere, which is the position an
  implementation is most likely to exempt, since spec/02 had to say in prose
  that there is no exempt position.
- **Rule 5** — a `bond` field resolving to an atom, a type 2 filler resolving to
  an atom, a filler count that does not match the resolved template's variable
  count.
- **Rule 6** — a public block whose `refs` name a private block.
- **Rule 10, own-chain half** — a block referencing its own predecessor.
- **Succession** — a genesis block that is not public in the successor
  position, and two genesis blocks claiming one rotation block.

The `chain` section demonstrates the accepting side of rules 3, 4 and the
succession, and the `forks` section demonstrates rule 9. The rejecting side has
no bytes at all, which means two implementations can pass every case in the file
and still disagree about which chains exist — the same defect todo 058 recorded
for `entities.json`, one layer up and with a store in the middle.

## Findings

- `vectors/blocks.json`, `invalid` (23 cases): every case is rejected by
  `decodeBlock` plus structural validation alone; none needs a store. Verified
  against the TypeScript implementation, where all 23 fail before rule 3 is
  reached.
- The rule strings the section uses name rules 1, 2, 7, 8 and 10 (the duplicate
  half), "Validation dispatch", "Rotation block", "Private block" and
  spec/03's "Internal references". Rules 3, 4, 5, 6 and 9 appear nowhere in the
  `invalid` section.
- `vectors/README.md` prescribes replaying the `chain` section into a store and
  validating each block, which exercises the accepting side of rules 3 and 4
  and nothing else.
- The TypeScript suite's chain-relative rejections — a dozen of them — are
  hand-written from the prose (`ts/test/block.test.ts`, everything below "The
  rules the vectors do not pin"). Nothing in `vectors/` says the reference
  implementation must agree with any of them.

## Proposed Solutions

### Option 1: Add a scenario-shaped invalid section (Recommended)

The existing `invalid` shape (`bytes`, `rule`, `reason`) cannot express a
rejection that depends on a store. Add a second section — `invalid_in_chain`,
say — whose cases carry a small ordered list of blocks to replay plus the block
that MUST be rejected, and the rule it violates:

```jsonc
{
  "name": "molecule_bond_resolves_to_an_atom",
  "rule": "spec/02-block-format.md, Validation rule 5 (data model conformance)",
  "reason": "The bond field names an atom created by an earlier operation.",
  "setup": ["<block hex>", "..."],   // accepted in this order
  "bytes": "<block hex>"             // MUST be rejected
}
```

Cases worth pinning, one per rule above, all reusing the existing test keys:
`prev_of_another_chain`, `appended_after_rotation`, `unreachable_bond`,
`forward_reference_in_same_block`, `unreachable_scalar_unit`,
`bond_resolves_to_an_atom`, `filler_kind_mismatch`, `filler_count_mismatch`,
`public_block_references_a_private_block`, `refs_names_own_chain`, and
`two_genesis_blocks_claim_one_rotation` (which is a detection, like `forks`,
not a rejection).

- **Pros**: the interop contract finally covers the half of validation that
  needs a node; the `setup` list is the same replay `vectors/README.md` already
  asks readers to perform; costs no committed byte.
- **Cons**: a new case shape, and readers must build a store before they can use
  it — which is precisely the point, and is step 3 of the README's walkthrough
  anyway.
- **Effort**: Medium (generator + vectors), Small (implementations)
- **Risk**: Low — additive.

### Option 2: Extend the `chain` section with rejected blocks

Add blocks to the existing scenario, each marked `"accept": false`.

- **Pros**: one section, one replay.
- **Cons**: a scenario that must not accept some of its own blocks is easy to
  misread, and the rejected block's position in the chain becomes significant
  in a way the file does not otherwise use.
- **Risk**: Medium

### Option 3: Leave it

- **Pros**: nothing to do.
- **Cons**: an implementation that never checks rule 5's digest-kind binding, or
  quietly exempts a scalar filler's `unit` from rule 4, passes every committed
  vector. Both mistakes produce blocks other nodes refuse.
- **Risk**: High — these are the rules whose omission is invisible until two
  implementations exchange blocks.

## Recommended Action

Option 1, with `unreachable_scalar_unit` and `filler_kind_mismatch` as the two
cases to write first: the specification had to argue both of them in prose
("There is no exempt position"; "each digest the operation carries MUST resolve
to an entity of the kind its position names"), which is a reliable sign that an
implementer will get them wrong.

## Technical Details

- **Affected Files**: `vectors/blocks.json` (new section), `vectors/README.md`
  (the file table's section counts and the case-shape description), the vector
  generator, both implementations' block test suites
- **Related Components**: L1 validation, conformance vectors
- **Database Changes**: No

## Acceptance Criteria

- [x] `vectors/blocks.json` pins at least one rejection per chain-relative rule
      (3, 4, 5, 6, the own-chain half of 10) and the ambiguous succession —
      except the succession, which is not constructible as a rejection; see the
      work log
- [x] `vectors/README.md` records the new section, its shape and its case count
- [x] Both implementations replay each case's setup and reject its block with
      the named rule

## Work Log

### 2026-08-18 - Filed While Implementing ts/src/block.ts

**By:** Claude

Found building the second implementation from `spec/` and `vectors/` only. All
23 invalid cases passed on the first run, and every one of them was rejected by
the decoder — the store the implementation had just been given had nothing to
do. The rules that took the work, and the ones where a reading had to be chosen
(what a `unit` digest is subject to; whether a public block may name a rotation
block; what happens to a block signed by a rotated-away key), are pinned by
nothing.

### 2026-08-18 - Ratified and Applied

**By:** Claude

**Decision (project lead):** Option 1. `blocks.json` gains an `invalid_in_chain`
section whose cases carry a `setup` list to replay and the block that must then
be rejected.

**Changes:**

- `vectors/blocks.json`: a new `invalid_in_chain` section of **12 cases**. Each
  carries `setup` (ordered block hexes to replay into a fresh store, every one
  valid), `bytes` (the block that MUST be rejected), the `rule` it violates and
  a `reason`; `scan_limit` appears on the one case whose rejection is the limit.
  The cases: `prev_of_another_chain` and `appended_after_rotation` (rule 3);
  `unreachable_bond`, `bond_defined_in_an_unreferenced_chain`,
  `forward_reference_in_same_block`, `unreachable_scalar_unit` and
  `scan_limit_exceeded` (rule 4); `bond_resolves_to_an_atom`,
  `unit_resolves_to_a_bond` and `filler_count_mismatch` (rule 5);
  `public_block_references_a_private_block` (rule 6); `refs_names_own_chain`
  (rule 10's own-chain half). Two new test keys, `carol` and `dave`, so a refs
  graph can be three hops deep — every hop must cross to another author,
  because rule 10 forbids naming one's own chain.
- `go/internal/vectors/blocks_chain.go`: the generator, with a
  `verifyChainRejection` in the spirit of `mustReject`. Each case must satisfy
  three things before it is emitted: every setup block validates as it is
  replayed, the rejected block *decodes* — a case here is never a decoder's
  rejection, which is what separates this section from `invalid` — and the
  reference validator rejects it under the very rule number the case's label
  names. A case carrying `scan_limit` is additionally checked valid under the
  default limit, so it pins the limit and not the block.
- `go/internal/vectorfile/vectorfile.go`: the `InvalidInChainCase` shape.
- `go/conformance_test.go`: `checkChainRejection` replays each case and asserts
  the `*block.RuleError` carries the rule number the label names.
- `ts/test/block.test.ts`: the same replay through `BlockStore`, mapping each
  rule label to the `BlockErrorCode` this implementation reports, and the
  per-case `scanLimit` where present. All twelve passed on the first run
  against an implementation built from the prose alone — which is the outcome
  the section exists to make checkable rather than lucky.
- `vectors/README.md`: the section, its shape, its case count, and where it
  belongs in the walkthrough (after the store works).

**What could not be pinned.** None of the succession conditions is a
chain-relative *rejection*:

- A successor genesis block that omits the rotation reference is not invalid —
  the specification says it is "a valid chain of an unrelated author, as far as
  any node can tell".
- A non-public successor genesis block is a private block whose `refs` the
  rejecting node cannot read; the party that decrypts it rejects it, and no
  vector can hand a node a key it does not have.
- `new_pub == pub` is a relation between two fields of one block and is already
  a bytes-only case, `rotation_to_the_same_key`.
- Two genesis blocks claiming one rotation is a *detection*, like `forks`, not
  a rejection, and the `forks` section is where a detection belongs.

**Outcome:** the interop contract now covers the half of validation that needs
a node. An implementation that exempts a scalar filler's `unit` from rule 4, or
skips the digest-kind binding, or counts the scan limit some other way, fails a
committed vector instead of quietly producing blocks other nodes refuse.

## Notes

Source: TypeScript implementation, phase 3 (block). Sibling of todo 058, which
recorded the same gap for `entities.json` and was settled by adding an `invalid`
section there.
