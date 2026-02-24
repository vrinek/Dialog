# Block Format

**Version:** 1.0 (2026-02-20) | **Status:** Draft

## Abstract

This document defines the structure of Dialog blocks, the three operation types, chain linking rules, and block validation. A block is the unit of data in Layer 1 — a signed, append-only container of ontology operations.

## Terminology

The key words "MUST", "MUST NOT", "SHOULD", "SHOULD NOT", and "MAY" are to be interpreted as described in [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119).

- **Author chain:** The linear sequence of blocks published by a single author, linked by `prev` hashes.
- **Genesis block:** The first block in an author's chain (`prev` is null).
- **Foreign block reference:** A hash pointing to a block in another author's chain.
- **Ancestor block:** Any block reachable by following `prev` links and foreign references recursively.

## Overview

Dialog uses an IOTA-inspired blockchain model where each author maintains their own chain. Every block is signed by its author, references the previous block in the same chain, and optionally references blocks in other authors' chains. The signed-block chain provides two non-negotiable properties:

1. **Verifiable authorship ordering** — a tamper-proof sequence of what each author published
2. **Strict append-only semantics** — data can only be added, never mutated

## Specification

### Public block

```cddl
public-block = {
  "v"    => uint,              ; protocol version
  "pub"  => bstr .size 32,    ; author's Ed25519 public key
  "sig"  => bstr .size 64,    ; Ed25519 signature
  "prev" => bstr .size 32     ; SHA-256 digest of previous block
           / null,             ; null for genesis block
  "refs" => [* bstr .size 32], ; foreign block references (SHA-256 digests)
  "ts"   => uint,              ; Unix timestamp (seconds since epoch)
  "ops"  => [+ operation]      ; ordered list of operations
}
```

| Field | Type | Description |
|-------|------|-------------|
| `v` | `uint` | Protocol version. MUST be `1` for this specification. Implementations MUST reject blocks with an unrecognized version. |
| `pub` | `bstr .size 32` | Author's Ed25519 public key (raw 32 bytes). |
| `sig` | `bstr .size 64` | Ed25519 signature over the block content. See [04-cryptography.md](04-cryptography.md) for the signing procedure. |
| `prev` | `bstr .size 32 / null` | SHA-256 digest of the previous block in this author's chain. MUST be `null` for the genesis block. MUST NOT be `null` for any other block. |
| `refs` | `[* bstr .size 32]` | Zero or more SHA-256 digests of blocks in other authors' chains. MAY be empty. |
| `ts` | `uint` | Self-reported Unix timestamp. Untrusted — useful for ordering heuristics but not for validation. |
| `ops` | `[+ operation]` | Ordered list of one or more operations. A block MUST contain at least one operation. |

### Private block

A private block has the same structure as a public block with one additional field and an encrypted operations field.

```cddl
private-block = {
  "v"     => uint,
  "pub"   => bstr .size 32,
  "sig"   => bstr .size 64,
  "prev"  => bstr .size 32 / null,
  "refs"  => [* bstr .size 32],
  "ts"    => uint,
  "ops"   => bstr,             ; encrypted operations (ciphertext)
  "nonce" => bstr              ; encryption nonce
}
```

In a private block, the `ops` field contains ciphertext (a byte string) instead of a plaintext array of operations. All other fields remain plaintext, allowing untrusted nodes to validate the chain DAG structure without reading operation content.

See [04-cryptography.md](04-cryptography.md) for the encryption scheme.

### Operations

There are exactly three operation types:

```cddl
operation = create-atom / create-bond / create-molecule

create-atom = {
  "op"          => "create_atom",
  "description" => tstr
}

create-bond = {
  "op"       => "create_bond",
  "template" => tstr
}

create-molecule = {
  "op"      => "create_molecule",
  "bond"    => bstr .size 32,     ; SHA-256 digest of the bond
  "fillers" => [+ filler]         ; ordered list of fillers
}
```

The `filler` type is defined in [01-data-model.md](01-data-model.md).

Operations in a block are ordered. The order determines evaluation sequence for validation purposes (an operation later in the list may reference entities created by earlier operations in the same block).

#### create_atom

Creates an atom. The atom's identifier is `SHA-256(dCBOR({"description": <description>}))`.

#### create_bond

Creates a bond. The bond's identifier is `SHA-256(dCBOR({"template": <template>}))`.

#### create_molecule

Creates a molecule. The molecule's identifier is `SHA-256(dCBOR({"bond": <bond_digest>, "fillers": <fillers>}))`.

