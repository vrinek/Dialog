---
status: pending
priority: p2
issue_id: "064"
tags: [meta-bonds, l3, specification-gap, truth]
dependencies: []
---

# A Retracted or Superseded Meta-Molecule Keeps Its Effect

## Problem Statement

There is no way to withdraw a meta-molecule. Once a subscribed author has
published `"A is the same as B"`, `"M contradicts N"` or `"M supersedes N"`,
that declaration applies to every L3 view built for anyone subscribed to them,
for as long as they are subscribed. The author cannot take it back.

The obvious move is to retract it. A meta-molecule *is* a molecule
(`spec/06-meta-bonds.md`, "Meta-molecules are regular molecules"), so
`"«A is the same as B» is untrue"` is a well-formed truth retraction over a
type 2 filler and a valid block. But the specification never says what a
retraction of a meta-molecule means, and the reference implementation reads
every meta-molecule in the view without consulting its truth state:

```
author publishes:  "Holland" is the same as "Netherlands"
author publishes:  «that equivalence» is untrue

L3:  Truth(equivalence)          = Retracted
     Equivalent(Holland, NL)     = true      ← still applied
     EquivalenceClass(Holland)   = both      ← still applied
     Conflicts()                 = none
```

So the retraction is recorded, is queryable, and does nothing. The same holds
for supersession: `"M2 supersedes M1"` where M1 is itself a supersession
meta-molecule changes nothing about what M1 marks.

The only ways to undo a published equivalence today are to unsubscribe from its
author — which discards everything else they said — or for every reader to
implement an unspecified rule of their own. Neither is a protocol answer, and
the second means two conformant nodes disagree about what is in an equivalence
class, which decides truth states under the informative reading of
`spec/06-meta-bonds.md` §1.

An author with no undo is a real problem for the use case the demo is built for.
Equivalence is the one meta-bond published about entities the author does not
own, and it is the one `spec/06-meta-bonds.md`, "Security Considerations", names
as an attack ("Equivalence attacks"). An author who publishes a wrong
equivalence — a typo in a digest, an atom that turned out to name something else
— currently has no way to say so.

## Findings

- `spec/06-meta-bonds.md` §2/§3 define truth assertion and retraction over "a
  molecule", with no exception for a molecule that is itself a meta-molecule,
  and no statement that the assertion has any effect on that molecule's own
  semantics.
- `spec/06-meta-bonds.md`, "Meta-molecules are regular molecules": establishes
  that a meta-molecule is an ordinary molecule everywhere except in L3's
  reading. It does not say whether L3's reading of one is conditional on
  anything.
- `spec/05-processing-model.md`, "Meta-molecule application": lists what L3
  applies; nothing about the order in which the five meta-bonds are applied to
  each other, or about applying them to each other at all.
- `go/accept/meta.go`, `read`: classifies every in-view molecule by its bond
  digest into equivalences, truths, contradictions and supersessions. The
  equivalence closure is built first, and the truth states are computed from the
  truth claims afterwards; no step filters a claim by the truth state of the
  meta-molecule that carries it. Reproduced against a two-author world in the
  demo module during phase 1.
- The reading is not obviously wrong. Applying truth to meta-molecules opens
  questions the specification has not answered: is a retraction of a retraction
  a re-assertion? Does an *unasserted* meta-molecule apply — which it must, or
  nothing would apply, since almost no meta-molecule carries a truth assertion?
  Does one author's retraction of another author's equivalence disable it, and
  if so how is that different from a truth disagreement, which is surfaced and
  never resolved?

## Proposed Solutions

### Option 1: A retraction by the meta-molecule's own author disables it (Recommended)

State in `spec/06-meta-bonds.md`, "Meta-molecules are regular molecules":

> A meta-molecule that a subscribed author has retracted, where that author is
> also an author of the meta-molecule itself, MUST NOT be applied: the
> declaration is withdrawn by the party who made it. A retraction published by
> any other subscribed author does not withdraw it; it is a disagreement about
> the meta-molecule and is surfaced as one. An unasserted meta-molecule applies
> normally — publishing a meta-molecule is the assertion.

- **Pros**: gives an author an undo without giving anyone else a veto; keeps the
  conflict machinery in charge of disagreements between authors; the rule is
  local and cheap (a meta-molecule's authorship records are already in L2); it
  falls out of the same "block order settles one author's own assertions" rule
  that `spec/05-processing-model.md`, "Assertion order", already states.
- **Cons**: L3 acquires an application order — truth over meta-molecules first,
  then the rest — which has to be specified; a meta-molecule re-published by a
  second author is only withdrawn for the pair (author, molecule) that retracted
  it, which needs saying.
- **Effort**: Small (spec), Medium (implementations)
- **Risk**: Low

### Option 2: Supersession is the undo, not retraction

Say that a meta-molecule superseded by another meta-molecule of the same author
is not applied, and leave retraction with no effect on meta-molecules.

- **Pros**: supersession is already the correction mechanism; a corrected
  equivalence has a replacement to point at.
- **Cons**: does not cover plain withdrawal — an author who wants to say "that
  equivalence was simply wrong" has nothing to supersede it *with*; and it
  leaves "«X is the same as Y» is untrue" a publishable statement that means
  nothing, which is the state this issue is about.
- **Risk**: Medium

### Option 3: State that meta-molecules are irrevocable

Say plainly that L3 applies every in-view meta-molecule regardless of any truth
assertion about it, and that a published declaration cannot be withdrawn —
unsubscribing is the only remedy.

- **Pros**: matches every implementation today; no new machinery; keeps L3 a
  single pass.
- **Cons**: leaves an author no way to correct a mistake in a statement about
  someone else's entities, and leaves a valid, well-formed molecule with a
  documented meaning ("this is untrue") that is defined to do nothing.
- **Risk**: Low, but it is a decision to live with permanently.

## Recommended Action

Option 1. Whatever is chosen, `spec/06-meta-bonds.md` must say explicitly
whether the truth meta-bonds apply to meta-molecules, because a valid block can
carry the statement either way and two implementations currently answer
differently with equal justification.

## Technical Details

- **Affected Files**: `spec/06-meta-bonds.md` ("Meta-molecules are regular
  molecules", §2, §3), `spec/05-processing-model.md` ("Meta-molecule
  application"), `go/accept`
- **Related Components**: L3 meta-molecule application, equivalence closure,
  conflict detection
- **Database Changes**: No

## Acceptance Criteria

- [ ] `spec/06-meta-bonds.md` says whether a truth retraction of a meta-molecule
      withdraws it, and whose retraction counts
- [ ] If it does, the order in which L3 applies the meta-bonds to each other is
      specified
- [ ] The reference implementation matches, with a test over a retracted
      equivalence, contradiction and supersession

## Work Log

### 2026-08-18 - Filed While Building the Grounding Demo

**By:** Claude

Found in phase 1 of the grounding demo, looking for a way to let an author
correct a naming equivalence. Reproduced against the reference implementation
with a single-author world: the equivalence's truth state is `Retracted` and
`Equivalent` still answers true.

## Notes

Source: grounding demo, phase 1 (content and chains).
