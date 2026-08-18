---
status: pending
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

- [ ] `spec/05-processing-model.md` names a default and says what it counts
- [ ] `vectors/blocks.json` pins at least one transitive resolution
- [ ] Both implementations use the specified default and allow configuration

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

## Notes

Source: TypeScript implementation, phase 3 (block).
