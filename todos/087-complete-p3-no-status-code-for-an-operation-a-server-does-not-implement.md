---
status: complete
priority: p3
issue_id: "087"
tags: [transport, specification-gap, http-binding]
dependencies: []
---

# No Status Code for an Operation a Server Does Not Implement

## Problem Statement

`announce` is OPTIONAL: "a read-only mirror is conforming". The status-code
table of `spec/07-transport.md` has no row for what such a mirror answers when
somebody posts to `/dialog/v1/announce`.

The three candidates each say something different, and the difference is
visible to a client deciding whether to try another source:

- **404** — "I do not have it", which in this profile is a fact about the
  source's store. A path is not a block, so the reading is a stretch, but it is
  the one that says "not here, ask elsewhere".
- **405** with `Allow` — the table defines 405 as "wrong method for a defined
  path", and on a read-only mirror the path is not defined at all. `Allow` would
  have to be empty, which RFC 9110 permits and nobody expects.
- **501** — the honest HTTP answer, and a status the profile's table does not
  contain. Adding it would be the first status outside the table.

## Findings

- `go/transport` answers **404** with problem details: a server built without an
  `Announcer` does not register the path, and every unrouted path under the
  prefix is a 404 whose detail says this server serves no resource there.
- The same question would arise for the two OPTIONAL subscription mappings if
  they were paths rather than parameters. They are not: long polling is a query
  parameter a server MUST ignore when it does not implement it, and the event
  stream is a path — `GET /dialog/v1/events` — with the same gap.
- A client of this profile has no discovery document to consult, by design
  ("What this profile leaves out": no `.well-known`), so the status code is the
  only way it learns that an optional operation is absent. That makes the choice
  worth pinning rather than leaving to taste.

## Proposed Solutions

### Option 1: 404, and say so

Add a sentence to "The six operations" or to the status table: a server that does
not implement an OPTIONAL operation answers 404 for its path, which a client
reads the same way it reads every other 404 — a fact about this source.

- **Pros**: no new status; consistent with the profile's reading of 404; a
  client's retry logic is already written for it.
- **Cons**: conflates "no such resource here" with "I do not hold that block",
  which a person debugging will notice.

### Option 2: 501 Not Implemented, added to the table

- **Pros**: exact; distinguishes a missing feature from a missing block.
- **Cons**: adds a status a client must handle, for one case.

### Option 3: Leave it to the server

- **Pros**: nothing to write.
- **Cons**: a client cannot branch on an answer that varies by deployment.

## Recommended Action

Option 1 if the profile wants its status table closed; Option 2 if it would
rather be precise. Either is better than the current silence. The event stream
path should be covered by the same sentence.

## Technical Details

- **Affected Files**: `spec/07-transport.md` ("The six operations", "Status
  codes", "Subscription mapping")
- **Related Components**: `announce`, the event stream, client retry
- **Database Changes**: No

## Acceptance Criteria

- [x] The specification says what a server answers for an OPTIONAL operation it
      does not implement
- [x] `go/transport`'s read-only construction matches it

## Work Log

### 2026-08-19 - Filed While Implementing spec/07

**By:** Claude

Found building the read-only server the profile calls conforming: there is no
row in the status table for the path it does not serve.

### 2026-08-19 - Ratified and Applied

**By:** Claude

**Option 1**, with the precision Option 2 wanted taken from a problem type
rather than from a new status code.

- `spec/07-transport.md`, "The six operations": a new common rule — an OPTIONAL
  operation a server does not offer answers **404**, which covers `announce` on
  a read-only mirror and the event stream, the other optional thing with a path
  of its own. A client reads it as it reads every other 404 here: a fact about
  this source. Why it has to be pinned is in the same bullet — this profile has
  no discovery document, so the status code is the only way a client learns that
  an optional operation is absent.
- "Status codes": the 404 row names the case, and the section now defines the
  profile's two problem types, both 404s — `urn:dialog:problem:not-held`
  (another source may hold it, and this one may later) and
  `urn:dialog:problem:operation-not-offered` (asking again will not change the
  answer). A server SHOULD send the applicable one and MAY send `about:blank`;
  a client MUST still branch on the status code. They are URNs because they are
  identifiers and not links: nothing is published at them, and a URL would tie a
  protocol identifier to a hostname somebody has to keep answering.
- "Subscription mapping": the event stream's OPTIONAL now says what its absence
  answers.
- `go/transport`: `problem.go` grows the three type constants and
  `writeTypedProblem`; the read-only server registers an explicit handler for
  `/announce` — and one for `/events`, which it never offers — answering 404 with
  `operation-not-offered` for every method, rather than falling through to the
  mux's generic "no resource here"; the two "not held" 404s, `tip` and `block`,
  now carry `not-held`. A path this profile never defined at all keeps
  `about:blank`.
- Tests: `server_test.go`, `TestOperationNotOffered` covers the three
  not-offered paths, the two not-held ones, and a server built with an
  `Announcer` still serving the path. `assertProblem` admits the two new types.

**Vectors: no byte moved.**

## Notes

Source: the first implementation of `spec/07-transport.md`.
