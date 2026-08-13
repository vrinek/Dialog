---
status: complete
priority: p2
issue_id: "034"
tags: [specification-gap, data-model, encoding, content-addressing]
dependencies: []
---

# Datetime Range Endpoints Are Under-Specified

## Problem Statement

`spec/01-data-model.md` defines a scalar filler's datetime range as

```cddl
datetime-range = {
  "from" => tstr,              ; RFC 3339 datetime string
  "to" => tstr                 ; RFC 3339 datetime string
}
```

and lists RFC 3339 as a normative reference. That is the whole of it. Three
questions a conforming implementation must answer are left open:

1. **Is the format validated at all?** The CDDL type is `tstr`; "RFC 3339
   datetime string" appears only in a comment and in the prose "A datetime
   range (two RFC 3339 timestamps)". No MUST tells a decoder to reject
   `{"from": "yesterday", "to": "soon"}`, which is a well-formed molecule
   under the CDDL and therefore has a valid digest and CID.
2. **Which RFC 3339 profile?** RFC 3339 admits a numeric offset
   (`+01:00`), the `Z` designator, lowercase `t`/`z` separators, unbounded
   fractional seconds, and the leap second `23:59:60`. Nothing says which of
   these a Dialog implementation must accept, and every implementation will
   inherit the accident of its standard library. Go's `time.Parse` rejects
   the leap second and lowercase `t`; other languages differ.
3. **Is there any ordering rule?** Nothing requires `from` to precede `to`,
   so `{"from": "2026-02-20T23:59:59Z", "to": "2026-02-20T00:00:00Z"}` is a
   valid molecule that denotes nothing.

The third question is cosmetic next to what the first two do to content
addressing. Because entities are hashed over their raw bytes with no
normalization (`spec/03-encoding.md`, "Text strings and Unicode"), the same
instant has many spellings:

```
2026-02-20T00:00:00Z
2026-02-20T00:00:00.000Z
2026-02-19T23:00:00-01:00
2026-02-20T01:00:00+01:00
```

Four distinct molecules, four distinct CIDs, one instant. Two authors
recording the same fact from different timezones produce entities that never
converge — the exact failure content addressing exists to prevent, and one
the specification already went out of its way to avoid for text (which is why
it recommends NFC at capture time rather than in the protocol).

## Findings

- `spec/01-data-model.md:112-115`: the `datetime-range` CDDL, with RFC 3339
  named only in comments.
- `spec/01-data-model.md:139`: "A datetime range (two RFC 3339 timestamps)".
- `spec/01-data-model.md:145`: the worked example uses `Z` and second
  precision — `2026-02-20T00:00:00Z` to `2026-02-20T23:59:59Z` — which is
  suggestive of a profile but states none.
- `spec/01-data-model.md` References, Normative: RFC 3339 is listed, so the
  intent to constrain the format clearly exists; only the MUST is missing.
- No other section (02, 03, 05) mentions datetime validation.
- `go/entity` (this implementation) validates both endpoints as RFC 3339 on
  construction and on decode, accepts every offset form, imposes no ordering,
  and inherits Go's rejection of the leap second. Each of those three choices
  is a guess.

## Proposed Solutions

### Option 1: Constrained profile, validated (Recommended)

- Normative: both endpoints MUST be RFC 3339 `date-time` strings.
  Implementations MUST reject a datetime range whose endpoints are not.
- Pin one spelling per instant, in the same spirit as the tag 4
  canonicalization of issue #32: the offset MUST be `Z` (UTC), the separator
  MUST be uppercase `T`, fractional seconds MUST NOT be used (or MUST be
  omitted when zero), and the leap second MUST NOT be used. Authoring tools
  convert to UTC at capture time, exactly as they normalize to NFC.
- State that `from` MUST NOT be later than `to`.
- **Pros**: one instant, one encoding, one CID; validation rules are testable
  and identical in every language; matches the decision already taken for
  decimal fractions and for text normalization at capture time.
- **Cons**: loses the author's local offset, which some applications treat as
  information; a strict profile means some RFC 3339 strings are invalid
  Dialog, which must be documented prominently.
- **Effort**: Small (spec), Small (implementations)
- **Risk**: Low

### Option 2: Validate the syntax, canonicalize nothing

- Both endpoints MUST be RFC 3339 `date-time` strings; any offset, any
  precision. No ordering rule.
- **Pros**: minimal change; keeps the author's offset.
- **Cons**: leaves the convergence failure in place — the same instant still
  has unboundedly many CIDs — and leaves the leap second and lowercase `t`
  to each implementation's standard library, so two implementations will
  disagree about whether a given molecule is valid.
- **Effort**: Trivial (spec)
- **Risk**: Medium

### Option 3: Treat the endpoints as opaque text

- State explicitly that the strings are not validated and RFC 3339 is a
  recommendation for authors.
- **Pros**: honest about what the CDDL says today; no decoder work.
- **Cons**: `{"from": "yesterday"}` becomes a conforming Dialog entity;
  the normative RFC 3339 reference should then be moved to Informative.
