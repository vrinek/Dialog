---
status: complete
priority: p3
issue_id: "080"
tags: [block-format, processing-model, validation, specification-gap]
dependencies: ["078"]
---

# Rules 6 and 10 Stayed Two-Valued

## Problem Statement

`todos/078` made validation rule 4 three-valued: resolvable, definitively
unresolvable, or not determinable — and the third outcome keeps the block out of
L2 until the missing block arrives. Rules 6 and 10 face exactly the same
situation and answer it differently.

Both are properties of a *referenced* block: rule 6 rejects a public block whose
`refs` name a private block, rule 10 rejects any block whose `refs` name a block
of its own author's chain. Both are evaluated as a referenced block is resolved,
and `spec/02-block-format.md` says a node "reports the rule as unchecked for an
entry whose block it does not hold" (settled in `todos/041`, and deliberately:
demand-driven resolution must not be obliged to fetch a block only to read its
type).

So a node can now hold a block that is *valid* while one of its rules was never
evaluated, and nothing says what that costs. The same absence that leaves rule 4
undecided — and the block out of L2 — leaves rules 6 and 10 unchecked and the
block fully accepted. If the unheld entry later turns out to be a private block,
the node accepted, promoted to L2 and served a block that rule 6 forbids.

*Unchecked* is also still undefined as a status: it appears in three places, has
no stated consequence for the block, and is a third vocabulary beside *stored
but unvalidated* and "does not count" — which is what `todos/078`'s Option 3
proposed to unify and the ratified Option 1 did not.

## Findings

- `spec/02-block-format.md`, rule 6: "reports the rule as unchecked for an entry
  whose block it does not hold". Rule 10's own-chain half says the same by
  reference.
- `spec/05-processing-model.md`, "Public/private reference rules": "a node that
  resolves a referenced block and finds it private MUST reject the referencing
  public block, and reports the rule as unchecked for an entry it does not
  hold."
- `spec/02-block-format.md`, rule 4 (as amended by 078): the same absence,
  answered with a status that has fully defined consequences — MUST NOT reach
  L2, MUST NOT be a rule 3 predecessor, MAY be revalidated, MAY be discarded.
- `go/block/validate.go` records the unheld entries as warnings on rules 6 and
  10 and validates the block; `ts/src/block.ts` returns them in the report's
  `uncheckedRefs` and does the same. Both match the specification; neither has
  anywhere to record that the block's acceptance was conditional.
- The asymmetry is not obviously wrong. Rule 4's undecided verdict means the
  node cannot show the block is *sound*; rules 6 and 10 unchecked mean it cannot
  show the block is *unsound*, and treating "I have not found a violation" as a
  reason to withhold a block would make every unfetched `refs` entry block L2.
  The question is whether that reading is intended, and it is nowhere written.

## Proposed Solutions

### Option 1: State the asymmetry and its justification

Say in rule 6 (and by reference rule 10) that an unchecked entry does not affect
the block's validity, that a node MUST re-evaluate the rule if it later holds
the block, and that the block MUST be rejected then — with the L2 consequence of
a rejection after promotion spelled out.

- **Pros**: smallest edit; keeps demand-driven resolution's whole point; names
  the one obligation that is currently implicit (re-evaluate on arrival).
- **Cons**: needs an answer for a block already in L2 when the violation is
  found, and L2 is append-only.

### Option 2: Give *unchecked* a definition, once

Define the status where it is first used, list which rules can carry it, and
state its consequence for the block — which Option 1 supplies. This is
`todos/078`'s Option 3, which that decision left on the table.

- **Pros**: removes the third vocabulary; every status in the validation model
  then has a definition.
- **Cons**: a larger edit across both documents, for a status that turns out to
  mean one thing.

### Option 3: Make rules 6 and 10 three-valued too

Treat an unheld `refs` entry as leaving the block undecided, exactly as rule 4
does.

- **Pros**: one rule for the whole validation model; no block reaches L2 with an
  unevaluated rule.
- **Cons**: contradicts `todos/041`'s ratified reading, and makes every block
  with an unfetched `refs` entry unvalidated — including blocks whose digests all
  resolved without it, which 078 explicitly kept valid.

## Recommended Action

