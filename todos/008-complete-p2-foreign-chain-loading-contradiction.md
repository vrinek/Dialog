---
status: complete
priority: p2
issue_id: "008"
tags: [specification-consistency, processing-model, foreign-chains]
dependencies: ["022"]
---

# Foreign Chain Loading Contradiction

## Problem Statement

`05-processing-model.md:70` says foreign chains MUST be loaded to genesis. Line 136 says implementations SHOULD set limits on loading depth/size. These are contradictory — an implementation cannot comply with both if a foreign chain exceeds the limit.

## Findings

- `spec/05-processing-model.md:70`: "the foreign chain's history up to and including the referenced block MUST be loaded into L2"
- `spec/05-processing-model.md:136`: "Implementations SHOULD set reasonable limits on foreign chain loading depth or size"
- A MUST that can be overridden by a SHOULD is effectively a SHOULD
- Malicious authors can reference enormous foreign chains as a DoS vector

## Proposed Solutions

### Option 1: Add explicit exception to MUST (Approved)
- Keep MUST at line 70 but add: "except that implementations MAY reject foreign references that exceed a configured depth or size limit"
- When a foreign chain is rejected, the referencing block MUST be treated as if the foreign reference is unresolvable
- Document the trade-off: rejecting large chains protects nodes but may cause valid blocks to be rejected
- Remove the contradictory SHOULD from Security Considerations (or reword as a note about the MAY exception)
- **Pros**: Clear normative hierarchy, explicit exception, trade-off documented
- **Cons**: None
- **Effort**: Small
- **Risk**: Low

## Recommended Action

Superseded by Issue #22 (block reference model redesign). The full redesign eliminates chain-to-genesis loading entirely, replacing it with explicit refs to CID-providing blocks and demand-driven recursive resolution.

## Technical Details

- **Affected Files**: `spec/05-processing-model.md` (lines 70 and 136)
- **Related Components**: Foreign chain loading, L2 accumulation, block validation
- **Database Changes**: No

## Acceptance Criteria

- [ ] MUST rule has explicit MAY exception for size/depth limits
- [ ] Behavior when foreign chain is rejected is defined
- [ ] Security Considerations reworded to reference the exception, not contradict the rule
- [ ] No MUST/SHOULD contradiction remains

## Resources

- Original finding: Multi-agent review (SpecFlow Analyzer, Architecture Strategist)
- Related: Issue #9 (reachability definition inconsistency)

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
**Actions:**
- Issue approved during triage with custom modification
- User chose "exception" approach over downgrading MUST to SHOULD
- Status: ready

**Learnings:**
- Pattern: MUST + SHOULD in same doc = contradiction. Use MUST + MAY exception.

## Notes

Source: Triage session on 2026-02-24
