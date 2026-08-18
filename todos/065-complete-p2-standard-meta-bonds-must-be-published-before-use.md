---
status: complete
priority: p2
issue_id: "065"
tags: [meta-bonds, block-format, validation, specification-gap]
dependencies: []
---

# The Standard Meta-Bonds Must Be Published On-Chain Before They Can Be Used

## Problem Statement

Every example in `spec/06-meta-bonds.md` publishes a meta-molecule like this:

```
create_molecule(
  bond: <digest of "_A_ is the same as _B_">,
  fillers: [...]
)
```

and no example publishes the bond. Read on its own, the section suggests that
the five standard meta-bonds are ambient — identifiers every implementation
knows, since the section requires every implementation to support them and to
recognize them by digest.

They are not. `spec/02-block-format.md`, "Validation" rule 4, makes every entity
digest an operation carries subject to reachability, and says so exhaustively:

> The entity digests an operation carries are, exhaustively: a
> `create_molecule`'s `bond` field, each of its filler values of type 0, 1 or 2,
> and the optional `unit` inside each of its scalar filler values. **There is no
> exempt position** — every digest an operation carries is subject to this rule.

So a block carrying `create_molecule(bond: <"_A_ is true">, …)` is **invalid**
unless some reachable block — the same block, an ancestor of the author's chain,
or a block named in `refs` and traversed transitively — carries
`create_bond("_A_ is true")`. The two documents are consistent; nothing says the
combination out loud, and the section an author would read to learn how to
publish a meta-molecule shows only the half that does not work on its own.

The grounding demo hit it on its first meta-molecule. What it had to do:

- `atlas` publishes `create_bond("_A_ is true")` in the block where it first
  asserts something true.
- `gazetteer` re-publishes four of the five standard bonds in its genesis block
  — the same entity `atlas` already published, which accumulates a second
  authorship record in L2 rather than a second entity.
- `errata` instead names `gazetteer`'s genesis block in `refs`, which is the
  other way to satisfy rule 4 and which couples its blocks to another author's
  chain layout.

All three are correct and the choice between them is not obvious. The
consequence is that the five best-known entities in the protocol have no
canonical publication: they exist once per author who bothered to create them,
each with its own authorship record, and an author who wants to use one either
copies it or takes a dependency on whoever did.

There is an L3 consequence too. A bond is an entity like any other, so it is
subject to the L3 filtering rule: a view whose subscriptions do not include any
author who published `"_A_ is true"` does not contain that bond, though it
contains molecules built from it — the case `todos/053` describes in general.
For the standard meta-bonds this is unnecessary: L3 recognizes a meta-molecule
by comparing its `bond` digest against the standard digests, which it knows
without holding the entity.

## Findings

- `spec/06-meta-bonds.md`, "Standard meta-bond library": "These bonds are
  content-addressed like any other bond — their identifiers are computed from
  their template strings". True, and it is the sentence that makes the digests
  well known; nothing follows it about whether the entity must exist on a chain.
- `spec/06-meta-bonds.md`, examples in "Declaring atom equivalence", "Declaring
  bond equivalence", "Declaring molecule equivalence": four `create_molecule`
  operations naming meta-bond digests, no `create_bond`.
- `spec/02-block-format.md`, "Validation" rules 4 and 5, quoted above: the bond
  digest must be reachable and must resolve to a bond.
- `spec/05-processing-model.md`, "Resolution procedure": the three resolution
  paths. None of them is "a well-known entity table".
- `go/block`: `validateReferences` resolves a `create_molecule`'s bond digest
  through the same resolver as every other reference; `entity.LookupMetaBond`
  is not consulted anywhere in validation, which is the correct reading of
  "no exempt position".
- `vectors/blocks.json`: no case publishes a meta-molecule, so neither reading
  is pinned in bytes.
- The demo's `atlas` chain shows the cost is small — one `create_bond`
  operation, once per author — but the demo also shows that authors will pick
  different strategies, and `refs` into another author's chain is a real
  coupling.

## Proposed Solutions

### Option 1: State the requirement, and name the strategies (Recommended)

Add to `spec/06-meta-bonds.md`, "Meta-molecules are regular molecules":

> A meta-bond is an entity, so a molecule naming one satisfies validation rule 4
> the same way as any other molecule (`spec/02-block-format.md`): the bond MUST
> be reachable from the block that uses it. An author publishes
> `create_bond` for the meta-bonds they use — in the block that first uses one
> or in an earlier block of their chain — or names a block that did in `refs`.
> The standard meta-bonds are not implicitly present in any chain.

and add a `create_bond` operation to the examples so they are publishable as
written.

- **Pros**: no change to validation, to the data model, or to any
  implementation; removes the only reading under which the examples are
  complete; keeps L1 free of a well-known-entity table, which is the same
  principle `todos/059` settled for filler shapes — whether a block validates
  must not depend on which meta-bonds the validator knows.
- **Cons**: leaves the five standard bonds with one entity and many authorship
  records, and leaves the L3 filtering wrinkle above unaddressed.
