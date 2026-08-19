# Dialog conformance vectors

Language-agnostic test vectors for the [Dialog protocol](../spec/00-overview.md).
Every byte the protocol fixes — canonical encodings, digests, CIDs, signing
inputs, signatures, ciphertexts — is written down here, so that two
implementations can be compared without either of them reading the other's
source.

These files are the **interop contract**. The specification is normative; these
vectors are what "conforming" means in bytes. If your implementation reproduces
them, it interoperates. If it does not, one of the two has a bug — or the
specification is ambiguous where it looked clear, which is worth reporting.

## Files

| File | Area | Specification | Sections (cases) |
|------|------|---------------|------------------|
| [`dcbor.json`](dcbor.json) | Deterministic CBOR profile | [03-encoding.md](../spec/03-encoding.md) | `encoding_reference` (10), `canonical` (26), `decimal_fractions` (6), `invalid` (54) |
| [`entities.json`](entities.json) | Atoms, bonds, molecules, fillers | [01-data-model.md](../spec/01-data-model.md), [06-meta-bonds.md](../spec/06-meta-bonds.md) | `atoms` (5), `bonds` (2), `meta_bonds` (5), `molecules` (4), `fillers` (12), `invalid` (38) |
| [`blocks.json`](blocks.json) | Blocks, chains, signatures | [02-block-format.md](../spec/02-block-format.md), [04-cryptography.md](../spec/04-cryptography.md), [05-processing-model.md](../spec/05-processing-model.md), [06-meta-bonds.md](../spec/06-meta-bonds.md) | `chain` (6), `forks` (1), `fork_block` (1), `invalid` (23), `invalid_in_chain` (13) |
| [`privacy.json`](privacy.json) | Private blocks, key wrapping | [04-cryptography.md](../spec/04-cryptography.md) | `payload` (1), `aead` (4), `x25519` (3), `key_wrap` (2), `private_block` (1), `invalid` (13) |

## How they are produced

They are generated, never edited by hand:

```bash
cd go && go run ./cmd/genvectors
```

The generator is deterministic — no timestamps, no randomness, every key and
nonce a constant documented in the file's `inputs` — so two runs produce
identical bytes. The Go reference implementation's test suite reads the
committed files back and fails if it no longer reproduces them, and CI
regenerates them and fails on any diff.

**A diff in this directory is never cosmetic.** It means the canonical bytes of
the protocol moved, which breaks every implementation that already matches the
old ones. Review it as a breaking change and say so in the commit.

## File format

Every file has the same envelope:

```jsonc
{
  "vectors": "dialog-conformance/1",   // format version, not the protocol version
  "area": "entities",
  "description": "...",
  "spec": ["spec/01-data-model.md"],   // where the rules live
  "inputs": { ... },                   // fixed constants the cases derive from, if any
  "sections": [
    { "name": "atoms", "description": "...", "cases": [ ... ] }
  ]
}
```

All byte strings are lowercase hex with no separators. Every case has a `name`
that is unique within its section, and most carry a `note` explaining what the
case is for.

