---
status: complete
priority: p3
issue_id: "097"
tags: [transport, typescript, implementation]
dependencies: [086]
---

# A Second Definition of a Tip Lives in the TypeScript Store

## Problem Statement

`spec/07-transport.md` defines a tip **constructively**, as the end of a forward
walk from the genesis position through the blocks a source holds, and says out
loud why the other definition is refused:

> The alternative definition — the tip is any block the store holds that nothing
> else names as a predecessor — is the one a store's own index answers cheaply,
> and it is the one that makes a server report a tip it cannot serve a `range`
> to, which server rule 1 refuses.

`ts/src/block.ts`'s `BlockStore.tip()` is that alternative definition, written
out: it scans the store for a block of the author that no sibling set names as a
predecessor. It also skips blocks the store holds as unvalidated, which server
rule 7 (todo 091) forbids a *source* from doing, and it keeps the **last**
candidate it finds in map-insertion order, so a store holding a fork answers with
whichever branch happened to be added second.

## Findings

- **Nothing on the serving path calls it.** `ts/src/transport.ts` answers `tip`
  and `range` from its own constructive walk (`walkChain` / `sourceTip`), so the
  server is conforming and this is not a live conformance bug. Grepping `ts/src`
  and `ts/test` for callers outside the transport finds none at all.
- That is exactly what makes it worth a todo rather than a fix in passing. It is
  an unused method with the right name and the wrong definition, in the module a
  future implementer of anything tip-shaped would reach for first, and every one
  of the three ways it differs from the profile's definition is a way that
  matters: it reports a tip across a hole that `range` cannot reach (server rule
  1), it withholds a block whose verdict is undecided (server rule 7), and it
  picks a fork's branch by insertion order rather than deterministically and
  stably (todo 086, `tip`).
- The Go implementation has no equivalent: `block.MemStore.Tips` returns *every*
  candidate rather than one, so it cannot be mistaken for the profile's tip, and
  `transport.Server.tipOf` does the constructive walk.
- The store's own use for such a method is real, though — "which of my blocks is
  the end of this chain" is a question a node asks itself for reasons that have
  nothing to do with serving. The fix is probably not deletion.

## Proposed Solutions

### Option 1: Make it the profile's definition

Replace the body with the constructive forward walk from the genesis position,
crossing blocks whatever their verdict, choosing a fork's branch by lowest digest
— the same walk the transport does — and let the transport call it instead of
carrying its own.

- **Pros**: one definition of a tip in the codebase; the transport's walk stops
  being a private copy; the store answers the question a serving node actually
  has.
- **Cons**: the store gains a chain walk it did not need; a caller that wanted
  "the newest block I hold" no longer has one.

### Option 2: Rename it to what it is

`heads()`, returning *every* block of the author nothing names as a predecessor,
plural and unfiltered, the way the Go store's `Tips` does. Callers wanting one
block choose for themselves.

- **Pros**: honest; a plural answer cannot be mistaken for the profile's tip; no
  new walk.
- **Cons**: leaves the transport carrying its own walk, which is fine.

### Option 3: Delete it

- **Pros**: nothing calls it.
- **Cons**: throws away a question the store is the right place to answer.

## Recommended Action

Option 2, with a doc comment pointing at `spec/07-transport.md`'s definition and
saying that this is not it. Option 1 if a caller turns up that wants the
profile's tip from the store.

Whichever is chosen, the unvalidated-block filter should go: a store's own head
question has no more business ignoring an undecided block than a source's does.

## Technical Details

- **Affected Files**: `ts/src/block.ts` (`BlockStore.tip`)
- **Related Components**: `ts/src/transport.ts`'s `walkChain`/`sourceTip`;
  `spec/07-transport.md`, "tip" and server rules 1 and 7; todos 086, 091
- **Database Changes**: No

## Acceptance Criteria

- [x] `ts/` has one definition of a tip, or two that cannot be confused
- [x] Whatever remains does not filter by verdict

## Work Log

### 2026-08-19 - Filed While Aligning ts/ to the Settled spec/07

**By:** Claude

Found auditing the TypeScript serving path for the verdict filter todo 091
forbids. The serving path is clean; this method, which nothing calls, is not.

### 2026-08-19 - Ratified and Applied

**By:** Claude

**Option 3**, deletion, against the todo's own recommendation of Option 2.

The argument for keeping something was that "which of my blocks is the end of
this chain" is a real question a store is the right place to answer. It is — but
nothing asked it, and the question the *codebase* actually asks is the profile's,
which `walkChain`/`sourceTip` already answer constructively. Renaming to `heads()`
would have kept an unused method whose only readers would be people looking for a
tip, in the module they look in first; a plural name makes the answer harder to
misuse but does not make an uncalled method worth carrying. The rejected
definition is now absent rather than parked under a better name, and if a caller
ever turns up wanting one, it will arrive with the definition its call site needs.

- `ts/src/block.ts`: `BlockStore.tip()` is gone. Nothing referenced it — grepping
  `ts/src` and `ts/test` found only `DialogClient.tip` and `DialogServer.tip` —
  so no caller had to be realigned. A comment stands where it was: there is
  deliberately no tip here, `walkChain`/`sourceTip` is the codebase's only
  definition, and the three ways the deleted one differed are named (a tip across
  a hole, which server rule 1 refuses; a verdict filter, which server rule 7
  forbids; a fork's branch chosen by insertion order, which todo 086's stability
  rule refuses).

**Vectors: no byte moved.**

## Notes

Source: the clean-room implementation's own store, read while applying todos 090
to 095.
