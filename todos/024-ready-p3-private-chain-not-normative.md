---
status: done
priority: p3
issue_id: "024"
tags: [specification-gap, processing-model]
dependencies: []
---

# "Each User Has at Least One Private Chain" Stated as Fact, Not Normative

## Problem Statement

`05-processing-model.md:127` states "Each user has at least one private chain" as fact. Should be optional — not all users need private chains.

## Findings

- `spec/05-processing-model.md:127`: Implicit requirement for private chains
- Some users may only need public data

## Proposed Solutions

### Option 1: Reword to MAY (Recommended)
- "A user MAY maintain one or more private chains"
- **Effort**: Small
- **Risk**: Low

## Recommended Action

One sentence reword.

## Technical Details

- **Affected Files**: `spec/05-processing-model.md` (line 127)

## Acceptance Criteria

- [ ] Private chains stated as optional (MAY), not assumed

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
- Status changed from pending → ready

## Notes

Source: Triage session on 2026-02-24
