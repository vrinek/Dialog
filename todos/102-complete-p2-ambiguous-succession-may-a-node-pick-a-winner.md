---
status: complete
priority: p2
issue_id: "102"
tags: [specification-gap, key-rotation, block-validation, normative-consistency]
dependencies: [042, 101]
---

# Ambiguous Succession: May a Node Pick a Winner on Its Own?

## Problem Statement

When more than one genesis block references the same rotation block, the
succession is ambiguous — two or more chains claim to be the one true
successor of a rotated key. Every document agrees a node MUST detect this
and MUST surface it. They disagree about what a node MAY additionally do
about it: treat one candidate as *the* successor for its own bookkeeping
purposes (auto-subscribe, "the set of known chains", block order across the
junction) while still surfacing the ambiguity, or refuse to prefer any
candidate at all.

- `spec/02-block-format.md`, "rotate_key" → "Verifiable succession" (the
  second of "Two further constraints follow"):

  > If a node holds more than one genesis block referencing the same
  > rotation block, the succession is ambiguous and the node MUST surface
  > the conflict. This is a fork condition and is treated like any other:
  > detection is required, and the handling strategy (reject, flag,
  > **accept-first-seen**) is **implementation-scoped**, exactly as in
  > validation rule 9.

  Validation rule 9 (fork detection, `02-block-format.md`, "Validation")
  explicitly lists "accept-first-seen" as one of the implementation-scoped
  strategies for an ordinary fork. This paragraph says an ambiguous
  succession gets exactly the same treatment — so a node MAY pick one
  candidate, silently, as long as it also surfaces the ambiguity.

- `spec/05-processing-model.md`, "Chain succession (key rotation)":

  > If more than one genesis block references the same rotation block, the
  > succession is ambiguous: the node MUST surface the conflict as it
  > surfaces a fork, and **MUST NOT pick a successor on its own**.

  This forbids exactly the "accept-first-seen" strategy the other document
  permits.

- `spec/05-processing-model.md`, "Assertion order" restates the stricter
  reading: "the node MUST surface the conflict rather than pick a
  successor."
- `spec/07-transport.md`, "Chain succession" (client rules) also states the
  stricter reading: "more than one genesis block claiming the same rotation
  is the ambiguous-succession condition and the node MUST surface it rather
  than pick."

Two implementations reading different documents would conform to different,
incompatible behaviors: one that silently continues auto-subscribing to
"the" successor chain by first-seen order, and one that refuses to treat any
candidate as the successor until the ambiguity is resolved by the
application. That is an interoperability-relevant split in a
security-relevant path (key rotation, the mechanism by which control of an
identity moves to a new key).

## Findings

