---
status: pending
priority: p3
issue_id: "036"
tags: [specification-gap, data-model, encoding, content-addressing]
dependencies: ["034"]
---

# Which Calendar a Timestamp Uses Before the Gregorian Reform Is Unstated

## Problem Statement

The canonical timestamp profile ratified in issue #34 requires that a
timestamp "denote an existing instant: month `01`-`12`, a day that exists in
that month of that year". For every date after 1582 that sentence is
unambiguous. Before it, it is not: whether a day exists in a month depends on
which calendar is being counted in, and the specification names none.

`1500-02-29T00:00:00Z` is the smallest example. Under the Julian calendar,
which was in civil use in 1500, February 1500 had 29 days and the timestamp
denotes a real instant. Under the proleptic Gregorian calendar — the Gregorian
rules extended backwards before their 1582 introduction — 1500 is not a leap
year (divisible by 100, not by 400), February had 28 days, and the timestamp
denotes nothing.

RFC 3339 does not close the question either. It states that its dates use the
Gregorian calendar and observes in § 5.8 that dates before the reform are
outside its intended scope, without forbidding them.

The consequence is the one Dialog's encoding rules exist to prevent: a
molecule whose datetime range starts at `1500-02-29T00:00:00Z` is a valid
entity with a stable CID for one implementation and undecodable garbage for
another. Historical assertions are a plausible use of a knowledge graph, so
this is not a purely theoretical corner — "the Battle of X occurred on this
day" is precisely the kind of statement Dialog is for.

A second, smaller question rides along: the `timestamp` regexp admits the year
`0000`, and `0000-02-29T00:00:00Z` is a valid date under proleptic Gregorian
(year 0 is divisible by 400) while the year 0 does not exist in the historical
era numbering at all. Whether Dialog permits a year that no calendar in use
names is unstated.

## Findings

- `spec/01-data-model.md` § "Datetime ranges", rule 6: "a day that exists in
  that month of that year" — the rule that needs a calendar to be evaluated.
- `spec/01-data-model.md` § Molecules, the `timestamp` CDDL rule: the regexp
  admits any four-digit year, `0000` to `9999`, and cannot express leap-year
  arithmetic at all, so the whole question lives in the prose.
- `go/entity/filler.go` (`ValidateTimestamp`): delegates calendar validity to
  Go's `time.Parse`, which is proleptic Gregorian throughout. It therefore
  rejects `1500-02-29T00:00:00Z` and `1900-02-29T00:00:00Z` and accepts
  `1600-02-29T00:00:00Z` and `0000-02-29T00:00:00Z`. Verified against Go 1.24.
- Standard libraries do not agree here. Most modern date types are proleptic
  Gregorian, but several date libraries implement a Julian-to-Gregorian
  switchover, and the switchover date itself differs by country (1582 in much
  of Catholic Europe, 1752 in Britain and its colonies, 1918 in Russia), so
  "the calendar in civil use" is not a single rule either.
- The rest of the profile of issue #34 is fully determined; this is the one
  component of it that a conforming implementation can still get wrong while
  following the text.

## Proposed Solutions

### Option 1: Proleptic Gregorian, stated normatively (Recommended)

- Add to § "Datetime ranges": "Dates are interpreted in the proleptic
  Gregorian calendar — the Gregorian leap-year rules applied to all years,
  including those before the calendar's introduction in 1582. `1500-02-29` is
  therefore not a valid date."
- Optionally state whether the year `0000` is permitted, and either accept it
  as proleptic Gregorian year zero or restrict the year to `0001`-`9999`.
- **Pros**: one rule, no switchover table, matches what almost every date
  library already does and what `go/entity` does today; keeps validation a
  pure function of the string.
- **Cons**: a historical date recorded from a Julian source must be converted
  before it becomes a Dialog timestamp, and the conversion is the author's
  responsibility — the same trade already accepted for time zones in issue
  #34.
- **Effort**: Trivial (spec), none (Go)
- **Risk**: Low

### Option 2: Restrict the permitted year range

- Require the year to be `1583` or later (or some other floor), so the
  question cannot arise, and represent earlier events some other way.
- **Pros**: no calendar rule needed at all.
- **Cons**: makes Dialog unable to state when anything before 1583 happened,
  which is a large hole in a general-purpose knowledge graph.
- **Effort**: Trivial (spec), Small (implementations)
- **Risk**: Medium

### Option 3: Leave it to implementations

- **Cons**: reintroduces exactly the divergence issue #34 was ratified to
  remove, in the one place where it is hardest to notice.
- **Risk**: Medium

## Recommended Action

Option 1, and decide the year `0000` question in the same sentence. The cost
is one paragraph; the benefit is that rule 6 becomes computable from the
specification rather than from whichever date library an implementation
happens to link.

## Technical Details

- **Affected Files**: `spec/01-data-model.md` (§ "Datetime ranges", possibly
  the `timestamp` CDDL comment), `go/entity/filler.go` (`ValidateTimestamp`
  doc comment), `go/entity/filler_test.go`
- **Related Components**: Scalar fillers, molecule content addressing,
  conformance vectors
- **Database Changes**: No

## Acceptance Criteria

- [ ] `spec/01-data-model.md` names the calendar in which a timestamp's date
      components are interpreted
- [ ] Whether the year `0000` is a permitted year is stated
- [ ] `go/entity` matches the resolved rule and its doc comment cites it
- [ ] Tests cover a pre-reform leap day in each direction (`1500-02-29`,
      `1600-02-29`)

## Resources

- [RFC 3339 § 5.8](https://datatracker.ietf.org/doc/html/rfc3339#section-5.8)
  — the note on dates preceding the Gregorian calendar
- `todos/034-complete-p2-datetime-range-underspecified.md` — the ratified
  profile whose rule 6 this completes
- [ISO 8601-1:2019 § 5.2.1](https://www.iso.org/standard/70907.html) — uses
  the proleptic Gregorian calendar and requires mutual agreement for years
  before 1583, which is the agreement this issue asks Dialog to make

## Work Log

### 2026-08-13 - Filed While Applying Issue #34
**By:** Claude

Surfaced while implementing the ratified timestamp profile. Every other rule
in the profile was mechanically checkable from the text; "a day that exists in
that month of that year" was the only one that required the implementation to
supply a fact the specification does not state, and Go's answer (proleptic
Gregorian) is recorded here rather than presented as the specification's
intent.

## Notes

Source: Go reference implementation, phase 3 (entity), applying issue #34.
