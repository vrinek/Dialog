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
| [`dcbor.json`](dcbor.json) | Deterministic CBOR profile | [03-encoding.md](../spec/03-encoding.md) | `encoding_reference` (10), `canonical` (25), `decimal_fractions` (6), `invalid` (51) |
| [`entities.json`](entities.json) | Atoms, bonds, molecules, fillers | [01-data-model.md](../spec/01-data-model.md), [06-meta-bonds.md](../spec/06-meta-bonds.md) | `atoms` (5), `bonds` (2), `meta_bonds` (5), `molecules` (3), `fillers` (11) |
| [`blocks.json`](blocks.json) | Blocks, chains, signatures | [02-block-format.md](../spec/02-block-format.md), [04-cryptography.md](../spec/04-cryptography.md) | `chain` (5), `forks` (1), `fork_block` (1), `invalid` (23) |
| [`privacy.json`](privacy.json) | Private blocks, key wrapping | [04-cryptography.md](../spec/04-cryptography.md) | `payload` (1), `aead` (4), `x25519` (3), `key_wrap` (2), `private_block` (1) |

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
  reject every one of them.
- **Privacy** (`privacy.json`): named `hex` byte strings for the plaintext
  payload, the AAD and the ciphertext; the Ed25519-to-X25519 conversions; and
  the per-recipient wrap, with its shared secret, wrapping key and 72-byte
  wrapped key.

## Using them in a new implementation

A reasonable order to work through, each step depending on the last:

1. **`dcbor.json`** — get the encoder and the strict decoder right first.
   Nothing above it can be correct while it is not. The `invalid` section is
   the important half: a decoder that accepts non-canonical input will compute
   identifiers for structures other implementations refuse.
2. **`entities.json`** — build an atom, a bond and a molecule; check the bytes,
   then the digest, then both forms of the CID. The `cid_text` values are the
   ones users and APIs see.
3. **`blocks.json`** — reproduce `signing_bytes` before worrying about the
   signature: if those bytes are right and the signature is not, the problem is
   in the signing procedure, not the encoding. Then replay the `chain` section
   in order into a store and validate each block; the `forks` section is the
   one condition [02-block-format.md](../spec/02-block-format.md) rule 9
   requires you to detect.
4. **`privacy.json`** — the AAD binds a ciphertext to its block, so check it
   before the ciphertext. The X25519 private half is derived by hashing the
   Ed25519 seed with SHA-512, not from the seed directly; that is the single
   most common place to go wrong, and the `x25519` section isolates it.

All keys in these files are test keys with published private material. They
exist so that signatures and ciphertexts are reproducible. Never use them for
anything else, and note that a real private block MUST use a fresh nonce per
block — the fixed nonces here exist only so the bytes can be pinned.
