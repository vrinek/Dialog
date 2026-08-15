---
status: pending
priority: p2
issue_id: "053"
tags: [specification-gap, processing-model, layer-3, subscriptions]
dependencies: ["009"]
---

# L3 Can Hold a Molecule Whose Bond It Does Not Hold

## Problem Statement

`spec/05-processing-model.md` § "Filtering rules" is written per entity and says
so plainly:

> 1. For each entity in L2, check if any of its authors (from the authorship
>    tags) is in the user's subscription list
> 2. If yes, the entity is included in L3
> 3. If no, the entity is excluded from L3

A molecule is an entity, and so are the bond and the atoms it names. Nothing
requires them to be authored by the same person, and the specification's own
foreign-loading example is a case where they are not: Bob's block creates a
molecule using a bond that only Alice ever published
(§ "Foreign chain loading (demand-driven resolution)"). A user who subscribes to
Bob and not to Alice therefore has, in L3:

- Bob's molecule, whose `bond` field is a digest of something not in L3
- neither the bond nor, if Alice created them, the atoms its fillers name

The molecule is in the user's truth and cannot be rendered, because rendering it
means substituting the fillers into the bond's template
(`spec/01-data-model.md`, "Molecules"). L1 guarantees the digests *resolve* —
rule 4 makes an unresolvable reference a validation error — so the data is in
L2; it is L3's filter that takes it away, and only for this user.

Three questions:

**1. Is that the intended outcome?** It follows from the rules as written. It is
also the kind of thing a specification usually says out loud, because an
application author will otherwise discover it as a crash.

**2. Should L3 close over dependencies?** Including a molecule's bond and fillers
when the molecule is included would keep every molecule renderable, at the cost
of admitting entities from unsubscribed authors — which is exactly what
§ "Filtering rules" says does not happen: "Foreign chain data that was loaded
into L2 for validation context is excluded from L3 unless the user
independently subscribes to the foreign author."

**3. Or should L3 exclude the molecule?** Dropping molecules whose dependencies
are missing keeps the view self-contained, and silently loses data the user
*did* subscribe to. It also cascades: a molecule filler pointing at a dropped
molecule drops its referrer too.

## Findings

- `spec/05-processing-model.md`, "Filtering rules": the three steps, and the
  foreign-chain sentence.
- `spec/05-processing-model.md`, "Examples", "Foreign chain loading": Bob's
  block_B uses a bond created in Alice's block_2, which is the shape of the
  problem.
- `spec/05-processing-model.md`, "Examples", "Full data flow": "Carol's L3 does
  NOT include Alice's data — unless Carol subscribes to an author who
  references Alice's blocks (in which case Alice's data is in L2 but not L3)".
  This is the closest the document comes to acknowledging the case, and it
  states the L2/L3 split rather than what the referring author's own entities
  then look like.
- `spec/01-data-model.md`, "Molecules": a molecule is a bond digest plus
  fillers, so it carries no copy of the template.
- `spec/02-block-format.md`, "Validation" rule 4: every digest an operation
  names must resolve, so L2 always holds them.
- `go/accept`: filters per entity, exactly as written, and documents the
  consequence — `View.Has` can be true for a molecule and false for its bond.
  `TestFilteringIsPerEntity` pins it.

## Proposed Solutions

### Option 1: Say that filtering is per entity, and what follows (Recommended)

- One paragraph in § "Filtering rules": the test is per entity and is not
  transitive; an entity may therefore be in L3 while the entities it references
  are not; applications MUST handle a reference into L2 that L3 does not hold,
  and MAY resolve it through L2 for display without treating the referenced
  entity as accepted.
- **Pros**: keeps the filter simple and honest, and names the case an
  application has to handle instead of leaving it to be discovered.
- **Cons**: the application-side rule ("read L2 for display only") sits awkwardly
  beside § "Application interface", which says applications MUST NOT read from
  L2.
- **Effort**: Small (spec), none (Go)
- **Risk**: Low

### Option 2: Close L3 over dependencies

- An entity admitted to L3 brings the entities its own fields name with it,
  transitively.
- **Pros**: every molecule in L3 is renderable; no application-side rule needed.
- **Cons**: contradicts the foreign-chain sentence directly, and lets a
  subscribed author pull an unsubscribed author's entities into the user's truth
  by referencing them — which is the control inversion todo 051 rejected for
  content keys.
- **Risk**: High

### Option 3: Exclude entities whose dependencies are missing

- **Pros**: L3 is self-contained.
- **Cons**: silently drops data the user subscribed to, cascades unpredictably,
  and makes a view's contents depend on which *other* authors the user has, in a
  way no user could predict.
- **Risk**: Medium

## Recommended Action

Option 1, together with a decision on the § "Application interface" tension: the
cleanest resolution is that L3 exposes the referenced entity's *bytes* for
rendering while keeping it out of the accepted set, so the application still
never reads L2 itself.

## Technical Details

- **Affected Files**: `spec/05-processing-model.md` ("Filtering rules",
  "Application interface")
- **Related Components**: L3 filtering, foreign chain loading, applications
- **Database Changes**: No

## Acceptance Criteria

- [ ] Whether L3 filtering is transitive over an entity's references is stated
- [ ] What an application does with a reference L3 does not hold is stated
- [ ] The interaction with "Applications MUST NOT read directly from L1 or L2"
      is resolved

## Work Log

### 2026-08-15 - Filed While Implementing go/accept

**By:** Claude

Found writing the filtering loop, which is four lines because the rule is four
lines. The question surfaced when a test subscribed to Bob alone and got back a
molecule whose bond was not in the view — valid under every sentence in the
specification, and useless to an application. The implementation keeps the rule
as written and documents the consequence.

## Notes

Source: Go reference implementation, phase 7 (L3 truth distillation).
