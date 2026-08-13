---
status: complete
priority: p3
issue_id: "045"
tags: [specification-gap, block-format, key-rotation]
dependencies: ["044", "042"]
---

# A Successor Chain May Not Begin With a Rotation Block, and the Reason Given Does Not Say Why

## Problem Statement

`spec/02-block-format.md` § "Verifiable succession" now reads:

> **The successor's genesis block MUST be a public block.** [...] A private
> block's `refs` are inside its `enc` field, so a node without the decryption
> key would be asked to act on a reference it cannot read.

The rule admits one block type; the rationale excludes one. A **rotation** block
in the genesis position falls between them: its `refs` are in the clear, so
every node can read the back-reference and the stated reason does not reach it,
yet the MUST forbids it.

The case is not hypothetical. An author who rotates key A to key B and then
learns that B is compromised before publishing anything with it must, under this
rule, publish a public block with key B before rotating B to C. That block
carries at least one operation (rule 7), so the author is required to publish
content in order to abandon a key.

## Findings

- `spec/02-block-format.md`, "Verifiable succession": the MUST and its
  rationale, both added by issue #44.
- `spec/02-block-format.md`, "Validation" rule 6: permits a public block's
  `refs` to name a rotation block; nothing there or in "Rotation block" forbids
  a rotation block from being a genesis block in general — `prev` may be null
  for any type.
- `spec/02-block-format.md`, "Validation" rule 7: a rotation block's single
  `rotate_key` operation satisfies it, so a rotation-only chain is otherwise
  well-formed.
- `go/block/chain.go`, `ValidateSuccession` and `Successors`, and
  `go/block/builder.go`, `build`: all implement the ratified rule literally —
  public only.

## Proposed Solutions

### Option 1: Keep public-only, state the reason (Recommended)

- Add a sentence saying what the restriction buys beyond readability: a
  successor chain that begins by ending publishes no ontology and leaves a node
  chaining rotations with nothing to attach the author identity to, so v1
  requires a chain to exist before it may be handed on.
- **Pros**: no behaviour change; the rule stops looking like an oversight.
- **Cons**: the immediate-re-rotation case stays awkward.
- **Effort**: Trivial (spec), none (Go)
- **Risk**: Low

### Option 2: Admit a rotation block in the genesis position

- "The genesis block of a successor chain MUST NOT be a private block."
- **Pros**: matches the stated rationale exactly; makes immediate re-rotation
  expressible; one rule instead of a type list.
- **Cons**: admits a chain of one block that is both genesis and terminal, which
  every consumer of `Chain` then has to handle; `Successors` and
  `ValidateHistory` would need to walk a rotation-only chain.
- **Effort**: Small (spec), Small (Go)
- **Risk**: Medium

### Option 3: Say nothing and let the type list stand

- **Cons**: the next reader asks this question again.
- **Risk**: Low

## Recommended Action

Option 1 unless the immediate-re-rotation case is judged to matter, in which
case Option 2. Either way the specification should say which of the two
rationales the rule rests on.

## Technical Details

- **Affected Files**: `spec/02-block-format.md` ("Verifiable succession"),
  `spec/05-processing-model.md` ("Chain succession"), `go/block/chain.go`,
  `go/block/builder.go`
- **Related Components**: key rotation, chain validation
- **Database Changes**: No

## Acceptance Criteria

- [x] The specification states why a rotation block may not begin a successor
      chain, or admits one
- [x] `go/block` matches whichever is ratified

## Work Log

### 2026-08-13 - Filed While Applying Issue #44
**By:** Claude

The ratified wording for #44 is "MUST be a public block", which the Go
implementation follows literally: `ValidateSuccession` rejects a rotation
genesis block with a message that only says the type is wrong, because there is
no better reason to give it. The private case has a reason; this one does not
yet.

### 2026-08-13 - Ratified and Applied
**By:** Claude

**Decision (project lead):** neither Option 1 nor Option 2 as written — the
question is answered a level below. **A rotation block MUST NOT be a genesis
block: its `prev` MUST NOT be null.** Rotation semantics presuppose an existing
chain to end, so a rotation block can never reach the genesis position at all,
and the public-only MUST of "Verifiable succession" needs no separate rationale
for excluding it: the only type that rule turns away in practice is the private
one, which is exactly the case its stated reason covers.

The immediate-re-rotation case is accepted as a cost and named as such: a key
that is abandoned before it is used must publish the genesis block of its own
chain before it can rotate. The alternative — a chain consisting of one block
that is both genesis and terminal — would be a special case for every consumer
of a chain, for a situation an author reaches by publishing a key they never
meant to use.

**Changes:**

- `spec/02-block-format.md`: "Rotation block" gains the normative paragraph
  ("A rotation block is never a genesis block": `prev` MUST NOT be null,
  rejected on the author side and on the wire alike) and an informative note on
  its cost; the `rotation-block` CDDL narrows `prev` to `bstr .size 32`, the
  one place in the specification where the field is not a union with null;
  "Verifiable succession" gains the connecting sentence saying that the rule
  and its reason cover the same ground.
- `spec/04-cryptography.md`: a note under `signing-input-public`, which the
  public and rotation block types share, recording that a rotation block's
  `prev` is never null.
- `go/block/block.go`: `Content.Validate` rejects a rotation block whose `Prev`
  is nil, so both the author side (`Sign`) and the wire side (`Decode`, through
  `assemble`) refuse one; the doc comments of `Content.Prev` and
  `Content.Validate` state the rule.
- `go/block/builder.go`: `build` refuses a rotation block from a Builder that
  has signed nothing, in the terms the author is working in ("no chain to
  end"); `Rotation`'s doc comment says so.
- `go/block/chain.go`: `ValidateSuccession`'s doc comment records that its type
  check turns away only the private case, a rotation block having been refused
  a step earlier by `IsGenesis`.
- `go/block/block_test.go`: the rotation rows of `TestContentValidateRejections`
  now carry a `prev`, so each tests the rule it names; new rows for a rotation
  block as a genesis block and for a rotation to its own key.
  `TestRotateKeyOnlyInRotationBlocks` opens the chain before ending it.
- `go/block/decode_test.go`: `rotationMap` carries a `prev`; new wire cases for
  a rotation block with a null `prev` and one rotating to its own key.
- `go/block/validate_test.go`: `TestRotation`'s "rotation block as successor
  genesis" is replaced by "a rotation block is never a genesis block" —
  builder, `Sign`, the successor position, and `Successors` declining to count
  a rotation block that references the rotation it cannot succeed; the happy
  path now also round-trips the rotation block through `Decode`.
- `go/privacy/privacy_test.go`: the successor-genesis subtest opens the old
  chain before rotating it.

## Notes

Source: Go reference implementation, phase 4 (privacy), applying issue #44.
