---
status: complete
priority: p2
issue_id: "055"
tags: [specification-gap, meta-bonds, layer-3, key-rotation, subscriptions]
dependencies: ["050", "049"]
---

# Which Block Dates an Author's Assertion, and Who Is "the Same Author"

## Problem Statement

Todo 050 established what "later" means for one author's assertions: block
order, the `prev` sequence of their chain, continuing across a key rotation
(`spec/05-processing-model.md`, "Assertion order"). Applying that rule needs two
things the specification does not supply.

**1. Which block dates an assertion that was published more than once?** L2
accumulates authorship rather than entities: "If an entity with the same CID
already exists in L2 [...] the new authorship record is added alongside the
existing one" (§ "Accumulation rules"). So one author can carry several
authorship records for one meta-molecule, at several positions of their chain.
Suppose an author publishes "M is true" in block 3, "M is untrue" in block 9,
and "M is true" again in block 12 — the third block re-creating an entity that
already exists, which is legal and idempotent. Is their position "true", because
their latest carrying block is 12? Or "untrue", because block 12 added nothing
new and the assertion it re-published was first made in block 3? Both readings
are consistent with "the later assertion (by block order) takes precedence"; the
first treats a re-publication as a re-statement, the second treats the entity's
first appearance as its date. The specification has no notion of an operation
being a no-op, so it cannot distinguish them.

**2. Is a successor key the same author?** Block order continues across a
rotation, which only matters if the two chains' assertions are weighed against
each other — and that is an identity claim: "Author identity (mapping multiple
keys to a single author) is implementation-scoped"
(§ "Chain succession (key rotation)"). An implementation that keeps the keys
apart at L3 will treat a pre-rotation assertion and a post-rotation retraction
by the same person as *two authors disagreeing*, and surface a conflict where
there is none. One that joins them silently overrides one key's word with
another's.

**3. Does a subscription follow the succession?** § "Chain succession" step 3 is
an L1 SHOULD: "If the user subscribes to the old key's author, the
implementation SHOULD auto-subscribe to the new key's chain, treating it as the
same logical author." § "Filtering rules" is an L3 MUST written entirely in
terms of keys. So a user who subscribed to Alice before her rotation either
keeps seeing her data because their node acted on a SHOULD, or stops seeing it
because L3 asks about a key that no longer signs anything — and the
specification permits both. This is the question todo 049 explicitly deferred.

## Findings

- `spec/05-processing-model.md`, "Assertion order" (added for todo 050): block
  order, and its continuation across a rotation.
- `spec/05-processing-model.md`, "Accumulation rules": re-publication adds an
  authorship record, and the graph has no concept of a redundant operation.
- `spec/05-processing-model.md`, "Chain succession (key rotation)", steps 3 and
  4: the auto-subscribe SHOULD, and author identity as implementation-scoped.
- `spec/05-processing-model.md`, "Filtering rules": the per-key MUST.
- `spec/02-block-format.md`, "Verifiable succession": the evidence a succession
  rests on — a public genesis block whose `refs` name the rotation block that
  appoints its key.
- `go/accept`: the readings this implementation took, all three documented in
  `order.go` and `truth.go`.
  - A re-publication re-states: an author's position is the one their *latest*
    carrying block holds (`markLatest`).
  - A verified succession is one logical author *for ordering only*
    (`blockOrder`): the two chains form one lineage, so the later chain's word
    wins rather than conflicting with the earlier one.
  - Filtering stays strictly per key: a successor chain's entities reach a view
    when the successor key is subscribed, and not because its predecessor was.
    An application that wants the L1 SHOULD's behaviour subscribes to the new
    key.
  - An ambiguous succession joins nothing and is surfaced as a conflict, since
    an ambiguous succession is an ambiguous order.
- Related: todo 050 (where block order came from), todo 049 (which deferred
  question 3), todo 042 (key succession linkage).

## Proposed Solutions

### Option 1: Answer all three narrowly (Recommended)

- **Re-publication.** Say that an author's position on a molecule is the one
  their latest block naming it holds, and that re-publishing a meta-molecule
  re-states it. It is the only reading an implementation can apply without a
  notion of "this operation changed nothing", which L2 does not have and should
  not grow.
- **Identity for ordering.** Say that a verified succession makes the two chains
  one sequence for the purposes of assertion order, and that this is an ordering
  rule and not an identity rule: it does not merge the keys, does not change
  authorship tags, and does not change filtering.
