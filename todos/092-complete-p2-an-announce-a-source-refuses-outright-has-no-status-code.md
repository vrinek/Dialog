---
status: complete
priority: p2
issue_id: "092"
tags: [transport, specification-gap, http-binding]
dependencies: []
---

# An Announce a Source Refuses Outright Has No Status Code

## Problem Statement

`spec/07-transport.md`, `announce`: "A source MAY refuse an announce entirely,
for reasons that are its own policy: quota, rate, acquaintance, disk."

The status-code table has a row for two of those four and for neither of the
other two:

| Reason | Code |
|--------|------|
| rate | 429, with `Retry-After` |
| disk (body too large) | 413 |
| disk (no space at all) | — |
| quota | — |
| acquaintance | — |

So a server that will not take an announce from a stranger, or from an
announcer over its quota, has no code the profile names. 403 is the obvious HTTP
answer and appears nowhere in this document; 503 is listed as "temporarily
unable", which is true of a full disk and false of a policy; 202 means the
opposite ("taken for later processing"). A server could return 200 with a
receipt rejecting every block, which is a lie — the blocks were not judged, the
request was.

The gap matters because a client's next move differs per answer, exactly as the
document argues for the two 404 problem types: retry later (429, 503), never
retry here (a policy refusal), retry with fewer blocks (413).

## Findings

- The document is otherwise careful to give a client a branch for every outcome:
  it invented two problem-type URNs so that a client could tell "not held" from
  "not offered" behind one 404. A policy refusal is the same shape of question
  and has neither a code nor a type.
- Server rule 5 forbids requiring authentication **for the five read
  operations** and says nothing about `announce`, which is consistent with a
  server that authenticates announcers — and a server that authenticates
  announcers needs 401 and 403, neither of which the profile lists.
- 404 with `urn:dialog:problem:operation-not-offered` is already the answer for
  a read-only mirror. That covers "this server never takes announces" but not
  "this server takes announces, not yours", and a client MUST NOT read the
  operation-not-offered type as anything but "asking again will not change the
  answer" — which is wrong for a quota that resets.
- The second implementation (`ts/src/transport.ts`) answers 403 with
  `about:blank` for a policy refusal, which is defensible and unpinned; the
  refusal hook exists precisely so the case is exercised
  (`ts/test/transport.client.test.ts`, "a source may refuse one outright").

## Proposed Solutions

### Option 1: Add 401, 403 and 507 rows scoped to `announce`

Add to the status-code table: 401 "the announce endpoint requires
authentication this deployment defines", 403 "this source refuses this announce
by policy — quota, acquaintance", 507 or 503 "no room". State that these are
defined for `announce` only, and that server rule 5's prohibition is unchanged
for the read operations.

- **Pros**: a client gets one branch per outcome; matches ordinary HTTP; nothing
  changes for a read-only mirror.
- **Cons**: 401 drags in a `WWW-Authenticate` scheme the profile deliberately
  does not define, and the document's whole posture is that authentication is
  out of scope.

### Option 2: One code — 403 — and no more

Add a single row: 403 "the source refuses this announce by its own policy; the
reason is not a protocol matter". Leave authentication out entirely, as the
document already does.

- **Pros**: minimal; keeps authentication out of scope while giving policy a
  code; a client learns "not this source, or not now" and can try another.
- **Cons**: does not distinguish a quota that resets from a refusal that never
  will — but neither does 404, and the profile is comfortable with that
  elsewhere.

### Option 3: A third problem type, `urn:dialog:problem:announce-refused`

- **Pros**: exact; the client branches on the type as it already does for 404.
- **Cons**: a third URN for a case the status code alone answers well enough;
  the document's own rule is that "every other error in this profile uses
  `about:blank`: the status code already says the whole of it".

## Recommended Action

Option 2. One row in the status-code table, and a sentence in `announce` saying
that a refusal by policy is 403 and that the receipt is absent — a refused
announce has no dispositions, because nothing was judged.

## Technical Details

- **Affected Files**: `spec/07-transport.md` ("Status codes"; `announce`)
- **Related Components**: server rule 5; "Resource limits"
- **Database Changes**: No

## Acceptance Criteria

- [x] The specification names a status code for an announce refused by policy
- [x] It says whether such a response carries a receipt

## Work Log

### 2026-08-19 - Filed While Writing the Second Implementation

**By:** Claude

Found implementing the announce policy hook: the profile explicitly permits the
refusal and names no way to express it.

### 2026-08-19 - Ratified and Applied

**By:** Claude

**Option 3**, not the recommended Option 2: 403 *and* a third problem type.

The recommendation was one status row and no URN, on the ground that the
document's own rule is that every other error uses `about:blank` because the
status code says the whole of it. That is exactly what is not true here. A 403
with a blank type leaves "this source refuses this announce" and "this source
refuses you" indistinguishable, and the two problem types this profile already
defines exist for precisely that reason — a client's next move differs. A
refusal by policy is the same shape of question, so it gets the same shape of
answer.

- `spec/07-transport.md`, "Status codes": a 403 row, scoped to `announce` and to
  no other operation, and a third problem type
  `urn:dialog:problem:announce-refused`. The table's preamble now says three
  types rather than two, and the paragraph after it asks a server to send the
  applicable type on a 403 as on a 404.
- `announce`: **a refusal by policy is 403 and carries no receipt.** Nothing was
  judged, so there are no dispositions to report, and a source answering 200
  with every block `rejected` would be reporting a verdict it never reached. The
  paragraph also separates it from the 404 of a server that does not implement
  `announce` at all: that one is a fact about the server which asking again will
  not change; this one is a fact about the request, and another source may take
  the blocks, and this one may take them later.
- Authentication stays out, as Option 1 would not have allowed. Server rule 5's
  prohibition covers the five read operations and is untouched; 401 is not
  defined, because the profile defines no scheme to challenge with.
- `go/transport`: `ErrAnnounceRefused` runs in both directions. An `Announcer`
  returns it, or an error wrapping it, to refuse a request rather than to judge a
  block; the server answers 403 with the typed problem and no receipt;
  `StatusError.Unwrap` maps a 403 back to it, so `errors.Is` spells the same fact
  on either side. Every other error from an `Announcer` stays 503 — unable rather
  than unwilling. `TestAnnounceRefusedByPolicy` covers the status, the type, the
  absent receipt, the untouched store, and the 503 that must not collapse into it.
- `ts/src/transport.ts` was answering 403 with `about:blank`; it now emits
  `PROBLEM_ANNOUNCE_REFUSED` on the 403 and keeps its refusal hook able to answer
  429 or 503 for rate and temporary grounds, which the profile already had rows
  for and which are not this type. Its tests assert the absent receipt on the
  server side and, on the client side, that no verdict is read from the refusal,
  that the store is untouched, and that the same blocks are then accepted by
  another source; a bare typeless 403 covers the server that sends none of the
  three types, which a client MUST still work against.

**Vectors: no byte moved.**

## Notes

Source: the second implementation of `spec/07-transport.md` (`ts/`), written
clean-room against the specification alone.
