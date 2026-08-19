---
status: complete
priority: p2
issue_id: "090"
tags: [transport, specification-gap, http-binding, encoding]
dependencies: []
---

# Is a Percent-Encoded Identifier a Second Spelling?

## Problem Statement

`spec/07-transport.md`, "HTTP binding", is exact about the spelling of every
identifier a URL carries: an author key is the 56-character text form beginning
`b5ua`, a CID is the 59-character form beginning `bafyrei`, both are
case-sensitive, and "a server MUST reject any other spelling of either with 400
rather than normalizing it — the two forms are canonical in both directions, and
a server that accepted a variant would be minting aliases."

It does not say whether **percent-encoding** produces another spelling.
`%62afyreicwspbfzq…` and `bafyreicwspbfzq…` are the same URI by RFC 3986 —
percent-decoding an unreserved character is a normalization every URI library
performs — but they are different bytes on the wire, and after decoding they are
the same string. So:

- A server that parses its path and query with a standard URL library sees one
  spelling and cannot tell them apart. It accepts both.
- A server that reads the raw request target sees two, and can reject the
  encoded one.

The one place the profile touches this is `limit`, where it says the value must
have "no percent-encoded variant" of a sign, a decimal point or whitespace, and
names `%201` as malformed. That sentence only bites if the server is looking at
the raw, undecoded value — which implies the raw form is what the profile means
by "spelling", but it says so about one numeric parameter and never about the
identifiers, where the aliasing argument is much stronger.

## Findings

- The second implementation (`ts/src/transport.ts`) parses the query string from
  the raw URL and never percent-decodes it, and validates each path segment
  against the canonical text form directly. `%62afyrei…` therefore fails, and so
  does `%201`. The reasoning: decoding could only ever *admit* spellings the
  profile refuses, and the alphabets in play — base32 for both identifiers,
  ASCII digits for `limit` — contain nothing that needs encoding, so a
  well-behaved client never sends a percent sign at all.
- That reading has a cost. A generic HTTP client library that percent-encodes
  aggressively, or an intermediary that re-encodes a request target, turns a
  valid request into a 400 for reasons the user cannot see. This is a real
  failure mode with proxies, and it is invisible in a test suite where the
  client and the server are the same code.
- The opposite reading has a cost too, and it is the one the profile's own
  aliasing argument names: two byte strings that name the same block, one of
  which a cache keys separately, is exactly the alias the canonical text forms
  exist to prevent. `GET /dialog/v1/blocks/%62afyrei…` and
  `GET /dialog/v1/blocks/bafyrei…` are two cache entries for one immutable
  resource.
- Note that the aliasing here is weaker than the one spec/03 warns about: a
  percent-encoded identifier still decodes to exactly one identifier, so it
  cannot make two different blocks look like one, and it cannot make one block
  answer under a digest that is not its own. The damage is cache fragmentation
  and log noise, not a confusion of identity.

## Proposed Solutions

### Option 1: Percent-encoding is another spelling, and is 400

Add to "HTTP binding": the path segments and query values of this profile are
drawn from alphabets that need no percent-encoding, and a request whose target
carries any percent-encoded octet in a segment or value this profile defines
MUST be rejected with 400.

- **Pros**: one spelling, exactly as everywhere else in the binding; consistent
  with the `limit` sentence already in the text; caches key one resource once.
- **Cons**: a client behind an over-eager encoding proxy gets a 400 it cannot
  diagnose; servers built on frameworks that hand the handler a decoded path
  cannot implement the rule without reaching for the raw target.

### Option 2: Percent-decoding happens first, and the canonical form is checked after

Add: a server percent-decodes the request target per RFC 3986 before applying
the canonical-form rules, and rejects only what is non-canonical after decoding.

- **Pros**: works with every HTTP framework; robust against intermediaries.
- **Cons**: `%201` for `limit` is then *valid* — it decodes to `" 1"`, which the
  whitespace rule still refuses, so that case survives, but `%31` decodes to
  `"1"` and would have to be accepted, contradicting the spirit of the sentence
  that names `%201`; and the immutable block resource acquires infinitely many
  URL spellings.

### Option 3: Say nothing, and let it be a deployment matter