- **Subscription.** Say that L3 filtering remains per key, and that the L1
  auto-subscribe SHOULD is what carries a user across a rotation — so an
  implementation that follows the SHOULD keeps the user's view intact, and one
  that does not leaves the user to subscribe to the new key.
- **Pros**: each answer is the smallest one that makes the rule applicable; none
  of them promotes author identity from implementation-scoped to normative.
- **Cons**: a user on a node that ignores the SHOULD silently loses an author's
  data at a rotation, which the specification would then be documenting rather
  than fixing.
- **Effort**: Medium (spec), none (Go)
- **Risk**: Low

### Option 2: Make the L3 subscription follow the succession

- Turn step 3 into a rule about L3 filtering: a subscription to a key is a
  subscription to its verified successors.
- **Pros**: removes the failure mode where an author rotates and vanishes.
- **Cons**: it is author identity by another name, which § "Chain succession"
  step 4 deliberately leaves open; and a compromised key that publishes a
  fraudulent rotation (see `spec/06-meta-bonds.md`, "Key rotation abuse") would
  then inherit every one of that author's subscribers automatically.
- **Risk**: High

### Option 3: Say nothing

- **Cons**: question 1 makes the same chain resolve differently on two
  implementations; question 2 makes a routine key rotation look like a
  disagreement; question 3 makes an author's data disappear on some nodes and
  not others.
- **Risk**: High

## Recommended Action

Option 1, with Option 2 revisited only if pre-rotation or social recovery ever
enters scope — the security argument against it is the same one that makes
rotation abuse a listed threat today.

## Technical Details

- **Affected Files**: `spec/05-processing-model.md` ("Assertion order",
  "Filtering rules", "Chain succession (key rotation)")
- **Related Components**: L3 truth distillation, key rotation, subscriptions
- **Database Changes**: No

## Acceptance Criteria

- [x] Which of an author's blocks dates an assertion they published more than
      once is stated
- [x] Whether a successor chain's assertions are ordered against its
      predecessor's, or conflict with them, is stated
- [x] Whether an author subscription follows a key succession into L3 filtering
      is stated

## Work Log

### 2026-08-15 - Filed While Implementing go/accept

**By:** Claude

Found while building the per-author block-order index. Question 2 was
unavoidable: without an answer, `TestRotationContinuesAssertionOrder` — an
author who asserts, rotates, and retracts — surfaces a conflict between a person
and themselves. The implementation joins the chains for ordering and refuses to
join them for anything else, which is the narrowest choice that makes a rotation
survivable, and it is a choice.

### 2026-08-18 - Ratified and Applied

**By:** Claude

**Decision (project lead):** Option 1, all three answers narrow.

1. **Re-publication re-states.** An author's assertion is dated by the block
   carrying its latest publication by that author, so an author who asserts,
   retracts and publishes the assertion again holds it. It is the only reading
   an implementation can apply without a notion of "this operation changed
   nothing", which L2 does not have and should not grow. Dating by first
   appearance stays conformant; an implementation must choose one, because the
   two orders resolve the same chain differently.
2. **Identity for ordering only.** Continuing block order across a rotation is
   an ordering rule and not an identity rule: it does not merge the keys, does
   not change authorship tags, and does not change filtering. Author identity
   stays implementation-scoped, as "Chain succession" step 4 has it.
3. **Subscription.** L3 filtering stays per key, and the question is already
   answered at L1: the auto-subscribe SHOULD of "Chain succession" step 3 is
   what carries a user across a rotation. Cross-referenced rather than restated,
   and nothing new added.

**Changes:**

- `spec/05-processing-model.md`, "Assertion order": two informative paragraphs —
  re-publication as re-statement, with the alternative left conformant; and the
  ordering-not-identity point, ending in the cross-reference to step 3.
- `go/accept`: no behaviour change. `markLatest` already dates an author's
  position by their latest carrying block and `blockOrder` already joins a
  verified succession for ordering alone; both doc comments now quote the
  specification instead of citing this todo, and `order.go` names
  `Subscriptions.Subscribe` as what a node following the L1 SHOULD calls.
  `TestRotationContinuesAssertionOrder` keeps pinning the rotation case.

## Notes

Source: Go reference implementation, phase 7 (L3 truth distillation).
