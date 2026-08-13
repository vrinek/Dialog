---
status: complete
priority: p2
issue_id: "037"
tags: [specification-gap, block-format, encoding, interoperability]
dependencies: []
---

# Whether a Decoder Rejects Unknown Map Keys Is Never Stated

## Problem Statement

`spec/02-block-format.md` defines each block type and each operation as a
CDDL map with a fixed set of entries. No sentence anywhere says what a decoder
must do with a map that carries those entries **and one more**:

```
{"v": 1, "type": "public", "pub": ..., "sig": ..., "prev": null,
 "refs": [], "ts": 1740067200, "ops": [...], "note": "hello"}
```

Under RFC 8610 a map with no wildcard entry does not match an input with extra
entries, so the CDDL alone implies rejection. But the specification's prose
never says so, `spec/03-encoding.md`'s dCBOR rules constrain key *order* and
*uniqueness* without saying anything about which keys may appear, and the `v`
field's presence suggests a forward-compatibility story that is never told:
whether a v1 node should ignore fields a v2 node adds, or refuse the block.

The consequence is not cosmetic. A block's identity is the hash of its
encoding, so an implementation that ignores unknown keys and one that rejects
them disagree about which blocks exist, and the permissive one accepts blocks
whose signed content it does not fully understand. The same question applies to
the four operation maps, where the stakes include the entity digest an
operation defines.

## Findings

- `spec/02-block-format.md`, the three block CDDL definitions and the four
  operation definitions: closed maps, no wildcard entries, no prose about
  unknown keys.
- `spec/02-block-format.md` "Validation dispatch" comes closest — "`public`:
  `ops` is a plaintext array, no `nonce` or `enc` field" — but it forbids
  *known* fields in the wrong block type, not unknown ones.
- `spec/03-encoding.md` "Deterministic CBOR" rules 2 and 3 govern key order and
  duplicates only.
- `spec/02-block-format.md` field table for `v`: "Implementations MUST reject
  blocks with an unrecognized version." This is the only forward-compatibility
  statement in the document, and it points towards whole-block rejection rather
  than field-level tolerance.
- `go/entity` already rejects unknown keys in atoms, bonds, molecules and
  fillers, for the same content-addressing reason, but that decision was made
  in the implementation rather than read out of the specification.

## Proposed Solutions

### Option 1: State that unknown keys are rejected (Recommended)

- Add to `spec/02-block-format.md`, "Validation": "A block map MUST carry
  exactly the keys its `type` defines, and an operation map exactly the keys
  its `op` defines. Implementations MUST reject a block or operation carrying
  any other key. New fields are introduced by a new protocol version, which
  the `v` field announces."
- **Pros**: one rule, matches the CDDL, matches what every Dialog structure
  already does at the entity layer, and keeps signatures meaningful — a signer
  cannot smuggle content past a verifier that ignores it.
- **Cons**: no field-level extensibility; every addition costs a version bump.
- **Effort**: Trivial (spec), none (Go — this is already the behaviour)
- **Risk**: Low

### Option 2: Ignore unknown keys

- Decoders skip keys they do not know, so that later versions can add fields.
- **Cons**: two implementations then compute the same block digest for
  structures they interpret differently, and an operation could carry a field
  that changes an entity's meaning for one node and not another. Content
  addressing makes this considerably worse than it is in a typical wire format.
- **Risk**: High

### Option 3: Leave it implementation-scoped

- **Cons**: guarantees divergence on exactly the inputs an attacker chooses.
- **Risk**: High

## Recommended Action

Option 1, stated once for blocks and operations together, with a sentence
pointing at `v` as the extension mechanism.

## Technical Details

- **Affected Files**: `spec/02-block-format.md` ("Validation", possibly
  "Validation dispatch"), `spec/01-data-model.md` (the same question for
  entity maps, already implemented that way)
- **Related Components**: block decoding, operation decoding, content
  addressing, signature semantics
- **Database Changes**: No

## Acceptance Criteria

- [x] `spec/02-block-format.md` states what a decoder does with a map key no
      definition declares, for both blocks and operations
- [x] The relationship between unknown fields and the `v` field is stated
- [x] `go/block` matches the ratified rule and its doc comment cites it

## Work Log

### 2026-08-13 - Filed While Implementing go/block
**By:** Claude

The decoder had to decide, and there was no text to follow. It rejects unknown
keys — the CDDL's own reading, and the only one under which a signature covers
exactly what the verifier sees — and `Decode`'s doc comment points here rather
than presenting the choice as the specification's intent.

### 2026-08-13 - Ratified and Implemented
**By:** Claude

**Decision (project lead):** Option 1, stated once and for the whole protocol.
Maps in Dialog are **closed**: every map carries exactly the key set its
definition declares. A decoder MUST reject a map that carries a key no
definition declares, and MUST reject one that omits a key a definition
requires; only an entry the CDDL marks optional (`?`) may be absent. Unknown
keys are never ignored — under content addressing, ignoring one would let two
implementations compute the same digest for structures they read differently,
and would let a signer put content past a verifier that never saw it. New
fields arrive with a new protocol version, which the `v` field announces and
validation rule 1 rejects here.

**Changes:**

- `spec/03-encoding.md` § "Deterministic CBOR": new rule 8, "Closed maps",
  covering every map the specification defines — entities, blocks, operations,
  and the maps nested inside them (a filler, a scalar filler's value) — with an
  informative paragraph on why content addressing makes the rule sharper than
  it would be in an ordinary wire format, and on `v` as the extension
  mechanism.
- `spec/02-block-format.md` § "Validation dispatch": states the rule for block
  and operation maps and points at rule 8 of `spec/03-encoding.md`; validation
  rule 8 ("Deterministic encoding") now names the closed-map rule as part of
  what valid dCBOR means for a block.
- `go/block/decode.go`: no behaviour change — `requireKeys` already enforced
  exactly this. `Decode`'s and `requireKeys`'s doc comments now cite the
  ratified rule instead of recording the decision as this package's own.
- `go/block/decode_test.go` (`TestDecodeRejections`): the closed-map rule is
  now exercised at every depth a block nests to — undeclared keys on the block,
  on an operation, on a filler and inside a scalar filler's value — and a
  missing declared key at the operation level (`create_molecule` without
  `fillers`).

## Notes

Source: Go reference implementation, phase 3 (block).
