---
status: complete
priority: p2
issue_id: "026"
tags: [meta-bonds, data-model, specification-accuracy]
dependencies: []
---

# Equivalence Meta-Bond Should Support Bonds and Molecules

## Problem Statement

`_A_ is the same as _B_` currently specifies fillers as `A = atom (type 0), B = atom (type 0)`. It should also support bond and molecule equivalence.

## Findings

- `spec/06-meta-bonds.md:32-33`: Fillers restricted to atom type 0
- Use cases: two implementations create the same bond with different templates, or equivalent molecules

## Proposed Solution

Change filler types to accept atoms, bonds, or molecules. Either widen to any entity type or define separate equivalence bonds per type.

## Technical Details

- **Affected Files**: `spec/06-meta-bonds.md` (equivalence section)

## Acceptance Criteria

- [x] Equivalence meta-bond supports atom, bond, and molecule fillers
- [x] Examples updated to show bond/molecule equivalence

## Work Log

### 2026-02-24 - Approved for Work
**By:** User review
- User identified that equivalence should not be limited to atoms

### 2026-08-20 - Audit against current spec
**By:** Claude audit pass
**Actions:**
- Resolved. `spec/06-meta-bonds.md`, "1. Equivalence", declares `A = atom (type 0) / bond (type 1) / molecule (type 2)` for both fillers, with a MUST that both be the same type and a stated consequence for mixed types (a valid molecule declaring no equivalence, never an invalid block).
- Worked examples exist for all three: "Declaring atom equivalence", "Declaring bond equivalence" and "Declaring molecule equivalence" (same file).
- The informative paragraphs also pin what equivalence does *not* close over -- no composition from bond or filler equivalence up to molecules (todo 063).

## Notes

Source: User review on 2026-02-24
