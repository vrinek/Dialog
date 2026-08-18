---
status: complete
priority: p3
issue_id: "059"
tags: [specification-gap, meta-bonds, data-model, validation]
dependencies: []
---

# A Meta-Bond's Filler Shapes Are Stated but Never Required

## Problem Statement

Each of the five standard meta-bonds in `spec/06-meta-bonds.md` is printed with
two lines:

```
Template: "_A_ is true"
Fillers:  A = molecule (type 2)
```

The `Fillers:` line has no normative force anywhere. It is not RFC 2119 text, it
is not repeated in prose, and no validation rule refers to it. Only equivalence
adds a MUST — "Both fillers MUST be the same type (both atoms, both bonds, or
both molecules)" — and even that MUST names no consequence: nothing says whether
a molecule violating it is invalid, or valid and inert, or valid and applied
anyway.

So an implementation building `create_molecule` for a meta-bond has no answer to
a question it must answer in code:

- Is `{"bond": <digest of "_A_ is true">, "fillers": [{"type": 0, "value": <an
  atom>}]}` a valid molecule? A truth assertion about an atom is meaningless,
  but the data model it must satisfy (`spec/02-block-format.md`, rule 5) is
  `spec/01-data-model.md`, which knows nothing about meta-bonds: the filler
  count matches, the filler shape is a legal type 0 filler, and the bond digest
  resolves to a bond. By that rule the molecule is valid.
- Is an equivalence between an atom and a molecule a block-validation failure,
  or a molecule L3 should ignore, or one L3 should apply?

The two readings diverge observably. Under "reject", a block carrying such a
molecule is refused, its digest never enters L2, and an implementation that
accepted it holds an entity its peer cannot receive. Under "accept", the entity
exists everywhere and implementations differ only in what L3 does with it —
which `spec/06-meta-bonds.md` already declares implementation-scoped.

## Findings

- `spec/06-meta-bonds.md` §§1-5: each meta-bond's `Fillers:` line is inside the
  same fenced block as its template, in the style of an annotation. §1 alone
  adds "Both fillers MUST be the same type"; §§2-5 state nothing normative about
  their fillers.
- `spec/02-block-format.md`, "Validation" rule 5 (data model conformance):
  requires the filler count to match the template's variables and each digest to
  resolve to an entity of the kind its *position* names — where "position" means
  the filler's own type tag (type 2 → a molecule), not the meta-bond's
  expectation. A type 0 filler in `_A_ is true` resolves to an atom and
  satisfies the rule.
- `spec/01-data-model.md` contains no meta-bond rule at all; the bond is just a
  template string, and a molecule is fillers positionally matched to variables.
- `spec/05-processing-model.md`, "Meta-molecule application": "Implementations
  MUST recognize the standard meta-bonds" and the behaviour is
  implementation-scoped; nothing about malformed meta-molecules.
- `vectors/entities.json`, `molecules/paris_equivalence`: the one meta-molecule
  in the vectors, and a well-formed one (two type 0 fillers). No case exercises
  a mismatched pair, so both readings pass the conformance suite.
- The same silence covers a subtler case: `_A_ contradicts _B_` with the same
  molecule digest in both fillers, or `_A_ supersedes _B_` where A and B are
  equal. Self-contradiction and self-supersession are meaningless; nothing
  forbids them.

## Proposed Solutions

### Option 1: State that filler shapes are an L3 concern, not a validation rule (Recommended)

Add to `spec/06-meta-bonds.md`, "Meta-molecules are regular molecules":

> The `Fillers:` line of each meta-bond above describes the filler types the
> meta-bond's semantics are defined for. It is not a validation rule: a molecule
> built from a meta-bond is validated exactly as any other molecule
> (`spec/02-block-format.md`, "Validation"), and a meta-molecule whose fillers
> do not match the shape its meta-bond expects is a valid molecule with no
> defined L3 semantics. Implementations MUST NOT reject it at L1 or L2, and
> SHOULD ignore it when applying meta-molecule semantics.

and downgrade §1's MUST to the same footing ("A meta-molecule whose two fillers
are of different types has no defined equivalence semantics").

- **Pros**: keeps L1 validation schema-driven and meta-bond-agnostic, which is
  what "meta-molecules are regular molecules" already claims and what makes
  custom meta-bonds work; no implementation needs a table of standard templates
  to validate a block; the divergence disappears because both halves are stated.
