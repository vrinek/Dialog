---
status: complete
priority: p2
issue_id: "027"
tags: [meta-bonds, key-rotation]
dependencies: ["001", "015"]
---

# Remove Key Rotation Meta-Bond (Deprecated)

## Problem Statement

`_A_ rotates key to _B_` is deprecated in favor of the rotation block type (decided in Issue #1 triage). The meta-bond library should have 5 standard meta-bonds, not 6.

## Findings

- `spec/06-meta-bonds.md:83-94`: Key rotation meta-bond still present
- Triage decided: key rotation is a 4th operation type in a dedicated rotation block
- Meta-bond #6 is now redundant

## Proposed Solution

Remove key rotation meta-bond from `06-meta-bonds.md`. Update all references to "six standard meta-bonds" → "five standard meta-bonds". Update abstract, examples, and any cross-references.

## Technical Details

- **Affected Files**: `spec/06-meta-bonds.md`, `spec/00-overview.md`, `README.md`

## Acceptance Criteria

- [x] Key rotation meta-bond removed from 06
- [x] "Six" → "five" everywhere
- [x] Key rotation examples removed or moved to 02-block-format
- [x] Cross-references updated

## Work Log

### 2026-02-24 - Approved for Work
**By:** User review
- Confirmed deprecation per triage decision on rotation block type

### 2026-08-20 - Audit against current spec
**By:** Claude audit pass
**Actions:**
- Resolved. `spec/06-meta-bonds.md` contains no `_A_ rotates key to _B_` meta-bond; the abstract (line 7) and the library heading (line 26) both say five, and the only rotation mentions left are cross-references to the L1 `rotate_key` operation (line 66, and "Key rotation abuse" at line 151).
- `spec/00-overview.md` and `README.md` both describe key rotation as a block type; `README.md`, "Standard meta-bond library (v1)", lists five rows.
- Key rotation itself is documented in `spec/02-block-format.md`, "Rotation block" and "rotate_key".

## Notes

Source: User review on 2026-02-24