- **Pros**: no text.
- **Cons**: two conforming servers answer the same request differently, which is
  what a profile exists to prevent, and the first thing a conformance suite
  written against this section will disagree about.

## Recommended Action

Option 1, with the `limit` sentence generalized: "The path segments and query
values this profile defines are drawn from alphabets that need no
percent-encoding. A request target that percent-encodes any octet of one is a
malformed request and MUST be rejected with 400." One sentence, and it makes the
existing `%201` clause a consequence rather than a special case.

If Option 2 is chosen instead, the `%201` example in the `limit` bullet must be
removed or rewritten, because it is only malformed under Option 1's reading.

## Technical Details

- **Affected Files**: `spec/07-transport.md` ("HTTP binding", the paragraph on
  `{author}` and `{cid}`, and the `limit` bullet)
- **Related Components**: every operation with a path parameter; caching
- **Database Changes**: No

## Acceptance Criteria

- [x] The specification says whether a percent-encoded identifier is a second
      spelling
- [x] The `limit` bullet's `%201` example is consistent with that answer

## Work Log

### 2026-08-19 - Filed While Writing the Second Implementation

**By:** Claude

Found writing `ts/src/transport.ts`'s query parser: the `limit` rule forced a
decision about whether to percent-decode at all, and that decision silently
decides the identifiers too. The TypeScript implementation took Option 1 and
tests it (`ts/test/transport.server.test.ts`, "a non-canonical author key or CID
is 400 rather than normalized"); nothing in the specification requires it.

### 2026-08-19 - Ratified and Applied

**By:** Claude

**Option 1**, as recommended, with the `limit` sentence generalized.

- `spec/07-transport.md`, "HTTP binding": one spelling means one byte sequence,
  and percent-encoding is a second spelling. The path segments and query values
  this profile defines are drawn from alphabets that need no percent-encoding —
  base32 for both text forms, ASCII digits for `limit` — so a target that
  percent-encodes any octet of one is malformed and MUST be 400, and a server
  applies the canonical-form rules to the request target **as received** rather
  than to a percent-decoded copy. The cost is written down beside the rule: an
  intermediary that re-encodes a target turns a valid request into a 400 nobody
  can see, and that is accepted, because the alternative gives every immutable
  block resource an unbounded set of URLs.
- The `limit` bullet no longer carries the rule on its own. `%201` is now
  malformed twice over — by the whitespace rule and by the general one — and the
  bullet says so, which is what "consistent with that answer" asked for. The 400
  row of the status table names a percent-encoded spelling too.
- `go/transport` had the bug this todo predicts, in both places. Go's `ServeMux`
  matches on the decoded path and hands `PathValue` the decoded segment, so
  `%62afyrei…` reached the handler as a canonical CID; `r.URL.Query()`
  percent-decodes, so `limit=%31` reached it as the number 1. The server now
  checks the escaped path under its own prefix for a percent-encoded octet
  (`Server.canonicalTarget`) and parses the query string without decoding it
  (`rawQuery`), which is all it takes — a percent sign then fails the
  canonical-form check each value already had. Paths outside the prefix are left
  alone: nothing this profile defines lives there, and 404 for being nowhere is
  a better answer than 400 for being spelled oddly.
- `TestNonCanonicalSpellingsAreRejected` grew six cases: a percent-encoded author
  key on `tip` and on `range`, a percent-encoded CID, a percent-encoded `after`
  and `prev`, and `limit=%31`.
- `ts/src/transport.ts` had taken Option 1 already and needed no behaviour
  change; its comments now cite the general rule rather than the `limit`-only
  one. Its tests grew an encoded *trailing* octet beside the leading one, a
  positive control that the canonical CID still answers 200, a case per
  percent-encoded query value, and — the one that could not be inferred from a
  fetch call alone — the same check over a real socket through the Node adapter,
  which passes the raw request target through undecoded.

The two implementations reaching this rule differently is worth recording: the
TypeScript one arrived at it from first principles while writing a query parser,
and the Go one had to be corrected, because the standard library did the
normalizing for it in two places at once.

**Vectors: no byte moved.**

## Notes

Source: the second implementation of `spec/07-transport.md` (`ts/`), written
clean-room against the specification alone.
