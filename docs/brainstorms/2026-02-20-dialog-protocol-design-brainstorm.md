# Dialog Protocol Design Brainstorm

**Date:** 2026-02-20
**Status:** Complete (v1 design)

## What We're Building

Dialog is a distributed, append-only ontology graph protocol. The goal is to define a protocol — not a single piece of software — so that different implementations by different people can interoperate. Think Ethereum network vs Ethereum clients.

The protocol defines:
1. **Block format and validation rules** — how data is structured and verified
2. **Content-addressed ontology model** — atoms, bonds, molecules
3. **Three-layer processing model** — L1 (heard) → L2 (known) → L3 (accepted)
4. **Standard meta-bond library** — a minimal set of meta-bonds that all implementations should support

The protocol does NOT define:
- Transport (how nodes discover each other and exchange blocks)
- Conflict resolution strategy (implementation decides)
- Query interface (SQL, GraphQL, etc. are implementation choices)
- L3 processing rules beyond subscription-based filtering

## Why This Approach

Dialog separates universal truth (content-addressed data) from subjective truth (author subscriptions and conflict resolution). The protocol is a data spec + standard library, not a behavior spec. This means:

- All implementations are compatible at L1/L2 (same block format, same ontology model)
- Implementations that share the same conflict resolution strategy are also compatible at L3
- Transport is orthogonal — blocks can move via relays, P2P, USB sticks, or carrier pigeons

## Key Decisions

### Three-layer processing model

**Layer 1 — "What we heard":** The raw blockchain data. Each author has their own chain of signed, append-only blocks.

**Layer 2 — "What we know":** The union of all operations from all subscribed blocks, extracted and added to a single ontology graph, tagged with authorship. No interpretation — just accumulation. When a block references a foreign (non-subscribed) author's block, that foreign chain's history up to the referenced block is automatically pulled into L2 for validation context, even if the user doesn't subscribe to that author.

**Layer 3 — "What we accept":** L2 filtered by the user's author subscriptions (local config, private). Only data from subscribed authors passes from L2 to L3. Meta-molecules are applied here (interpretation is implementation-scoped). Conflicts between subscribed authors are flagged. This is the application's source of truth.

### Content-addressed everything

All ontology entities are content-addressed, author-independent:

- **Atom:** `hash(description_string)` — "Paris, the capital of France" hashes the same regardless of who creates it
- **Bond:** `hash(template_string)` — "_A_ is the capital of _B_" is universal
- **Molecule:** `hash(bond_id, filler_1, filler_2, ...)` — the same assertion by different authors produces the same molecule ID

Any string difference = different atom. "Paris, France" and "Paris, the capital of France" are different atoms. Equivalence is handled explicitly through "is the same as" meta-molecules.

### Filler value types

A molecule's fillers (the values that fill a bond's variables) can be one of five types:

1. **Atom reference** — hash of an atom
2. **Bond reference** — hash of a bond
3. **Molecule reference** — hash of a molecule (enables nesting, e.g., `([Elon Musk] founded [Tesla]) before ([Elon Musk] founded [OpenAI])`)
4. **IPFS URI** — for files (unique, non-interpreted data that doesn't represent identity)
5. **Scalar literal** — a unitless number, a number with a unit (the unit is an atom), or a datetime range

Bond and molecule references are essential for meta-molecules (e.g., `_A_ is true` where A is a molecule) and for complex ontologies that express relationships between relationships.

There are no plain dates in Dialog. "Thursday, Feb 19, 2026" is a datetime range from 00:00 to 23:59.

### Block format

A public block contains 7 fields:

1. **Protocol version** — for forward compatibility
2. **Author's public key**
3. **Signature** — over the entire block content
4. **Previous block hash** — tip of this author's chain (null for genesis block)
5. **Foreign block references** — optional hashes of blocks in other authors' chains
6. **Timestamp** — self-reported, untrusted but useful for ordering heuristics
7. **Operations** — ordered list of ontology operations (plaintext)

A private block has the same structure plus one additional field:

8. **Nonce** — for encryption

In a private block, the operations field is encrypted. All other fields (version, pubkey, signature, previous hash, foreign refs, timestamp) remain plaintext, allowing untrusted nodes to validate the chain DAG without reading content.

### Three operation types

1. **create_atom** — description string → content hash
2. **create_bond** — template string → content hash
3. **create_molecule** — bond ID + ordered list of fillers → content hash

That's it. Files are IPFS URIs used as fillers. Meta-molecules are regular molecules at L1/L2 — they only become "meta" during L2→L3 processing. A block can contain as many operations as needed. A block is valid if all its operations reference IDs from the same block or any ancestor block — including the author's own chain history and "uncle" blocks (foreign block references) and their ancestors.

### Serialization: CBOR

Blocks are serialized using CBOR (Concise Binary Object Representation). Deterministic encoding makes content-addressing reliable, and it aligns with the IPFS/IPLD ecosystem already used for files.

### Author identity

- An author IS their public key
- Profile information (name, avatar, etc.) is just molecules published in the author's chain that describe the key
- Key rotation is supported: an author publishes a block signed by the old key declaring a new key takes over the chain

### Meta-molecules are implementation-scoped

Meta-molecules exist as regular molecules in L1/L2. The L2→L3 transformation interprets them — but HOW they're interpreted is up to the implementation. The protocol provides a "standard library" of meta-bonds, but implementors can add more.

### Conflict resolution is implementation-scoped

When subscribed authors disagree, the protocol doesn't dictate the resolution. Possible strategies (implementation chooses):
- Flag for user intervention
- Author priority ranking
- Latest-wins
- Application-specific logic

### Transport is out of scope

The protocol only defines the data format, validation rules, and layer model. How blocks get from one node to another is an implementation concern. Candidates explored so far: Nostr (relay-based) and Jazz (CRDT sync engine, useful as transport but not as a replacement for the blockchain model).

### Blockchain model is essential

Jazz's CRDTs were evaluated but cannot replace the IOTA-inspired blockchain. The signed-block chain provides two non-negotiable properties:
1. **Verifiable authorship ordering** — a tamper-proof sequence of what each author published
2. **Strict append-only semantics** — data can only be added, never mutated

### Git as a potential L1 implementation

Git's internal model maps naturally to Dialog's Layer 1:

| Dialog L1 | Git |
|-----------|-----|
| Author's chain | Branch with linear commits |
| Block | Commit (CBOR operations as blob content) |
| Previous block hash | Parent commit |
| Foreign block reference | Additional commit parent |
| Block signature | Signed commit (GPG/SSH) |
| Content-addressed IDs | SHA object hashes |

Git is a DAG of signed, content-addressed blocks with battle-tested transport (push/pull over HTTP/SSH), efficient storage (packfiles, delta compression), and decades of tooling. Unlike Nostr or Jazz, git could implement the entire L1 layer — not just transport.

**Friction points:** Git's object model is file-system-oriented (trees/blobs), so ontology operations would be encoded within that structure. Git doesn't enforce single-author-per-branch natively. Foreign block references in Dialog are read-only context, while git merge parents imply integration — the semantics differ even if the DAG structure matches.

### Applications read from L3 only

Applications MUST only read from Layer 3 — never write to it directly. To write, the application sends operations to a Layer 1 blockchain node and waits for propagation through L1→L2→L3. Implementations are free to use optimistic heuristics (e.g., reflecting a change in the UI immediately while awaiting L3 confirmation with a timeout), but the protocol requires that L3 is the sole source of truth for reads.

### Private blockchain

Each user has at least one public (plaintext, signed) and one private (encrypted) blockchain. The private chain is incorporated into the user's L2 ontology graph like any other chain.

**Protocol-level (encrypted block format + key sharing):**
- Private blocks use the same structure as public blocks, plus a nonce. The operations field is encrypted; all metadata remains plaintext.
- Block content is encrypted with a symmetric key.
- That symmetric key is encrypted per-recipient using their Ed25519 public key (converted to X25519 for key agreement). Adding a reader means wrapping the symmetric key for their public key — no need to re-encrypt block content.
- Default: single-user (only the author's own devices hold the key). Optionally shared with specific other authors.

**Implementation-level (device sync + transport):**
- How devices discover and sync with each other (Tailscale, local network, relay servers, etc.) is an implementation concern, consistent with transport being out of scope.

### Schema-less by design

No global schema. Each application publishes its own schema using meta-molecules. Since it's a graph, a few meta-molecules suffice to bridge subgraphs with different schemas.

## Standard Meta-Bond Library

The minimal set of meta-bonds that implementations SHOULD support:

| Meta-bond | Purpose |
|-----------|---------|
| `_A_ is the same as _B_` | Transitive atom equivalence |
| `_A_ is true` | Assert a molecule |
| `_A_ is untrue` | Retract/deny a molecule |
| `_A_ contradicts _B_` | Declare two molecules contradictory |
| `_A_ supersedes _B_` | Versioning — molecule A replaces molecule B |
| `_A_ rotates key to _B_` | Key rotation — author declares new public key |

## Open Questions

1. **Key compromise handling** — If a private key is stolen, an attacker could publish a fraudulent key rotation. Most systems (Bitcoin, Nostr) don't handle this at the protocol level. KERI's pre-rotation is the most promising approach: each rotation commits to a hash of the next key, so an attacker with the current key can't rotate. Deferred to a future protocol version — v1 supports key rotation but not pre-rotation. Other ideas to revisit:
   - KERI-style pre-rotation (strongest, trustless)
   - Revocation by pre-designated trusted authors (social recovery)
   - Time-locked rotation with a cancellation window
   - Retroactive revocation: "after block X, author key Y is revoked"

2. **Needs real-world usage research** — The following questions can't be answered without building and using the system. Defer to prototype phase:
   - **L2 scalability**: As subscriptions grow and foreign block references pull in more chains, L2 grows unboundedly. Acceptable trade-off or does the protocol need pruning rules?
   - **Bond schema evolution**: Meta-molecules handle schema bridging across applications, but what are the edge cases?
   - **Conflict resolution boundaries**: Where exactly are the lines between mechanical, AI, and human resolution? (Strategy is implementation-scoped, but real usage will reveal which patterns work.)


## Resolved Questions

- **Jazz vs blockchain for L1** — Blockchain stays. Jazz could serve as transport but not as the core data model.
- **Transport specification** — Out of scope. The protocol defines data, not transport.
- **Conflict resolution** — Implementation decides. Protocol provides standard meta-bonds, implementations choose resolution strategy.
- **L3 query interface** — Implementation convenience, not a protocol requirement.
- **Atom identity model** — Pure content-addressed. `hash(description)`, no author in the hash.
- **Serialization format** — CBOR.
- **Write roundtrip** — Protocol mandates apps read from L3 only. Writes go through L1→L2→L3. Optimistic UI heuristics are an implementation choice.
- **Hash algorithm** — SHA-256 with multihash encoding. Aligns with both IPFS (SHA-256 default) and git (SHA-256 supported). Multihash provides algorithm agility for the future without locking in.
- **Signature algorithm** — Ed25519. Fast, small keys/signatures, and aligns with both git (SSH signing) and IPFS/libp2p (default peer identity). No need for Bitcoin/Nostr secp256k1 compatibility.
- **Private blockchain** — Protocol defines encrypted block format (operations encrypted, metadata plaintext, nonce field) and key-wrapping scheme (symmetric key encrypted per-recipient via X25519). Device sync/transport is implementation-scoped.
- **Standard meta-bond library** — The 6 listed meta-bonds (is-same-as, is-true, is-untrue, contradicts, supersedes, rotates-key-to) are the v1 standard library. New bonds will be adopted from real-world usage via an RFC-like process.
- **Key rotation mechanism** — Expressed as a meta-molecule (`[old_key] rotates key to [new_key]`), not a special operation or block type. Keeps the 3-operation model pure.
- **L2 processing** — Simple accumulation: extract operations from valid blocks, add to the graph tagged with authorship. No interpretation. Foreign-referenced chains are auto-pulled into L2 for validation context.
- **L3 processing** — L2 filtered by local author subscriptions (private config). Meta-molecules applied here. Conflicts flagged.
- **Author subscriptions** — Local config only. Private. Nobody else knows who you subscribe to.