- **Cons**: nonsense meta-molecules are storable and propagate; L3 carries the
  filtering burden.
- **Effort**: Small (spec), Small (implementations)
- **Risk**: Low

### Option 2: Make the filler shapes a validation rule

Add to `spec/02-block-format.md`, "Validation": a `create_molecule` whose bond
digest matches a standard meta-bond MUST carry fillers of the types that
meta-bond declares, and implementations MUST reject one that does not.

- **Pros**: nonsense meta-molecules never enter the graph.
- **Cons**: block validation would depend on the meta-bond library, so the
  standard library becomes part of L1 rather than L3 — and a future meta-bond,
  or a custom one, changes which blocks validate. It also cuts against
  "Meta-molecules are created with the same `create_molecule` operation as any
  other molecule" and against the extension process, where an implementation may
  define meta-bonds others do not know.
- **Risk**: Medium

## Recommended Action

Option 1, and add one `entities.json` case for a meta-molecule whose fillers do
not match its meta-bond, marked valid, so that the reading is pinned in bytes as
well as prose. Whatever is chosen, §1's MUST needs a consequence attached to it.

## Technical Details

- **Affected Files**: `spec/06-meta-bonds.md` (§1 and "Meta-molecules are
  regular molecules"), possibly `spec/02-block-format.md` ("Validation"),
  `vectors/entities.json`
- **Related Components**: meta-bond library, block validation, L2→L3 processing
- **Database Changes**: No

## Acceptance Criteria

- [x] `spec/06-meta-bonds.md` says whether a meta-molecule with unexpected
      filler types is invalid, or valid with no defined semantics
- [x] §1's "Both fillers MUST be the same type" names the consequence of
      violating it
- [x] A conformance vector fixes the answer

## Work Log

### 2026-08-18 - Filed While Implementing ts/src/entity.ts

**By:** Claude

Found building the second implementation from `spec/` and `vectors/` only. The
TypeScript implementation takes the defensible reading — Option 1: the meta-bond
constants expose each template, its variables and its digest, and
`newMoleculeForBond` checks the filler *count* against the template like any
other bond, but no filler *type* check is applied to a meta-bond, because
`spec/02-block-format.md` rule 5 cites only `spec/01-data-model.md` and that
document has no notion of a meta-bond. An implementation that read the
`Fillers:` lines as normative would reject molecules this one accepts.

### 2026-08-18 - Ratified and Applied

**By:** Claude

**Decision (project lead):** Option 1. A meta-bond's `Fillers:` line is a
recognition criterion applied during L2→L3 processing, not a rule of block
validity.

**Changes:**

- `spec/06-meta-bonds.md`, "Meta-molecules are regular molecules": a normative
  paragraph and three bullets. Implementations MUST NOT reject a block, or
  refuse an entity at L1 or L2, because a molecule's bond matches a standard
  meta-bond while its fillers do not match the declared shape; MUST NOT apply
  the meta-bond's L3 semantics to it; and SHOULD surface it to the application
  rather than discard it. Followed by the reason — validation stays
  schema-driven and meta-bond-agnostic, so whether a block validates cannot
  depend on which meta-bonds the validator happens to know — and an informative
  paragraph recording that the reference implementation keeps such a molecule
  in the L3 view and lists it separately.
- `spec/06-meta-bonds.md` §1: the MUST now names its consequence. It binds the
  author; a molecule violating it is valid and declares no equivalence, and
  implementations MUST NOT unify the entities it names.
- `vectors/entities.json`: `molecules/truth_of_an_atom` — `"_A_ is true"` filled
  with an atom — is a **valid** entity of the file, which fixes the reading in
  bytes. `ts/test/entity.test.ts` asserts it is still recognized as a
  meta-molecule by its bond digest.
- `go/accept`: no behaviour change. `MalformedMetaMolecules` already did exactly
  this — the molecule stays in the view, is read as no assertion, and is listed
  — and `TestMalformedMetaMoleculesAreIgnored` already covered the three shapes.
  Its comments and the package documentation now cite the section that requires
  it rather than deriving it.
- `ts/`: has no L3, so nothing beyond the vector case above.

## Notes

Source: TypeScript implementation, phase 2 (entity).
