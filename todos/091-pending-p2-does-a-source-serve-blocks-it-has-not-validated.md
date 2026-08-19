---
status: pending
priority: p2
issue_id: "091"
tags: [transport, specification-gap, processing-model]
dependencies: []
---

# Does a Source Serve Blocks It Has Not Validated?

## Problem Statement

Every operation of `spec/07-transport.md` is defined over what a source
**holds**: `tip` walks "the block the source holds" at each position, `range`
returns what it holds contiguously, `block` and `blocks` answer for "blocks in
its own store", `siblings` returns "every block the source holds" at a position.

`spec/05-processing-model.md` gives a node a third state for a block it holds:
**stored but unvalidated**. A block whose predecessor has not arrived, or whose
`refs` the node cannot resolve, is neither valid nor invalid — the node has not
been able to decide — and it is kept.

Nothing says whether such a block is part of what a source *serves*. The two
readings give different answers to the same request:

- **Serve what you hold.** A store that received blocks 3, 4 and 5 of a chain
  before block 0 holds three blocks it has not validated. Under this reading it
  answers `block` for each of them, and `siblings` names them.
- **Serve what you have validated.** The same store answers 404 for all three,
  and reports an empty sibling set, until the ancestry arrives.

The profile's own worked example leans on the first reading without stating it:
"a store holding blocks 3, 4 and 5 of a chain whose first three it never
received reports no tip and serves an empty range, **while still serving those
three blocks by digest** — `block` and `blocks` make no claim about a chain, and
a store with a hole can answer them honestly." That sentence settles `block` and
`blocks` for the *hole* case, which is exactly the case where the blocks are
unvalidated. It does not generalize the rule, and it says nothing about
`siblings`, where the stakes are higher.

## Findings

- `siblings` is where it bites. A node's fork detection (validation rule 9) is
  fed by sibling sets; a server that withholds an unvalidated block at a
  position is withholding one side of a fork it cannot yet judge, which is
  precisely the omission the multi-source rule exists to defeat. A server that
  serves it is serving a block that may turn out to be invalid — but a client
  MUST validate everything on receipt anyway, so it costs the client nothing but
  bandwidth, and it costs the client a *detection* not to.
- The tip walk inherits the question. If the walk crosses unvalidated blocks, a
  server can report a tip whose ancestry it has never checked. If it does not,
  a server that is behind on validation reports a tip that lags its store for
  reasons no client can see, which is indistinguishable from withholding.
- The withholding reading also lets a lazy server look honest: "I have not
  validated it yet" is unobservable, unfalsifiable, and identical on the wire to
  "I do not have it".
- The second implementation (`ts/src/transport.ts`) serves everything it holds,
  including stored-but-unvalidated blocks, at every operation including the tip
  walk. The reasoning: withholding is the source deciding a validity question on
  the client's behalf, and the client rules of this same document forbid a
  client from delegating exactly that decision ("A client MUST NOT let a
  source's answer decide a validation outcome that the source's bytes do not
  compel").
- There is a real argument the other way for `announce` replication chains: a
  server that serves unvalidated blocks becomes a cheap amplifier for garbage
  that is well-formed and signed but chain-invalid. The bound is the same one
  `announce` already has — validation bounds what can be *stored*, and a
  stored-but-unvalidated block is by definition one that passed the structural
  and signature rules.

## Proposed Solutions

### Option 1: A source serves what it holds, whatever verdict it has reached

Add to "What a conforming server serves": a source answers every operation from
the blocks it holds, including blocks it holds as *stored but unvalidated*
(`spec/05-processing-model.md`, "Block reception"). A source MUST NOT withhold a
block on the ground that it has not been able to validate it; the client
validates everything it receives regardless, and a withheld block is a
detection the client loses.

- **Pros**: makes the hole example a consequence rather than a special case;
  keeps `siblings` honest, which is the operation whose honesty rule 9 depends
  on; nothing a server does about its own verdicts is observable, which is the
  right shape for a profile whose whole security argument is that a source
  cannot lie about contents.
- **Cons**: a server relays blocks it may later find invalid.

### Option 2: A source serves only what it has validated

- **Pros**: a mirror never relays a block it has judged nothing about.
- **Cons**: contradicts the hole example already in the text; makes fork
  detection worse; makes a slow validator indistinguishable from a censor.

### Option 3: Per operation — `block`, `blocks` and `siblings` serve what is held; the tip walk crosses only validated blocks

- **Pros**: the tip is then a claim a server has itself checked.
- **Cons**: two definitions of "hold" in one document; a server whose validation
  lags reports a tip behind its own range, which server rule 1 exists to
  prevent.

## Recommended Action

Option 1, as one sentence under "What a conforming server serves", with the
existing hole paragraph cited as the case it generalizes.

## Technical Details

- **Affected Files**: `spec/07-transport.md` ("What a conforming server serves";
  the `tip` and `siblings` sections)
- **Related Components**: `spec/05-processing-model.md`, "Block reception";
  validation rule 9
- **Database Changes**: No

## Acceptance Criteria

- [ ] The specification says whether a stored-but-unvalidated block is served
- [ ] `siblings` states it explicitly, since fork detection depends on it

## Work Log

### 2026-08-19 - Filed While Writing the Second Implementation

**By:** Claude

Found building the server over a `BlockStore` that carries the third state: the
store's `siblings` index holds unvalidated blocks, and whether to filter them
was a one-line choice with no rule to hang it on. The TypeScript implementation
serves them and tests the hole case (`ts/test/transport.server.test.ts`, "a
store with a hole reports no tip and serves no range across the hole").

## Notes

Source: the second implementation of `spec/07-transport.md` (`ts/`), written
clean-room against the specification alone.
