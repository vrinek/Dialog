---
status: complete
priority: p3
issue_id: "060"
tags: [processing-model, validation, interoperability, conformance-vectors]
dependencies: []
---

# The Scan Limit Has No Default, So Validity Is Local Policy

## Problem Statement

`spec/05-processing-model.md`, "Scan limit", is the only place in the
specification where a *number an implementation picks* decides whether a block
is valid:

> Implementations MAY set a user-configurable limit on the number of foreign
> blocks scanned during recursive resolution. This limit SHOULD have a safe
> default. If the limit is reached before all digests resolve, the block MUST be
> treated as invalid (unresolvable references).

The first sentence makes the limit optional, the second requires a default
without saying what one is, and the third makes reaching it a rejection. Three
implementations can therefore disagree about the same block and all three be
conformant: one has no limit and accepts it, one defaults to 64 and rejects it,
one defaults to 4096 and accepts it. `vectors/blocks.json` pins no case that
comes near any limit — the deepest resolution in the file scans one block — so
the divergence is invisible to the interop contract.

This is not the same kind of local policy as fork handling. Fork handling is
scoped to what a node *does* with a condition every node detects identically;
the scan limit changes the answer to "is this block valid", which every other
rule in `spec/02-block-format.md` fixes exactly.

## Findings

- `spec/05-processing-model.md`, "Scan limit": MAY set, SHOULD default, MUST
  reject on reaching it. No number anywhere in the document.
- `spec/05-processing-model.md`, "Security Considerations", "Recursive
  resolution depth": repeats "SHOULD set a safe default and allow user
  configuration", again without a number. The attack it names — deeply nested
  ref chains — is real, so removing the bound is not an option.
- `spec/02-block-format.md`, "Validation" rule 4, defines reachability with no
  bound at all, and refers to spec/05 for the procedure. A reader who
  implements rule 4 literally has no limit; a reader who implements spec/05 has
  one whose value they invent.
- `vectors/blocks.json`: the `chain` section's deepest resolution is
  `bob_foreign_reference`, which scans exactly one foreign block. Nothing in
  the file exercises transitive resolution through a referenced block's own
  `refs`, and nothing approaches any plausible limit.
- The TypeScript implementation picked `DEFAULT_SCAN_LIMIT = 1024`, counted over
  foreign blocks actually fetched from the store during one block's validation,
  and made it configurable per validation. The number is defensible and
  arbitrary; nothing in `spec/` or `vectors/` constrains it.

## Proposed Solutions

### Option 1: Fix the default in the specification (Recommended)

Name a number in "Scan limit" — "the default limit SHOULD be N foreign blocks
per block validated" — say what it counts (blocks fetched during one block's
validation, not digests resolved and not recursion depth), and add a
`blocks.json` case that resolves an entity two or three hops out, so that the
transitive half of the procedure is pinned even though the bound is not
reached.

- **Pros**: two implementations then agree on every block a real author
  publishes; the counting unit stops being guesswork; the security bound stays.
- **Cons**: any fixed number is a policy choice the specification has so far
  avoided making; a node with unusual storage may want a much lower one, which
  the SHOULD still permits.
- **Effort**: Small (spec), Small (vectors), Small (implementations)
- **Risk**: Low — additive, and no committed byte changes.

### Option 2: Say plainly that the limit is local policy and validity is relative to it

Keep the number unspecified, but state the consequence where rule 4 is defined:
a block whose resolution exceeds a node's limit is invalid *for that node*, in
the same sense that a block whose ancestry has not arrived is unvalidated for
that node.

- **Pros**: honest about what the current text already implies, and the
  "validity is relative to what a node holds" framing already exists in
  spec/02, "Validation".
- **Cons**: leaves an interop hazard the vectors cannot test; an author has no
  way to know how deep a refs graph they may publish.
- **Risk**: Medium

### Option 3: Leave it

- **Pros**: nothing to do.
- **Cons**: the one number in the protocol that decides validity stays
  unwritten, and the first implementation to pick a small one silently drops
  other implementations' blocks.
- **Risk**: Medium

## Recommended Action

