---
status: complete
priority: p2
issue_id: "099"
tags: [transport, specification-gap, forks]
dependencies: [096]
---

# Which Position a Client Asks a New Source From

## Problem Statement

`spec/07-transport.md` never says which position a client asks a source's first
`range` from, and the two implementations chose differently. The choice is
observable — it decides whether the divergence between two sources arrives as
blocks in a range or as the backward walk of "Pursuing an advertised tip", and
therefore whether that section's obligation is ever reached at all.

Two readings, both consistent with the text as written:

- **From the genesis position.** The `after` parameter is per source, and a
  source never asked has handed over nothing, so the walk starts where every walk
  starts. This is what `go/transport`'s `Syncer` did: it remembers a resume
  position *per source*, because two sources may be on different branches and a
  position one holds may be one the other has never heard of.
- **From where the client is.** The client already holds a chain; asking for it
  again is bandwidth spent on blocks it has. So it asks each source after the tip
  its own store reaches. This is what `ts/`'s `syncChain` did.

## Findings

- **The section that needs the second reading is "Pursuing an advertised tip".**
  It opens with "the range after the position the client asked from was empty",
  and closes by calling an empty range and an unreachable tip "the *normal*
  answer a second source gives about a forked chain". A client that asks every
  new source from the genesis position never receives that answer: the source
  serves its own chain from the beginning, the divergent blocks arrive in the
  range, rule 9 fires on the store, and the walk is never taken. The obligation
  is not violated — it is never triggered.
- **Both find the fork, and the informative paragraph of that same section says
  so**: "Re-issuing the range from the genesis position costs one request and
  re-downloads the shared prefix; it delivers the divergent blocks too, so rule 9
  fires on it as well." So neither implementation is wrong. What is missing is a
  sentence saying that both are allowed and what each costs, and — if the
  pursuit's obligation is to mean anything operationally — a SHOULD.
- **The interop harness measured the difference.** Over the same two servers, the
  two ways produce identical stores, identical chains, identical forks and
  identical verdicts; the only difference in the summary document is `pursuits`,
  empty in one and carrying the walk in the other. Asked from the held position,
  the fork scenario reports `held` after one fetch and the genesis scenario
  reports `genesis` after two — which is the outcome todo 096 named, reached
  across implementations for the first time.
- The two are not equivalent in cost, and the difference grows with the chain:
  from the genesis position the second source re-sends every block of the shared
  prefix, and the pursuit's whole argument for the backward walk is that "its
  cost is the distance between the two branches rather than the length of the
  chain".
- There is one asymmetry in favour of the per-source position, and it deserves to
  be written down rather than lost: a client asking from *its own* tip is asking
  a question about a branch it chose, and a source that has never heard of that
  block answers an empty range whether it is on another branch or simply behind.
  Both cases then cost a pursuit, and the pursuit's own text already says a
  source that is behind and a source that is withholding are indistinguishable.

## Proposed Solutions

### Option 1: Say both are allowed, and SHOULD the held position

Add to "Pursuing an advertised tip", or to the multi-source rule: a client MAY
ask a source it has not used before from the genesis position, and SHOULD ask it
from the position its own chain reaches; state the two costs (a re-sent prefix
against a pursuit per source that is merely behind), and note that the second is
the case the pursuit is written for.

- **Pros**: makes the pursuit's obligation reachable in practice rather than
  vacuously satisfiable; names the cost of the alternative; one paragraph.
- **Cons**: a SHOULD that turns "this source is behind" into a backward walk at
  every sync — which is bounded and self-limiting, but is work.

### Option 2: Say both are allowed, and prefer neither

The same paragraph without the SHOULD.

- **Pros**: honest; the profile is optional and this is policy.
- **Cons**: leaves a conformance suite unable to test the pursuit against an
  arbitrary conforming client, which is where this todo came from.

### Option 3: Say nothing

- **Pros**: nothing is broken; both implementations find every fork.
- **Cons**: two implementations of one profile already differ observably here,
  which is the thing settling 090 to 097 was for.

## Recommended Action

