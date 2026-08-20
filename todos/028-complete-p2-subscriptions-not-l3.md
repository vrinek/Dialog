---
status: done
priority: p2
issue_id: "028"
tags: [architecture, processing-model, subscriptions]
dependencies: []
---

# Subscriptions Are Not an L3 Concern

## Problem Statement

The spec frames subscriptions as an L3 filtering mechanism, but subscriptions are not an L3 concern. Need to clarify where subscriptions actually belong in the layer model.

## Findings

- `spec/05-processing-model.md:84-103`: L3 defined as "L2 filtered by subscriptions"
- `spec/00-overview.md:60`: "L3 is filtered by the user's author subscriptions"
- `README.md:33`: "L2 filtered by the user's author subscriptions"
- User review: subscriptions should not be characterized as an L3 concern

## Proposed Solution

Subscriptions are a cross-cutting concern that operates at two layers:

- **L1**: Determines which chains to pull and store (you don't fetch chains you're not subscribed to)
- **L3**: Determines which authors' data to accept into application truth (filter L2 by subscribed authors)

L2 is unaffected — it accumulates everything pulled at L1, unfiltered.

Rework layer descriptions to reflect this. Stop framing subscriptions as purely L3 filtering. L3's role beyond subscriptions is meta-molecule application and conflict surfacing.

## Technical Details

- **Affected Files**: `spec/05-processing-model.md`, `spec/00-overview.md`, `README.md`

## Acceptance Criteria

- [x] Subscriptions described as cross-cutting (L1 + L3)
- [x] L1 description mentions subscriptions drive chain fetching
- [x] L3 description clarified: subscription filtering + meta-molecule application + conflict surfacing
- [x] L2 description unchanged (raw accumulation, no filtering)
- [x] Architecture diagram updated if needed (no change needed; diagram shows data flow only)

## Work Log

### 2026-02-24 - Approved for Work
**By:** User review
- User identified subscriptions are misplaced as an L3 concern
- Clarified: subscriptions are L1 (what to pull) + L3 (what to accept)

### 2026-02-26 - Implemented
**By:** Claude
- Reworked L3 section title from "Subscription filtering and truth distillation" to "Truth distillation" in `spec/05-processing-model.md`
- Updated abstract in `spec/05-processing-model.md` to say "L3 truth distillation" instead of "L3 subscription filtering"
- Updated L3 description in `spec/00-overview.md` to present subscriptions as one of three L3 activities, not the defining one
- Updated scope section in `spec/00-overview.md` to say "author filtering" instead of "subscription-based filtering"
- Cross-cutting note already present at line 145 of `spec/05-processing-model.md`; L1 chain management already mentions subscriptions
- README.md intentionally not updated (deferred to TODO 029)

## Notes

Source: User review on 2026-02-24
