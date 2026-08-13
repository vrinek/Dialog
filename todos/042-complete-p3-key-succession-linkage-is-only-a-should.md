---
status: complete
priority: p3
issue_id: "042"
tags: [specification-gap, key-rotation, block-validation, security]
dependencies: ["001"]
---

# Key Succession Is Unverifiable Because the Linkage Is Only a SHOULD

## Problem Statement

A rotation block names the author's next public key. The new key then starts a
fresh chain with a genesis block, and:

> The new key's genesis block SHOULD reference the rotation block via `refs` to
> establish verifiable key succession.

Because that is a SHOULD, a conforming successor chain may carry nothing at all
that ties it to the chain it succeeds. The rotation block points forward (it
names `new_pub`), but the genesis block need not point back, and there is no
signature by the old key over anything the new key produced. A node that sees
only the new chain cannot tell it apart from an unrelated author's chain, and a
node that sees both has an unsigned assertion in one direction only.

Three further cases are unaddressed:

1. **Rotation to a key that already has a chain.** Nothing says the successor
   key must be fresh, so a rotation can name a key that is already publishing
   its own chain with its own genesis block, and the two roles collide.
2. **Rotation to the same key.** `new_pub == pub` is legal under the CDDL and
   ends the chain in favour of itself, which no node can act on sensibly.
3. **Two chains claiming one rotation.** Several genesis blocks may reference
   the same rotation block; only one can be the successor, but nothing says so
   and nothing decides which.

`spec/05-processing-model.md` says "Author identity (mapping multiple keys to a
single author) is implementation-scoped", which explains why the linkage is not
a MUST — but the mapping being local does not make the *evidence* local, and
today there is no evidence to reason from.

## Findings

- `spec/02-block-format.md`, "rotate_key": the SHOULD quoted above, plus
  "Implementations MUST mark the old key as inactive" and "MUST NOT accept
  further blocks signed by the old key".
- `spec/05-processing-model.md`, "Chain succession": the node "MUST add the new
  key ... to the set of known chains" and SHOULD auto-subscribe, all on the
  strength of a link that need not exist.
- `spec/02-block-format.md`, "Rotation block" CDDL: `new_pub` is a bare
  `bstr .size 32` with no constraint relative to `pub`.
- `spec/04-cryptography.md`, "Key rotation": repeats the effect, adds nothing
  about the linkage.

## Proposed Solutions

### Option 1: Make the back-reference a MUST (Recommended)

- "The genesis block of the successor chain MUST list the rotation block's
  digest in its `refs`. A node MUST NOT treat a chain as the successor of a
  rotation unless its genesis block carries that reference."
- Add: `new_pub` MUST NOT equal `pub`; and if more than one genesis block
  references the same rotation block, the succession is ambiguous and the node
  MUST surface the conflict (the same treatment forks get).
- **Pros**: succession becomes checkable from the blocks alone; the successor's
  own signature covers the reference, so the new key attests to the link.
- **Cons**: an author who loses the rotation block's digest cannot start the
  successor chain; a public successor chain referencing a private rotation
  block runs into rule 6, so a private chain's rotation needs its own answer.
- **Effort**: Small (spec), Small (implementations)
- **Risk**: Medium

### Option 2: Keep the SHOULD, state the consequence

- Say plainly that without the reference the succession is not verifiable and
  the node treats the new chain as an unrelated author's until some
  out-of-band mapping says otherwise.
- **Pros**: no new rejection; honest about what v1 provides.
- **Cons**: leaves the MUSTs in "Chain succession" resting on nothing.
- **Risk**: Low

### Option 3: Sign the succession

- Have the rotation block's author sign the new key, or the new key's genesis
  block carry a signature by the old key over `new_pub`.
- **Pros**: real cryptographic succession, in both directions.
- **Cons**: a new field and a second signature in v1; overlaps with the
  key-compromise work already deferred to a future version.
- **Risk**: Medium

## Recommended Action

