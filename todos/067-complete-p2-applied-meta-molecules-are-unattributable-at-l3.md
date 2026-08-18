---
status: complete
priority: p2
issue_id: "067"
tags: [layer-3, meta-bonds, api, provenance, grounding-demo]
dependencies: []
---

# An Applied Equivalence or Supersession Cannot Be Attributed Through the L3 API

## Problem Statement

L3 applies the five standard meta-bonds and reports the *result* of applying
them. For the two truth meta-bonds it also reports the *record*: `View.Assertions`
hands back every assertion with its author, its block and the digest of the
meta-molecule that made it, which is exactly what an application needs to say
"atlas asserted this, in this block". For a surfaced conflict the same holds:
`Conflict.Meta` and `Conflict.Declarers` name the meta-molecules and the authors
behind the disagreement.

Equivalence and supersession have no such surface. `View.EquivalenceClass`,
`View.Equivalent`, `View.Supersedes`, `View.SupersededBy`, `View.IsSuperseded`
and `View.Current` return digests and booleans, and there is no query that
answers "which meta-molecule put these two entities in one class" or "who
declared that this figure replaces that one". The reading is applied and its
provenance is dropped.

This surfaced while building `demo/cmd/dialog-mcp`, whose whole purpose is
attribution: an assistant grounding an answer must be able to say who is
responsible for every step of it. "Holland and the Netherlands are the same
place" is a claim by an author — gazetteer — and an application that reports it
without saying so has laundered an opinion into a fact. The same goes for "the
current Poland figure is 36,621,000, because errata replaced the earlier one".

The workaround the demo uses (`metaDeclarations` in
`demo/cmd/dialog-mcp/server.go`) is to walk `View.DigestsOfKind(KindMolecule)`,
recognize the meta-bond by its digest, check that every filler names a member of
the class in hand, and skip anything in `View.WithdrawnMetaMolecules`. That is
the application re-implementing a piece of L3's own reading — including the
withdrawal rule, which it has to get right or it will attribute a class to a
declaration nobody stands behind any more. It is also quadratic in the view, and
it cannot distinguish a declaration that contributed to the closure from one
that merely names two members that some other declaration already unified.

## Findings

- `go/accept/accept.go`: `EquivalenceClass`, `Equivalent`, `Supersedes`,
  `SupersededBy`, `IsSuperseded`, `Current`, `Contradictions` — all return
  digests only.
- `go/accept/accept.go`: `Assertions` is the counter-example. It returns
  `Assertion` values carrying `Author`, `Block`, `Meta` and `Subject`, which is
  the shape the other readings lack.
- `go/accept/supersede.go`: `applySupersession` already builds `metas[edge]` and
  `by[edge]` — the declaring meta-molecules and the subscribed authors, per
  edge. They are consumed only to populate a `ConflictSupersessionCycle` and are
  then discarded. The information is computed and thrown away.
- `go/accept/accept.go`: `closeEquivalences` unions the pairs into a union-find
  and keeps no record of which claim produced which merge; `claims.equivalences`
  (`go/accept/meta.go`) holds `claim` values that already carry `meta` and
  `prov`.
- `go/accept/meta.go`: the `claim` type carries exactly what is wanted —
  `meta`, `a`, `b`, `typ`, `prov` — for every one of the five meta-bonds.
- `spec/06-meta-bonds.md`, "Equivalence" and "Supersession": both define the
  reading and neither says anything about reporting who declared it.
  `spec/05-processing-model.md`, "Accumulation rules", requires L2 to keep the
  authorship tag of every entity; nothing requires L3 to keep the link between
  a reading and the tagged entity that produced it.
- `demo/cmd/dialog-mcp/server.go`: `metaDeclarations`, the workaround, and
  `demo/cmd/dialog-mcp/equivalents.go`, its caller.

## Proposed Solutions

### Option 1: Report the declarations beside each reading (Recommended)

Add to `go/accept` a `Declaration` value — meta-molecule digest, the two
entities it names, and the subscribed authorship records behind it — and three
accessors returning it:

    func (v *View) Equivalences(d cid.Digest) []Declaration   // the class's declarations
    func (v *View) Supersessions(d cid.Digest) []Declaration  // the edges touching d
    func (v *View) ContradictionsOf(d cid.Digest) []Declaration

The data already exists at build time in `claims`; `applySupersession` already
groups it per edge, and `closeEquivalences` would keep the claims it unioned
instead of dropping them. Withdrawn meta-molecules are already excluded from
`claims` handling, so the accessors are correct by construction rather than by
the application repeating the rule.

- **Pros**: makes attribution a first-class L3 answer, which is what the
  grounding use case is for; removes the demo's re-implementation; the cost is
  retaining values the package already computes.
