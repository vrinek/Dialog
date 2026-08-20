---
status: complete
priority: p3
issue_id: "025"
tags: [documentation, meta-bonds, references]
dependencies: []
---

# 06-meta-bonds.md Missing 03-encoding.md in References

## Problem Statement

`06-meta-bonds.md` normative references list omits `03-encoding.md`, despite meta-bond CIDs being computed per encoding rules.

## Findings

- `spec/06-meta-bonds.md:179-186`: References 01, 02, 04, 05 but not 03
- Meta-bonds are content-addressed — encoding rules are a normative dependency

## Proposed Solutions

### Option 1: Add reference (Recommended)
- Add `03-encoding.md` to normative references
- **Effort**: Small
- **Risk**: Low

## Recommended Action

One line addition.

## Technical Details

- **Affected Files**: `spec/06-meta-bonds.md` (references section)

## Acceptance Criteria

- [x] `03-encoding.md` listed in normative references

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
- Status changed from pending → ready

### 2026-08-20 - Audit against current spec
**By:** Claude audit pass
**Actions:**
- Resolved. `spec/06-meta-bonds.md`, References, Normative, now lists `03-encoding.md` -- "CBOR encoding and CID computation" -- alongside 01, 02, 04 and 05.
- The body links it where it matters too: line 104 (meta-bonds identified by the SHA-256 digest of their dCBOR-encoded template) and line 26.

## Notes

Source: Triage session on 2026-02-24
