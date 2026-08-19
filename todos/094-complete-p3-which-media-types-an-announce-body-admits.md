---
status: complete
priority: p3
issue_id: "094"
tags: [transport, specification-gap, http-binding]
dependencies: []
---

# Which Media Types an Announce Body Admits

## Problem Statement

`spec/07-transport.md`, "Bodies and content types", says two things about the
block-sequence media type, and they are asymmetric:

- On a **response**: "A client SHOULD send `Accept:
  application/dialog-blocks+cbor-seq` and MUST accept `application/cbor-seq` as
  an equivalent, since a plain file server offering a directory of chain files
  will send the generic type and its bytes are the same bytes. A server MUST NOT
  serve a block sequence under any other type."
- On a **request**: the status table says 415 is "the request body's media type
  is not the one the operation defines", and the body table gives `announce`'s
  body as "a block sequence", whose type is
  `application/dialog-blocks+cbor-seq`.

So a server that receives `Content-Type: application/cbor-seq` on an announce
has no stated answer. The equivalence rule is written only for clients reading
responses, but its *reason* — a chain file on disk carries the generic type —
applies at least as strongly the other way, since the profile says in "As a
file" that "a chain file offered to a server is a valid announce body". The
person offering that file has whatever type their tooling attached to it.

## Findings

- The two sentences pull opposite ways. "A server MUST NOT serve a block
  sequence under any other type" is strict about what a server *emits*, and
  strictness there is free. Being strict about what a server *accepts* costs
  interoperability with exactly the case the profile went out of its way to
  bless — a directory of `.dialog` files served by a plain file server, or
  uploaded by one.
- Nothing else in the profile depends on the distinction: the body is the same
  bytes under either type, and the sequence carries no metadata, so a server
  that accepts both cannot be confused about what it received.
- The same question applies, less urgently, to `POST /dialog/v1/blocks/fetch`:
  the body is JSON, and whether `application/json; charset=utf-8` is admitted is
  ordinary HTTP (it is, parameters and all) and needs no text.
- A related silence: whether `Accept` is enforced on `announce` at all. The
  operation's only response body is JSON, so a client sending `Accept:
  application/dialog-blocks+cbor-seq` — which is what a client that speaks this
  profile has in its default headers — would get a 406 from a server that
  enforces 406 uniformly, for a request it would otherwise have accepted. The
  second implementation does not enforce `Accept` on `announce` for that reason.
- The second implementation (`ts/src/transport.ts`) accepts both types on an
  announce body and 415s anything else.

## Proposed Solutions

### Option 1: State the equivalence in both directions

Amend the paragraph: the two types are equivalent wherever a block sequence
appears, in a request body as in a response body. A server MUST accept an
announce body under either, and MUST NOT emit one under anything but
`application/dialog-blocks+cbor-seq`.

- **Pros**: one clause; makes "a chain file offered to a server is a valid
  announce body" actually true of a file whose type came from a file server.
- **Cons**: none identified.

### Option 2: Require the specific type on requests

- **Pros**: the announcer is a Dialog client and can set a header.
- **Cons**: contradicts the file-form story; a curl-and-a-file announce fails
  for a reason that changes nothing about the bytes.

## Recommended Action

Option 1, plus one sentence saying that `Accept` is not enforced on `announce`
(or that a server MUST accept `application/json` implicitly there), so that a
client's standing `Accept` header cannot 406 a write.

## Technical Details

- **Affected Files**: `spec/07-transport.md` ("Bodies and content types")
- **Related Components**: `announce`; "As a file"
- **Database Changes**: No

## Acceptance Criteria

- [x] The specification says which media types an announce body may carry
- [x] It says whether `Accept` is enforced on `announce`

## Work Log

### 2026-08-19 - Filed While Writing the Second Implementation

**By:** Claude

Found writing the announce handler's 415 check and the 406 check beside it: both
are one line, and the profile pins neither.

### 2026-08-19 - Ratified and Applied

**By:** Claude

**Option 1**, as recommended, plus the `Accept` sentence.

- `spec/07-transport.md`, "Bodies and content types": **the equivalence holds in
  both directions.** An `announce` request body is a block sequence, and its
  `Content-Type` MUST be `application/dialog-blocks+cbor-seq` or
  `application/cbor-seq`; a server MUST reject any other type, and a missing one,
  with 415. Admitting the generic type is what makes "a chain file offered to a
  server is a valid announce body" true of a file whose type came from a file
  server, and it confuses nothing: the two types are the same bytes and a
  sequence carries no metadata to disagree about.
- **`Accept` is not evaluated on `announce`.** Its only response bodies are JSON —
  a receipt or a problem document — which the server produces whatever the
  request asked for, so there is nothing for a 406 to protect; and the standing
  header of a client that speaks this profile names the block-sequence type,
  which no announce response ever carries. 406 is defined for the five read
  operations, and the status table's 406 and 415 rows now say so.
- `go/transport` already accepted both types (`isBlockSeqType` was written for
  the response side and is symmetric) and was enforcing 406 on `announce`, which
  is now gone along with `acceptsJSON`; a comment in its place says why there is
  no such function. `TestAnnounceMediaTypes` covers both admitted types, the four
  refusals including a missing `Content-Type`, and three `Accept` headers that
  must not 406 a write.
- `ts/src/transport.ts` was already conforming on both halves; the reasoning it
  had to infer is now in the specification and cited in `DialogServer.announce`.
  Its tests grew a missing-`Content-Type` case and a new one pairing an announce
  under the block-sequence `Accept` (200) with the same header at `tip` (406), so
  that the asymmetry is asserted rather than assumed.

**Vectors: no byte moved.**

## Notes

Source: the second implementation of `spec/07-transport.md` (`ts/`), written
clean-room against the specification alone.
