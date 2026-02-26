---
status: done
priority: p1
issue_id: "003"
tags: [specification-consistency, data-model, encoding]
dependencies: []
---

# Float/Int Contradiction

## Problem Statement

`01-data-model.md:90` defines the numeric filler CDDL as `"value" => number` with comment `; integer or float`. However, `03-encoding.md:35` states "All numbers MUST be integers." These two normative statements directly contradict each other, breaking interoperability.

## Findings

- `spec/01-data-model.md:90`: CDDL uses `number` type (includes floats per RFC 8610)
- `spec/01-data-model.md:116-117`: Prose says "integer or float"
- `spec/03-encoding.md:35`: "All numbers MUST be integers"
- dCBOR profile bans floating-point — encoding doc is correct
- CDDL `number` = `int / float` per RFC 8610 §3.4

## Proposed Solutions

### Option 1: Change `number` to `int` + add CBOR tag 4 decimal fractions (Approved)
- Update CDDL: `"value" => number` → `"value" => int / #6.4([int, int])`
- Update prose: replace "integer or float" with "integer or decimal fraction (CBOR tag 4)"
- CBOR tag 4 encodes decimals as `[exponent, mantissa]` — both integers, deterministic under dCBOR
- Example: `3.14` = `#6.4([-2, 314])`
- Aligns with dCBOR (no IEEE 754 floats) while supporting decimal values
- **Pros**: Supports decimals, deterministic encoding, no float ambiguity
- **Cons**: Slightly more complex than int-only
- **Effort**: Small
- **Risk**: Low

## Recommended Action

Change `number` to `int / #6.4([int, int])` in CDDL. Update prose to explain decimal fraction encoding. Add a note that CBOR tag 4 decimal fractions are the protocol-standard way to represent non-integer numbers.

## Technical Details

- **Affected Files**: `spec/01-data-model.md` (lines 90, 116-117)
- **Related Components**: Filler type definitions, CDDL schemas
- **Database Changes**: No

## Acceptance Criteria

- [ ] CDDL uses `int / #6.4([int, int])` not `number` for numeric filler value
- [ ] Prose explains decimal fraction encoding (CBOR tag 4)
- [ ] No IEEE 754 float references remain in data model
- [ ] Consistent with `03-encoding.md:35` (integers only + tag 4)
- [ ] Example showing decimal fraction encoding (e.g., `3.14` → `[-2, 314]`)

## Resources

- Original finding: Multi-agent review (all 5 agents flagged this)
- RFC 8610 §3.4: `number` = `int / float`
- dCBOR draft: floats banned

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
**Actions:**
- Issue approved during triage session
- Status changed from pending → ready

**Learnings:**
- Quick win — two-line fix with high impact on interoperability

## Notes

Source: Triage session on 2026-02-24