The `bond` field and any atom/bond/molecule references in `fillers` MUST refer to entities that are **reachable** from this block (see Validation below).

### Chain linking

Each author maintains a single linear chain of blocks:

```
genesis → block_1 → block_2 → ... → block_n (tip)
```

The `prev` field links each block to its predecessor. This forms a singly-linked list from tip to genesis.

Foreign block references (`refs`) create cross-chain links, forming a DAG (directed acyclic graph) across all authors' chains. A foreign reference means: "this block's operations may reference entities defined in the referenced foreign block or any of its ancestors."

### Validation

A block is **valid** if and only if:

1. **Version check.** The `v` field is a recognized protocol version.
2. **Signature check.** The `sig` field is a valid Ed25519 signature over the block content, verified against the `pub` key. See [04-cryptography.md](04-cryptography.md).
3. **Chain integrity.** If `prev` is not null, it MUST reference an existing, valid block by the same author.
4. **Operation validity.** Every operation in `ops` MUST reference only entity IDs that are **reachable** — defined in:
   - The same block (an earlier operation in the `ops` list), or
   - Any ancestor block in the author's own chain (reachable via `prev`), or
   - Any foreign-referenced block (listed in `refs`) or its ancestors (reachable via the foreign block's own `prev` and `refs`, recursively)
5. **Non-empty operations.** The `ops` list MUST contain at least one operation.
6. **Deterministic encoding.** The block MUST be encoded as valid dCBOR. See [03-encoding.md](03-encoding.md).

For private blocks, validation of rule 4 is only possible by entities that hold the decryption key.

### Block identification

A block's CID is computed from its complete dCBOR encoding, including the signature:

```
block-cid = CID(dCBOR(block))
```

Internal references to blocks (in `prev` and `refs` fields) use the raw SHA-256 digest (32 bytes), not the full CID.

## Security Considerations

- The signature covers the entire block including all operations. Tampering with any field invalidates the signature.
- Timestamps are self-reported and untrusted. Implementations MUST NOT use timestamps for validation decisions. They are informational only.
- Foreign block references expand the trust boundary: by referencing a foreign block, an author asserts that the foreign chain's history up to that block is available and relevant. See [05-processing-model.md](05-processing-model.md) for how foreign references affect Layer 2.
- Private blocks hide operation content but expose chain structure (prev links, foreign refs). An observer can see that an author published a block and when, but not what it contains.

## Examples

### Genesis block with three operations

A genesis block creating two atoms, one bond, and one molecule:

```
{
  "v":    1,
  "pub":  <32 bytes: author's Ed25519 public key>,
  "sig":  <64 bytes: Ed25519 signature>,
  "prev": null,
  "refs": [],
  "ts":   1740067200,
  "ops": [
    {"op": "create_atom", "description": "Paris, the capital of France"},
    {"op": "create_atom", "description": "France"},
    {"op": "create_bond", "template": "_A_ is the capital of _B_"},
    {"op": "create_molecule",
     "bond": <SHA-256 of bond>,
     "fillers": [
       {"type": 0, "value": <SHA-256 of "Paris, the capital of France">},
       {"type": 0, "value": <SHA-256 of "France">}
     ]}
  ]
}
```

This block is valid because:
- `prev` is null (genesis block)
- The `create_molecule` operation references the bond and atoms created by earlier operations in the same block
- The `refs` list is empty (no foreign dependencies)

### Block with foreign reference

```
{
  "v":    1,
  "pub":  <author B's key>,
  "sig":  <signature>,
  "prev": <hash of author B's previous block>,
  "refs": [<hash of a block in author A's chain>],
  "ts":   1740153600,
  "ops": [
    {"op": "create_molecule",
     "bond": <SHA-256 of a bond defined in author A's chain>,
     "fillers": [...]}
  ]
}
```

This block is valid because the bond referenced in the molecule is reachable through the foreign block reference to author A's chain.

## References

### Normative
- [01-data-model.md](01-data-model.md) — Filler type definitions
- [03-encoding.md](03-encoding.md) — dCBOR encoding and CID computation
- [04-cryptography.md](04-cryptography.md) — Signature and encryption procedures
- [05-processing-model.md](05-processing-model.md) — How blocks are processed through L1/L2/L3

### Informative
- [IOTA Tangle](https://wiki.iota.org/learn/protocols/iota2.0/core-concepts/data-structures/) — Inspiration for the multi-chain DAG model