Option 1. The pursuit is the completion of the multi-source rule and the profile
spends a page on it; a client that never reaches it satisfies the rule the way
rule 9 is satisfied vacuously by a node with one source, which is exactly the
failure mode this profile exists to prevent.

## Technical Details

- **Affected Files**: `spec/07-transport.md` ("Pursuing an advertised tip", "The
  multi-source rule")
- **Related Components**: `go/transport`'s `Syncer.AskFromHeldPosition` and its
  per-source `resume`; `ts/`'s `SyncOptions.from`; `interop/run.sh`'s second
  pass; todos 096 and 098
- **Database Changes**: No

## Acceptance Criteria

- [x] The profile says which position a client asks a source it has not used
      before from, or says that both are allowed and what each costs
- [x] The two implementations default to the same one

## Work Log

### 2026-08-19 - Filed While Crossing the Two Implementations

**By:** Claude

Found by the interop harness: the two clients produced the same store from the
same servers and disagreed about whether a pursuit had happened, because they
disagreed about where to ask a new source from. Both implementations now carry
the choice as an option, defaulting to the genesis position so that a run says
what it did; the default is what this todo has to settle.

### 2026-08-19 - Ratified and Applied

**By:** Claude

**Decision (project lead):** Option 1 with the SHOULD inverted. Both positions
are permitted and the preferred one is the **genesis position**, not the held
one. The reasoning against the filed recommendation: the SHOULD is a default
for a client that has to pick without knowing which case it is in, and there
the genesis position buys completeness at a cost that is known and bounded —
the shared prefix, once, per source — while the held position buys a saving
whose price is that one section's obligation carries the whole of fork
detection. A client that takes the MAY is not worse off, but it has to
implement the pursuit for real, and the text says so. The interop concern the
todo was filed on is answered without the SHOULD: what a conformance suite
needs is a client that says which position it asked from, which is the third
clause of the rule and what both implementations' `-from` already is.

**Changes:**

- `spec/07-transport.md`, "Client rules", new subsection **"First contact with
  a source"**, between "The multi-source rule" and "Pursuing an advertised
  tip": positions are per source and why; the rule (SHOULD genesis, MAY held,
  SHOULD record which); a paragraph on each position's cost, the genesis one
  noting that the divergence arrives as blocks and rule 9 fires on the store,
  the held one that the divergence is then found by the pursuit *and by nothing
  else*, so the pursuit is not optional in effect for such a client; the
  asymmetry the todo asked to be written down rather than lost — a source that
  has never heard of the client's block answers the empty range whether it is
  on another branch or merely behind; and that neither choice is a conformance
  question. One informative paragraph: both reference implementations expose
  the choice and default to the genesis position, and the harness runs both
  ways with the same store and the same verdicts either way.
- `spec/07-transport.md`, "The multi-source rule": the sentence naming where
  the comparison is written down now names both sections.
- `spec/07-transport.md`, "Open questions": a paragraph for the two gaps found
  by crossing the implementations rather than by either alone (096 and this
  one), which the earlier per-implementation accounting had no place for.
- Doc comments now cite the paragraph instead of this todo:
  `go/transport`'s `Syncer.AskFromHeldPosition` (and why the zero value is the
  one to have), `go/cmd/dialog-sync`'s `-from`, `go/transport`'s
  `TestAskingASourceFromTheHeldPosition`, `ts/src/transport.ts`'s
  `SyncOptions.from`, `ts/scripts/sync.ts`'s `-from`, `interop/run.sh` and
  `interop/README.md`.
- **No behaviour changed.** Both clients already default to the genesis
  position; the go battery, the ts battery and `interop/run.sh` (71 checks)
  pass unchanged.

One thing the application surfaced and did not fix: `ts/`'s `syncChain`
*library* default is the held position — `from` omitted means the store's own
tip — while `go/transport`'s `Syncer` zero value is the genesis position. The
two *clients* agree because `scripts/sync.ts` passes `from: null`, which is
what the interop harness and the acceptance criterion measure, but a caller of
the TypeScript library that omits the option silently takes the MAY. Filed as
`todos/100`.

## Notes

Source: `interop/`, and the second implementation's reading of "Pursuing an
advertised tip".
