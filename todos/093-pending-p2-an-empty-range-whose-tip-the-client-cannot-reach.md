---
status: pending
priority: p2
issue_id: "093"
tags: [transport, specification-gap, forks]
dependencies: [070]
---

# An Empty Range Whose `Dialog-Tip` the Client Cannot Reach

## Problem Statement

`spec/07-transport.md`, `range`: "If it holds none, the response is an empty
sequence — which is the answer both when the client is already at the tip and
when the source's store stops there, and those two are distinguished by
comparing against `tip`, not by the emptiness."

There is a third case, and it is the one that carries the profile's most
important property. A client that has synced a chain asks a second source for a
range after the position it holds, and gets:

```
200 OK
Dialog-Tip: bafyrei…            ← a block the client does not hold
<empty sequence>
```

The source holds a tip, so it is not the "source's store stops there" case. The
client is not at that tip, so it is not the "already caught up" case. The source
is serving a chain the client's position is **not on** — which is what a fork
looks like from the second source, and also what a source ahead on a branch the
client has a different version of looks like.

The profile does not say what the client does next. It matters more than it
looks: this is the exact moment the multi-source rule either fires or does not.
A client that treats the empty range as "nothing new here" walks away from a
fork it was one request from detecting, and satisfies validation rule 9
vacuously — the failure mode the multi-source rule exists to prevent.

## Findings

- The worked example in "A full sync session" describes the situation
  informally: "A different tip means one of three things, and the client tells
  them apart by fetching the range from the second source too", followed by a
  `siblings` query "at the divergent position". Neither step is normative, and
  the example assumes the client already knows *which* position is divergent —
  which is what it is trying to find out.
- The two mechanisms available cost differently:
  - **Re-ask from the genesis position.** One request. The second source's range
    is then a prefix of the first's, an extension of it, or a walk that diverges
    at some position; in the third case the divergent blocks land in the store
    and rule 9 fires on them. Costs re-downloading the shared prefix.
  - **Walk backwards asking `siblings` at each position.** One request per
    block until the divergence is found, and it finds the exact position with no
    redundant bytes. Worst case is the whole chain.
  - A **binary search** over the client's own chain using `range?limit=1` at
    each probe is the middle option: logarithmic requests, no redundant blocks,
    and more client code than either.
- Nothing detects the difference between this case and a source that is simply
  withholding the blocks between the client's position and its declared tip.
  That is the freshness gap (todo 075) and is not fixable here; but the client's
  *next request* is a choice the profile could pin, and today does not.
- The second implementation (`ts/src/transport.ts`, `syncChain`) re-asks from
  the genesis position, once, and records that it did so. It is the cheapest
  option that always finds the divergence, and it is the move the worked example
  makes. `ts/test/transport.sync.test.ts`, "two sources with different branches
  produce a fork at the client", is the scenario end to end: two sources each
  serving one branch, each answering a one-member sibling set, and the fork
  appearing at the client that asks both.

## Proposed Solutions

### Option 1: Name the case and require a client to pursue it

Add a paragraph to "A partial range" or to "The multi-source rule": a client
that receives an empty range whose `Dialog-Tip` names a block it does not hold
has learned that this source serves a chain its position is not on. It MUST NOT
treat this as "no new blocks". It SHOULD locate the divergence — by re-issuing
the range from the genesis position, or by `siblings` at the positions of its
own chain — and it MUST surface any fork the result reveals.

- **Pros**: the multi-source rule stops depending on a client noticing an
  unwritten case; a conformance suite can test it; costs one paragraph.
- **Cons**: prescribes client behaviour in a profile that has so far described
  obligations rather than algorithms — though "Verification obligations" already
  does exactly that.

### Option 2: Add a server-side signal

Let `range` answer 409, or carry a header naming the position at which the
requested one diverges from the branch it serves.

- **Pros**: one request, no guessing.
- **Cons**: requires the server to compute reachability of an arbitrary position
  against its own branch, which is real work and real state; and it is a claim a
  client cannot verify, so it buys nothing a client can rely on. Against the
  profile's grain.

### Option 3: Leave it to the client

- **Pros**: no text.
- **Cons**: the case is not obscure — it is the *normal* second-source case for
  any forked chain — and two implementations will silently differ on whether
  forks are ever detected at all, which is the one property the profile claims
  the multi-source rule delivers.

## Recommended Action

Option 1, sited in "The multi-source rule" rather than in `range`, because it is
that rule's operational content: the rule says "obtain each chain from more than
one source, and compare", and this is what comparing actually consists of.

## Technical Details

- **Affected Files**: `spec/07-transport.md` ("A partial range"; "The
  multi-source rule")
- **Related Components**: `siblings`; validation rule 9; todo 070
- **Database Changes**: No

## Acceptance Criteria

- [ ] The specification names the empty-range-with-an-unreachable-tip case
- [ ] It says what a client does about it, at least as a SHOULD

## Work Log

### 2026-08-19 - Filed While Writing the Second Implementation

**By:** Claude

Found writing `syncChain`: the obvious loop — resume from the local tip until
the range comes back empty — detects no fork ever, against two sources that each
serve one branch honestly. Nothing in the profile says so, and the test that
catches it had to be written before the code that passes it.

## Notes

Source: the second implementation of `spec/07-transport.md` (`ts/`), written
clean-room against the specification alone.
