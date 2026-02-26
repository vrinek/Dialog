---
status: ready
priority: p3
issue_id: "023"
tags: [documentation, examples, consistency]
dependencies: ["014"]
---

# Placeholder Notation Inconsistent

## Problem Statement

Examples use inconsistent placeholder notation: `<CID of ...>`, `<hash of ...>`, `<different hash>`. Should be standardized after Issue #14 resolves CID vs digest semantics.

## Findings

- Mixed notation across `spec/01-data-model.md`, `spec/02-block-format.md`, `spec/06-meta-bonds.md`
- No convention established for placeholder format in examples

## Proposed Solutions

### Option 1: Standardize notation (Recommended)
- After Issue #14 decides CID vs digest for each field, use consistent format
- E.g., `<CID: "description">` for CIDs, `<digest: "description">` for digests
- Find-and-replace across all spec docs
- **Effort**: Small
- **Risk**: Low

## Recommended Action

Do after Issue #14 is resolved.

## Technical Details

- **Affected Files**: All spec docs with examples

## Acceptance Criteria

- [ ] Single consistent placeholder format across all examples
- [ ] Notation distinguishes CIDs from digests per Issue #14 decisions

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
- Status changed from pending → ready

## Notes

Source: Triage session on 2026-02-24
