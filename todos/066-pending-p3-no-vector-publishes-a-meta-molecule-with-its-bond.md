---
status: pending
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

- [ ] `vectors/blocks.json` holds a valid block publishing a meta-bond and a
      meta-molecule that uses it
- [ ] `vectors/blocks.json` holds an `invalid_in_chain` case rejecting the same
      block without the `create_bond`, for rule 4
- [ ] The case counts in `vectors/README.md` and the TypeScript tests agree

## Work Log

### 2026-08-18 - Filed While Closing 065

**By:** Claude

`todos/065` was applied as a spec-only change under an explicit constraint that
`vectors/` stay byte-identical, so its third acceptance criterion was deferred
here rather than dropped.

## Notes

Source: the ratification of `todos/065`.
