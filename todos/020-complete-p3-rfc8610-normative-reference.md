---
status: complete
priority: p3
issue_id: "020"
tags: [documentation, encoding, references]
dependencies: []
---

# RFC 8610 Listed as Informative, Arguably Normative

## Problem Statement

RFC 8610 (CDDL) is listed as Informative but the spec uses CDDL for all normative schema definitions. Should be Normative.

## Findings

- `spec/03-encoding.md:149`: RFC 8610 in Informative references
- Every spec document uses CDDL schemas as normative definitions

## Proposed Solutions

### Option 1: Move to Normative (Recommended)
- Move RFC 8610 from Informative to Normative references
- **Effort**: Small
- **Risk**: Low

## Recommended Action

Move one line between reference sections.

## Technical Details

- **Affected Files**: `spec/03-encoding.md` (references section)

## Acceptance Criteria

- [x] RFC 8610 listed as Normative reference in `03-encoding.md`

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
- Status changed from pending → ready

### 2026-08-20 - Audit against current spec
**By:** Claude audit pass
**Actions:**
- Resolved. RFC 8610 (CDDL) is now listed under `### Normative` in the References section of `spec/03-encoding.md` (line 293, beneath the Normative heading at line 285).

## Notes

Source: Triage session on 2026-02-24
