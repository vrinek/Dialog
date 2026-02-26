---
status: complete
priority: p2
issue_id: "009"
tags: [architecture, block-validation, foreign-chains, processing-model]
dependencies: ["022"]
---

# Reachability and Foreign Chain Loading Model

## Problem Statement

Two issues combined: (1) `02-block-format.md` and `05-processing-model.md` define reachability differently (prev+refs vs prev-only), and (2) foreign chain loading requires fetching entire chains to genesis, which is wasteful and contradicts size limits. Both need a unified, demand-driven model.

## Findings

- `spec/02-block-format.md:136-137`: Reachability follows `prev` AND `refs` recursively
- `spec/05-processing-model.md:74-76`: Foreign chain loading only follows `prev` to genesis
- `spec/05-processing-model.md:70`: MUST load entire foreign chain (contradicts line 136 limits)
- These give different reachability graphs and create normative contradictions

## Proposed Solution: Parent/Uncle Model with Demand-Driven Loading

**Note: Superseded by Issue #22** (block reference model redesign). #22 goes further — refs become explicit CID providers, prev is ordering-only, private metadata encrypted. Implement #22 instead.

Replace the current reachability and foreign loading rules with:

### Block validity rule

A block "A" is valid if every CID it references was created in:
- The same block "A", OR
- A **parent block** "B" (any ancestor in the same author chain via `prev`), OR
- An **uncle block** "C" (any block in a foreign chain referenced via `refs`)

### Foreign chain loading rule

When an implementation encounters CIDs in block "A" that were not created in any parent block:
1. The implementation MUST look into the referenced foreign chains (`refs`)
2. The implementation only needs to traverse each foreign chain's blocks until all CIDs of block "A" are resolved (early termination)
3. If all CIDs resolve, the block is valid
4. If CIDs remain unresolved after scanning all referenced foreign chains, the block is invalid

### Configurable scan limit

The implementation MAY set a user-configurable limit on the number of foreign blocks it will scan for unresolved CIDs. This limit SHOULD have a safe default. If the limit is reached before all CIDs resolve, the block MUST be treated as invalid (unresolvable references).

### Why this is better

- **Demand-driven**: Only load foreign blocks you actually need
- **Early termination**: Stop as soon as CIDs resolve — no loading to genesis
- **Natural DoS protection**: Limit is on scan depth, not all-or-nothing
- **No normative contradiction**: MUST resolve CIDs + MAY limit scan depth = clean hierarchy
- **Clear terminology**: Parent (own chain) and uncle (foreign chain) — unambiguous

## Technical Details

- **Affected Files**:
  - `spec/02-block-format.md` — rewrite reachability rules, add parent/uncle terminology
  - `spec/05-processing-model.md` — rewrite foreign chain loading section (lines 69-78), update Security Considerations (line 136)
- **Related Components**: Block validation, L1→L2 processing, foreign chain management
- **Database Changes**: No

## Acceptance Criteria

- [ ] Parent/uncle terminology defined in both docs
- [ ] Block validity rule uses parent + uncle model
- [ ] Foreign chain loading is demand-driven (stop when CIDs resolve)
- [ ] Configurable scan limit with safe default specified
- [ ] Behavior when limit is hit defined (block invalid)
- [ ] No contradiction between normative rules and Security Considerations
- [ ] Reachability definition consistent across 02 and 05
- [ ] Examples updated to show demand-driven loading

## Resources

- Original finding: Multi-agent review (SpecFlow Analyzer, Architecture Strategist)
- Supersedes: Issue #8 foreign chain loading approach (update #8 to reference this)

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
**Actions:**
- Issue approved during triage with user-designed parent/uncle model
- Combines original Issues #8 and #9 into a unified solution
- Status: ready

**Learnings:**
- Demand-driven loading is much cleaner than "load everything then limit"
- Parent/uncle terminology from blockchain conventions aids clarity

## Notes

Source: Triage session on 2026-02-24
User-designed solution during triage.
