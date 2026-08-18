---
status: complete
priority: p2
issue_id: "085"
tags: [transport, specification-gap, http-binding]
dependencies: []
---

# Dialog-Tip Is Required Where There Is No Tip

## Problem Statement

`spec/07-transport.md`, "HTTP binding", says:

> A response to `tip` or `range` MUST carry a `Dialog-Tip` header whose value is
> the CID text form of the tip the server holds for that author at the moment of
> the response.

A `range` for an author the server holds no block from is a 200 with an empty
sequence — "range" says so explicitly: "If it holds none, the response is an
empty sequence". There is no tip to name. The MUST cannot be satisfied, and the
specification offers no spelling for "there is none": the header's value is
defined as a CID text form, and the profile forbids alias spellings of
positions elsewhere (`after=null` is a 400) precisely so that no field has two
readings.

The same hole appears one step in: a `range` that returns blocks from a store
with a hole before the tip (see `todos/086`) may have a tip the server cannot
serve, and a `range` for a *known* author at a position past everything held
returns an empty sequence too.

## Findings

- `go/transport`'s server omits the header when it holds no block for the
  author, and its client treats an absent header as "the source claimed no tip"
  (`RangeResult.Tip == nil`, `AtTip()` false). That is a decision the
  specification does not authorise, taken because the alternative — inventing a
  value — would be worse.
- Omission is also the only choice that keeps the header's grammar single. An
  empty header value, the string `null`, or a zero CID would each be a second
  spelling of a position, which is what "HTTP binding" refuses for `after` and
  `prev`.
- A client cannot distinguish "the source holds nothing for this author" from
  "the source is not implementing the header" by the header alone. It can by
  asking `tip`, which answers 404 for the first case — so the information is
  reachable, at one more request.
- 304 responses to `tip`: this implementation sends the header (and the ETag)
  on a 304 as well, which RFC 9110 permits. The specification does not say
  whether it must, and a client that read the header only on 200 would still
  work, since a 304 means the value it already holds is current.

## Proposed Solutions

### Option 1: The header is omitted when the source holds no tip

State it in "HTTP binding": the header is present on every response that has a
tip to report, and absent otherwise; a client MUST treat its absence as "this
source claims no tip for this author" and MUST NOT treat it as an error.

- **Pros**: one spelling of a position stays one spelling; it is what an
  implementation does anyway.
- **Cons**: turns a MUST into a conditional MUST, which needs care in wording.

### Option 2: Make an empty range for an unheld author a 404

Then the empty-sequence case that has no tip disappears for an unknown author —
but not for a known one asked at a position past the tip, which still returns an
empty 200 and does have a tip to report. This narrows the problem without
closing it, and it contradicts "range"'s own text.

- **Pros**: 404 is already "I do not have it".
- **Cons**: `range` explicitly defines the empty sequence as the answer; and
  the empty-range-at-the-tip case is the normal steady state.

### Option 3: Define a "no tip" value

- **Pros**: the header is then unconditionally present.
- **Cons**: mints exactly the alias the profile refuses everywhere else.

## Recommended Action

Option 1, with the 304 question answered in the same paragraph: the header
SHOULD accompany a 304, and a client MUST NOT require it there.

## Technical Details

- **Affected Files**: `spec/07-transport.md` ("HTTP binding", the `Dialog-Tip`
  paragraph)
- **Related Components**: `range`, `tip`, the client's continuation decision
- **Database Changes**: No

## Acceptance Criteria

- [x] The specification says what a response with no tip to report carries
- [x] The specification says whether a 304 carries the header
- [x] `go/transport` matches whatever it says

## Work Log

### 2026-08-19 - Filed While Implementing spec/07

**By:** Claude

Found writing the range handler: the header's MUST has no satisfiable form for
an author the server holds nothing from.

### 2026-08-19 - Ratified and Applied

**By:** Claude

**Option 1**, as recommended, with the 304 answered in the same passage.

- `spec/07-transport.md`, "HTTP binding": the MUST is now scoped — a **200**
  response to `tip` or `range` MUST carry `Dialog-Tip`, and the header is
  defined for those two operations and no other (`block`, `blocks` and
  `siblings` answer about blocks rather than about where a chain ends, so a
  client MUST NOT expect it there).
- A new **Where the server holds no tip** passage says what happens in the state
  that had no spelling: `tip` answers 404 — the author is unknown to this
  source, in the same sense as every other 404 — and `range` answers 200 with an
  empty sequence and **omits** the header. Omission rather than an empty value,
  the literal `null` or a zero CID, because each of those would be a second
  spelling of a position, which is what `after=null` is refused for. A client
  MUST read an absent header as "this source claims no tip for this author" and
  MUST NOT treat it as an error; a client that must tell that from a server not
  implementing the header asks `tip`.
- The 304 question: a 304 to `tip` SHOULD carry the header alongside the
  `ETag`, and a client MUST NOT require it there.
- The status table's 200 and 404 rows now name the case.
- `go/transport` already did all of this and now cites it: `HeaderTip`'s
  doc comment (`transport.go`), `handleRange`'s omission (`server.go`), and
  `tipHeader`'s absent-is-not-an-error reading (`client.go`).
  `server_test.go`, `TestNoTipToReport` pins the three: an empty range with no
  header, a 404 from `tip` for the same author, and the header on a 304.

**Vectors: no byte moved.** The transport profile encodes nothing the vectors
cover.

## Notes

Source: the first implementation of `spec/07-transport.md`.
