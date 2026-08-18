---
status: pending
priority: p3
issue_id: "073"
tags: [transport, specification-gap, privacy, security, subscriptions, rfc2119]
dependencies: []
---

# What Does the Subscription-Privacy SHOULD Actually Demand?

## Problem Statement

`spec/05-processing-model.md`, Security Considerations, contains the only
sentence in the specification addressed to a transport:

> Author subscriptions are never published, protecting users from social graph
> analysis. However, the set of blockchains a node requests from the network may
> reveal subscription information at the transport layer. Transport
> implementations SHOULD consider this.

"SHOULD consider" is not a requirement. It names no addressee — the project has
never specified, described or sketched a transport layer — and no behaviour
either: an implementation that considered the leak and did nothing has complied.
As RFC 2119 language it is unfalsifiable, which makes it either a promise the
protocol cannot keep or informative text wearing a keyword.

The survey behind the design document found that no deployed substrate solves
query privacy at all, so the choice is not between a strong rule and a weak one.
It is between a checkable minimum and honest informative text.

This is a deliberately-open design question for the `spec/07-transport.md`
drafting phase. Nothing here is decided.

## Findings

- `spec/05-processing-model.md`, Security Considerations: the SHOULD quoted
  above.
- `spec/05-processing-model.md`, "Chain management": "a user MAY subscribe to
  additional blockchains", so the requested set is already a superset of the
  accepted set; demand-driven resolution widens it further with foreign blocks
  from authors nobody subscribed to. An observer of the request stream sees
  blockchain subscriptions, which are not author subscriptions — the distinction
  `todos/028` drew.
- Subscriptions are local configuration: nothing obliges a node to disclose its
  whole set to one server, so partitioning requests is available at no protocol
  cost (`docs/design/2026-08-18-transport-design.md` §1 R6).
- `docs/design/2026-08-18-transport-design.md` §2.9: "Nobody has shipped query
  privacy." libp2p's DHT discloses the target key to every node it touches;
  Nostr filters are plaintext pubkey lists; ActivityPub publishes the follow
  graph by design; Willow's Private Interest Overlap detection is the only
  protocol-level attempt and is an unfunded Proposal that disclaims its own
  strength.
- `docs/design/2026-08-18-transport-design.md` §4.3: the three subscription
  modes the sketch describes have different leaks — polling `/tip` with
  `If-None-Match` is unlinkable per request, while an event stream over many
  authors hands one party the whole set in one durable, correlated act.
- `docs/design/2026-08-18-transport-design.md` §2.5: an uncomfortable
  observation worth keeping — when mirroring a whole network is cheap,
  "fetch everything and filter locally" is the only mitigation on the page that
  actually works.

## Proposed Solutions

The design document lists two directions and picks neither.

### Option 1: Turn it into something checkable

Replace "SHOULD consider this" with concrete requirements on a conforming
transport: no persistent client identifier, no authentication by default,
supersets over minimal fetches where the cost is bounded, and a recommendation
to partition requests across sources.

- **Pros**: an implementation can be judged against it; the requirements are
  cheap and each one removes a specific linkability; it pairs with the
  multi-source rule of `todos/070`, which partitions requests anyway.
- **Cons**: these are properties of a *profile*, and the sentence sits in the
  processing model, which is transport-agnostic; a rule that says "no
  authentication by default" is awkward for a private server behind an
  authenticating proxy, which is a legitimate deployment.

### Option 2: Downgrade it to informative text

Keep the observation, drop the keyword: say plainly that a server you ask is a
server that knows, that the leak is real, that no cheap mechanism removes it,
and that the two structural mitigations (the requested set is a superset; the
set need not go to one party) blunt it without closing it.

- **Pros**: honest; stops the specification implying a guarantee nobody in the
  field has shipped; leaves profiles free to do better.
- **Cons**: the only sentence directed at transport stops asking for anything.

### Option 3: Split it

Informative text in `spec/05-processing-model.md` stating the leak and its
ceiling, and checkable requirements in `spec/07-transport.md` binding the
profile that defines the requests.

- **Pros**: each statement lands in the document that can carry it.
- **Cons**: two places to keep consistent.

## Recommended Action

None yet — deliberately. Filed open for the `spec/07-transport.md` drafting
phase. Whichever option is taken, the design document's conclusion should
survive into the text: any v1 profile claiming to solve subscription privacy
would be lying, so the deliverable is a stated leak, cheap mitigations, and a
written-down list of what is not solved.

## Technical Details

- **Affected Files**: `spec/05-processing-model.md` (Security Considerations),
  `spec/07-transport.md` (unwritten)
- **Related Components**: subscriptions, chain management, any transport
  profile's request shape
- **Database Changes**: No

## Acceptance Criteria

- [ ] The subscription-privacy sentence either states a checkable requirement or
      drops its RFC 2119 keyword
- [ ] The text says what is *not* solved, in as many words
- [ ] The superset and partitioning facts are recorded, since both are free and
      neither is obvious

## Work Log

### 2026-08-18 - Filed From the Transport Design Document

**By:** Claude

Carried over as-is from
[`docs/design/2026-08-18-transport-design.md`](../docs/design/2026-08-18-transport-design.md)
§5, Q6. No decision taken.

## Notes

Source: `docs/design/2026-08-18-transport-design.md` §5, Q6 (and §1 R6, §2.9,
§4.3).
