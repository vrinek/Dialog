---
status: complete
priority: p2
issue_id: "078"
tags: [transport, specification-gap, processing-model, block-format, validation]
dependencies: []
---

# An Unfetchable `refs` Entry Has No Defined Verdict

## Problem Statement

A node validating block H must resolve every entity digest H's operations name.
When resolution needs a block listed in H's `refs` and the node cannot obtain it
— no source it knows holds it, the network is down, the reference points into a
chain nobody serves — the specification does not say what verdict H gets.

Three verdicts are individually defensible and mutually incompatible:

- **Invalid.** Validation rule 4 requires every digest an operation names to be
  *reachable*, and an unreachable digest is not reachable.
- **Stored but unvalidated.** The node has not been able to decide, which is
  exactly what that status means everywhere else.
- **Valid.** Not defensible, but a node that treats an unavailable `refs` entry
  the way it treats an unchecked rule 6 could drift into it.

Two nodes taking different readings disagree about a block's validity for a
reason that has nothing to do with the block, and one of them lets a network
failure turn into a permanent rejection of a block that is in fact valid. This
is a lying-source amplifier: a source that withholds one foreign block can make
a client reject a valid public block, which is the same shape as the attack
`todos/069` describes for a plaintext head.

The gap is invisible until a transport exists, because a node whose whole store
arrives as a directory of files either has a referenced block or does not, once,
at load time. It becomes acute the moment fetching is an operation that can fail
and be retried.

## Findings

- `spec/05-processing-model.md`, "Block reception", step 4 defines the *stored
  but unvalidated* status for exactly one cause: "If the block cannot be
  validated because its `prev` predecessor is not held, or is itself
  unvalidated". A missing or unfetchable `refs` target is not that cause, and no
  other step covers it.
- `spec/02-block-format.md`, "Validation", preamble: "A block whose ancestry is
  not available locally is neither valid nor invalid. It is **stored but
  unvalidated** [...] Validity in this sense is relative to what a node holds —
  two nodes may disagree about a block until both have its ancestry — **which is
  already true of rule 4's reachability**." The final clause plainly intends
  rule 4's reachability to behave the same way, and the sentence it appears in
  defines the status for *ancestry* only. The intent is legible; the rule is not
  written.
- `spec/02-block-format.md`, validation rule 4, states the reachability
  requirement with no availability qualifier, so a literal reading gives
  "invalid".
- `spec/05-processing-model.md`, "Scan limit", is careful about the adjacent
  case and settles it the other way: "A `refs` entry the node **does not hold**
  does not count: nothing was fetched and no operation was read." So the
  specification already knows that a `refs` entry a node does not hold is a
  distinct situation from one it scanned — it just never says what happens to
  the block.
- The one place a verdict *is* fixed for this family is the scan limit itself:
  "If resolution must scan a further foreign block once the limit has been
  reached, the block being validated MUST be treated as invalid (unresolvable
  references)." That is a bound the node chose being exceeded, not a fetch
  failing, and reading it across to cover fetch failure would make every network
  hiccup a permanent invalidity.
- `spec/05-processing-model.md`, "Public/private reference rules", uses a third
  vocabulary for the same shape: a node "reports the rule as unchecked for an
  entry it does not hold". *Unchecked* is a rule-level status with no defined
  consequence for the block, and rules 6 and 10 are the only rules that have it.
- `spec/07-transport.md`, "Interaction with the scan limit", point 3, has to
  legislate around the gap: it forbids a client from reporting a block invalid
  on the strength of a failed fetch. That is a profile-level patch on a
  specification-level hole, and it binds only nodes that speak the profile.

## Proposed Solutions

### Option 1: Extend *stored but unvalidated* to cover unresolvable references

State in `spec/05-processing-model.md`, "Block reception", that a block whose
validation cannot complete because a `refs` target is not available is stored but
unvalidated, exactly as one whose `prev` is not available is; and in
`spec/02-block-format.md`, "Validation", that rule 4 distinguishes *resolved to
nothing* (invalid) from *not obtainable* (undecided).

- **Pros**: matches what the "Validation" preamble already implies; makes a
  network failure temporary rather than permanent; costs one paragraph in each
  document and no format change; the status already exists with defined
  semantics (not valid, not invalid, MUST NOT reach L2, MUST be re-validated
  when the missing piece arrives).
- **Cons**: a node must now hold undecided blocks for an unbounded time or have a
  policy for giving up, and the specification would either leave that
  implementation-scoped (probably right) or have to bound it.

### Option 2: Keep "invalid", and say so explicitly

Add an availability clause to rule 4 saying a block whose `refs` cannot be
resolved with what the node can obtain is invalid, full stop.

- **Pros**: one verdict, no third state to carry, and validity stays a function
  of what the node holds — which the preamble already admits it is.
- **Cons**: makes a transient fetch failure permanent unless a node re-validates
  rejected blocks, which nothing requires; hands any source that withholds one
  foreign block the power to make a client reject a valid public block; and
  contradicts the preamble's own "which is already true of rule 4's
  reachability".

### Option 3: Give "unchecked" a definition and use it here

Generalise the rule-level *unchecked* status that rules 6 and 10 already carry,
define its consequence for the block once, and route rule 4's unobtainable case
through it.

- **Pros**: unifies three vocabularies (*stored but unvalidated*, *unchecked*,
  "does not count") into one; the status is already in the text and undefined,
  so defining it is owed regardless of this todo.
- **Cons**: the largest edit of the three, touching both documents' validation
  sections; and a block with an unchecked rule is either usable or not, so this
  ends up having to pick Option 1's or Option 2's answer anyway.

## Recommended Action

