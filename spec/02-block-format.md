# Block Format

**Version:** <<VERSION>> | **Status:** Draft

## Abstract

This document defines the structure of Dialog blocks, the four operation types, chain linking rules, and block validation. A block is the unit of data in Layer 1 — a signed, append-only container of ontology operations.

## Terminology

The key words "MUST", "MUST NOT", "SHOULD", "SHOULD NOT", and "MAY" are to be interpreted as described in [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119).

- **Author chain:** The linear sequence of blocks published by a single author, linked by `prev` digests.
- **Genesis block:** The first block in an author's chain (`prev` is null).
- **Foreign block reference:** A 32-byte digest pointing to a specific block in another author's chain that defines entities needed by the referencing block.
- **CID-providing block:** A block listed in `refs` whose operations define entities referenced by the current block.

## Overview

Dialog uses an IOTA-inspired blockchain model where each author maintains their own chain. Every block is signed by its author, references the previous block in the same chain, and optionally references blocks in other authors' chains. The signed-block chain provides two non-negotiable properties:

1. **Verifiable authorship ordering** — a tamper-proof sequence of what each author published
2. **Strict append-only semantics** — data can only be added, never mutated

## Specification

### Public block

```cddl
public-block = {
  "v"    => uint,              ; protocol version
  "type" => "public",          ; block type
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
| `type` | `tstr` | Block type. MUST be `"public"`, `"private"`, or `"rotation"`. |
| `pub` | `bstr .size 32` | Author's Ed25519 public key (raw 32 bytes). |
| `sig` | `bstr .size 64` | Ed25519 signature over the block content. See [04-cryptography.md](04-cryptography.md) for the signing procedure. |
| `prev` | `bstr .size 32 / null` | SHA-256 digest of the previous block in this author's chain. Used strictly for chain ordering (append-only semantics and fork detection), NOT for CID resolution. MUST be `null` for the genesis block. MUST NOT be `null` for any other block. |
| `refs` | `[* bstr .size 32]` | Zero or more SHA-256 digests of CID-providing blocks (blocks whose operations define entities needed by this block). MAY be empty. Entries MUST be pairwise distinct, and MUST NOT name a block of the author's own chain. Public blocks MUST only reference public blocks. The order of the entries carries no meaning. See [The refs list](#the-refs-list) and [Validation](#validation). |
| `ts` | `uint` | Self-reported Unix timestamp. Untrusted — useful for ordering heuristics but not for validation. The `ts` field SHOULD be greater than or equal to the `ts` of the previous block in the same chain. Implementations SHOULD warn on non-monotonic timestamps. |
| `ops` | `[+ operation]` | Ordered list of one or more operations. A block MUST contain at least one operation. |

### The refs list

The `refs` list names the CID-providing blocks a block's operations depend on. Two rules constrain the list itself; both apply to a private block's encrypted `refs` exactly as they do to a plaintext one, and are checked by the parties that decrypt it.

**No duplicates.** A digest MUST NOT appear twice in one `refs` list. A repeated entry denotes the same dependency twice and changes nothing but the block's bytes, and therefore its digest and its CID. The check needs no other block, so implementations MUST make it when the block is decoded.

**No own-chain references.** A `refs` entry MUST NOT name a block of the referencing block's own chain — any block reachable from it through `prev`. Such a block is already a resolution path under validation rule 4, so the reference adds nothing and a block that referenced its own predecessor would assert a dependency the chain already provides. Every block of a chain carries the same `pub` (see [Chain linking](#chain-linking)), so the check is a comparison of the referenced block's `pub` with the referencing block's: they MUST differ. A successor chain's genesis block referencing the rotation block that ended the previous chain is unaffected — those two blocks are signed by different keys and belong to different chains.

*Informative.* The order of the entries carries no meaning: an implementation resolving references MAY visit them in any order, and nothing in the protocol reads their sequence. The order is nonetheless inside the signed bytes, so it is part of the block's identity: an implementation MUST preserve the order an author chose and MUST NOT re-sort a block's `refs`, which would change its digest and invalidate its signature. Two blocks that differ only in the order of their `refs` are distinct blocks with the same meaning; authors SHOULD NOT publish both.

### Private block

A private block encrypts `refs`, `ts`, and `ops` together. Only chain management fields remain in plaintext, minimizing metadata leakage (timing information and social graph via refs are hidden from non-recipients).

```cddl
private-block = {
  "v"     => uint,
  "type"  => "private",
  "pub"   => bstr .size 32,
  "sig"   => bstr .size 64,
  "prev"  => bstr .size 32 / null,
  "enc"   => bstr .size (16..), ; encrypted payload (refs + ts + ops)
  "nonce" => bstr .size 24      ; 192-bit XChaCha20 nonce
}
```

The `enc` field MUST be at least 16 bytes long. Sixteen bytes is the size of the Poly1305 authentication tag that XChaCha20-Poly1305 appends to every ciphertext (see [04-cryptography.md](04-cryptography.md)), so a shorter value cannot be the output of the AEAD and the block is structurally invalid. Implementations MUST reject such a block, without attempting decryption and whether or not they hold the key: this is a check every node can make, and private blocks are exactly the blocks most nodes only ever check structurally.

*Informative.* The protocol sets no upper bound on `enc`. An implementation MAY impose a resource limit on the size of a block it accepts or stores; such a limit is local policy and not part of block validity, so a block one node declines to store on size grounds remains valid for another.

The `enc` field contains the ciphertext of a CBOR map with three fields: `refs`, `ts`, and `ops`. When decrypted, this yields:

```cddl
private-block-payload = {
  "refs" => [* bstr .size 32], ; foreign block references
  "ts"   => uint,              ; Unix timestamp
  "ops"  => [+ operation]      ; ordered list of operations
}
```

**Plaintext fields** (`v`, `type`, `pub`, `sig`, `prev`) allow untrusted nodes to validate chain structure (append-only ordering, fork detection) without accessing encrypted content. **Encrypted fields** (`refs`, `ts`, `ops`) are only available to recipients holding the decryption key.

See [04-cryptography.md](04-cryptography.md) for the encryption scheme.

### Rotation block

A rotation block signals the end of the current key's chain. It MUST contain exactly one `rotate_key` operation and no other operations. The `new_pub` field contains the raw bytes of the new Ed25519 public key.

```cddl
rotation-block = {
  "v"    => uint,
  "type" => "rotation",
  "pub"  => bstr .size 32,
  "sig"  => bstr .size 64,
  "prev" => bstr .size 32 / null,
  "refs" => [* bstr .size 32],
  "ts"   => uint,
  "ops"  => [rotate-key-op]       ; exactly one operation
}