Where a file's `inputs` carry `keys`, each entry is one Ed25519 identity: its
32-byte `seed`, the 64-byte `private_key` (`seed || public_key`, the expanded
form many libraries take), the raw `public_key`, and `public_key_text` — the
canonical text form of that key, multibase base32 over `0xed 0x01 || key`
([03-encoding.md](../spec/03-encoding.md), "Text representation of author
keys"). The text form is derived, not independent: an implementation must
produce it from the key bytes and read the same bytes back out of it. Like
`cid_text` in [`entities.json`](entities.json), it is a rendering rather than a
byte dump — the form an author is named in outside the protocol's CBOR
structures — and its near-misses (uppercase, padded, a wrong multicodec prefix)
MUST be rejected.

### The value model

Wherever a file shows a CBOR value's structure, it uses one JSON shape, so that
you can rebuild the value and encode it without first writing a CBOR parser:

```jsonc
{"type": "uint",    "number": "1"}                 // major type 0
{"type": "neg",     "number": "-1"}                // major type 1
{"type": "text",    "text": "France"}              // major type 3
{"type": "bytes",   "bytes": "e57761b4..."}        // major type 2, hex
{"type": "array",   "items": [ ... ]}              // absent "items" = empty array
{"type": "map",     "entries": [{"key": "...", "value": { ... }}]}
{"type": "decimal", "exponent": "-2", "mantissa": "314"}   // tag 4
{"type": "null"}
```

Integers are **decimal strings**, not JSON numbers: CBOR's integer range does
not survive a JSON number in every language, and these vectors include the
extremes on purpose. Map entries are listed in canonical order — the bytewise
lexicographic order of the encoded keys — so a reader that preserves the order
it finds produces canonical bytes without sorting anything.

### Case shapes

- **dCBOR** (`dcbor.json`): `value` (the model above) and `dcbor` (the one byte
  string that encodes it). `Encode(value)` must produce `dcbor`, and
  `Decode(dcbor)` must produce `value`.
- **Entities** (`entities.json`): `kind` (`atom`, `bond` or `molecule`),
  `description` or `template` where the entity has one, `value`, `dcbor`,
  `digest` (32-byte SHA-256), `cid` (36 bytes, hex) and `cid_text` (the
  canonical multibase base32 form, which is how a CID is written down).
  Fillers carry `type`, `value` and `dcbor` only — a filler is hashed as part
  of its molecule and has no identifier of its own.
- **Blocks** (`blocks.json`): the summary fields (`type`, `prev`, `refs`, `ts`,
  and `enc`/`nonce` for a private block), the complete block map as a `value`,
  and four byte strings: `signing_bytes` (dCBOR of the block without `sig`),
  `signing_input` (`"dialog-v1-block"` prepended — what Ed25519 actually
  signs), `signature`, and `block` (the complete encoding, signature included,
  which is what `digest` hashes).
- **Invalid cases** (`invalid` sections): `bytes` plus the `rule` they violate,
  named as the specification numbers it, and a `reason`. Your decoder must
  reject every one of them. In `entities.json` each case also carries a `kind`
  — `atom`, `bond`, `molecule` or `filler` — naming the decoder the bytes are
  handed to, because the entity layer has one decoder per kind and a case is a
  rejection by *its* decoder. Those bytes are well-formed dCBOR that the *data
  model* refuses, so they exercise a layer above `dcbor.json`'s; the one
  exception says so in its `reason`.
- **Chain-relative rejections** (`invalid_in_chain` in `blocks.json`): a block
  that decodes, verifies, and is wrong only in relation to what a node holds.
  Each case carries `setup` — block hexes to replay into a **fresh** store, in
  order, every one of which MUST be accepted — and then `bytes`, the block that
  MUST be rejected, with the `rule` it violates and a `reason`. `setup` is empty
  for a block that is wrong about itself and needs no store. A case carrying
  `scan_limit` MUST be validated with that limit configured; the same block
  against the same store is valid under the default limit of 256, so the case
  pins the limit and nothing else. Every rule 4 case here is a **definitive**
  rejection: the `setup` holds every block resolution needs, so the digest is
  provably absent from the reachable set rather than merely unfetched, and an
  implementation that answers *stored but unvalidated* for one of them (the
  third outcome of rule 4, see
  [02-block-format.md](../spec/02-block-format.md)) has the distinction the
  wrong way round. This is the half of validation no decoder can
  perform — rules 3, 4, 5, 6 and the own-chain half of rule 10 — and the half
  two implementations are most likely to disagree about.
- **Privacy** (`privacy.json`): named `hex` byte strings for the plaintext
  payload, the AAD and the ciphertext; the Ed25519-to-X25519 conversions; and
  the per-recipient wrap, with its shared secret, wrapping key and 72-byte
  wrapped key.
- **Privacy rejections** (`invalid` in `privacy.json`): every rejection rule
  spec/04-cryptography.md states in prose, plus the two of them that in fact
  live in spec/02-block-format.md (the enc floor and the rotate_key scoping).
  A case's populated fields say which function it exercises, since the four
  shapes this section holds are not interchangeable: `public_key` alone is an
  Ed25519 public key the X25519 conversion itself MUST refuse; `own` with
  `peer_public_key` is a small-order peer the key agreement MUST refuse before
  any wrapping key is derived; `own`, `peer` and `wrapped_key` is a wrap to
  attempt unwrapping, of the wrong length or the right length but tampered;
  `content_key` with `block` is a complete, decodable private block to attempt
  opening, whose enc, nonce or AAD-covered fields make the AEAD reject it, or
  whose enc is too short to be a ciphertext at all. Every case still names the
  `rule` it violates and a `reason`, exactly as the other three files'
  `invalid` sections do.

## Using them in a new implementation

A reasonable order to work through, each step depending on the last:

1. **`dcbor.json`** — get the encoder and the strict decoder right first.
   Nothing above it can be correct while it is not. The `invalid` section is
   the important half: a decoder that accepts non-canonical input will compute
   identifiers for structures other implementations refuse.
2. **`entities.json`** — build an atom, a bond and a molecule; check the bytes,
   then the digest, then both forms of the CID. The `cid_text` values are the
   ones users and APIs see. Then the `invalid` section, which is the half that
   decides which entities exist at all: an implementation that accepts an empty
   description, a template with no variable, a filler whose value does not
   match its type tag, or a timestamp its platform's date library likes and
   Dialog does not, mints digests for structures another implementation refuses
   to parse. The timestamp cases are where a stock date parser will quietly
   disagree with the specification.
3. **`blocks.json`** — start with the `inputs`: derive each key from its seed,
   and render each `public_key_text`, which is the identifier a chain is named
   by anywhere outside the blocks. Then reproduce `signing_bytes` before
   worrying about the signature: if those bytes are right and the signature is not, the problem is
   in the signing procedure, not the encoding. Then replay the `chain` section
   in order into a store and validate each block; the `forks` section is the
   one condition [02-block-format.md](../spec/02-block-format.md) rule 9
   requires you to detect. One pair straddles the two halves and is worth
   checking together: the chain's `bob_meta_molecule` publishes a standard
   meta-bond and the meta-molecule built from it in one block, and
   `invalid_in_chain`'s `unreachable_meta_bond` is the same block with the
   `create_bond` removed. A meta-bond is an entity like any other and is
   present in no chain until an author creates it
   ([06-meta-bonds.md](../spec/06-meta-bonds.md)), so an implementation that
   treats the five well-known digests as ambient accepts a block every other
   node rejects. Leave the rest of `invalid_in_chain` for when the store works:
   its cases are the rules that relate a block to what a node holds, each one
   replayed into a store of its own, and they are where an implementation that
   passes everything above still turns out to accept blocks another node
   refuses.
4. **`privacy.json`** — the AAD binds a ciphertext to its block, so check it
   before the ciphertext. The X25519 private half is derived by hashing the
   Ed25519 seed with SHA-512, not from the seed directly; that is the single
   most common place to go wrong, and the `x25519` section isolates it. Then
   the `invalid` section: the two X25519 conversion rejections and the
   small-order agreement first, since a wrapping key derived from a rejected
   input is wrong however the rest of the section behaves; then the key-wrap
   length and tamper cases; then the AEAD and payload cases, which is where an
   implementation that authenticates correctly can still differ on *when* it
   applies strict dCBOR decoding and the rotate_key scoping rule to what
   comes out of the AEAD.

All keys in these files are test keys with published private material. They
exist so that signatures and ciphertexts are reproducible. Never use them for
anything else, and note that a real private block MUST use a fresh nonce per
block — the fixed nonces here exist only so the bytes can be pinned.
