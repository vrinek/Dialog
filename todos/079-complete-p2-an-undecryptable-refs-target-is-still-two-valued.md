---
status: complete
priority: p2
issue_id: "079"
tags: [processing-model, block-format, validation, cryptography, specification-gap]
dependencies: ["078"]
---

# An Undecryptable `refs` Target Is Still Two-Valued

## Problem Statement

`todos/078` settled that a `refs` target a node cannot *obtain* leaves rule 4
undecided: the block is stored but unvalidated, never invalid, because the
absence of a block is not evidence about validity. A `refs` target a node holds
but cannot *read* is the same shape and gets the opposite answer.

`spec/05-processing-model.md`, "Undecryptable reference handling", says: "If a
node can decrypt block H but cannot decrypt a block listed in H's `refs`, this
is a validation error. The node MUST surface this error to the application
layer. The node MUST NOT silently accept the block with partial validation."
That was written when rule 4 had two outcomes. It does not say whether the
error is a rejection or an inability to decide, and both reference
implementations now read it as a rejection — the digest simply fails to
resolve, exactly as it would if nothing defined it.

The two situations differ only in *what* is missing: a block, or a key. A
content key can arrive later (`spec/04-cryptography.md`, "Key management", wraps
one per recipient), so a node that rejects the block permanently has let a key
it does not yet hold decide that another author's block is invalid — the
lying-source shape 078 removed from the block-fetch path, left standing on the
key path.

## Findings

- `spec/05-processing-model.md`, "Undecryptable reference handling": three
  sentences, none of which names a verdict. "A validation error" is the whole
  of it.
- `spec/02-block-format.md`, validation rule 4 (as amended by 078): the third
  outcome is written in terms of a block "not held and cannot be obtained". A
  held-but-unreadable block is neither held-and-scanned nor absent, so the rule
  as written does not cover it.
- `go/block/validate.go`, `extendRefs`: a private block with no decrypter
  contributes no definitions and produces a warning; the digest then fails to
  resolve through the definitive branch of `resolve`, so the block is invalid.
- `ts/src/block.ts`, `Resolver.indexBlock`: `if (!isPlaintextBlock(block))
  return;` — same outcome, reached the same way.
- The adjacent rule already distinguishes the cases: rule 6 forbids a *public*
  block from naming a private one precisely so that a node without a key is
  never asked to validate what it cannot read. The gap is therefore only about a
  **private** block whose `refs` name another private block the node cannot
  open, which is a real configuration: two authors sharing one chain's key and
  not the other's.

## Proposed Solutions

### Option 1: An undecryptable target is the same undecided verdict

Treat a `refs` target the node cannot decrypt as a block it cannot read, and
therefore as rule 4's third outcome: stored but unvalidated, revalidated if the
key arrives.

- **Pros**: one rule for "resolution could not read what it needed", whatever
  stopped it; removes the last path by which something outside the block decides
  the block is wrong; matches 078's reasoning exactly.
- **Cons**: contradicts the current "this is a validation error" sentence, which
  would have to be rewritten rather than clarified; and a node that will never
  hold the key holds the block forever, which is Option 1 of 078's cost again.

### Option 2: Keep it a rejection, and say why it is different

State that an undecryptable target is a definitive rejection because the author
of the referencing block chose a dependency this node was not made a recipient
of — the reference is unusable to this node by the author's own act, not by a
source's omission.

- **Pros**: keeps the existing MUST intact; the distinction is defensible, since
  who can read a private block is decided by the author and not by the network.
- **Cons**: a key can be wrapped for a reader after the fact, so "not a
  recipient" is not permanent; and it leaves two adjacent rules answering the
  same question ("resolution could not read the block") differently.

### Option 3: Neither — surface it without a verdict

Define the "validation error" the current text demands as an application-level
surfacing that is explicitly not a validity decision, and say the block's rule 4
verdict is whatever the *other* branches produce.

- **Pros**: honours the existing sentence literally; the surfacing is what the
  node's user actually needs, since the fix is to obtain a key.
- **Cons**: leaves the verdict question open, which is the thing 078 was filed
  about; a block whose only path to a digest is an unreadable block still needs
  an answer.

