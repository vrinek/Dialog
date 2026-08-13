---
status: complete
priority: p2
issue_id: "040"
tags: [specification-gap, block-validation, data-model, reachability]
dependencies: ["009"]
---

# Whether a Scalar Filler's Unit Atom Must Be Reachable Is Unstated

## Problem Statement

`spec/02-block-format.md`'s validation rule 4 says every operation "MUST
reference only entity digests that are **reachable**". Two paragraphs earlier,
the `create_molecule` section enumerates which digests it means:

> The `bond` field and any atom/bond/molecule references in `fillers` are
> 32-byte digests ... and MUST refer to entities that are **reachable** from
> this block.

A scalar filler (type 4) can carry a third kind of digest that neither
sentence clearly covers: the optional `unit` field of `scalar-value`, which
`spec/01-data-model.md` defines as the "SHA-256 digest of a unit atom".

```cddl
scalar-value = {
  ? "unit" => bstr .size 32,  ; SHA-256 digest of a unit atom
  "value" => int / #6.4([int, int]),
}
```

Is that digest an "atom reference in `fillers`", and so subject to
reachability, or is it outside the enumeration because it sits *inside* a
filler's value rather than being the filler? Rule 4's own wording ("every
operation ... entity digests") reads as covering it; the `create_molecule`
enumeration reads as not quite mentioning it.

An implementation that requires reachability rejects a molecule whose unit atom
was never published; one that does not, accepts it and adds to L2 a molecule
whose unit is a dangling digest. Both are defensible from the text, and the
blocks in question are perfectly ordinary — "70 kilograms" is the motivating
example of the feature.

## Findings

- `spec/01-data-model.md`, "Scalars": the unit is "an atom, referenced by its
  SHA-256 digest".
- `spec/03-encoding.md`, "Internal references": lists the reference-carrying
  positions — `prev`, `refs` entries, a molecule's `bond`, and "filler values
  of type 0 (atom), 1 (bond), and 2 (molecule)". The scalar `unit` is not in
  the list, though it is unambiguously a 32-byte internal reference by every
  other criterion the section gives.
- `spec/02-block-format.md` rule 4 and the `create_molecule` paragraph, quoted
  above, which do not agree on their own scope.
- The same question exists for `spec/03-encoding.md`'s list: if `unit` is not
  an internal reference, what is it?

## Proposed Solutions

### Option 1: Reachability covers every entity digest an operation carries (Recommended)

- Restate rule 4's bullet list in terms of "every entity digest carried by the
  operation, including a scalar filler's `unit`", and add `unit` to
  `spec/03-encoding.md`'s list of internal references.
- **Pros**: one rule with no exceptions; a molecule in L2 never points at an
  entity nothing defines; the unit atom is exactly the kind of shared
  vocabulary that refs exist to import.
- **Cons**: an author quoting a unit must publish or reference the unit atom,
  which is one more block dependency for a very common filler.
- **Effort**: Small (spec), none (Go — implemented this way)
- **Risk**: Low

### Option 2: Exempt the unit digest

- State that the unit is informational and need not resolve.
- **Pros**: cheaper blocks for scalar-heavy data.
- **Cons**: creates a second class of digest with different rules, and a
  consumer cannot render "70 <unknown>" any more usefully than it can render a
  dangling atom reference.
- **Risk**: Medium

## Recommended Action

Option 1, with the `spec/03-encoding.md` "Internal references" list corrected
in the same edit, since that omission is what makes the question possible.

## Technical Details

- **Affected Files**: `spec/02-block-format.md` ("create_molecule",
  "Validation" rule 4), `spec/03-encoding.md` ("Internal references")
- **Related Components**: reachability resolution, L2 accumulation
- **Database Changes**: No

## Acceptance Criteria

- [x] The specification says whether a scalar filler's `unit` digest must be
      reachable
- [x] `spec/03-encoding.md`'s internal-reference list is complete
- [x] `go/block` matches the ratified rule and its tests cover both directions

## Work Log

### 2026-08-13 - Filed While Implementing go/block
**By:** Claude

The resolver treats the unit digest as a reference that must resolve to an
atom, reading rule 4's "every entity digest" as the governing sentence. The
`TestDataModelConformance/scalar_unit_atom` case covers both directions and
points here.

### 2026-08-13 - Ratified and Implemented
**By:** Claude

**Decision (project lead):** Option 1. The optional `unit` digest of a scalar
filler **is** an internal reference, with no special status: it is subject to
reachability like every other entity digest an operation carries, and it MUST
resolve to an atom. There is no second class of digest. An author who quotes a
unit publishes the unit atom or references the block that did — the unit
vocabulary is exactly the kind of shared vocabulary `refs` exists to import —
so no molecule in the graph points at an entity nothing defines.

**Changes:**

- `spec/03-encoding.md` § "Internal references": the list of
  reference-carrying positions now includes the optional `unit` field of a
  scalar filler's value, whose omission is what made the question possible.
- `spec/02-block-format.md` § "create_molecule": the three kinds of digest a
  `create_molecule` carries are enumerated with the entity kind each must
  resolve to, and the reachability requirement is stated for all of them
  together.
- `spec/02-block-format.md` § "Validation": rule 4 now names the digest
  positions exhaustively and says there is no exempt one; rule 5 states the
  kind each position requires, including `unit` → atom.
- `go/block/op.go`: no behaviour change — `CreateMolecule.References` already
  returned the unit as a `KindAtom` reference — but the doc comment now cites
  the ratified rules rather than the reading this package chose.
- `go/block/validate_test.go` (`TestDataModelConformance/scalar_unit_atom`):
  three directions now, not two — unit reachable and an atom (valid), unit
  unknown (rule 4), and unit resolving to a bond (rule 5).

## Notes

Source: Go reference implementation, phase 3 (block).
