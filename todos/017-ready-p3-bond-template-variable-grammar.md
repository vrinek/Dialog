---
status: done
priority: p3
issue_id: "017"
tags: [data-model, specification-gap, parsing]
dependencies: []
---

# Bond Template Variable Parsing Has No Formal Grammar

## Problem Statement

Bond templates use `_VariableName_` for filler slots but no formal grammar defines valid variable names, escaping, or how to distinguish variables from literal underscores.

## Findings

- `spec/01-data-model.md:54-55`: Variables shown by example only (`_A_`, `_B_`)
- No regex or grammar for variable names
- Ambiguous cases: `_A_B_`, `type_of`, `_a_` (lowercase?)
- Templates are content-addressed — parsing ambiguity = different CIDs

## Proposed Solutions

### Option 1: Simple uppercase grammar (Recommended)
- Variables: `_[A-Z]+_` (underscore + one or more uppercase ASCII letters + underscore)
- All other underscores are literal text
- **Pros**: Simple, unambiguous, matches existing examples
- **Cons**: Limits variable names to uppercase letters only
- **Effort**: Small
- **Risk**: Low

## Recommended Action

Add grammar rule to `01-data-model.md`.

## Technical Details

- **Affected Files**: `spec/01-data-model.md` (bond template section)

## Acceptance Criteria

- [ ] Formal grammar for template variables defined
- [ ] Ambiguous cases resolved by the grammar
- [ ] Examples consistent with grammar

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
- Status changed from pending → ready

## Notes

Source: Triage session on 2026-02-24