## Recommended Action

Option 1, unless the project lead reads the author's recipient choice as
load-bearing. It is the same argument 078 accepted, applied to the one remaining
way resolution can fail to read a block it holds.

## Technical Details

- **Affected Files**: `spec/05-processing-model.md` ("Undecryptable reference
  handling", "Resolution procedure"), `spec/02-block-format.md` (rule 4's third
  outcome), `go/block/validate.go`, `ts/src/block.ts`
- **Related Components**: L1 validation, private chains, key management
- **Database Changes**: No

## Acceptance Criteria

- [x] The specification says whether a `refs` target the node holds and cannot
      decrypt makes the referencing block invalid or undecided
- [x] Rule 4's third outcome either covers the case or explicitly excludes it
- [x] Both implementations agree with the text, and a test pins the verdict

## Work Log

### 2026-08-18 - Filed While Applying 078

**By:** Claude

Found while making rule 4 three-valued: the new outcome is defined for a block
that is not held, and the one other way resolution can fail to read a block it
needs — holding it as ciphertext without a key — was left on the old two-valued
reading in both the specification and both implementations.

### 2026-08-19 - Ratified and Applied

**By:** Claude

**Decision (project lead):** Option 1. A `refs` target the node holds and cannot
decrypt — or one reached transitively, or a private ancestor of the author's own
chain — leaves the referencing block in the same *stored but unvalidated* state
`todos/078` defined: undecidable **by this node**, revalidatable if the key
arrives, MUST NOT reach L2, and the node MUST still surface the situation to the
application. The existing MUST stays, reworded from "a validation error" to the
undecided state; the MUST NOT silently accept stays as it was. The reason is the
one 078 accepted: validity is objective, the same block is decidable for a key
holder, and a capability this node lacks is not evidence about the block — the
more so as a content key can be wrapped for a further recipient at any time.
Rule 6 is untouched and is explicitly not this case: a *public* block naming a
private one is a defect every node reads in the clear, and it is invalid on
discovery; 079 is about a *private* block's `refs`, which may name any type.

**Changes:**

- `spec/05-processing-model.md`: "Undecryptable reference handling" rewritten —
  the verdict, the rationale, the surfacing MUST, and the contrast with rule 6.
  The *stored but unvalidated* definition gained **readability** as its third
  cause, "Block reception" step 4 and the resolution procedure's outcome 3 name
  it.
- `spec/02-block-format.md`: the "Validation" preamble and rule 4's third
  outcome cover a block held without its key; the MUST NOT records "neither the
  absence of a block nor the absence of a key is evidence about validity".
- `go/block/validate.go`: the resolver records the first block it needed, held
  and could not read. A digest unresolved with that gap open is a rule 4 error
  wrapping the new `ErrUndecryptable`, which `IsUnvalidated` now answers true to
  beside `ErrNotFound`; the error names the block a key is wanted for, which is
  the surfacing. `go/block/payload.go`'s `ValidatePayload` doc says the same for
  the key holder's pass.
- `go/block/validate_test.go`: new `TestUndecryptableReferenceIsUndecided` — a
  private `refs` target held without its key is undecided, stable across
  validations and valid once a `Decrypter` supplies the payload; a private
  ancestor of a public block's own chain is the same; an unreadable block no
  digest needs decides nothing.
- `ts/src/block.ts`: the resolver exposes `unreadableBlock`, rule 4 throws
  `unvalidated` for it, and `BlockError` carries `undecryptable` — separate from
  `awaiting`, because no arrival settles it and the store has nothing to file
  the held block under. TypeScript has no decryption, so any private block its
  resolution reaches is one of these.
- `ts/test/block.test.ts`: three tests — a public block over a private ancestor,
  one reached through the refs graph, and an unreadable block no digest needs.

**Vectors: no byte moved.** No case in `vectors/blocks.json` depends on an
unreadable block: the rule 6 case rejects on the referencing block's own type
and the rule 4 cases hold every block resolution reads. `genvectors` reproduces
`vectors/` byte for byte.

## Notes

Source: applying `todos/078`.
