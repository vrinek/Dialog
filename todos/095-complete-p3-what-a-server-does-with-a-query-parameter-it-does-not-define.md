---
status: complete
priority: p3
issue_id: "095"
tags: [transport, specification-gap, http-binding]
dependencies: [089]
---

# What a Server Does With a Query Parameter It Does Not Define

## Problem Statement

`spec/07-transport.md`, "HTTP binding", settled the repeated-parameter question
(todo 089): "**A query parameter given more than once is malformed** and MUST be
rejected with 400. `after` twice is two positions and this profile does not say
which would win; `prev` and `limit` are refused on the same ground."

The rule is stated over the parameters the operations define. Two neighbouring
cases are not covered, and they interact:

1. **A parameter the operation does not define.** `GET
   .../chains/{author}/blocks?after=…&prev=…`, or `?limit=2&page=3`, or a
   tracking parameter an intermediary appended. Ignore it, or 400?
2. **A parameter this server does not implement.** The long-poll bullet is
   explicit in the other direction: "a server that does not implement it MUST
   ignore the parameter and answer immediately, which degrades to polling." So
   at least one undefined-to-this-server parameter MUST be ignored.

Read together, the two rules do not compose. If unknown parameters are 400, a
server that does not implement long polling must special-case `wait` to ignore
it — which is a rule about a parameter it does not implement, stated nowhere
except in the long-poll bullet. If unknown parameters are ignored, then
`?prev=…` on a range endpoint is silently ignored, and a client that sent it
believing it named a position gets the genesis position's range instead, with no
signal at all. That is the same class of error the `after=null` prohibition
exists to prevent.

And the repeated-parameter rule inherits the ambiguity: is `?wait=5&wait=6`
malformed on a server that ignores `wait` entirely?

## Findings

- HTTP's own convention is to ignore unrecognized query parameters, and it is
  what makes URLs survive intermediaries, analytics wrappers and copy-paste. The
  profile has departed from HTTP convention deliberately elsewhere (the
  canonical spellings), so the departure would not be out of character — but
  here the cost lands on requests that are otherwise perfectly well-formed.
- The `wait` bullet's MUST-ignore is load-bearing for the graceful degradation
  of the subscription mapping, so any answer must keep it.
- The dangerous case is narrow and nameable: a parameter this profile defines
  *somewhere*, sent to an operation that does not take it — `prev` on `range`,
  `after` on `siblings`. Those are the ones a client sends by mistake and reads
  the answer wrongly.
- The second implementation (`ts/src/transport.ts`) ignores every parameter the
  matched operation does not define, including repeats of one, and applies the
  repeated-parameter 400 only to the parameters the operation does define. It is
  the reading that keeps the `wait` rule true without a special case.

## Proposed Solutions

### Option 1: Ignore what the operation does not define, and 400 a cross-operation parameter

Add: a server MUST ignore a query parameter the operation does not define,
except that `after` on `siblings` and `prev` on `range` — parameters this
profile defines for the *other* operation — MUST be rejected with 400, since a
client that sends one has named a position the server would otherwise silently
disregard.

- **Pros**: keeps `wait`'s degradation; catches the one confusable mistake;
  tolerant of everything harmless.
- **Cons**: a two-part rule where implementers would like one.

### Option 2: Ignore everything the operation does not define

- **Pros**: one sentence, ordinary HTTP, `wait` needs no exception.
- **Cons**: `?prev=bafyrei…` on a range silently answers from the genesis
  position.

### Option 3: 400 on anything not defined for the operation, with `wait` named as the exception

- **Pros**: strictest, consistent with the binding's habit.
- **Cons**: the exception is unavoidable and awkward, and any future optional
  parameter needs the same exemption; breaks under intermediaries that append
  parameters.

## Recommended Action

Option 1. Two clauses, and it is the only one of the three that both preserves
the long-poll degradation and refuses the mistake a client can actually make.

## Technical Details

- **Affected Files**: `spec/07-transport.md` ("HTTP binding", the bullets under
  the method-and-path table)
- **Related Components**: `range`; `siblings`; "Subscription mapping"
- **Database Changes**: No

## Acceptance Criteria

- [x] The specification says what a server does with a parameter the operation
      does not define
- [x] The rule composes with the long-poll MUST-ignore

## Work Log

### 2026-08-19 - Filed While Writing the Second Implementation

**By:** Claude

Found writing the query parser, immediately after implementing todo 089's
repeated-parameter rule: the rule needed a scope, and the profile gives it none.

### 2026-08-19 - Ratified and Applied

**By:** Claude

**Option 2**, not the recommended Option 1: ignore everything the operation does
not define, with no cross-operation exception.

Option 1 would have caught the one confusable mistake — `prev` on a range,
`after` on a sibling set — at the price of a two-part rule whose second part has
to be re-litigated every time this profile adds a parameter, and which puts the
long poll's MUST-ignore back where it started, as an exception written in one
bullet and nowhere else. Forward compatibility is the property worth buying
here: a future `wait`, or a future anything, must not turn today's servers into
400s. The mistake Option 1 caught is real and is now written down as a cost
rather than prevented.

- `spec/07-transport.md`, "HTTP binding": the repeated-parameter 400 is scoped to
  **the parameters this profile defines for the operation being invoked**, and a
  parameter the operation does not define MUST be ignored, once or many times.
  The bullet names what that covers — a tracking parameter an intermediary
  appended, a parameter of a later version, a parameter of another operation —
  says that `wait=5&wait=6` on a server without long polling is ignored twice
  rather than refused, and states the cost plainly: `prev` sent to a range is
  answered from the genesis position with no signal that it went nowhere. No
  block is misidentified by that, because the client verifies everything it
  receives. The 400 row of the status table is scoped to match.
- Both implementations already behaved this way, by only ever asking the query
  for the names the matched operation defines, and neither said so. `go/transport`
  states it on the `Server` type and proves it in
  `TestUnknownQueryParametersAreIgnored`: a tracking parameter, a percent-encoded
  value of an undefined parameter, `wait` once and twice, `prev` on a range once
  and twice — all 200 — beside `limit` twice, which is still 400.
- `ts/src/transport.ts` states it on `oneParameter` and splits its test in two,
  the second covering `wait=5&wait=6` at `tip`, a repeated tracking parameter,
  and the sharp edge from both sides: `limit` twice at `siblings` and `prev`
  twice at `range` are answered from the genesis position rather than refused.

**Vectors: no byte moved.**

## Notes

Source: the second implementation of `spec/07-transport.md` (`ts/`), written
clean-room against the specification alone.