Option 1, with the wording taken from the "Validation" preamble's own final
clause, which reads as though the rule were already written. If it is taken,
`spec/07-transport.md`'s point 3 collapses from a profile rule with a caveat into
a pointer at `spec/05`.

## Technical Details

- **Affected Files**: `spec/05-processing-model.md` ("Block reception", possibly
  "Resolution procedure"), `spec/02-block-format.md` ("Validation", rule 4),
  `spec/07-transport.md` ("Interaction with the scan limit"), `go/block`'s
  validation path and its `Report.Unchecked`
- **Related Components**: L1 validation, demand-driven resolution, the transport
  profile's client rules
- **Database Changes**: No

## Acceptance Criteria

- [x] The specification states what verdict a block gets when a `refs` target
      cannot be obtained, and it is the same verdict on every node
- [x] The distinction between "resolved to nothing" and "not obtainable" is
      explicit in rule 4 or in the text rule 4 points at
- [x] `spec/07-transport.md` cites the rule rather than legislating around it

## Work Log

### 2026-08-18 - Filed While Drafting the Transport Profile

**By:** Claude

Found while writing `spec/07-transport.md`'s client rules. The profile has to
say what a client does when a `blocks` fetch returns 404 for a digest a block's
`refs` names, and there is no rule to point at: `spec/05`'s *stored but
unvalidated* is defined for a missing `prev` only, rule 4 reads as "invalid" on
its face, and the "Validation" preamble says reachability behaves like ancestry
without ever saying what that means. The draft states the client rule and cites
this todo; no specification text was changed.

### 2026-08-18 - Ratified and Applied

**By:** Claude

**Decision (project lead):** Option 1, sharpened into a three-valued verdict for
rule 4. Resolution either resolves a digest (the rule passes), proves it absent
from the reachable set (invalid), or cannot say because a block it needs is not
held and not obtainable (stored but unvalidated). *Definitive* was pinned to
mean exactly two things: a resolution that completed against the blocks the node
holds, or the scan limit being reached — a resource bound is a definitive
verdict by design, because the node chose it and every default-configured node
shares it. The absence of a block is not evidence about validity, which is what
keeps a source that withholds a foreign block from invalidating a valid one.

**Changes:**

- `spec/05-processing-model.md`: "Block reception" step 4 now names both causes;
  the *stored but unvalidated* definition lists them (ancestry, reference
  resolution) and says they are one state; a new "Absence is not evidence"
  paragraph carries the MUST NOT and points at `spec/07`'s client rule. The
  "Resolution procedure" gained its three outcomes, with the note that outcome 3
  is reached only when the missing block could have mattered — a `refs` entry
  demand-driven resolution never needed leaves the block valid. "Scan limit"
  gained the explicit contrast between a bound reached and a block absent.
- `spec/02-block-format.md`: the "Validation" preamble's "which is already true
  of rule 4's reachability" is replaced by the rule itself, and states that the
  disagreement between two nodes is between *valid* and *undecided*, never
  *valid* and *invalid*. Rule 4 carries the three outcomes, each with what a
  node MUST and MAY do about it.
- `spec/07-transport.md`: "Interaction with the scan limit", point 3, cites the
  settled rule instead of this todo, and the verification obligation (point 4)
  names the verdict a 404 for a `refs` entry produces. The open-questions table
  lost its 078 row.
- `go/block/validate.go`: the resolver records the first block it needed and the
  source did not hold — an ancestor deeper in the chain, a `refs` entry, or a
  block reached transitively through one. A digest left unresolved with such a
  gap open is a rule 4 error wrapping `ErrNotFound`, which is the pathway rule
  3's missing predecessor already used; a resolution that completed reports the
  unchanged, definitive error. New exported `IsUnvalidated(err)` names the
  question a caller must ask before recording a rejection, and says why the scan
  limit is the deliberate opposite.
- `go/block/store.go`: `ErrNotFound`'s doc points at `IsUnvalidated`.
- `go/block/validate_test.go`: new `TestUnobtainableReferenceIsUndecided` — an
  absent `refs` entry, an absent transitively-reffed block and an ancestor gap
  are each undecided and each validate unchanged once the block arrives; a
  `refs` entry resolution never needed leaves the block valid with zero blocks
  scanned; the verdict is stable across repeated validations of one store.
  `TestReachability` now asserts that its three definitive rejections — an
  unknown digest, an entity known to the store but not named by the block, and
  the scan limit — are *not* the undecided verdict.
- `ts/src/block.ts`: the same distinction, written from the specification text.
  The resolver exposes the block it could not read; rule 4 throws the existing
  `unvalidated` code for it. `BlockError` gained `awaiting`, the digest whose
  arrival would settle the verdict, which rule 3's two `unvalidated` throws also
  carry — so `BlockStore` files a held block under the missing block whatever
  rule left it undecided, and one arrival path re-validates both.
- `ts/test/block.test.ts`: four tests mirroring the Go ones, including that an
  invalid block is not held while an undecided one is.
- `vectors/README.md`: says that every rule 4 case in `invalid_in_chain` is a
  definitive rejection — the `setup` holds every block resolution needs — so an
  implementation answering *stored but unvalidated* for one of them has the
  distinction the wrong way round.

**Vectors: no byte moved.** No `invalid_in_chain` case relied on the old
missing-block reading: the four rule 4 cases have empty `refs` and complete
setups, so each is a resolution that completed and found nothing, and the
scan-limit case is definitive by the unchanged MUST. Nothing needed
re-expressing. `genvectors` reproduces `vectors/` byte for byte.

**Filed:** `todos/079` (an undecryptable `refs` target is still two-valued) and
`todos/080` (rules 6 and 10 stayed two-valued while rule 4 became three-valued).

## Notes

Source: drafting `spec/07-transport.md` against
`docs/design/2026-08-18-transport-design.md` §1 R4 and §4.2.
