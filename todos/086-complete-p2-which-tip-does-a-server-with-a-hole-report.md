---
status: complete
priority: p2
issue_id: "086"
tags: [transport, specification-gap, http-binding]
dependencies: []
---

# Which Tip Does a Server With a Hole Report?

## Problem Statement

Two requirements of `spec/07-transport.md` disagree about a store with a gap in
it.

`tip` says the response is "the block that occupies the tip position of that
author's chain in the source's store". Server rule 1 says a server MUST serve,
through `range`, "every block it holds of that chain, in chain order, from the
genesis block to the tip it reports", and that "a server that answers `tip` for
an author and cannot answer `range` from the genesis position for the same
author is not conforming". Server rule 2 says that where the server holds a gap
it MUST end the range before the gap.

A server holding blocks 3, 4 and 5 of a chain whose first three blocks it never
received holds a block that occupies the tip position — block 5 — and cannot
serve a range that reaches it. Reporting block 5 as the tip violates rule 1.
Reporting block 2 as the tip is not "the block that occupies the tip position".
Reporting nothing is neither.

The same question arises at a fork, one step further on: `tip` MUST answer with
exactly one candidate and `range` MUST follow one branch "consistently with what
its `tip` reports", but the specification does not say whether the choice has to
be *stable over time*. A server that picked a different branch per request would
satisfy both sentences read individually and be useless.

## Findings

- `go/transport` defines the tip as the last block of the forward walk from the
  genesis position, using the same branch choice `range` uses. That makes rules
  1 and 2 hold by construction, and it makes the store above report no tip at
  all and serve an empty range — while still serving blocks 3, 4 and 5 by
  digest, where no claim about a chain is being made.
- The alternative reading — the tip is any block the store holds that nothing
  else names as predecessor (`block.MemStore.Tips`) — is the one a store's own
  index answers cheaply, and it is the reading that breaks rule 1.
- The branch choice is a deterministic function of the blocks in this
  implementation (lowest digest bytewise), so it is stable across requests, across
  restarts, and across two servers holding the same blocks. The specification
  asks for none of that; it asks only for consistency between `tip` and `range`
  within a response pair.
- Serving by digest and serving by chain are different claims, and the profile
  does not distinguish them. `GET /blocks/{cid}` says "here is a block"; `range`
  says "here is a contiguous run of this author's chain". A store with a hole can
  honestly do the first and not the second.

## Proposed Solutions

### Option 1: The tip is the end of what `range` can serve

"The tip a server reports is the last block of the contiguous run it holds from
the author's genesis block. A server that holds no genesis block for an author
reports no tip and serves an empty range, whatever else it holds of that chain."

- **Pros**: rules 1 and 2 become consequences rather than separate obligations;
  a client's `Dialog-Tip` comparison means exactly one thing.
- **Cons**: a server that received a chain out of order reports no tip until the
  genesis block arrives, which looks like silence from the author. It is
  distinguishable from silence by asking for the blocks by digest — if the client
  knows their digests, which is the same problem `todos/072` names.

### Option 2: The tip is any leaf, and rule 1 is weakened

Let a server report a leaf it cannot serve a range to, and change rule 1 to
require only that a range from the genesis position return what the server holds
contiguously from there.

- **Pros**: a server reports the newest block it actually has.
- **Cons**: the `Dialog-Tip` comparison stops meaning "you are caught up", which
  is the header's only purpose; a client would loop asking for a range that never
  reaches the claimed tip.

### Option 3: Say the branch and tip policy is the source's, and require only
stability

- **Pros**: honest about what is policy.
- **Cons**: leaves the rule 1 contradiction untouched.

## Recommended Action

Option 1, plus one sentence requiring the fork choice to be stable for as long as
the server holds the same blocks — which costs nothing to implement as a
deterministic function of the blocks and gives a client's repeated requests a
meaning.

## Technical Details

- **Affected Files**: `spec/07-transport.md` (`tip`, `range`, "Server rules"
  1 and 2)
- **Related Components**: `Dialog-Tip`, the client's continuation decision, fork
  serving
- **Database Changes**: No

## Acceptance Criteria

- [x] The specification defines the tip of a store with a hole in it
- [x] The specification says whether a fork's branch choice must be stable
- [x] `go/transport`'s `tipOf` matches whatever it says

## Work Log

### 2026-08-19 - Filed While Implementing spec/07

**By:** Claude

Found writing `tipOf`: the natural implementation (ask the store for its leaves)
produces a server that fails server rule 1 on any store with a gap.

### 2026-08-19 - Ratified and Applied

**By:** Claude

**Option 1**, as recommended, plus the stability sentence.

- `spec/07-transport.md`, "tip": the tip is now defined **constructively** —
  begin at the genesis position, walk forward through the source's own store
  along the branch it serves, and the tip is the last block reached. Two
  consequences are stated: a source holding no block at the genesis position
  holds no tip whatever else it holds, and a source with a hole has as its tip
  the last block before the hole and serves a range ending there.
- Server rules 1 and 2 now say they are consequences of that definition rather
  than obligations a server checks for itself; a server that computes its tip
  some other way owes both separately. The rules themselves are unchanged, so
  nothing that conformed stopped conforming.
- The hole is stated informatively as the server's problem to fix — it fetches
  the missing block and the walk goes further on its own — and the blocks past
  it stay serveable by digest, where no claim about a chain is made. The
  rejected reading (the tip is any leaf) is named with the reason: it is the
  cheap one for a store's index and the one that breaks rule 1.
- The fork choice MUST be **deterministic and stable per author**: for as long
  as the source holds the same blocks, every `tip` and every `range` follows the
  same branch, and the two MUST agree. The reference rule is informative — the
  lowest digest bytewise, the order `siblings` already sorts in — chosen because
  it is a function of the blocks alone and therefore stable across requests,
  restarts and sources. `range`'s own fork sentence points at it.
- `go/transport` already worked this way; `tipOf` and `branch` now cite the
  rules. Tests: `TestNoGenesisIsNoTip` (a store holding blocks 2-4 only reports
  404 from `tip`, serves an empty range, and still serves those blocks by
  digest) and two additions to `TestServerServesOneBranchOfAFork` (repeated
  requests to one server, and a second server holding the same blocks in the
  opposite insertion order, report the same branch). `TestServerStopsBeforeAHole`
  already covered the mid-chain hole.

**Vectors: no byte moved.**

## Notes

Source: the first implementation of `spec/07-transport.md`.
