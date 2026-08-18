---
status: complete
priority: p3
issue_id: "089"
tags: [transport, specification-gap, http-binding]
dependencies: []
---

# Does `limit` Have One Spelling or Several?

## Problem Statement

`spec/07-transport.md`, "HTTP binding", fixes exactly one spelling of an author
key, exactly one spelling of a CID and exactly one spelling of a position, and
says why: a server that accepted a variant would be minting aliases. Of `limit`
it says only "`limit` is a positive decimal integer."

So `limit=01`, `limit=+1`, `limit=%201`, `limit=1.0` and `limit=1&limit=2` have
no defined treatment. Unlike the identifiers, none of these is an alias for an
identity — a limit names nothing — so accepting them mints nothing. But a server
that accepts them and one that rejects them answer the same request differently,
and the profile has been strict everywhere else in the query string.

## Findings

- `go/transport` rejects anything but the canonical decimal spelling with 400,
  including a repeated parameter, on the reasoning that one spelling is the
  profile's habit and a client has no reason to send another. Nothing in the
  specification requires this.
- A repeated parameter is the one case with a real consequence: `after` given
  twice would be two positions, and the profile does not say which wins.
  `go/transport` rejects a repeated `after`, `prev` or `limit` with 400 for the
  same reason.
- The status table already has a row for it either way: 400 is "a bad `limit`".
  What is missing is which spellings are bad.

## Proposed Solutions

### Option 1: One spelling, like everything else in the query string

"`limit` is a positive decimal integer with no leading zero, no sign and no
surrounding whitespace. A parameter given more than once is a malformed request."

- **Pros**: consistent with the rest of the binding; no ambiguity about a
  repeated `after`.
- **Cons**: a hand-written client that sends `limit=+10` gets a 400 for
  something harmless.

### Option 2: A server parses leniently and caps

- **Pros**: forgiving.
- **Cons**: two servers disagree about `limit=1.9`.

## Recommended Action

Option 1, extended to say that any of the profile's query parameters given more
than once is a 400. The sentence costs a line and closes three cases.

## Technical Details

- **Affected Files**: `spec/07-transport.md` ("HTTP binding", the bullet list
  under the method-and-path table)
- **Related Components**: `range`, `siblings`
- **Database Changes**: No

## Acceptance Criteria

- [x] The specification says which spellings of `limit` are admitted
- [x] The specification says what a repeated query parameter means

## Work Log

### 2026-08-19 - Filed While Implementing spec/07

**By:** Claude

Found writing the query parsing: the profile is exact about every identifier in
a URL and silent about the one number.

### 2026-08-19 - Ratified and Applied

**By:** Claude

**Option 1**, extended to every query parameter, as recommended.

- `spec/07-transport.md`, "HTTP binding": `limit` has exactly one spelling — one
  or more ASCII digits, the first not `0`, with no sign, no decimal point, no
  whitespace and no percent-encoded variant of any of those. `01`, `+1`, `1.0`,
  `%201` and `1e3` are named as malformed and MUST be 400. A server MAY cap the
  value it honours, MUST NOT exceed its cap, and MAY reject a value too large to
  be a plausible count of blocks.
- A second bullet: **a query parameter given more than once is malformed** and
  MUST be 400, because `after` twice is two positions and no rule says which
  wins; `prev` and `limit` go the same way. The 400 row of the status table now
  names it.
- `go/transport` already rejected exactly this set. The conformance test
  `TestNonCanonicalSpellingsAreRejected` grew the cases that were missing: the
  sign, the decimal point, the leading whitespace, exponent notation, an
  out-of-range value, an empty value, and `limit`, `after` and `prev` each given
  twice. The leading zero was already covered. `limit`'s doc comment cites the
  rule.

**Vectors: no byte moved.**

## Notes

Source: the first implementation of `spec/07-transport.md`.
