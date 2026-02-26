---
status: done
priority: p3
issue_id: "016"
tags: [block-format, specification-gap]
dependencies: []
---

# Timestamp Monotonicity Not Required

## Problem Statement

No rule requires timestamps to increase within an author's chain. Non-monotonic timestamps make time-based conflict resolution unreliable.

## Findings

- `spec/02-block-format.md`: `ts` field defined but no ordering requirement
- "Latest-wins" conflict resolution (mentioned in `05-processing-model.md:116`) relies on meaningful timestamps
- Clock drift is real — MUST would be too strict

## Proposed Solutions

### Option 1: Add SHOULD monotonicity (Recommended)
- "The `ts` field SHOULD be greater than or equal to the `ts` of the previous block in the same chain"
- Implementations SHOULD warn on non-monotonic timestamps
- **Effort**: Small
- **Risk**: Low

## Recommended Action

One sentence addition to `02-block-format.md`.

## Technical Details

- **Affected Files**: `spec/02-block-format.md` (timestamp field section)

## Acceptance Criteria

- [ ] SHOULD monotonicity rule added for timestamps within a chain

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
- Status changed from pending → ready

## Notes

Source: Triage session on 2026-02-24
