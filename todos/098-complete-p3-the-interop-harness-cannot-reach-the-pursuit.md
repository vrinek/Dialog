---
status: pending
priority: p3
issue_id: "098"
tags: [transport, interop, testing]
dependencies: [096]
---

# The Interop Harness Cannot Reach the Pursuit

## Problem Statement

`interop/` crosses the two implementations over every read operation of
`spec/07-transport.md` and asserts they end up holding the same blocks. It does
not exercise **"Pursuing an advertised tip"**, which is the one client rule of
the profile whose absence is invisible: a client that skips the pursuit still
passes every assertion the harness makes today.

The reason is structural rather than an oversight. A pursuit begins when a
source's advertised tip is a block the client does not hold **and** the range
after the client's position came back empty. A client meeting a source for the
first time asks its range from the genesis position, so the source answers with
its own chain from the beginning and the client receives the divergence as
ordinary blocks. The empty range needs the client to already be at a position
that source's walk does not pass through — a *cursor* — and a one-shot process
that starts with an empty store has none.

## Findings

- The `genesis` scenario was built to be the pursuit's case and is not: with two
  servers each holding one of two chains that share no block, both clients
  receive both chains by `range`, and the fork at the genesis position surfaces
  from the store rather than from the walk. That is a correct outcome and worth
  asserting — it is validation rule 9 at the genesis position, cross-checked
  between implementations — but `pursuits` is `[]` in every expectation, and the
  `genesis` **end** that todo 096 settled is never reported by either client
  during a harness run.
- Both implementations do cover the walk in their own suites, including the
  `genesis` end (`go/transport`'s `TestPursuitToAGenesisBlock`, and `ts/`'s
  scenario test). What is missing is the *crossing*: neither implementation has
  ever pursued the other's server.
- Two ways to reach it, both real work:
  1. **A client that persists its cursor.** `-state FILE` on both sync programs:
     the per-source, per-author position the last run ended at, written out and
     read back. The harness then runs the client twice, and between the two runs
     the source's contents change. This is also what a real subscriber does, so
     it is not test scaffolding — but `go/transport`'s `Syncer` keeps its resume
     positions private and would have to expose seeding them.
  2. **A server whose contents change between two rounds of one run.** Both
     servers can already serve `announce` (`-announce`), so the harness could
     start the second server holding only the shared prefix, run the client, POST
     the divergent branch, and run the client again — if the client kept its
     cursor, which is (1) again, or if the client itself ran two rounds with the
     announce in between, which puts harness logic inside the client.
- A third option, a `-after` flag naming the position to range from, would reach
  the empty range in one request and prove almost nothing: it is the harness
  telling the client what to ask, rather than the client arriving at the
  question the way the profile says it does.

## Proposed Solutions

### Option 1: `-state FILE` on both sync programs, and a two-round scenario

Persist the per-source cursor; add a `pursuit` scenario in which the second
server gains the divergent branch (by `announce`, or by being restarted over a
second fixture directory) between two runs of the same client.

- **Pros**: exercises the rule as written, in both directions; the persisted
  cursor is a feature a real client wants anyway; the `genesis` end of the walk
  becomes a cross-implementation assertion rather than two unit tests.
- **Cons**: the largest of the three; needs an exported way to seed a `Syncer`'s
  resume positions on the Go side, and its TypeScript equivalent.

### Option 2: Assert the gap instead of closing it

Leave the harness as it is and keep the paragraph in `interop/README.md`, "What
this does not prove", accurate.

- **Pros**: nothing to build; the gap is written down where a reader meets it.
- **Cons**: the one client rule that a conforming-looking client can skip
  entirely stays uncrossed.

### Option 3: A `-after` flag

- **Pros**: one flag.
- **Cons**: tests the client's obedience to the harness rather than its reading
  of the profile. Rejected above.

## Recommended Action

Option 1, when the persisted cursor is wanted for its own sake. Option 2 until
then — the gap is stated in `interop/README.md` rather than hidden, and both
implementations do cover the walk against their own servers.

## Technical Details

- **Affected Files**: `interop/run.sh`, `interop/README.md`, `go/cmd/dialog-sync`,
  `ts/scripts/sync.ts`, `go/transport/sync.go` (the private `resume` map)
- **Related Components**: `spec/07-transport.md`, "Pursuing an advertised tip";
  todo 096; the `pursuits` field of the interop summary document
- **Database Changes**: No

## Acceptance Criteria

- [ ] A harness scenario in which one implementation pursues a tip advertised by
      the other, in both directions
- [ ] The `genesis` end of the walk is asserted across implementations, not only
      within each

## Work Log

### 2026-08-19 - Filed While Building the Interop Harness

**By:** Claude

Found while writing the fork scenarios: the two-server genesis fixture was built
for the pursuit and reached it in neither implementation, because a client with
an empty store and no cursor never asks a range from anywhere but the genesis
position.

## Notes

Source: `interop/`, the first cross-implementation harness.