Option 1 for v1, with option 3 noted as the direction for the version that
takes up key compromise. Either way, the `new_pub != pub` constraint and the
ambiguous-successor case should be stated.

## Technical Details

- **Affected Files**: `spec/02-block-format.md` ("rotate_key", "Rotation
  block"), `spec/05-processing-model.md` ("Chain succession")
- **Related Components**: chain walking, key rotation, subscriptions
- **Database Changes**: No

## Acceptance Criteria

- [x] The specification says whether the successor genesis block MUST
      reference the rotation block
- [x] `new_pub == pub` is permitted or forbidden explicitly
- [x] The case of several genesis blocks referencing one rotation block is
      addressed
- [x] `go/block`'s `ValidateSuccession` matches the ratified rules

## Work Log

### 2026-08-13 - Filed While Implementing go/block
**By:** Claude

`ValidateSuccession` requires what the specification requires — a rotation
block, a genesis block, and the successor key matching `new_pub` — and warns
rather than fails when the `refs` back-reference is missing, since it is a
SHOULD. The warning text says the succession is unverifiable without it, which
is the honest reading and the reason this issue exists.

### 2026-08-13 - Ratified and Implemented
**By:** Claude

**Decision (project lead):** Option 1 for v1, with option 3 left where the
issue put it — a signature by the old key over `new_pub` belongs to the version
that takes up key compromise.

1. The genesis block of a successor chain **MUST** list the rotation block's
   digest in its `refs`, upgraded from SHOULD. A node MUST NOT treat a chain as
   the successor of a rotation unless its genesis block carries the reference;
   a chain that omits it is a valid chain of an unrelated author, as far as any
   node can tell. The genesis block's own signature covers the reference, which
   is what makes it evidence rather than an assertion anyone could make.
2. `new_pub` **MUST NOT** equal the rotation block's `pub`. Rotating to the key
   that signed the block ends a chain in favour of itself and no node can act
   on it.
3. Several genesis blocks referencing one rotation block is an **ambiguous
   succession**, and it is surfaced the way forks are: detection required,
   resolution implementation-scoped. Such blocks are a fork under rule 9 too —
   distinct blocks signed by the successor key, all claiming its genesis
   position — so no new machinery is needed to report them.

The specification is explicit that this makes succession *checkable*, not
cryptographically proven: the old key still signs nothing the new key
produced.

**Changes:**

- `spec/02-block-format.md` § "Rotation block": states the `new_pub != pub`
  constraint and why CDDL cannot carry it.
- `spec/02-block-format.md` § "rotate_key": new "Verifiable succession"
  paragraph with the MUST, the two further constraints, and an informative note
  on what succession does and does not prove.
- `spec/05-processing-model.md` § "Chain succession": the SHOULD becomes the
  MUST, steps 2 and 3 are tied to the chain that carries the reference, and the
  ambiguous case is stated.
- `go/block/block.go` (`Content.Validate`): rejects a rotation block whose
  `new_pub` is its own `pub`, so no such `*Block` can exist.
- `go/block/chain.go`: `ValidateSuccession`'s warning is now an error; new
  `Successors` finds the genesis blocks claiming a rotation block and returns a
  `*Fork` when there is more than one; `ValidateHistory` puts that fork in the
  successor chain's report.
- `go/block/store.go`: new `Referrers` interface (the refs graph read
  backwards) and a `MemStore` index implementing it.
- `go/block/builder.go`: `Succeeds`'s doc comment states the requirement rather
  than a SHOULD.
- `go/block/validate_test.go` (`TestRotation`): the missing-reference subtest
  now expects an error and checks that the block is still a valid chain of its
  own; new subtests for a rotation naming its own key and for two chains
  claiming one rotation (`Successors`, the fork in `ValidateHistory`'s report,
  and a source that cannot answer).

**Follow-up filed:** `todos/044` — the mandatory back-reference makes rule 6's
public/private dichotomy load-bearing for a third block type, since a public
successor genesis block now MUST reference a rotation block.

## Notes

Source: Go reference implementation, phase 3 (block).
