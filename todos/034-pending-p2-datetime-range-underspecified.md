---
status: pending
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

- [ ] `spec/01-data-model.md` states normatively whether datetime endpoints
      are validated, and against what
- [ ] The permitted RFC 3339 profile is pinned (offset form, separator case,
      fractional seconds, leap second) or explicitly left open
- [ ] The ordering of `from` and `to` is stated or explicitly left free
- [ ] `go/entity` matches the resolved rule and its doc comments cite it
- [ ] Conformance vectors include an accepted and a rejected datetime range

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

## Notes

Source: Go reference implementation, phase 3 (entity).
