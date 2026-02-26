---
status: done
priority: p3
issue_id: "029"
tags: [documentation, readme]
dependencies: ["001", "015", "022", "026", "027", "028"]
---

# Update README After Spec Changes

## Problem Statement

README reflects the current spec. After applying all spec changes (key rotation redesign, block type field, reference model, equivalence update, meta-bond removal, subscription rework), the README needs a review pass to stay in sync.

## Proposed Solution

Review and update README after all spec changes land. Single pass at the end.

## Technical Details

- **Affected Files**: `README.md`

## Acceptance Criteria

- [ ] README matches updated spec on all points
- [ ] Key rotation described as block type, not meta-bond
- [ ] 5 meta-bonds, not 6
- [ ] Subscriptions correctly described
- [ ] Block reference model updated
- [ ] Equivalence described for atoms, bonds, and molecules

## Work Log

### 2026-02-24 - Approved for Work
**By:** User review
- Do this last, after all spec changes

## Notes

Source: User review on 2026-02-24
