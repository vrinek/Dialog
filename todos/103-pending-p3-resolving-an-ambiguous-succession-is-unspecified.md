---
status: pending
priority: p3
issue_id: "103"
tags: [processing-model, key-rotation, specification-gap, subscriptions]
dependencies: []
---

# Resolving an Ambiguous Succession Is Unspecified

## Problem Statement

Todo 102 settled what a node must NOT do when more than one valid genesis block
references the same rotation block: it must surface every claimant and must not
pick a successor on its own. `spec/05-processing-model.md` now also says what
happens while the ambiguity stands — chain succession's later steps (successor
auto-subscription, block ordering across the junction) wait.

What nothing specifies is how the wait *ends*. "The application resolves the
ambiguity" is the implied answer, but the spec gives an application no
vocabulary for doing so: no defined act of choosing a claimant, no way to
express that choice durably, and no statement about what a resolved choice means
for L3 (is it a subscription decision? a local annotation? something
publishable?).

This is a deliberately-open design question, in the same class as the transport
questions (069–075): it should be settled from real-world experience with key
rotation, not in the abstract.

## Findings

- `spec/02-block-format.md` "Verifiable succession": MUST NOT pick, with
  cross-reference to 05 (added closing todo 102).
- `spec/05-processing-model.md` "Chain succession": steps 2 and 3 wait while an
  ambiguous succession stands (added closing todo 102). Step 4 already leaves
  author identity mapping implementation-scoped.
- `go/block/chain.go`: `AmbiguousSuccessionError` names every claimant;
  `ValidateHistory` refuses the junction for any claimant. Each claimant chain
  still validates alone.
- `ts/`: `BlockStore.ambiguousSuccessions` names every claimant; no "the
  successor" accessor exists.
- `vectors/blocks.json` `ambiguous_succession` pins the surfaced-not-resolved
  behavior in both implementations.
- A subscriber's cheap local resolution today: subscribe to the claimant you
  believe (subscriptions are local and private), which is a per-reader choice —
  consistent with L3's "true is local" stance. The spec does not say this.

## Proposed Solutions

### Option 1: Say resolution is a subscription decision

State in 05 that choosing among claimants is exactly a subscription decision:
a reader who subscribes to one claimant chain has resolved the ambiguity for
their own L3 view, locally and revocably; the junction stays ambiguous as a
fact about the blocks.

- **Pros**: no new mechanism; matches the protocol's per-reader trust model.
- **Cons**: gives large deployments no shared vocabulary for the choice.

### Option 2: A meta-bond idiom

Recommend an is-true assertion on (a molecule describing) the chosen
succession, making the choice publishable and attributable like any other
claim.

- **Pros**: reuses existing machinery; choices become visible and disputable.
- **Cons**: needs a standard bond shape to be interoperable; more design work.

### Option 3: Leave unspecified until rotation sees real use

Record the gap; revisit alongside the key-compromise work deferred to v2
(pre-rotation would shrink the ambiguity surface anyway).

## Recommended Action

None yet — deliberately. Experience-gated, like 069–075.

## Technical Details

- **Affected Files**: `spec/05-processing-model.md`, possibly
  `spec/06-meta-bonds.md`
- **Related Components**: chain succession, subscriptions, L3
- **Database Changes**: No

## Acceptance Criteria

- [ ] The spec says how (or explicitly whether) an application ends the wait on
      an ambiguous succession
- [ ] Whatever is chosen is consistent with the per-reader trust model
- [ ] If a publishable idiom is chosen, it has a pinned bond shape and a vector

## Work Log

### 2026-08-20 - Filed While Closing Todo 102

**By:** Claude

The 102 fix made nodes wait on ambiguous successions but left the end of the
wait unspecified. Filed as experience-gated rather than settled in the
abstract.

## Notes

Source: todo 102's work log ("worth further attention" note from the closing
audit).
