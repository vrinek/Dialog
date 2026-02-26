---
status: done
priority: p3
issue_id: "021"
tags: [documentation, overview]
dependencies: []
---

# Overview Has Duplicate Document Index Tables

## Problem Statement

`00-overview.md` has two tables listing the same spec documents. Maintenance burden — two places to update.

## Findings

- `spec/00-overview.md:17-24`: Scope section table (concern → document)
- `spec/00-overview.md:120-128`: Document index table (# → document → covers)
- Same information, slightly different format

## Proposed Solutions

### Option 1: Remove Scope table, keep Document index (Recommended)
- Replace Scope table with prose or bullet list
- Keep the more complete Document index at the bottom
- **Effort**: Small
- **Risk**: Low

## Recommended Action

Remove one table, keep the bottom index.

## Technical Details

- **Affected Files**: `spec/00-overview.md`

## Acceptance Criteria

- [ ] Only one document index table remains
- [ ] Scope section still communicates what each doc covers

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
- Status changed from pending → ready

## Notes

Source: Triage session on 2026-02-24
