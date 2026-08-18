---
status: pending
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

- [ ] The specification says whether reference resolution may read a block the
      node has not accepted as valid
- [ ] Whatever it says, `ts/`'s `StoredBlock.valid` and `go/block`'s
      verdict-free `Source` agree with it

## Work Log

### 2026-08-19 - Filed While Applying 079 and 080

**By:** Claude

Noticed while tracing which blocks `ts/src/block.ts`'s resolver will read: it
consults the store's `valid` flag nowhere, and the specification never asks it
to. `todos/078` made held-but-unvalidated blocks common enough for the question
to matter.

## Notes

Source: applying `todos/079` and `todos/080`.