- **Cons**: three more methods and one more exported type on `accept`; the
  determinism obligation extends to their ordering (by meta digest, then
  author).
- **Effort**: Small
- **Risk**: Low — additive, changes no existing answer.

### Option 2: One general accessor over the meta-molecules that applied

    func (v *View) AppliedMetaMolecules(d cid.Digest) []cid.Digest

returning every standing meta-molecule of the view that names `d` or a member of
its class, leaving the application to look each one up and read it.

- **Pros**: one method, no new type.
- **Cons**: the application still has to decode the molecule and decide what
  each declaration means, which is most of the work; it cannot say which
  reading a given meta-molecule contributed to.
- **Risk**: Low

### Option 3: Say in the specification that L3 SHOULD expose it, and leave the
API to implementations

- **Pros**: keeps the reference implementation's surface small.
- **Cons**: the demo shows the query is not optional for the protocol's founding
  use case; leaving every implementation to invent it invites the quadratic scan
  this todo is about.
- **Risk**: Medium — divergent implementations, unattributable applications.

## Recommended Action

Option 1, with a sentence in `spec/06-meta-bonds.md` noting that an
implementation applying a meta-bond SHOULD be able to name the meta-molecule and
author it applied, since attribution is the point of the authorship tags. Then
delete `metaDeclarations` from the demo and call the new accessors.

## Technical Details

- **Affected Files**: `go/accept/accept.go`, `go/accept/meta.go`,
  `go/accept/supersede.go`, `go/accept/conflict.go`,
  `demo/cmd/dialog-mcp/server.go`, `demo/cmd/dialog-mcp/equivalents.go`,
  `demo/cmd/dialog-mcp/truth.go`, `spec/06-meta-bonds.md`
- **Related Components**: L3 meta-bond application, equivalence closure,
  supersession graph, the MCP grounding demo
- **Database Changes**: No

## Acceptance Criteria

- [x] An application can name the meta-molecule and the author behind any
      equivalence class it is shown
- [x] The same for a supersession edge
- [x] The accessors exclude withdrawn meta-molecules without the caller
      repeating the rule
- [x] The answers are deterministic, and the determinism test covers them
- [x] `demo/cmd/dialog-mcp` drops its own scan

## Work Log

### 2026-08-18 - Filed From the MCP Demo

**By:** Claude

Found while building phase 2 of the grounding demo. `dialog_equivalents` is
supposed to answer "who says these are the same thing", and the L3 API can only
answer "they are".

### 2026-08-18 - Settled: Option 1, With the Backing Per Author

**By:** Claude

Implemented as `accept.Declaration` and `accept.Backing`, read by
`View.EquivalenceDeclarations`, `View.SupersessionDeclarations` and
`View.ContradictionDeclarations` — the third for symmetry with `Conflict.Meta`
and `Conflict.Declarers`, which report the same fact per surfaced conflict
rather than per entity. A `Declaration` carries the meta-molecule's digest, the
standard bond's template, the two entities its fillers name, and the `Backing`
records: author, block, and that block's position in the author's chain
(`todos/068`).

Two decisions beyond the proposal:

- **The backing is per author, not per molecule.** `applyStanding` already
  decided which of a meta-molecule's publishing authors still back it and threw
  away everything but the yes or no; it now returns the set. An author who
  retracted their own declaration therefore drops out of the record while the
  declaration stands on whoever is left, and no caller repeats the rule of
  `spec/06-meta-bonds.md`, "Withdrawing meta-molecules". A meta-molecule nobody
  backs any more appears in no `Declaration` at all — it is in
  `WithdrawnMetaMolecules`, where it always was.

- **No declaration claims to have caused a merge.** Every standing equivalence
  naming a member of a class is reported, including one naming two members
  another declaration had already unified. Which pair the union-find merged
  first is an artifact of iteration order and not a fact about the data, and
  reporting it would be inventing provenance. The todo listed this as something
  the demo's scan could not do; it turns out not to be worth doing.

The specification was deliberately left alone. The proposal suggested a sentence
saying an implementation SHOULD be able to name what it applied; this is an API
answer that moves no wire byte and binds no other implementation's surface, and
the reference implementation demonstrating it is enough. Revisit it if a second
implementation grows an L3.

`demo/cmd/dialog-mcp`'s `metaDeclarations` scan is deleted; `dialog_equivalents`
and `dialog_truth` read the new accessors, and both now attribute the
supersession and the contradiction as well as the equivalence class. The
conformance vectors are unchanged, as an additive API must leave them.

## Notes

Source: `docs/plans/2026-08-18-grounding-demo.md`, phase 2.
