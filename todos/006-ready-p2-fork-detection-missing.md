---
status: done
priority: p2
issue_id: "006"
tags: [architecture, security, block-validation]
dependencies: []
---

# Fork Detection Missing

## Problem Statement

The spec has no normative rule for what happens when two blocks claim the same `prev` hash — an author forking their own chain. Without fork detection, a malicious or buggy author could publish conflicting histories, and different nodes would silently diverge.

## Findings

- `spec/02-block-format.md` validation rules (lines 120-140) cover chain integrity but not fork detection
- Append-only semantics imply linear chains, but this is never stated normatively
- No mention of "fork" anywhere in the spec
- Author chains are described as "linear sequence" in `spec/05-processing-model.md:13` but only in terminology, not as a normative constraint

## Proposed Solutions

### Option 1: Add fork detection requirement (Recommended)
- Add normative rule to `02-block-format.md` validation section
- "If a node receives a block whose `prev` matches a `prev` already used by another block from the same author, the node MUST detect this as a chain fork"
- Detection is MUST, handling strategy is implementation-scoped (reject both, accept first-seen, flag for user)
- **Pros**: Ensures all implementations detect forks, preserves implementation flexibility for handling
- **Cons**: None
- **Effort**: Small
- **Risk**: Low

### Option 2: Mandate linear chains normatively
- State "each author chain MUST be strictly linear — each block (except genesis) has exactly one successor"
- Nodes MUST reject any block that would create a fork
- **Pros**: Stronger guarantee, simpler mental model
- **Cons**: "Reject" may not always be possible if you see the fork after storing both
- **Effort**: Small
- **Risk**: Low

## Recommended Action

Option 1. Require detection, leave handling implementation-scoped. Consistent with how the spec handles conflict resolution elsewhere.

## Technical Details

- **Affected Files**: `spec/02-block-format.md` (add to validation rules section)
- **Related Components**: L1 block validation, chain management
- **Database Changes**: No

## Acceptance Criteria

- [ ] Normative fork detection rule added to `02-block-format.md`
- [ ] "Linear chain" stated as normative property, not just terminology
- [ ] Fork handling strategy explicitly noted as implementation-scoped
- [ ] Security Considerations section updated to mention fork attacks

## Resources

- Original finding: Multi-agent review (Architecture Strategist, Security Sentinel)
- Related: `spec/05-processing-model.md:13` (terminology defines "linear sequence")

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
**Actions:**
- Issue approved during triage session
- Status changed from pending → ready

**Learnings:**
- Pattern: the spec says "linear" in terminology but never enforces it normatively

## Notes

Source: Triage session on 2026-02-24