rotate-key-op = {
  "op"      => "rotate_key",
  "new_pub" => bstr .size 32      ; new Ed25519 public key (raw bytes)
}
```

The `new_pub` field MUST NOT equal the rotation block's `pub` field. A rotation to the key that signed it ends a chain in favour of itself, which no node can act on: the old key is marked inactive and the successor chain would have to be signed by a key that may no longer be used. Implementations MUST reject such a block. The constraint is a relation between two fields and cannot be written in CDDL; it is checked alongside the schema.

### Validation dispatch

Implementations MUST check the `type` field to determine block structure:

- `"public"`: `ops` is a plaintext array of `operation` values, no `nonce` or `enc` field. No `rotate_key` operation.
- `"private"`: `enc` field contains ciphertext (refs + ts + ops), `nonce` field required. No plaintext `ops`, `refs`, or `ts` fields. The encrypted `ops` are `operation` values: a party that decrypts the payload MUST reject the block if it finds a `rotate_key` operation.
- `"rotation"`: `ops` contains exactly one `rotate_key` operation and no other operation. This is the only block type in which a `rotate_key` operation may appear.

A block map carries exactly the keys the definition for its `type` declares, and an operation map exactly the keys the definition for its `op` declares. This is the closed-map rule of [03-encoding.md](03-encoding.md), "Deterministic CBOR" rule 8, which governs every map in this specification. Implementations MUST reject a block or an operation that carries an undeclared key, and MUST reject one that omits a declared key. A field introduced by a later protocol version arrives in a block whose `v` value this version does not recognize, and is rejected by validation rule 1; it never arrives as an extra key in a v1 block.

### Operations

There are exactly four operation types. Three of them may appear in the `ops`
list of a public or private block; the fourth, `rotate_key`, may appear only in
a rotation block:

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

The `filler` type is defined in [01-data-model.md](01-data-model.md). The fourth operation, `rotate-key-op`, is defined with the [rotation block](#rotation-block) and is not part of the `operation` rule: the `ops` list of a public or private block MUST NOT contain a `rotate_key` operation, and implementations MUST reject a public or private block that carries one. For a private block the rule is enforced by every party that decrypts the payload, since only they can see its operations; a node without the key validates the block's structure and learns nothing about its `ops` either way.

A chain therefore ends where the `type` field says it ends, and nowhere else. Chain-ending semantics belong to the rotation block type, which every node can read, and not to an operation that a private block would hide from all but its recipients.

Operations in a block are ordered. The order determines evaluation sequence for validation purposes (an operation later in the list may reference entities created by earlier operations in the same block).

#### create_atom

Creates an atom. The atom's identifier is `SHA-256(dCBOR({"description": <description>}))`.

#### create_bond

Creates a bond. The bond's identifier is `SHA-256(dCBOR({"template": <template>}))`.

#### create_molecule

Creates a molecule. The molecule's identifier is `SHA-256(dCBOR({"bond": <bond_digest>, "fillers": <fillers>}))`.

A `create_molecule` operation carries three kinds of entity digest, all of them 32-byte internal references (see [03-encoding.md](03-encoding.md), "Internal references"):

- the `bond` field, which MUST resolve to a bond;
- each filler value of type 0, 1 or 2, which MUST resolve to an atom, a bond or a molecule respectively;
- the optional `unit` field inside each scalar filler's value (type 4), which MUST resolve to an atom.

Every one of them MUST refer to an entity that is **reachable** from this block (see Validation below), the `unit` digest exactly like the others: an author who quotes a unit publishes or references the atom that names it, so that no molecule in the graph points at an entity nothing defines. The number of fillers MUST equal the number of variables in the referenced bond template (see [01-data-model.md](01-data-model.md)).

#### rotate_key

Rotates the author's key. A `rotate_key` operation MUST appear only in a block whose `type` is `"rotation"`, and such a block contains exactly one of them and no other operation. The rotation block is the last block in the current key's chain. A new chain begins with a genesis block signed by the new key.

Implementations MUST mark the old key as inactive after processing a rotation block. Implementations MUST NOT accept further blocks signed by the old key after the rotation block.

**Verifiable succession.** The genesis block of the successor chain MUST list the rotation block's digest in its `refs`. A node MUST NOT treat a chain as the successor of a rotation unless its genesis block carries that reference: the rotation block names the new key, the genesis block names the rotation block, and the genesis block's own signature — by the new key — is what makes the second half of that pair evidence rather than an assertion anyone could make. A chain whose genesis block omits the reference is a valid chain of an unrelated author, as far as any node can tell.

Two further constraints follow:

- `new_pub` MUST NOT equal the rotation block's `pub` (see [Rotation block](#rotation-block)).
- Only one chain can succeed a rotation. If a node holds more than one genesis block referencing the same rotation block, the succession is ambiguous and the node MUST surface the conflict. This is a fork condition and is treated like any other: detection is required, and the handling strategy (reject, flag, accept-first-seen) is implementation-scoped, exactly as in validation rule 9. Such genesis blocks are in fact a fork in the strict sense of rule 9 as well — they are distinct blocks signed by the successor key, all claiming the genesis position of its chain.

*Informative.* Succession is verifiable in the sense that the link between the two chains can be checked from the blocks alone. It is not a cryptographic proof of continuity: the old key never signs anything the new key produced, so an author who loses control of a key cannot be distinguished from one who rotated deliberately. Key compromise is deferred to a future protocol version, which is where a signature by the old key over `new_pub` belongs.

### Chain linking

Each author maintains a single linear chain of blocks:

```
genesis → block_1 → block_2 → ... → block_n (tip)
```

The `prev` field links each block to its predecessor. This forms a singly-linked list from tip to genesis. The `prev` link serves strictly for chain ordering — establishing append-only semantics and enabling fork detection. It is NOT used for CID resolution.

Each author chain MUST be strictly linear: each block (except the tip) has at most one successor.

Foreign block references (`refs`) create cross-chain links, forming a DAG (directed acyclic graph) across all authors' chains. A foreign reference means: "this block's operations reference entities defined in the referenced block." Each entry in `refs` points to a specific CID-providing block — the block where the needed entity was created. See [05-processing-model.md](05-processing-model.md) for the demand-driven resolution procedure.

### Validation

Validity is defined **inductively, from the genesis block forward**. A block is valid if and only if it satisfies every rule below — one of which, rule 3, requires that its predecessor be valid. The genesis block is the base case: its `prev` is null, so it depends on no earlier block of its chain, and block *n*'s validity rests on block *n−1*'s, back to it.

The definition does not ask an implementation to re-derive an ancestor's validity. Blocks are validated as they arrive, and a block a node has accepted at L1 was validated when it was received (see [05-processing-model.md](05-processing-model.md), "Block reception"), so rule 3 is a lookup in what the node already accepted, not a recursion. Validating a chain of *n* blocks costs *n* validations, not *n²*.

A block whose ancestry is not available locally is neither valid nor invalid. It is **stored but unvalidated** (see [05-processing-model.md](05-processing-model.md), "Block reception"): the node may hold its bytes, but it MUST NOT treat the block as valid and MUST NOT let its operations reach L2 until the missing ancestors arrive and the block validates. Validity in this sense is relative to what a node holds — two nodes may disagree about a block until both have its ancestry — which is already true of rule 4's reachability.

A block is **valid** if and only if:

1. **Version check.** The `v` field is a recognized protocol version.
2. **Signature check.** The `sig` field is a valid Ed25519 signature over the block content, verified against the `pub` key. See [04-cryptography.md](04-cryptography.md).
3. **Chain integrity.** If `prev` is not null, it MUST reference a block the node holds and has accepted as valid, carrying the same `pub` key. A block whose predecessor is absent, or is itself stored but unvalidated, is not valid; it is stored but unvalidated in turn. Within a single chain, all blocks MUST have the same `pub` field. A chain ends when a rotation block is published; the new key begins a separate chain.
4. **Operation validity.** Every operation in `ops` MUST reference only entity digests that are **reachable** — defined in:
   - The same block (an earlier operation in the `ops` list), or
   - Any ancestor block in the author's own chain (reachable via `prev`), or
   - Any CID-providing block listed in `refs`, or transitively through that block's own `refs` (demand-driven recursive resolution; see [05-processing-model.md](05-processing-model.md))

   The entity digests an operation carries are, exhaustively: a `create_molecule`'s `bond` field, each of its filler values of type 0, 1 or 2, and the optional `unit` inside each of its scalar filler values. There is no exempt position — every digest an operation carries is subject to this rule.
5. **Data model conformance.** Every `create_molecule` operation MUST satisfy the data model rules in [01-data-model.md](01-data-model.md). In particular, the number of fillers MUST equal the number of variables in the referenced bond template, and each digest the operation carries MUST resolve to an entity of the kind its position names: `bond` to a bond, a type 0, 1 or 2 filler value to an atom, a bond or a molecule respectively, and a scalar filler's `unit` to an atom.
6. **Public/private reference rules.** Public blocks MUST only reference public blocks in their `refs` field. Private blocks MAY reference either public or private blocks. The rule is evaluated on each referenced block as it is resolved: a node MUST reject a public block once it holds a private block that the public block's `refs` name, and reports the rule as unchecked for an entry whose block it does not hold. Resolution is demand-driven (see [05-processing-model.md](05-processing-model.md)), so a node is not obliged to fetch a referenced block for the sole purpose of reading its type.
7. **Non-empty operations.** The `ops` list MUST contain at least one operation.
8. **Deterministic encoding.** The block MUST be encoded as valid dCBOR, including the closed-map rule: the block map and every map nested in it carry exactly the keys their definitions declare, with no undeclared key and no missing declared one. See [03-encoding.md](03-encoding.md).
9. **Fork detection.** If a node receives a block whose `prev` value matches the `prev` of another block already stored from the same `pub` key, the node MUST detect this as a chain fork. Fork handling strategy (reject, flag, accept-first-seen) is implementation-scoped.
10. **Reference hygiene.** The `refs` list MUST NOT repeat a digest, and MUST NOT name a block of the author's own chain. See [The refs list](#the-refs-list). The duplicate half is structural and is checked when the block is decoded; the own-chain half is checked on each referenced block as it is resolved, like rule 6.

For private blocks, validation of rules 4, 5, 6, and 10 is only possible by entities that hold the decryption key (since `refs` and `ops` are encrypted).

### Block identification

A block's CID is computed from its complete dCBOR encoding, including the signature:

```
block-cid = CID(dCBOR(block))
```

Internal references to blocks (in the `prev` and `refs` fields) use the raw SHA-256 digest (32 bytes), not the full CID. See [03-encoding.md](03-encoding.md), "Internal references".

## Security Considerations

- The signature covers the entire block including all operations (and for private blocks, the encrypted payload). Tampering with any field invalidates the signature.
- Timestamps are self-reported and untrusted. Implementations MUST NOT use timestamps for validation decisions. They are informational only.
- Foreign block references expand the trust boundary: by referencing a foreign block, an author asserts that the referenced block's entities are needed and available. See [05-processing-model.md](05-processing-model.md) for how foreign references affect Layer 2.
- Private blocks hide operation content, timestamps, and foreign references. An observer can see that an author published a block and its position in the chain (`prev`), but not what it contains, when it was created, or what other blocks it references. This prevents metadata leakage of timing information and social graph (via refs).
- Chain forking: A malicious or buggy author could publish two blocks with the same `prev`, creating divergent histories. Implementations MUST detect forks (see Validation rule 9). The handling strategy is implementation-scoped.
- Public blocks MUST NOT reference private blocks. This prevents public data from depending on content that non-recipient nodes cannot validate.

## Examples

### Genesis block with four operations

A genesis block creating two atoms, one bond, and one molecule:

```
{
  "v":    1,
  "type": "public",
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
     "bond": <digest of bond>,
     "fillers": [
       {"type": 0, "value": <digest of "Paris, the capital of France">},
       {"type": 0, "value": <digest of "France">}
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
  "type": "public",
  "pub":  <32 bytes: author B's Ed25519 public key>,
  "sig":  <64 bytes: Ed25519 signature>,
  "prev": <digest of author B's previous block>,
  "refs": [<digest of a block in author A's chain>],
  "ts":   1740153600,
  "ops": [
    {"op": "create_molecule",
     "bond": <digest of a bond defined in author A's chain>,
     "fillers": [...]}
  ]
}
```

This block is valid because the bond referenced in the molecule was created in the specific block in author A's chain that is listed in `refs`. The ref points directly to the CID-providing block, not to a chain tip.

## References

### Normative
- [01-data-model.md](01-data-model.md) — Filler type definitions
- [03-encoding.md](03-encoding.md) — dCBOR encoding and CID computation
- [04-cryptography.md](04-cryptography.md) — Signature and encryption procedures
- [05-processing-model.md](05-processing-model.md) — How blocks are processed through L1/L2/L3

### Informative
- [IOTA Tangle](https://wiki.iota.org/learn/protocols/iota2.0/core-concepts/data-structures/) — Inspiration for the multi-chain DAG model
