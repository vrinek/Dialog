---
status: complete
priority: p2
issue_id: "054"
tags: [specification-gap, meta-bonds, layer-3, subscriptions]
dependencies: ["053"]
---

# A Meta-Molecule Whose Subject Is Not in L3

## Problem Statement

Two of the five standard meta-bonds condition their L3 semantics on the presence
of their subjects, and three do not:

> **Contradiction.** If both molecules are present in L3 (asserted by subscribed
> authors), the implementation MUST surface the contradiction.
>
> **Supersession.** If both A and B are in L3, implementations SHOULD present A
> and hide or deprecate B.
>
> **Truth assertion.** A molecule asserted as true by a subscribed author SHOULD
> be treated as factual in L3.
>
> **Truth retraction.** A molecule asserted as untrue by a subscribed author
> SHOULD be excluded or flagged in L3.
>
> **Equivalence.** Implementations SHOULD treat equivalent entities as
> interchangeable when querying L3.

Because filtering is per entity (todo 053), a subscribed author can publish a
meta-molecule about a molecule that is not in the reader's L3 — the reader
subscribes to the author who *commented*, not to the author who *stated*. The
meta-molecule is in L3; its subject is not.

**1. What does an assertion about an absent molecule do?** "Treated as factual
in L3" has no referent: there is nothing in L3 to treat. Does the assertion
admit its subject to L3 — making a truth meta-molecule a kind of endorsement
that pulls data in — or does it evaporate? The first would let any subscribed
author import arbitrary third-party molecules into a reader's truth by asserting
them, which is the control inversion the subscription model exists to prevent;
the second means a reader can hold a mountain of assertions about nothing.

**2. What does the parenthesis in Contradiction mean?** "present in L3 (asserted
by subscribed authors)" can be read two ways: *present* meaning admitted by the
filtering rule, with the parenthesis restating how entities get there; or
*present* meaning additionally carrying a truth assertion. The two readings
differ in exactly the case the MUST is about — a contradiction between two
molecules nobody has asserted true is surfaced under the first and suppressed
under the second — and a MUST should not depend on which one a reader picks.

**3. Is the supersession condition symmetric with contradiction's?** Supersession
says "If both A and B are in L3" with no parenthesis, which reads as the first
sense. Two neighbouring rules using different words for the same condition
invites the conclusion that they mean different things.

## Findings

- `spec/06-meta-bonds.md`: the five L3 semantics paragraphs quoted above.
- `spec/05-processing-model.md`, "Filtering rules": the per-entity test that
  makes the mismatch possible.
- `spec/05-processing-model.md`, "Meta-molecule application": "Implementations
  MUST recognize the standard meta-bonds" and "MUST surface conflicts" — neither
  qualified by the subject's presence.
- `go/accept`: the readings this implementation took. An assertion about a
  molecule the view does not hold has no effect and does not admit its subject
  (`applyTruth`, rule 1); a supersession whose A or B is absent records no edge
  (`applySupersession`); a contradiction is surfaced when both molecules are in
  the view in the *filtering* sense, without requiring a truth assertion, on the
  grounds that surfacing is a MUST and the narrower reading suppresses
  conflicts. `TestAssertionsAboutOutOfViewMoleculesHaveNoEffect`,
  `TestSupersessionOfOutOfViewMoleculesIsIgnored` and
  `TestContradictionIsSurfaced` pin all three.
- Related: todo 053 (filtering is per entity), todo 051 (a capability is not an
  acceptance).

## Proposed Solutions

### Option 1: One presence rule for all five (Recommended)

- State once, in § "Standard meta-bond library" or in
  `spec/05-processing-model.md` § "Meta-molecule application", that a
  meta-molecule takes effect only on entities that are themselves in L3; that a
  meta-molecule never admits its subject to L3, since admission is the
  subscription rule's business alone; and that "present in L3" throughout
  `06-meta-bonds.md` means admitted by § "Filtering rules" and nothing more.
- Replace Contradiction's parenthesis with a cross-reference so the two readings
  cannot both survive.
- **Pros**: one rule, five consistent meta-bonds, and the subscription model's
  direction of control preserved.
- **Cons**: leaves a reader holding assertions with no subject, which is
  harmless but looks like a bug in an interface until it is explained.
- **Effort**: Small (spec), none (Go)
- **Risk**: Low

### Option 2: An assertion admits its subject

- A truth assertion by a subscribed author pulls the asserted molecule into L3.
- **Pros**: matches the intuition that vouching for something shares it, and
  makes "treated as factual in L3" always meaningful.
- **Cons**: any subscribed author can then place arbitrary molecules in a
  reader's truth without the reader subscribing to their authors — the same
  inversion todo 051 rejected — and a retraction would have to admit its subject
  too, in order to exclude it.
- **Risk**: High

### Option 3: Say nothing

- **Cons**: the MUST in Contradiction is ambiguous in the one case it governs,
  and implementations will disagree about whether a conflict is surfaced, which
  the user experiences as a warning that appears on one node and not another.
- **Risk**: Medium

## Recommended Action

Option 1.

## Technical Details

- **Affected Files**: `spec/06-meta-bonds.md` (all five L3 semantics
  paragraphs), `spec/05-processing-model.md` ("Meta-molecule application")
- **Related Components**: L3 meta-molecule application, filtering, `go/accept`
- **Database Changes**: No

## Acceptance Criteria

- [x] Whether a meta-molecule takes effect when its subject is not in L3 is
      stated
- [x] Whether a meta-molecule can admit its subject to L3 is stated
- [x] "Present in L3" is defined once and used consistently across the five
      meta-bonds

## Work Log

### 2026-08-15 - Filed While Implementing go/accept

**By:** Claude

Found three times over, once per meta-bond family, while writing the code that
looks up a meta-molecule's subject and finds nothing. The contradiction
parenthesis was the one that could not be settled by reading harder: a MUST that
means one thing under one parse and the opposite under the other. The
implementation took the wider parse, because surfacing too much is recoverable
and suppressing a required warning is not.

### 2026-08-18 - Ratified and Applied

**By:** Claude

**Decision (project lead):** Option 1. Meta-molecule semantics are computed over
the subjects a view holds. Contradiction and supersession already state that
condition; the reading generalizes to truth assertion, truth retraction and
equivalence, and "present in L3" means "admitted by the filtering rules" and
nothing more — so a contradiction between two molecules nobody has asserted true
is surfaced, the wider parse of the MUST. Two consequences are stated: a
subscribed author's meta-molecule about an absent subject has no L3 effect while
the subject is absent and takes effect on a later rebuild that finds it present
(nothing is lost, the meta-molecule itself being in L2 and in the view), and a
meta-molecule never admits its own subject, admission being the subscription's
business alone.

**Changes:**

- `spec/05-processing-model.md`, "Meta-molecule application": an informative
  paragraph stating the presence rule once, for all five meta-bonds, and stating
  that a meta-molecule cannot admit its subject.
- `go/accept`: no behaviour change — `applyTruth`, `applySupersession` and
  `applyContradictions` already took these readings, and a rebuild is what makes
  the "takes effect later" half true, a `View` being a snapshot recomputed rather
  than maintained. The doc comments now cite the specification instead of this
  todo. `TestAssertionsAboutOutOfViewMoleculesHaveNoEffect`,
  `TestSupersessionOfOutOfViewMoleculesIsIgnored` and
  `TestContradictionIsSurfaced` keep pinning all three, and the first two already
  subscribe to the missing author afterwards to show the assertion taking effect.

## Notes

Source: Go reference implementation, phase 7 (L3 truth distillation).
