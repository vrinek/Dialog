---
status: done
priority: p3
issue_id: "018"
tags: [data-model, specification-gap, validation]
dependencies: ["017"]
---

# Variable Count = Filler Count Not Stated Normatively

## Problem Statement

The number of fillers in a molecule must match the number of variables in the bond template, but this is never stated as a normative rule.

## Findings

- `spec/01-data-model.md`: Molecule definition implies matching counts but no MUST rule
- Mismatched counts would produce valid-looking but semantically broken molecules
- Depends on Issue #17 (variable grammar) to define how variables are counted

## Proposed Solutions

### Option 1: Add MUST rule (Recommended)
- "The number of fillers in a molecule MUST equal the number of variables in the referenced bond template"
- **Effort**: Small
- **Risk**: Low

## Recommended Action

One sentence addition to `01-data-model.md` molecule section.

## Technical Details

- **Affected Files**: `spec/01-data-model.md` (molecule definition)

## Acceptance Criteria

- [ ] Normative MUST rule for filler count = variable count
- [ ] Validation section references this rule

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
- Status changed from pending → ready

## Notes

Source: Triage session on 2026-02-24
