---
status: pending
priority: p2
issue_id: "088"
tags: [transport, specification-gap, processing-model]
dependencies: ["083"]
---

# When Is an Announce Receipt's Disposition Decided?

## Problem Statement

`spec/07-transport.md` says every submitted block MUST appear in exactly one of
`accepted`, `held` and `rejected`, and what each of the three means. It does not
say *when* the source decides which.

The two readings give different receipts for the same request:

- **Block by block.** Each block's disposition is recorded as it is offered. A
  block whose predecessor arrives later *in the same sequence* is reported as
  `held`, even though the source accepted it before the response was written.
- **After the whole sequence.** Every block is offered first, and the receipt
  reads the source's final verdicts. The same block is reported as `accepted`.

Both satisfy the letter. The first produces a receipt that is already stale when
it is sent, and it produces one that varies with the order the announcer chose —
which the profile deliberately leaves loose for several authors: "blocks of
several authors MAY be interleaved in any way that keeps each author's own
blocks in chain order".

`todos/083` makes this sharper rather than softer. A held block is now settled by
the *arrival* of what it waits for, so a single announce carrying a foreign
definition and the block that uses it settles the second block during the first
block's admission — and a block-by-block receipt would report a `held` verdict
the source no longer holds.

## Findings

- `go/transport`'s `StoreAnnouncer` makes two passes: it offers every block, then
  reads the store's verdicts. The receipt therefore describes the source's state
  after the whole announce, which is the state the announcer's next request will
  meet.
- The two-pass reading is also the only one that keeps a receipt idempotent
  against re-announcing: offering the same sequence twice gives the same receipt,
  which the block-by-block reading does not guarantee.
- A source MAY refuse an announce entirely, which is a third thing again and is
  covered ("A source MAY refuse an announce entirely, for reasons that are its
  own policy"). This todo is only about the per-block dispositions of an announce
  the source did take.
- 202 is defined as "the announce was taken for later processing; the receipt is
  incomplete or absent", which is the escape hatch for a source that cannot
  answer either way in time. It does not resolve the question for a 200.

## Proposed Solutions

### Option 1: The receipt describes the source's state after the whole sequence

"A source MUST determine each block's disposition after it has processed the
entire announce, so that a block settled by a later block of the same sequence is
reported as accepted."

- **Pros**: a receipt describes a state the announcer can act on; identical for
  the same sequence announced twice; matches what `todos/083` made possible.
- **Cons**: obliges a streaming implementation to buffer its dispositions, which
  is one map.

### Option 2: The receipt is block by block, and says so

- **Pros**: a source can answer as it reads.
- **Cons**: the receipt reports verdicts the source has already moved past, and
  the announcer's re-announce gets a different receipt for the same bytes.

### Option 3: Leave it unstated

- **Cons**: two conforming servers answer the same announce differently, and an
  announcer that retries what it was told was held retries what was accepted.

## Recommended Action

Option 1. It is one sentence, and it makes the receipt mean "here is where these
blocks stand with me", which is the only reading an announcer can use.

## Technical Details

- **Affected Files**: `spec/07-transport.md` ("announce", and the receipt
  paragraph under "Bodies and content types")
- **Related Components**: `announce`, stored but unvalidated blocks,
  revalidation on arrival (`todos/083`)
- **Database Changes**: No

## Acceptance Criteria

- [ ] The specification says when a disposition is decided
- [ ] `go/transport`'s `StoreAnnouncer` matches it

## Work Log

### 2026-08-19 - Filed While Implementing spec/07

**By:** Claude

Found writing the announce handler: a sequence carrying a definition and the
block that uses it settles the second during the first's admission, and the
obvious block-by-block receipt would report it held.

## Notes

Source: the first implementation of `spec/07-transport.md`.
