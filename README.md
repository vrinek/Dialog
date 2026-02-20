# Dialog

Distributed, append-only ontology graph protocol.

**Goal:** Define a protocol first and foremost. Different software built by different people that interoperates — think Ethereum network vs Ethereum clients.

## What the protocol defines

1. **Block format and validation rules** — how data is structured and verified
2. **Content-addressed ontology model** — atoms, bonds, molecules
3. **Three-layer processing model** — L1 (heard) → L2 (known) → L3 (accepted)
4. **Standard meta-bond library** — a minimal set of meta-bonds all implementations should support

## What the protocol does NOT define

- Transport (how nodes discover each other and exchange blocks)
- Conflict resolution strategy (implementation decides)
- Query interface (SQL, GraphQL, etc. are implementation choices)
- L3 processing rules beyond subscription-based filtering

## Architecture

See [Dialog architecture.md](Dialog%20architecture.md) for the three-layer design.

### Layer 1 — "What we heard"
Raw blockchain data. Each author has their own chain of signed, append-only blocks.

### Layer 2 — "What we know"
Union of all operations from all subscribed blocks, added to a single ontology graph tagged with authorship. Foreign-referenced chains are auto-pulled for validation context.

### Layer 3 — "What we accept"
L2 filtered by the user's author subscriptions (local, private config). Meta-molecules applied here. Conflicts flagged. This is the application's source of truth.

## Ontology model

All entities are content-addressed and author-independent:

- **Atom:** `hash(description)` — a unique entity (e.g., "Paris, the capital of France")
- **Bond:** `hash(template)` — a sentence template (e.g., "_A_ is the capital of _B_")
- **Molecule:** `hash(bond_id, fillers...)` — a complete statement (e.g., "[Paris] is the capital of [France]")

Fillers can be: atom references, bond references, molecule references, IPFS URIs (files), or scalar literals (numbers, units, datetime ranges).

## Block format

A public block contains: protocol version, author's public key, signature, previous block hash, foreign block references, timestamp, and an ordered list of operations.

A private block adds a nonce — the operations field is encrypted, all metadata stays plaintext.

Three operation types: `create_atom`, `create_bond`, `create_molecule`.

A block is valid if all its operations reference IDs from the same block or any ancestor block (own chain + foreign references and their ancestors).

## Cryptography

- **Serialization:** CBOR (deterministic encoding for reliable content-addressing)
- **Hashing:** SHA-256 with multihash encoding
- **Signatures:** Ed25519
- **Private blockchain encryption:** Symmetric key, encrypted per-recipient via X25519

## Standard meta-bond library (v1)

| Meta-bond | Purpose |
|-----------|---------|
| `_A_ is the same as _B_` | Transitive atom equivalence |
| `_A_ is true` | Assert a molecule |
| `_A_ is untrue` | Retract/deny a molecule |
| `_A_ contradicts _B_` | Declare two molecules contradictory |
| `_A_ supersedes _B_` | Versioning — molecule A replaces molecule B |
| `_A_ rotates key to _B_` | Key rotation |

New meta-bonds adopted from real-world usage via RFC-like process.

## Key design decisions

- **Schema-less** — no global schema; applications publish their own using meta-molecules
- **Author = public key** — profile info is just molecules in the author's chain
- **Transport out of scope** — the protocol defines data, not how it moves
- **Conflict resolution is implementation-scoped** — possible strategies: flag for user, author priority, latest-wins, app-specific logic
- **Blockchain model is essential** — provides verifiable authorship ordering and strict append-only semantics
- **Applications read from L3 only** — writes go through L1→L2→L3; optimistic UI is an implementation choice

## Open questions

- **Key compromise handling** — deferred to future protocol version. KERI-style pre-rotation is the leading candidate.
- **L2 scalability, schema evolution, conflict resolution boundaries** — deferred to prototype phase for real-world research.

## Documents

- [Architecture](Dialog%20architecture.md) — three-layer design details
- [Protocol design brainstorm](docs/brainstorms/2026-02-20-dialog-protocol-design-brainstorm.md) — full design session with all decisions and rationale
