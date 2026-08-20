---
status: complete
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

- [x] Formal grammar for template variables defined
- [x] Ambiguous cases resolved by the grammar
- [x] Examples consistent with grammar

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
- Status changed from pending → ready

### 2026-08-20 - Audit against current spec
**By:** Claude audit pass
**Actions:**
- Resolved. `spec/01-data-model.md:55-72` gives the grammar (`variable = "_" 1*UCALPHA "_"`), the leftmost-longest matching rule, and worked cases for exactly the ambiguities raised here: `_AB_`, `_A_B_`, `_A__B_`, `type_of`, `_a_`.
- All other underscores are literal text, stated normatively. The examples elsewhere in the spec conform.

## Notes

Source: Triage session on 2026-02-24
