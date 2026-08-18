---
status: pending
priority: p2
issue_id: "063"
tags: [meta-bonds, l3, specification-consistency, equivalence]
dependencies: []
---

# Equivalence Does Not Compose Through a Molecule's Parts

## Problem Statement

`spec/06-meta-bonds.md`, "Declaring bond equivalence", ends its worked example
with a sentence that reads as a rule:

> In L3, molecules using either bond template are treated as expressing the same
> relationship.

Nothing in the specification says how an implementation is supposed to make that
true, and the reference implementation does not make it true. Equivalence is a
closure over the pairs subscribed authors have *declared*; no rule derives a new
equivalence from the equivalence of a molecule's parts. So two molecules whose
bonds are declared equivalent, and whose every filler is declared equivalent
position by position, are not equivalent — they are two classes of one, they
carry independent truth states, and a truth assertion about one says nothing
about the other.

The grounding demo hit this while modelling naming variants. Its `gazetteer`
author publishes two molecules of identical shape:

```
gazetteer: "Lisboa is the capital city of Portugal"
gazetteer: "Amsterdam is the capital city of Holland"
```

against `atlas`'s

```
atlas: "Lisbon is the capital of Portugal"
atlas: "Amsterdam is the capital of Netherlands"
```

with `"_A_ is the capital city of _B_"` ≡ `"_A_ is the capital of _B_"`,
`"Lisboa"` ≡ `"Lisbon"` and `"Holland"` ≡ `"Netherlands"` all declared. The
Lisboa pair is *also* declared equivalent at the molecule level and is unified
in L3. The Amsterdam pair is not, and is not unified — although the two
molecules differ only in entities the same author has just declared
interchangeable.

The consequence is not cosmetic. `spec/06-meta-bonds.md`'s informative
paragraph on equivalence makes the class the unit of truth: an assertion,
retraction, contradiction or supersession naming any member is a statement about
the whole class. An author who retracts atlas's Amsterdam molecule has therefore
said nothing about gazetteer's, even though a reader would say they state the
same fact — and an application grounding an answer in L3 sees two unrelated
facts where the publisher meant one.

## Findings

- `spec/06-meta-bonds.md`, "Declaring bond equivalence": the sentence quoted
  above. The atom-equivalence example is more careful — "Users who subscribe to
  Author C will see both atoms as equivalent in L3" — and claims nothing about
  molecules built from them.
- `spec/06-meta-bonds.md`, "Declaring molecule equivalence": "This is useful
  when atom-level or bond-level equivalence alone is insufficient because the
  molecules combine different atoms and different bond templates." This implies
  that atom- or bond-level equivalence *is* sometimes sufficient, without saying
  when — which is the same claim as the bond-equivalence sentence, from the
  other side.
- `spec/06-meta-bonds.md` §1, "L3 semantics": "Implementations SHOULD treat
  equivalent entities as interchangeable when querying L3. The specific
  deduplication strategy (merge, prefer one, show both) is
  implementation-scoped." "Interchangeable when querying" does not say whether
  the interchange happens *inside* a molecule.
- `go/accept`: `EquivalenceClass` is the transitive closure of declared
  equivalence molecules and nothing else. `TestEquivalenceDoesNotComposeThrough`
  `MoleculeStructure` in `demo/internal/replay` pins the current behaviour
  against the demo's committed chains.
- Deriving the equivalence structurally is not free. Two molecules are
  structurally equivalent when their bonds are equivalent, they have the same
  filler count, and each filler is either equal or an equivalence of the same
  type — which is a fixpoint computation over the whole view (a derived
  molecule equivalence can make two further molecules structurally equivalent),
  and one that has to decide what "equivalent" means for scalar and IPFS
  fillers, which no equivalence can name. It also widens the equivalence-attack
  surface `spec/06-meta-bonds.md`, "Security Considerations", already warns
  about: one bond equivalence would silently unify every molecule using either
  template.

## Proposed Solutions

### Option 1: Say that equivalence is declared, never derived (Recommended)

Replace the bond-equivalence example's closing sentence with a statement of what
bond equivalence actually gives an application, and add a normative sentence to
§1:

> Equivalence relates the entities a meta-molecule names. It is transitive, and
> it is not otherwise closed: implementations MUST NOT derive an equivalence
> between two molecules from the equivalence of their bonds or of their fillers.
> An author who wants two molecules treated as the same statement declares that
> with a molecule-level equivalence.

and let the example say instead: "In L3, an application that has found a
molecule using one template can find the other template's molecules by walking
the bond's equivalence class."

- **Pros**: matches every implementation that exists; keeps the closure cheap
  and its blast radius small; makes the molecule-equivalence section's reason
  for existing exact rather than approximate.
- **Cons**: an author publishing a naming variant must publish one equivalence
  per molecule, not one per atom — which is the cost the demo paid.
- **Effort**: Small (spec), none (implementations)
- **Risk**: Low

### Option 2: Define structural equivalence normatively

Specify the fixpoint: two molecules are equivalent if their bonds are equivalent
or equal, they carry the same number of fillers, and for each position the two
fillers are equal or equivalent entities of the same type. State that scalar and
IPFS fillers are equivalent only when identical, and bound the iteration.

- **Pros**: the bond-equivalence example becomes true as written; naming variants
  propagate the way a publisher expects.
- **Cons**: a much larger L3 computation, specified in full or implementations
  diverge; every implementation must agree on the fixpoint or two nodes disagree
  about what is in a class, which is exactly what a class carrying truth cannot
  afford; and one malicious bond equivalence unifies every molecule built on
  either bond.
- **Effort**: Large (spec and implementations)
- **Risk**: High

### Option 3: Leave it implementation-scoped, but say so

Keep both readings conformant and add a sentence saying that whether equivalence
composes through a molecule's parts is implementation-scoped.

- **Pros**: cheapest change.
- **Cons**: two conformant nodes would disagree about whether a fact is
  retracted, which is worse than either answer. Equivalence composition decides
  truth states, not presentation.
- **Risk**: Medium

## Recommended Action

Option 1, plus an `accept`-level test in `go/` naming the rule, and the demo's
existing test as the worked case.

## Technical Details

- **Affected Files**: `spec/06-meta-bonds.md` (§1 "L3 semantics", "Declaring
  bond equivalence", "Declaring molecule equivalence"), possibly
  `spec/05-processing-model.md` ("Meta-molecule application")
- **Related Components**: L3 equivalence closure, truth distillation
- **Database Changes**: No

## Acceptance Criteria

- [ ] `spec/06-meta-bonds.md` says whether an equivalence between two molecules
      can be derived from equivalences between their bonds and fillers
- [ ] The bond-equivalence example's closing sentence agrees with that answer
- [ ] The reference implementation's behaviour is either confirmed by a test
      naming the rule, or changed to match it

## Work Log

### 2026-08-18 - Filed While Building the Grounding Demo

**By:** Claude

Found in phase 1 of the grounding demo, modelling `gazetteer`'s naming variants
over `atlas`'s facts. The demo keeps both shapes — one pair declared equivalent
at the molecule level, one pair left to compose from its parts — so that the
difference is visible in the committed chains and pinned by a test.

## Notes

Source: grounding demo, phase 1 (content and chains).
