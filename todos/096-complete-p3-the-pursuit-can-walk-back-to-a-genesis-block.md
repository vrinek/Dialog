---
status: complete
priority: p3
issue_id: "096"
tags: [transport, specification-gap, forks]
dependencies: [093]
---

# The Pursuit Can Walk Back to a Genesis Block

## Problem Statement

`spec/07-transport.md`, "Pursuing an advertised tip", enumerates how the
backward walk ends:

> 4. Stop when the client reaches a block it holds, when a fetch fails, or when
>    the client's own bound on the walk's length is reached.

There is a fourth way, and both implementations of the rule hit it on the first
pass: the walk reaches a **genesis block**. Its `prev` is absent, so there is no
predecessor to ask for, and the client may still hold no block of that walk. The
enumeration does not cover it, and the paragraphs after it are written for the
three cases it does cover — "reaching a block the client holds is the point of
the exercise", and "a walk that fails ends in fetches that did not succeed".

Reaching a genesis block is neither. Nothing failed: every fetch succeeded and
every block verified. But no block the client holds was met either, so the
sentence that says what the pursuit *bought* does not apply.

## Findings

- The case is not exotic. It is what a source serving a **second genesis block**
  for one author looks like from a client that holds the first — the
  ambiguous-succession shape `siblings` at the genesis position is defined for
  (`spec/07-transport.md`, "siblings"; `spec/02-block-format.md`, "rotate_key").
  A client following a chain from one source and meeting a source that serves an
  entirely different chain of the same author walks its whole length and lands on
  a genesis block.
- **The outcome is benign and both implementations agree on it.** The walk's
  blocks are offered to the store like any others; the two genesis blocks then
  sit at the genesis position, which is a fork of the kind validation rule 9
  names and which the genesis position's sibling set shows. So rule 9 fires by
  itself, without the walk having met a held block — the mechanism the section
  describes still delivers, by a route the section does not describe.
- The two implementations label it differently, which is the observable part:
  `go/transport`'s `Syncer.pursue` breaks out of the loop and reports no
  `PursuitErr`, so it is indistinguishable in the report from having reached a
  held block; `ts/`'s `pursueTip` reports it as its own outcome, `"genesis"`,
  distinct from both success and failure. Neither is wrong under the current
  text, which is the problem: a conformance suite has nothing to check.
- The walk also terminates *correctly* in this case in both, so nothing is
  broken. What is missing is a sentence, and — if the answer is that it deserves
  a distinct outcome — a shared name for it.

## Proposed Solutions

### Option 1: Name it as a fourth way the walk ends, with no verdict

Add to step 4: "…or when the block the walk reaches is a genesis block, which
has no predecessor to ask for." And a sentence after: a walk that ends at a
genesis block has found that this source serves a chain sharing no block with
the client's; the blocks it fetched are stored and validated like any others,
and the two genesis blocks are then a sibling set at the genesis position, where
the ambiguous-succession condition is detected. No verdict follows from the
walk's end itself.

- **Pros**: one clause and one sentence; keeps "a failed walk is only failed
  fetches" true by not calling this a failure; points at the mechanism that does
  fire.
- **Cons**: none identified.

### Option 2: Fold it into "reaching a block it holds"

Say that the walk ends successfully when it can go no further, whether because
the client holds the block or because the block is a genesis block.

- **Pros**: three cases stay three.
- **Cons**: the two are not the same outcome — one leaves a fork at a position
  the client was already on, the other at the genesis position — and a client
  reporting them identically loses the distinction its own logs need.

### Option 3: Say nothing

- **Pros**: no text; nothing is broken today.
- **Cons**: two implementations already report the same event differently, which
  is what settling 090 to 095 was for.

## Recommended Action

Option 1. Also decide whether the outcome deserves a name the two
implementations share, since one has already given it one.

## Technical Details

- **Affected Files**: `spec/07-transport.md` ("Pursuing an advertised tip", step
  4 and the two paragraphs after it)
- **Related Components**: `siblings` at the genesis position; validation rule 9;
  `spec/02-block-format.md`, "rotate_key"; todo 093
- **Database Changes**: No

## Acceptance Criteria

- [x] The specification says what it means for the pursuit to reach a genesis
      block
- [x] The two implementations report that outcome by one name

## Work Log

### 2026-08-19 - Filed While Implementing Todo 093

**By:** Claude

Found by both implementations of "Pursuing an advertised tip" on the first pass,
independently: the loop needs a branch for a block with no `prev`, and the
specification's enumeration has none.

### 2026-08-19 - Ratified and Applied

**By:** Claude

**Option 1**, and the open question at the end of it answered: the outcome does
deserve a name, and the name is the one the implementation that already had one
gave it.

- `spec/07-transport.md`, "Pursuing an advertised tip": step 4 enumerates four
  stop conditions, the genesis block among them, and two paragraphs follow it.
  The first fixes **three names for what a client reports** — *held*, *genesis*
  and *failed* — folds the client's own bound into *failed* (a bound reached and
  a fetch unanswered both end the walk with no block at the end of it), permits a
  client to report a failure's kind more precisely, and forbids collapsing
  *genesis* into either of the other two: nothing failed, and no held block was
  met. The second says what the genesis end **found**: two chains claim this
  author with no block in common, the most fundamental fork there is; the blocks
  are stored and validated like any others; the two genesis blocks are two
  distinct blocks claiming one (empty) position, which is validation rule 9's
  condition and which `spec/02-block-format.md` already calls a fork in the
  strict sense under "rotate_key"; the client MUST surface it with the two
  genesis blocks as the sibling pair. No verdict follows from the walk's end
  itself.
- `go/transport`: `SourceSync.PursuitEnd`, of the new `PursuitEnd` type with
  `PursuitNone`/`PursuitHeld`/`PursuitGenesis`/`PursuitFailed`, beside the
  `PursuitErr` that carries the finer failure kind. `TestPursuitToAGenesisBlock`
  is the scenario: two chains under one key with different genesis blocks, the
  second source gaining the one whose genesis sorts lower, an empty range, a
  pursuit that ends as `genesis` with no error, and a fork at the genesis
  position whose sibling set is exactly the two genesis blocks.
- `ts/`: `PursuitEnd` with the same three values is what `TipPursuit.end`
  reports, and the five-value `PursuitOutcome` stays as the refinement the
  profile permits, folded onto the three by `pursuitEnd()`. Its scenario test
  asserts the same facts from the other implementation, including that the end is
  neither `failed` nor `held`, and that the store's genesis-position sibling set
  is exactly the two genesis blocks.

**Vectors: no byte moved.**

## Notes

Source: implementing the rule todo 093 settled, in `go/transport` and `ts/`.