- **Effort**: Small (spec), none (implementations)
- **Risk**: Low

### Option 2: Make the standard meta-bonds implicitly reachable

Add an exemption to rule 4: a digest matching a standard meta-bond resolves
without a defining block, and L2 holds the five bonds unconditionally, with no
authorship records.

- **Pros**: the section's examples become correct as written; meta-molecules
  cost one operation instead of two; no author has to take a `refs` dependency
  on another to say "this is true"; the bonds are in every L3 view, so a
  meta-molecule's template can always be rendered.
- **Cons**: makes L1 validation depend on the meta-bond library, which is what
  `spec/06-meta-bonds.md` and `todos/059` deliberately avoided: a future
  standard meta-bond would change which blocks validate, and a custom meta-bond
  would still need publishing, so authors would face two rules instead of one.
  It also puts entities in L2 that no author published, which the accumulation
  rules have no shape for (`spec/05-processing-model.md` requires an authorship
  tag naming a public key and a block).
- **Risk**: High

### Option 3: L3-only exemption

Keep rule 4 as it is, and say that L3 recognizes and renders a standard
meta-bond by its digest whether or not the bond entity is in the view.

- **Pros**: fixes the filtering wrinkle without touching L1; small.
- **Cons**: does not address the misleading examples, which is the part that
  costs an implementer a debugging session.
- **Risk**: Low

## Recommended Action

Option 1, and Option 3 alongside it as a sentence in
`spec/05-processing-model.md`, "Filtering rules". Add one `blocks.json` case
publishing a meta-molecule together with its `create_bond`, so the requirement
is pinned in bytes; a case with the `create_bond` omitted belongs in the invalid
section for rule 4.

## Technical Details

- **Affected Files**: `spec/06-meta-bonds.md` ("Meta-molecules are regular
  molecules", the three examples), `spec/05-processing-model.md` ("Filtering
  rules"), `vectors/blocks.json`
- **Related Components**: block validation rule 4, L2 accumulation, L3
  filtering
- **Database Changes**: No

## Acceptance Criteria

- [x] `spec/06-meta-bonds.md` says that a meta-bond must be reachable from the
      block whose molecule names it, and how an author arranges that
- [x] The examples in `spec/06-meta-bonds.md` are publishable as written — the
      requirement and the elision are both stated, rather than every example
      rewritten
- [ ] A conformance vector publishes a meta-molecule with its bond, and one
      without it is invalid for rule 4 — deferred to `todos/066`, this change
      set being spec-only and required to move no wire byte

## Work Log

### 2026-08-18 - Filed While Building the Grounding Demo

**By:** Claude

Found in phase 1 of the grounding demo, at the first `create_molecule` over
`"_A_ is true"`. Confirmed against the reference implementation by publishing
the assertion without the bond:

```
validation rule 4 (operation validity): operation 4 (create_molecule) bond:
entity d6dc10d7… is not reachable from this block, from an ancestor in the
author's chain, or from any block in the refs graph
```

All three of the demo's authors solve it differently — publish it, re-publish
it, or reference someone else's block — which is what suggested the strategies
are worth naming in the specification.

### 2026-08-18 - Ratified and Applied

**By:** Claude

**Decision (project lead):** Option 1. The standard meta-bonds are ordinary
on-chain entities: being standard means being recognized at L2→L3, not existing
before somebody publishes one. Rule 4 keeps its "no exempt position", L1
validation stays meta-bond-agnostic, and the cost — one `create_bond` per author
per meta-bond used, or a `refs` entry naming a block that has it — is the cost.
The examples are fixed by saying that their `create_bond` is elided, once, in
the first of them, rather than by rewriting all three.

**Changes:**

- `spec/06-meta-bonds.md`, "Meta-molecules are regular molecules": a paragraph
  stating the requirement, the three places a bond may be reachable from, the
  bolded "not implicitly present in any chain, and no digest is exempt from rule
  4", and the reason (whether a block validates cannot depend on which
  meta-bonds the validator knows — the same principle 059 settled for filler
  shapes).
- `spec/06-meta-bonds.md`: an informative paragraph naming the three publication
  strategies the demo's three authors use between them — publish it, re-publish
  it for an authorship record on an existing entity, or name a block that did in
  `refs` — and the consequence that the five best-known entities of the protocol
  have no canonical publication.
- `spec/06-meta-bonds.md`, "Declaring atom equivalence": one sentence saying the
  section's examples elide the `create_bond` and what a publishable block also
  carries.
- No implementation change: `go/block` already resolves a meta-bond digest
  through the ordinary resolver, which was the correct reading all along.
- The L3-filtering wrinkle (a view holding a meta-molecule whose bond entity it
  does not, because no subscribed author published the bond) is the general case
  of `todos/053`, which spec/05's "Filtering rules" informative paragraph
  already covers: the view renders such a molecule by reading the bond from L2.
  Nothing meta-bond-specific was added for it.
- The conformance-vector criterion is deferred to `todos/066`: this change set
  was required to leave `vectors/` byte-identical.

## Notes

Source: grounding demo, phase 1 (content and chains).