Option 1, folded into Option 2's single definition if the wording turns out to
be shared. Option 3 would undo `todos/041` and 078's own "outcome 3 is reached
only when the missing block could have mattered".

## Technical Details

- **Affected Files**: `spec/02-block-format.md` (rules 6 and 10, and the
  "Validation" preamble), `spec/05-processing-model.md` ("Public/private
  reference rules"), possibly `go/block`'s `Report.Unchecked` and `ts/`'s
  `uncheckedRefs`
- **Related Components**: L1 validation, demand-driven resolution, L2 promotion
- **Database Changes**: No

## Acceptance Criteria

- [x] *Unchecked* has a stated consequence for the block that carries it
      (none: the block is valid, and the status names what the verdict does not
      cover)
- [x] The specification says whether a node MUST re-evaluate rules 6 and 10 when
      a previously unheld `refs` entry arrives, and what happens to a block
      already promoted to L2 if it then fails (it is not obliged to, MUST NOT
      invalidate what it accepted, and nothing in L2 is undone)
- [x] The asymmetry with rule 4's third outcome is either justified in the text
      or removed (justified, in rule 6)

## Work Log

### 2026-08-18 - Filed While Applying 078

**By:** Claude

Found while writing rule 4's third outcome: the rule immediately below it
handles the same missing block with a different status, a different vocabulary
and no stated consequence. 078 was ratified as Option 1, which deliberately did
not unify the vocabularies; this is what it left behind.

### 2026-08-19 - Ratified and Applied

**By:** Claude

**Decision (project lead):** Option 1, stated as verdict stability. Rules 6 and
10 bind only for the `refs` entries a validation of that block resolved. An
entry no validation resolved is permanently outside the block's validity: a
block once valid stays valid, and discovering later — in another block's context
— that an unresolved entry names a private or own-chain block MUST NOT flip the
verdict. That answers the L2 question by dissolving it: nothing is undone in an
append-only graph, because nothing changes. *Unchecked* is explicitly
informational — it tells the application which entries the verdict does not
cover, and nothing more.

**Changes:**

- `spec/02-block-format.md`: the "Validation" preamble gained "A verdict moves
  in one direction"; rule 6 gained the meaning of *unchecked*, the binding
  scope, the MUST NOT re-open an accepted verdict, and the justification of the
  asymmetry with rule 4 (an unresolved digest means the node cannot show the
  block **sound**; an unchecked entry only that it has not found it
  **unsound**); rule 10's own-chain half cites both.
- `spec/05-processing-model.md`: "Public/private reference rules" gained the
  matching paragraph — the evaluation point fixes what the verdict covers, a
  node is not obliged to re-evaluate, and what it MAY do is surface the finding.
- `go/block/validate.go`: behaviour already matched — an unheld entry is a
  warning and never a rejection, and the package records no verdicts. The
  report gained `UncheckedRefs`, which names the entries rules 6 and 10 could
  not be evaluated against, so that a caller can tell which parts of an accepted
  verdict it must never re-open; the doc comments cite the rule.
- `go/block/validate_test.go`: new `TestUncheckedRefsAreInformational`.
- `ts/src/block.ts`: `BlockStore` already enforced the rule — a block stored as
  valid is never re-validated, so an entry arriving later cannot invalidate it.
  The doc comments on `uncheckedRefs` and on the rules 6/10 pass say so.
- `ts/test/block.test.ts`: a public block naming a not-yet-held private block is
  accepted, stays valid when that block arrives and is a duplicate when offered
  again — while a store that held the private block first still rejects it, so
  the rule itself is undiminished.

**Not changed, deliberately.** Both implementations check rules 6 and 10 against
every `refs` entry the source holds at validation time, not only against the
entries a digest needed. `vectors/blocks.json`'s
`public_block_references_a_private_block` pins that reading — its rejected block
carries a `create_atom`, so resolution needs nothing — and the specification's
"once it holds" trigger is unchanged. What the decision forbids is re-opening an
accepted verdict, which is the caller's obligation in Go, where `Validate` is a
function of a block and a source. `todos/081` asks whether that leaves an edge
worth closing in the API.

**Vectors: no byte moved.** `genvectors` reproduces `vectors/` byte for byte.

## Notes

Source: applying `todos/078`; see also `todos/041`, which settled the evaluation
point of rules 6 and 10.
