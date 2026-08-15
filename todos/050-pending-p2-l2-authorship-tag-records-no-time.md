---
status: pending
priority: p2
issue_id: "050"
tags: [specification-gap, processing-model, layer-2, layer-3, conflict-resolution]
dependencies: ["016"]
---

# L2 Records No Time, and L3 Is Offered a Latest-Wins Strategy

## Problem Statement

`spec/05-processing-model.md` § "Accumulation rules" fixes what an entity in L2
is tagged with, and the list has two entries:

> 3. Add the entity to the L2 graph, tagged with:
>    - The author's public key (from the block's `pub` field)
>    - The block's CID (provenance)

Two sections later, § "Meta-molecule application" offers L3 a menu of conflict
resolution strategies, one of which is:

> - Latest-wins (by timestamp or block order)

Neither input exists in L2. The block's `ts` is not among the tags; block order
is not among them either, and cannot be recovered from the tags, since a block
CID says nothing about where in a chain the block sits. An L3 view built from
the graph the accumulation rules describe therefore cannot implement a strategy
the same document suggests, without reaching back into L1 for every entity it is
weighing — across authors, and across chains it may hold only partially.

Two further questions hang off this:

**Is the tag list closed?** The rules are a MUST with a two-item list. An
implementation that also tags each entity with the block's `ts`, or with an
arrival sequence number, is either doing something the specification permits and
did not mention, or exceeding a normative list. Nothing says which.

**Which time, and is it usable?** `spec/02-block-format.md` is explicit that a
block's timestamp is self-reported and untrusted: "Timestamps MUST NOT be used
for validation decisions". Latest-wins by `ts` is not a validation decision, but
it is a truth decision made from a number an author picks freely — a strictly
worse position, since a lying author wins every conflict. And for a private
block the `ts` is inside `enc`: it exists for key holders and does not exist for
anyone else, so the same conflict resolves differently for two nodes depending
on which keys they hold. Todo 016 (timestamp monotonicity) is the same untrusted
field seen from L1.

## Findings

- `spec/05-processing-model.md`, "Accumulation rules", step 3: the two-item tag
  list.
- `spec/05-processing-model.md`, "Meta-molecule application": the strategy menu,
  including "Latest-wins (by timestamp or block order)", under "The protocol
  does NOT require any specific conflict resolution strategy".
- `spec/02-block-format.md`, "Security Considerations": "Timestamps are
  self-reported by authors and are not verified [...] MUST NOT be used for
  validation decisions."
- `spec/02-block-format.md`, "Private block" and `spec/04-cryptography.md`: `ts`
  is one of the three fields encrypted into `enc`.
- `spec/05-processing-model.md`, "Private chains", step 2: decryption recovers
  "`refs`, `ts`, and `ops`" — so a key holder has the timestamp and a
  non-recipient never will.
- `go/graph`, `Authorship`: carries exactly the two tags the rules name — author
  key and provenance block digest — and no time. `block.Block.TS` and
  `block.Payload.TS` remain available at L1 for a caller that wants them.

## Proposed Solutions

### Option 1: State that the tag list is a minimum, and where time comes from (Recommended)

- Say in § "Accumulation rules" that the two tags are the minimum an entity MUST
  carry, and that an implementation MAY record additional provenance — the
  block's `ts`, its position in its chain, local arrival order — as long as no
  such value affects validity.
- Add a sentence to the strategy menu noting that latest-wins requires
  provenance L2 is not required to keep, that `ts` is author-controlled and
  therefore adversarial input, and that a private block's `ts` is visible only to
  key holders, so the strategy is not uniformly available.
- **Pros**: keeps the tag list normative, unblocks the strategy the document
  itself suggests, and puts the warning where the strategy is offered.
- **Cons**: "MAY record more" is a licence an implementation could abuse to
  smuggle non-protocol data into L2; the "no effect on validity" clause is what
  bounds it.
- **Effort**: Small (spec), small (Go, if L2 grows an optional time field)
- **Risk**: Low

### Option 2: Drop latest-wins from the menu

- Remove the strategy that the layer below cannot feed, leaving flagging, author
  priority and application-specific logic.
- **Pros**: the document stops suggesting something it does not support.
- **Cons**: latest-wins is what most applications will reach for first; removing
  the mention does not remove the demand, and implementations will build it from
  L1 anyway, undocumented.
- **Risk**: Medium

### Option 3: Add `ts` to the required tag list

- Make the timestamp a third mandatory tag.
- **Pros**: the strategy becomes uniformly implementable for public data.
- **Cons**: it is not implementable at all for private blocks a node cannot
  decrypt, so the tag would be mandatory-but-absent; and it promotes an
  untrusted, author-chosen number into the layer the specification calls "what we
  know".
- **Risk**: Medium

## Recommended Action

Option 1. Keep the required tags as they are, say they are a floor rather than a
ceiling, and say plainly at the point of use what latest-wins costs.

## Technical Details

- **Affected Files**: `spec/05-processing-model.md` ("Accumulation rules",
  "Meta-molecule application")
- **Related Components**: L2 accumulation, L3 conflict resolution, private
  chains
- **Database Changes**: No

## Acceptance Criteria

- [ ] Whether the L2 tag list is closed or a minimum is stated
- [ ] The provenance a latest-wins strategy needs is either available at L2 or
      the strategy is qualified where it is suggested
- [ ] The asymmetry of `ts` between key holders and other nodes is acknowledged

## Work Log

### 2026-08-15 - Filed While Implementing go/graph

**By:** Claude

Found deciding what an `Authorship` record holds. The two required tags were
easy; the question was whether to add `ts`, and the specification answers it
twice in opposite directions — the accumulation rules do not list it, the L3
strategy menu assumes something like it. `go/graph` implements the rules as
written and keeps no time, which means the `accept` package (L3) cannot offer
latest-wins without a change here or a second read of L1.

## Notes

Source: Go reference implementation, phase 6 (L2 ontology graph).
