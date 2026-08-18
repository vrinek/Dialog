---
status: complete
priority: p3
issue_id: "082"
tags: [block-format, processing-model, validation, specification-gap]
dependencies: ["078"]
---

# May Resolution Read a Block That Is Not Valid?

## Problem Statement

Validation rule 4 says an operation's digests must be defined in the same block,
in an ancestor of the author's own chain, or in a block listed in `refs` or
reached transitively through one. It says nothing about the *validity* of the
block the definition is read from.

Since `todos/078`, a node routinely holds blocks that are neither valid nor
invalid: *stored but unvalidated*, kept while their ancestry or their own
references are still missing. If such a block is named in another block's
`refs`, resolution reads its operations and the referencing block can be
declared valid on the strength of definitions carried by a block whose own
validity nobody has established — and which may yet turn out invalid.

Both implementations do exactly that. It may well be the right answer: an entity
is determined by its content, and the digest resolves to the same entity whoever
published it. But it is nowhere written, and it interacts with a rule that *is*
written — a stored but unvalidated block "MUST NOT be made available for L2
processing".

## Findings

- `spec/02-block-format.md`, rule 4: the three branches name blocks, never valid
  blocks. Rule 3, by contrast, is explicit — `prev` "MUST reference a block the
  node holds **and has accepted as valid**".
- `spec/05-processing-model.md`, "Block reception": a stored but unvalidated
  block MUST NOT reach L2 and MUST NOT be another block's rule 3 predecessor.
  Neither prohibition mentions rule 4 resolution, which is an L1 activity.
- `ts/src/block.ts`, `Resolver.scanNext` and `loadAncestors`: both call
  `source.get(...)` and index the block's operations without consulting
  `StoredBlock.valid`, which the store does maintain.
- `go/block`: `MemStore` records no verdict at all, so resolution reads whatever
  the store holds.
- The question has a real shape: A is held unvalidated because its own `prev` has
  not arrived; B names A in `refs` and resolves a bond through it; B is valid and
  its operations reach L2. If A's predecessor arrives and A turns out invalid,
  B's validity rests on a block nobody accepted — while B itself is untouched,
  since nothing about B changed and `todos/080` forbids re-opening its verdict.

## Proposed Solutions

### Option 1: Say that resolution reads blocks, not verdicts

State in rule 4 that a definition may be read from any block the node holds and
can read, whatever that block's own validity, because a digest names content and
the content is the same either way.

- **Pros**: matches both implementations; keeps demand-driven resolution cheap;
  an author cannot make another author's block invalid by publishing a bad block
  of their own.
- **Cons**: a valid block can then depend on content nobody has validated, which
  reads oddly beside rule 3's explicit "accepted as valid".

### Option 2: Require a resolvable definition to come from a valid block

Make rule 4 read only from blocks the node has accepted, so that an unvalidated
`refs` target leaves the verdict undecided exactly as an absent one does.

- **Pros**: one standard for what a validity decision may rest on; the undecided
  verdict already exists and would simply cover one more case.
- **Cons**: a valid block's verdict would then depend on the *order* blocks
  arrive in unless every unvalidated block is re-checked, and a chain with a
  missing early block would block resolution for every author that references
  it.

### Option 3: Leave it implementation-scoped, and say so

- **Pros**: honest about a case with no security consequence anyone has shown.
- **Cons**: two implementations could then answer differently for the same
  store, which is the thing the validation rules exist to prevent.

## Recommended Action

Option 1, unless the project lead sees a way for the content of an invalid
block to be load-bearing. The entity is its content; validity is a property of
the block that carried it, and the two need not travel together.

## Technical Details

- **Affected Files**: `spec/02-block-format.md` (rule 4),
  `spec/05-processing-model.md` ("Block reception", "Resolution procedure")
- **Related Components**: L1 validation, demand-driven resolution, stored but
  unvalidated blocks
- **Database Changes**: No

## Acceptance Criteria

- [x] The specification says whether reference resolution may read a block the
      node has not accepted as valid (it may, and MUST NOT be required to check
      the block's validity — spec/05, "Resolution reads blocks, not verdicts")
- [x] Whatever it says, `ts/`'s `StoredBlock.valid` and `go/block`'s
      verdict-free `Source` agree with it (both already did; the doc comments
      now say so, and `go/block`'s new `ValidatingStore` hands over unvalidated
      blocks deliberately)

## Work Log

### 2026-08-19 - Filed While Applying 079 and 080

**By:** Claude

Noticed while tracing which blocks `ts/src/block.ts`'s resolver will read: it
consults the store's `valid` flag nowhere, and the specification never asks it
to. `todos/078` made held-but-unvalidated blocks common enough for the question
to matter.

### 2026-08-19 - Ratified and Applied

**By:** Claude

**Decision (project lead):** Option 1. Resolution MAY read entity definitions
from any held block that passes the block's self-contained checks — canonical
bytes, structure, signature: the checks a store performs on admission —
regardless of that block's chain validity (rules 3 and 9) or undecided status.
Content addressing makes a definition self-certifying: an operation defines the
digest D only if the entity's canonical bytes hash to D, which the reader
verifies or recomputes itself, and the source block's chain standing cannot
change what the bytes hash to. Requiring a valid source block would force the
transitive full validation of every foreign chain and destroy the demand-driven
cost model.

**Changes:**

- `spec/05-processing-model.md`, "Resolution procedure": the new rule
  ("Resolution reads blocks, not verdicts"), its rationale, and its boundaries —
  L2 accumulation unchanged, rules 6 and 10 unchanged on the source block, rule
  3 unchanged, and a verdict still moving in one direction if the source block
  later falls.
- `spec/02-block-format.md`, rule 4: the branches name blocks and not verdicts,
  with the contrast to rule 3 and a cross-reference.
- `go/block/validate.go`: behaviour already matched. The `resolver` doc comment
  states why it is sound *here* — a `*Block` exists only through `Decode`,
  `Sign` or `Assemble`, all of which enforce canonical bytes, structure and the
  signature, so every block a `Source` can hand out is structurally sound — and
  `define` documents that it indexes under the digest `op.Creates()` computes
  from the entity's own canonical bytes (`Atom.Digest` / `Bond.Digest` /
  `Molecule.Digest`, SHA-256 over the entity's dCBOR). Nothing duplicates that
  work.
- `go/block/validate_test.go`: `TestDefinitionFromAnUndecidedBlockResolves` — a
  bond read from a block whose predecessor never arrives; the referencing block
  is valid and the source block stays undecided.
- `ts/src/block.ts`: the `Resolver` ignoring `StoredBlock.valid` is *correct*
  and is now documented as the rule, with the same two reasons —
  `BlockStore.add` runs the self-contained checks before storing anything, and
  `indexBlock` keys entities under `entityDigest` of the entity it
  reconstructs. `StoredBlock.valid` gained the list of who reads it (rule 3, L2)
  and who does not (resolution).
- `ts/test/block.test.ts`: the same scenario, ending with the source block still
  `valid: false` and the referencing block `valid: true`.

**Vectors: no byte moved.**

## Notes

Source: applying `todos/079` and `todos/080`. `todos/081`'s `ValidatingStore`
was built to this decision: it records verdicts and still hands undecided blocks
to resolution.