- `git log -p -S "MUST NOT pick a successor" -- spec/05-processing-model.md`
  shows both sides of the split were written in **one commit**,
  `0a8266c99e3e13eb6842b4063552292739cf45cd` ("spec: make key succession
  verifiable (todo 042)"). The `02-block-format.md` hunk of that commit
  introduced "implementation-scoped... accept-first-seen"; the
  `05-processing-model.md` hunk of the *same* commit introduced "MUST NOT
  pick a successor on its own." This is an authoring inconsistency within
  one change, not two documents drifting apart over time.
- Todo 042's own "Ratified and Applied" work log entry (item 3) describes the
  decision as: "Several genesis blocks referencing one rotation block is an
  **ambiguous succession**, and it is surfaced the way forks are: detection
  required, **resolution implementation-scoped**." That matches
  `02-block-format.md`'s current text exactly, and does not match
  `05-processing-model.md`'s "MUST NOT pick a successor" — suggesting the
  `05-processing-model.md` wording is the one that overshot what was
  actually decided, whether by imprecise phrasing or by later editing this
  todo's log does not otherwise show.
- `go/block/chain.go`'s `Successors` function (the reference implementation)
  follows the `02-block-format.md` reading: its doc comment states the
  ambiguous case is "reported, not resolved," and the function itself
  returns every candidate plus a `*Fork` without forbidding a caller from
  treating one as the successor. It does not itself pick one, but it also
  does not encode a prohibition on a caller doing so — consistent with
  "implementation-scoped," not with a MUST NOT.
- Three documents (`05-processing-model.md` twice, `07-transport.md` once)
  state the stricter "MUST NOT pick" reading against one
  (`02-block-format.md`) stating the permissive "implementation-scoped"
  reading — but `02-block-format.md`'s "rotate_key" section is where the
  concept (verifiable succession, ambiguous succession) is originally
  defined, and is the document `05-processing-model.md` itself cites as the
  source of the rule ("see `02-block-format.md`, 'rotate_key'"). A
  head-count across documents does not settle which reading is intended;
  only a decision does.

## Proposed Solutions

### Option 1: Adopt the stricter reading — a node MUST NOT pick a successor on its own

Change `02-block-format.md`'s "Verifiable succession" paragraph to match
`05-processing-model.md` and `07-transport.md`: detection is required,
surfacing is required, and a node MUST NOT treat any one candidate as *the*
successor (for auto-subscribe, block-order continuation, or any other
purpose) while the ambiguity stands. Drop the "exactly as in validation rule
9" cross-reference, since rule 9's accept-first-seen option would no longer
apply to this case — ambiguous succession would be a *stricter* case than an
ordinary fork, not an identical one.

- **Pros**: matches the majority of the current text and the most
  security-sensitive reading — a node that silently follows a chain it
  cannot yet show is the real successor is exactly the failure mode
  verifiable succession (todo 042) was written to close.
- **Cons**: a node with an ambiguous succession now has no chain to
  auto-subscribe to and no block order across the junction until the
  application resolves it, which needs to be stated (what happens to steps
  2/3 of "Chain succession" in the interim); more restrictive than what
  `todos/042`'s own ratification recorded as the decision, so re-litigates a
  closed todo.

### Option 2: Adopt the permissive reading — implementation-scoped, as rule 9

Change `05-processing-model.md` and `07-transport.md` to drop "MUST NOT pick
a successor on its own" / "MUST surface it rather than pick," matching
`02-block-format.md`'s "detection required, handling strategy
implementation-scoped, exactly as validation rule 9."

- **Pros**: matches what todo 042 actually ratified; matches the reference
  implementation (`go/block`'s `Successors`, "reported, not resolved," which
  does not forbid a caller from picking); smaller diff (two documents to
  change, one already correct).
- **Cons**: a node MAY now auto-subscribe to a first-seen candidate and
  treat it as canonical for block ordering while the true successor is
  still ambiguous and surfaced — weaker than what three of the four
  references to this rule currently promise a reader.

### Option 3: Split the question — MUST NOT pick for L3 purposes, MAY pick for L1 bookkeeping

State that a node MUST NOT resolve the ambiguity itself (no verdict on which
chain is "the" successor), but MAY provisionally track one candidate the way
it tracks any stored-but-unvalidated chain, revising if the ambiguity
resolves — i.e., detection and surfacing are MUST, but nothing is asked of
what a node does with an unresolved ambiguity beyond not asserting a
verdict. This is close to Option 1 stated more precisely, separating "MUST
NOT decide" from "MAY continue serving/holding both."

- **Pros**: avoids the "no chain to auto-subscribe to" gap of Option 1 by
  being explicit that provisional tracking isn't the forbidden thing;
  precision may be exactly what the current imprecise "pick a successor"
  phrasing needs.
- **Cons**: most drafting effort of the three; a fourth document
  (`06-meta-bonds.md`) is untouched by this rule and doesn't need
  updating, but every place is touched needs to restate the same nuance
  consistently, which is exactly the kind of restatement that produced this
  conflict in the first place.

## Recommended Action

Option 1. The security argument for verifiable succession (todo 042) was
that a node should not act on an unproven chain of custody; permitting
accept-first-seen for an *ambiguous* succession undoes exactly that
argument in the one case where it matters most — two candidates both claim
the position, so *any* silent choice is a coin flip presented as a
decision. The project lead should confirm this against todo 042's original
intent before the text changes, since Option 1 revises that todo's
ratified decision rather than merely clarifying it.

## Technical Details

- **Affected Files**: `spec/02-block-format.md` ("rotate_key", "Verifiable
  succession"); `spec/05-processing-model.md` ("Chain succession (key
  rotation)", "Assertion order"); `spec/07-transport.md` ("Chain
  succession", client rules)
- **Related Components**: `go/block/chain.go`'s `Successors` and
  `ValidateHistory`; any equivalent in `ts/` if one exists
- **Database Changes**: No

## Acceptance Criteria

- [x] `spec/02-block-format.md`, `spec/05-processing-model.md`, and
      `spec/07-transport.md` state the same rule, in the same words or
      words that plainly mean the same thing, for whether a node may treat
      one candidate as the successor while an ambiguous succession stands
- [x] The decision is recorded against todo 042's original ratification —
      either as confirming it (Option 2/3) or as revising it (Option 1),
      explicitly
- [x] `go/block`'s `Successors`/`ValidateHistory` (and `ts/`'s equivalent,
      if any) match whichever reading is chosen

## Work Log

### 2026-08-20 - Filed from the Whole-Spec Normative Sweep

**By:** Claude

Found while verifying todo 005's closing criterion that no other normative
conflicts exist across the spec (`todos/101`). `git log -p -S` traced both
halves of the conflict to one commit, `0a8266c`, which closed todo 042 by
adding contradictory text to two documents at once — this is not two
documents drifting apart over time, it is one authoring pass that said two
things. This agent's writable scope was `todos/` only; resolving the
conflict needs write access to `spec/` and, per the Recommended Action, a
project-lead decision on whether Option 1 revises todo 042's ratified intent
or merely restates it more strictly than intended.

### 2026-08-20 - Ratified and Applied

**By:** Claude

**Decision (project lead): Option 1, the strict reading.** A node MUST NOT
silently pick a successor. An ambiguous succession is a surfaced conflict,
consistent with the protocol's core stance that conflicts are output and never
silently resolved, and with fork detection's MUST-surface rule.
Accept-first-seen is *not* a permissible strategy for succession specifically,
unlike ordinary fork handling where implementation-scoped strategies remain
allowed. This **revises** item 3 of todo 042's "Ratified and Implemented"
decision rather than restating it; a superseding note now sits in that todo's
work log, next to the item it revises, so a reader of 042 is not led back to
the permissive reading.

**Spec changes:**

- `spec/02-block-format.md`, "rotate_key" → "Verifiable succession": the
  "handling strategy... implementation-scoped, exactly as in validation rule 9"
  sentence is gone. The bullet now states the MUST NOT, cross-references
  `05-processing-model.md`, says what the prohibition covers
  (auto-subscription, block order across the junction, any other purpose), and
  says what it does not (holding and serving every claimant is not a choice
  between them). The reason is stated in the document's own terms: a silent
  choice between two chains claiming a rotated key is the unproven chain of
  custody verifiable succession exists to refuse.
- `spec/02-block-format.md`, validation rule 9: one added clause, so that a
  reader arriving from rule 9 — where accept-first-seen is still permitted for
  an ordinary fork — is told this one case is held to a stricter rule.
- `spec/05-processing-model.md`, "Chain succession (key rotation)": the MUST
  NOT was already there; added what happens to steps 2 and 3 in the interim,
  which was Option 1's one open cons item. They wait: no claimant is added as
  the successor and none is auto-subscribed to while the ambiguity stands.
- `spec/07-transport.md` needed no change; it already stated the strict rule.

**Go changes.** The permissive behaviour was *mostly* not there. `Successors`
already returned every claimant and a `*Fork` and preferred none; `accept`'s
`blockOrder` already refuses to join an ambiguous junction and surfaces a
`ConflictAmbiguousSuccession`; `transport` reports the claimants as a fork at
the genesis position and picks nothing. One place did bless a pick:
`ValidateHistory` validated whichever junction the caller named and merely
appended the fork to the successor chain's report, which is the API affirming a
chosen successor while the ambiguity stands.

- `go/block/chain.go`: new exported `AmbiguousSuccessionError`, naming the
  rotation block and every claimant. `ValidateHistory` returns it instead of
  validating the junction, for either claimant; each chain still validates on
  its own with `ValidateChain`, so what is refused is the junction and not the
  blocks. A source that does not implement `Referrers` cannot be asked the
  question and is unaffected. `Successors`'s doc comment no longer reads the
  permissive text ("reported, not resolved, exactly as rule 9") and states the
  caller's MUST NOT.
- `go/block/validate_test.go`: the "two chains claiming one rotation" subtest
  now proves the ambiguity is surfaced and not resolved — the typed error, both
  claimants named, both orderings refused, each chain still valid alone.

**TypeScript.** `ts/` does have succession logic despite its wire-format and
transport scope: `BlockStore` detects successions and exposes
`ambiguousSuccessions`. It already conforms — it names every claimant, exposes
no "the successor" accessor, and its doc comment already carried the MUST NOT.
No change was needed beyond the new vector's test.

**Vectors.** `vectors/blocks.json` gains an `ambiguous_succession` section:
`alice_successor_rival`, a second genesis block signed by the key
`alice_rotation` appoints and naming that same rotation block in `refs`. The
generator (`go/internal/vectors/blocks.go`) validates the claim it publishes —
the rival is a valid block, and against the scenario it leaves exactly two
claimants and a fork. Both suites consume it: the Go conformance test replays
the chain, offers the rival, checks both claimants are reported and that
`ValidateHistory` refuses either ordering; the TypeScript test runs it through
the byte-level checks and asserts `ambiguousSuccessions` names both claimants.
`vectors/README.md` records the section and the stricter rule.

**Verification:** `go test ./...` (all packages), `npm test` (456 tests),
`golangci-lint run` (0 issues), and the vectors round-trip guard
(`TestVectorsAreCurrent`) all pass.

## Notes

Source: `todos/101`, the whole-spec normative-keyword consistency sweep.
