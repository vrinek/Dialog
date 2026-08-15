---
status: pending
priority: p2
issue_id: "052"
tags: [specification-gap, meta-bonds, layer-3, equivalence]
dependencies: ["026"]
---

# What an Equivalence Does to the Assertions About Its Members

## Problem Statement

`spec/06-meta-bonds.md` gives equivalence one sentence of L3 semantics:

> **L3 semantics:** Implementations SHOULD treat equivalent entities as
> interchangeable when querying L3. The specific deduplication strategy (merge,
> prefer one, show both) is implementation-scoped.

"Interchangeable when querying" is clear enough for a *query about data*: ask
for molecules using bond X and you should also get the ones using the bond
declared equivalent to X. It says nothing about the four other meta-bonds,
which are also queries — of a kind — and whose subjects are exactly the sort of
entity an equivalence can name.

**1. Does a truth assertion cross the class?** Author C declares molecules M and
N equivalent. Author A asserts "M is true". Is N true? The equivalence example
in the same document says the two molecules "are treated as the same
assertion", which reads like yes; but "treated as the same assertion" is about
what the molecules *say*, and a truth meta-molecule is about a specific digest.
An implementation that answers no makes the equivalence almost useless for the
case the document's own example is about; one that answers yes lets any
subscribed author redirect an assertion made about something else onto a
molecule its author never mentioned.

**2. Does retraction cross it, and in which direction?** If it does, one
retraction can silence a whole class — including molecules published by authors
who never retracted anything. If it does not, a class can hold members in
opposite states while being "the same assertion".

**3. Does supersession or contradiction cross it?** "A supersedes B" where B is
equivalent to B′: is B′ superseded? "A contradicts B" where A is equivalent to
A′: does A′ contradict B? Neither is answered. Supersession is the sharper case,
because crossing the class changes what an application *shows*.

**4. What happens when the class is inconsistent?** Two members of one class,
each asserted by a different author, one true and one untrue. Under the crossing
reading that is a conflict; under the non-crossing reading it is two molecules
in two states which are also "the same assertion" — a state the specification
has no vocabulary for.

## Findings

- `spec/06-meta-bonds.md`, "Equivalence": the SHOULD, and
  "implementation-scoped" for the deduplication strategy only.
- `spec/06-meta-bonds.md`, "Declaring molecule equivalence": "In L3, both
  molecules are treated as the same assertion."
- `spec/06-meta-bonds.md`, "Truth assertion", "Truth retraction",
  "Contradiction", "Supersession": all four are written about a molecule, never
  about a class.
- `spec/06-meta-bonds.md`, "Security Considerations", "Equivalence attacks": a
  malicious equivalence is mitigated by subscription filtering — which bounds
  who is affected, not what the equivalence does to them.
- `go/accept`: the reading this implementation took. Truth crosses the class —
  the class, not the molecule, is what carries a `TruthState`, and
  `View.Assertions` reports every assertion bearing on any member. Supersession
  and contradiction do not cross: the edges are recorded between the molecules
  actually named, and `View.EquivalenceClass` is exposed so that an application
  wanting the wider reading can expand them itself.
- Related: todo 026 (equivalence bonds and molecules).

## Proposed Solutions

### Option 1: State it per meta-bond (Recommended)

- Say in "Equivalence" that a truth assertion or retraction about a member of an
  equivalence class is an assertion about the class, and that the class is the
  unit a truth state attaches to; that supersession and contradiction are
  recorded between the entities named and are not rewritten onto the class,
  since both change what is shown and neither is a query; and that an
  inconsistent class is a conflict to surface like any other.
- **Pros**: each of the four gets an answer where an implementer looks for it,
  and the answers differ where the meta-bonds differ.
- **Cons**: four rules where the document currently has one sentence; the
  asymmetry between truth and supersession needs its reason stated, or it reads
  as arbitrary.
- **Effort**: Medium (spec), none (Go — this is what it does)
- **Risk**: Low

### Option 2: Everything crosses the class

- One rule: every meta-molecule about a member is a meta-molecule about the
  class.
- **Pros**: one sentence, no asymmetry to justify.
- **Cons**: an equivalence published by any subscribed author then rewires
  supersession and contradiction edges across entities their authors never
  named; a single hostile equivalence can hide arbitrary data by chaining it to
  something superseded.
- **Risk**: High

### Option 3: Nothing crosses the class

- Equivalence affects data queries only, exactly as written.
- **Pros**: the narrowest reading, and the safest.
- **Cons**: the document's own worked example — two molecules stating one fact
  through different atoms and bonds — is then not usable for the thing it exists
  for: asserting the fact once.
- **Risk**: Medium

## Recommended Action

Option 1. The distinction that makes it non-arbitrary: truth is a statement
*about the assertion*, and equivalent molecules are by definition the same
assertion; supersession and contradiction are statements about *these
particular molecules*, and an author who wanted to replace the whole class could
have said so about each member.

## Technical Details

- **Affected Files**: `spec/06-meta-bonds.md` ("Equivalence", and a sentence in
  each of the other four)
- **Related Components**: L3 meta-molecule application, `go/accept`
- **Database Changes**: No

## Acceptance Criteria

- [ ] Whether a truth assertion or retraction applies across an equivalence
      class is stated
- [ ] Whether supersession and contradiction apply across an equivalence class
      is stated
- [ ] What an internally inconsistent equivalence class means is stated

## Work Log

### 2026-08-15 - Filed While Implementing go/accept

**By:** Claude

Found while deciding what `View.Truth` is keyed by. The choice could not be read
off the specification in either direction, and it is not a small one: keying
truth by class makes an equivalence a way of joining assertions, keying it by
molecule makes an equivalence a display hint. The implementation picked the
first for truth and the second for supersession and contradiction, and says so
in the doc comments of `applyTruth` and `applySupersession` — a reading, not a
rule.

## Notes

Source: Go reference implementation, phase 7 (L3 truth distillation).