- **Effort**: Trivial
- **Risk**: Medium (pushes the problem to every application)

## Recommended Action

Option 1. Its cost is one paragraph of spec text and a handful of lines in
each implementation; its benefit is that "Thursday, Feb 20, 2026" — the
example the data model itself gives — has one CID rather than a family of
them.

## Technical Details

- **Affected Files**: `spec/01-data-model.md` (§ Scalars, `datetime-range`
  CDDL and the surrounding prose), possibly `spec/03-encoding.md` (a
  canonicalization subsection beside "Decimal fractions"),
  `go/entity/filler.go` (`ValidateRFC3339`, `NewDatetimeRange`)
- **Related Components**: Scalar fillers, molecule content addressing,
  conformance vectors
- **Database Changes**: No

## Acceptance Criteria

- [x] `spec/01-data-model.md` states normatively whether datetime endpoints
      are validated, and against what
- [x] The permitted RFC 3339 profile is pinned (offset form, separator case,
      fractional seconds, leap second) or explicitly left open
- [x] The ordering of `from` and `to` is stated or explicitly left free
- [x] `go/entity` matches the resolved rule and its doc comments cite it
- [x] Conformance vectors include an accepted and a rejected datetime range
      (the accept/reject tables in `go/entity/filler_test.go` cover every rule
      in both directions; the `vectors/` files themselves land in phase 5 and
      are generated from these values)

## Resources

- [RFC 3339](https://datatracker.ietf.org/doc/html/rfc3339) — `date-time`
  grammar, §5.6, and the leap-second discussion in §5.7
- `todos/032-complete-p2-tag-4-vs-no-tags-rule.md` — the precedent for
  canonicalizing a value that would otherwise have several encodings
- `spec/03-encoding.md` § "Text strings and Unicode" — the parallel decision
  to normalize at capture time rather than in the protocol

## Work Log

### 2026-08-13 - Filed During Implementation
**By:** Claude

Surfaced while implementing `go/entity`. The decoder had to decide what to do
with a `tstr` whose only stated constraint is a CDDL comment. It validates
RFC 3339 syntax — the reading the normative reference supports — accepts any
offset, and imposes no ordering, but all three decisions are guesses and are
recorded here rather than being presented as the specification's intent.

### 2026-08-13 - Ratified and Implemented
**By:** Claude

**Decision (project lead):** Option 1. Both endpoints of a datetime range MUST
be RFC 3339 timestamps in one canonical profile, chosen so that each instant
has exactly one encoding — the requirement content addressing imposes:

- UTC only, expressed with `Z`; numeric offsets are forbidden, including
  `+00:00` and `-00:00`
- uppercase `T` and `Z`
- exactly second precision, no fractional-second part
- the full form `YYYY-MM-DDTHH:MM:SSZ`, 20 characters
- the seconds value is `00`-`59`; the leap second `60` is forbidden
- `from` MUST NOT be later than `to`, compared **as strings** — in this
  profile lexicographic order is chronological order, and the specification
  says so rather than leaving implementations to parse dates for the check
- encoders MUST NOT emit a violation and decoders MUST reject one

Times recorded in another zone or at another precision are converted before
the entity is created. The original zone is not preserved; that loss is
intentional and is recorded in the spec as an informative note, with the
remedy of asserting the zone as a separate molecule where it matters.

**Changes:**

- `spec/01-data-model.md` § Scalars: new "Datetime ranges" subsection stating
  the six rules, the ordering rule with its string-comparison justification,
  the encoder/decoder MUSTs, and the informative conversion note. The CDDL
  gains a `timestamp` rule — `tstr` with a `.regexp` control pinning the
  lexical form — used by both fields of `datetime-range`; calendar validity
  stays in the prose, since a regexp cannot express it.
- `spec/01-data-model.md` References: the normative RFC 3339 entry now says
  Dialog permits only the restricted profile.
- `go/entity/filler.go`: `ValidateRFC3339` is replaced by `ValidateTimestamp`,
  which checks the lexical form by hand — length, fixed punctuation, uppercase
  `T`/`Z`, digits, seconds `00`-`59` — before `time.Parse` with the
  `TimestampLayout` reference form checks calendar validity. Checking the form
  first is what makes the rejection identical in every language rather than an
  accident of the standard library. `Scalar.validate` now also rejects a range
  whose `from` sorts after its `to`.
- `go/entity/filler_test.go`: `TestValidateTimestamp` is a table with one
  accept and one reject case per rule (both offset signs and both zero
  offsets, fractional seconds, lowercase `t` and `z`, the leap second,
  impossible dates and times, malformed shapes); `TestDatetimeRangeOrdering`
  covers ascending, equal and three reversed pairs; the decoder rejection
  table gains the same violations on the wire.

## Notes

Source: Go reference implementation, phase 3 (entity).

The calendar question the profile does not settle — which leap-year rule
applies to years before the Gregorian reform — is filed separately as
todos/036.
