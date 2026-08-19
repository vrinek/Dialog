---
status: complete
priority: p3
issue_id: "066"
tags: [conformance-vectors, meta-bonds, validation, interoperability]
dependencies: ["065"]
---

# No Conformance Vector Publishes a Meta-Molecule With Its Bond

## Problem Statement

`todos/065` settled that the five standard meta-bonds are ordinary entities: a
block carrying `create_molecule(bond: <digest of "_A_ is true">, …)` is invalid
unless that bond is reachable from it, and `spec/06-meta-bonds.md`,
"Meta-molecules are regular molecules", now says so in as many words. Nothing
pins it in bytes.

`vectors/blocks.json` has no case publishing a meta-molecule at all — not in the
valid section, where a block carrying `create_bond("_A_ is true")` followed by
the assertion that uses it would show the shape an author has to produce, and
not in `invalid_in_chain`, where the same block with the `create_bond` omitted
is a rule 4 rejection like any other. An implementation could therefore read the
meta-bond digests as ambient, pass every vector, and reject blocks the reference
implementation accepts — or accept blocks it rejects — with nothing in the suite
to catch it.

The gap is narrow but it is exactly the kind the vectors exist for: the two
readings differ on whether a *valid* block is valid, which is an
interoperability failure and not a presentation choice.

## Findings

- `vectors/blocks.json`: no `create_molecule` naming a standard meta-bond
  digest, in any section.
- `vectors/entities.json`: has the `truth_of_an_atom` molecule, which pins a
  malformed meta-molecule as a valid *entity*. That is the L2→L3 recognition
  question (`todos/059`), not the rule 4 question.
- `spec/06-meta-bonds.md`, "Meta-molecules are regular molecules": the
  requirement, as of the change closing `todos/065`.
- `go/internal/vectors/blocks_chain.go`: the `invalid_in_chain` generator, whose
  rule 4 cases (`unreachable_bond`, `bond_defined_in_an_unreferenced_chain`) are
  the shape a meta-bond case would take.

## Proposed Solutions

### Option 1: One valid case and one invalid case (Recommended)

Add to `vectors/blocks.json` a valid block that carries `create_bond` for a
standard meta-bond together with the meta-molecule using it, and an
`invalid_in_chain` case whose block carries only the meta-molecule, rejected for
rule 4.

- **Pros**: pins both halves; costs two generator cases; needs no spec change.
- **Cons**: moves `vectors/blocks.json`, so every implementation re-runs its
  suite and the case counts in `vectors/README.md` and `ts/test/blocks.test.ts`
  move with it.
- **Effort**: Small
- **Risk**: Low

### Option 2: The invalid case only

- **Pros**: smaller diff; the rejection is the half an implementation can get
  wrong silently.
- **Cons**: leaves the publishable shape unshown, which is the half an
  implementer copies.
- **Risk**: Low

## Recommended Action

Option 1, together with the next change that is allowed to move `vectors/`.

## Technical Details

- **Affected Files**: `vectors/blocks.json`, `go/internal/vectors/blocks.go`,
  `go/internal/vectors/blocks_chain.go`, `vectors/README.md`,
  `ts/test/blocks.test.ts` (case counts)
- **Related Components**: block validation rule 4, meta-bond publication
- **Database Changes**: No

## Acceptance Criteria

- [x] `vectors/blocks.json` holds a valid block publishing a meta-bond and a
      meta-molecule that uses it
- [x] `vectors/blocks.json` holds an `invalid_in_chain` case rejecting the same
      block without the `create_bond`, for rule 4
- [x] The case counts in `vectors/README.md` and the TypeScript tests agree

## Work Log

### 2026-08-18 - Filed While Closing 065

**By:** Claude

`todos/065` was applied as a spec-only change under an explicit constraint that
`vectors/` stay byte-identical, so its third acceptance criterion was deferred
here rather than dropped.

### 2026-08-19 - Applied

**By:** Claude

Option 1, as filed and as ratified with 065. Two cases, one on each side of
rule 4, generated rather than transcribed like everything else in `vectors/`:

- `chain/bob_meta_molecule` — a sixth block in the scenario, the next block of
  Bob's chain. It carries `create_bond("_A_ is true")` and the
  `create_molecule` asserting the molecule his genesis block published, in that
  order and in one block. `refs` is empty: the asserted molecule was created by
  an ancestor of this block's own chain, so the only dependency needing
  publication is the meta-bond itself. This is the `atlas` strategy of
  spec/06's informative paragraph, which is the one an implementer copies.
- `invalid_in_chain/unreachable_meta_bond` — the same shape with the
  `create_bond` dropped: setup publishes the molecule, the rejected block names
  the meta-bond digest, and rule 4 refuses it. The generator verifies the rule
  number against the reference validator, as every case in that section does,
  so the file cannot pin a verdict this implementation does not reach.

**Changes:**

- `go/internal/vectors/blocks.go`: the new chain block (`tsBobMeta`, between
  Bob's genesis and Alice's rotation, so the section stays chronological), its
  description, the file description and `spec/06-meta-bonds.md` added to the
  file's `spec` list.
- `go/internal/vectors/blocks_chain.go`: `rejectUnreachableMetaBond`, grouped
  with the other rule 4 cases.
- `vectors/blocks.json`: regenerated. Purely additive — no existing block's
  bytes, digest or signature moved, because the new chain block is appended to
  a chain nothing else links to.
- `vectors/README.md`: counts `chain` (5 → 6) and `invalid_in_chain` (12 → 13),
  06-meta-bonds.md in the file's specification column, and a paragraph in step
  3 naming the pair and what an implementation reading the five digests as
  ambient gets wrong.
- `ts/test/block.test.ts`: the expected counts, the store size after replaying
  the chain, and the two indices the rotation/succession test reads.

Both suites consume the new cases through the machinery that was already
there: Go's `TestBlockVectors` replays the chain and the chain-relative
rejections by name, and the TypeScript test iterates `invalid_in_chain`
generically and maps the rule string to its `reachability` error code.

## Notes

Source: the ratification of `todos/065`.