Option 1, with the counting unit spelled out. The number matters less than the
agreement on what is being counted: "foreign blocks scanned" can be read as
blocks fetched, blocks visited including repeats, or recursion depth, and those
three limits reject different sets of blocks at the same value.

## Technical Details

- **Affected Files**: `spec/05-processing-model.md` ("Scan limit", "Security
  Considerations"), `spec/02-block-format.md` (rule 4's cross-reference),
  `vectors/blocks.json` (a transitive-resolution case), both implementations
- **Related Components**: L1 validation, foreign chain loading
- **Database Changes**: No

## Acceptance Criteria

- [x] `spec/05-processing-model.md` names a default and says what it counts
- [x] `vectors/blocks.json` pins at least one transitive resolution
- [x] Both implementations use the specified default and allow configuration

## Work Log

### 2026-08-18 - Filed While Implementing ts/src/block.ts

**By:** Claude

Found building the second implementation from `spec/` and `vectors/` only. Rule
4's resolution is implemented exactly as spec/05 describes it — same block,
then `prev` ancestors, then `refs` transitively, each stage entered only when
the one before left a digest unresolved — and the bound on the last stage is the
one value in the whole implementation that had to be invented rather than read.
The tests exercise it with a hand-built four-hop refs graph, which the reference
implementation may well reject or accept differently.

### 2026-08-18 - Ratified and Applied

**By:** Claude

**Decision (project lead):** Option 1, and the counting unit is the normative
half. The default is a SHOULD of 256; what a unit *is* is fixed exactly.

**Changes:**

- `spec/05-processing-model.md`, "Scan limit", rewritten. One unit is **one
  distinct foreign block scanned** — a block reached through the refs graph
  that the node fetches and reads for the definitions its operations carry.
  Four consequences are spelled out because each of them is a place two
  implementations would otherwise diverge: the unit is a block, not a digest
  resolved and not a level of recursion; a block the graph names twice costs
  one unit, and the count restarts for the next block validated; ancestors
  reached through `prev` are not foreign; and neither a `refs` entry the node
  does not hold nor a block fetched only to read its type or author for rules 6
  and 10 costs anything until resolution scans it. Reaching the limit is still
  a rejection under rule 4. The limit SHOULD be user-configurable and SHOULD
  default to **256**, with an informative paragraph on why a shared default is
  worth fixing at all: the same block gets the same verdict from every
  default-configured node, and what a lower limit accepts is a subset.
- `spec/05-processing-model.md`, "Security Considerations": the recursion-depth
  bullet names the default and the dedup, which is what stops a graph naming
  one block repeatedly from inflating the traversal.
- `spec/02-block-format.md`, rule 4: a paragraph pointing at the bound, so a
  reader who implements rule 4 literally no longer ends up with no limit.
- `go/block/validate.go`: `DefaultScanLimit` was already 256, but the counting
  was not the unit the specification now fixes — a `refs` entry was counted the
  moment the rules 6 and 10 pass fetched it, whether or not resolution ever
  read its operations. `fetch` no longer counts; `extendRefs` counts a block as
  it folds its operations in, behind the `visited` set that already made it
  once per block. Three new subtests in `TestScanLimitCountingUnit` pin the
  unit: a refs entry resolution never needs scans nothing, a block the graph
  names twice costs one unit (valid at a limit of 3, rejected at 2), and an
  ancestor costs none.
- `ts/src/block.ts`: `DEFAULT_SCAN_LIMIT` 1024 → 256. Its counting already
  matched the definition — a digest is enqueued at most once, the rules 6 and
  10 pass does not count, ancestors do not count — so the change is the number,
  the documentation, and three tests mirroring the Go ones.
- `vectors/blocks.json`: the `scan_limit_exceeded` case of the new
  `invalid_in_chain` section (todo 061) resolves three foreign blocks deep, is
  valid under the default limit, and MUST be rejected at the `scan_limit` of 2
  the case carries. That is the transitive resolution this todo asked for, and
  it is a case no implementation can pass by counting something else.

**Outcome:** the one number in the protocol that decides validity is written
down, and so is what it counts — which mattered more: the two implementations
had picked 1024 and 256, and were also counting two different things at those
numbers.

## Notes

Source: TypeScript implementation, phase 3 (block).
