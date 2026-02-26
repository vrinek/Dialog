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
- L3 processing rules beyond author filtering, meta-molecule application, and mandatory conflict detection

## Specification

The full protocol specification is in [`spec/`](spec/00-overview.md).

## Architecture

### Layer 1 — "What we heard"
Raw blockchain data. Each author has their own chain of signed, append-only blocks. Users subscribe to authors, determining which chains the node fetches and stores. The `prev` field links each block to its predecessor (ordering only); the `refs` field optionally references specific CID-providing blocks in other chains, forming a DAG.

### Layer 2 — "What we know"
Accumulated ontology graph. Operations are extracted from all stored blocks and added to a single graph, tagged with authorship. Foreign-referenced blocks are demand-pulled for validation context. No interpretation occurs — meta-molecules are stored as regular molecules.

### Layer 3 — "What we accept"
L2 distilled by filtering to subscribed authors, applying meta-molecule semantics, and surfacing conflicts. Subscriptions are a cross-cutting concern: at L1 they determine which chains to fetch; at L3 they determine which authors' data to accept. This is the application's source of truth.

## Ontology model

All entities are content-addressed and author-independent:

- **Atom:** `hash(description)` — a unique entity (e.g., "Paris, the capital of France")
- **Bond:** `hash(template)` — a sentence template (e.g., "_A_ is the capital of _B_")
- **Molecule:** `hash(bond_id, fillers...)` — a complete statement (e.g., "[Paris] is the capital of [France]")

Fillers can be: atom references, bond references, molecule references, IPFS URIs (files), or scalar literals (numbers, units, datetime ranges).

## Block format

Three block types: `public`, `private`, `rotation`.

A **public block** contains: protocol version, block type, author's public key, signature, previous block hash, foreign block references (CID-providing blocks), timestamp, and an ordered list of operations.

A **private block** encrypts `refs`, `ts`, and `ops` together into a single `enc` field. Only chain management fields (`v`, `type`, `pub`, `sig`, `prev`) remain in plaintext, minimizing metadata leakage. A `nonce` field provides the 192-bit XChaCha20 nonce.

A **rotation block** signals the end of the current key's chain. It contains exactly one `rotate_key` operation. The new key begins a fresh chain whose genesis block references the rotation block via `refs`.

Four operation types: `create_atom`, `create_bond`, `create_molecule`, `rotate_key`.

The `prev` field is strictly for chain ordering (append-only semantics, fork detection) — NOT for CID resolution. The `refs` field lists specific CID-providing blocks whose operations define entities needed by the current block. A block is valid if all its operations reference entity CIDs reachable from the same block, ancestor blocks (via `prev`), or referenced blocks (via `refs`, resolved transitively). Fork detection is a normative requirement.

## Cryptography

- **Serialization:** CBOR (deterministic encoding for reliable content-addressing)
- **Hashing:** SHA-256 with multihash encoding
- **Signatures:** Ed25519
- **Private blockchain encryption:** XChaCha20-Poly1305 (AEAD with AAD); symmetric key wrapped per-recipient via X25519 + HKDF-SHA-256
- **Domain separator:** `"dialog-v1-block"` byte prefix for signing input

## Standard meta-bond library (v1)

| Meta-bond | Purpose |
|-----------|---------|
| `_A_ is the same as _B_` | Transitive equivalence (atoms, bonds, or molecules) |
| `_A_ is true` | Assert a molecule |
| `_A_ is untrue` | Retract/deny a molecule |
| `_A_ contradicts _B_` | Declare two molecules contradictory |
| `_A_ supersedes _B_` | Versioning — molecule A replaces molecule B |

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

- [Protocol specification](spec/00-overview.md) — formal spec (start here)
- [Protocol design brainstorm](docs/brainstorms/2026-02-20-dialog-protocol-design-brainstorm.md) — full design session with all decisions and rationale
- [Original architecture notes](archive/Dialog%20architecture.md) — early design notes (archived, superseded by spec)

## PDF Specification

A compiled PDF of the full protocol specification is automatically generated on every push.

### Downloading Releases

Tagged releases include a PDF attachment with the version in the filename:

1. Go to [GitHub Releases](../../releases)
2. Download `dialog-protocol-vX.Y.Z.pdf` from the latest release (e.g., `dialog-protocol-v0.2.0.pdf`)

The PDF version inside the document matches the git tag.

### Local Building

Build the PDF locally using the provided scripts:

```bash
# Build PDF (requires pandoc and chromium)
./build-pdf.sh

# Build with version suffix (e.g., for releases)
./build-pdf.sh --version v0.2.0

# Build HTML version
./build-html.sh

# Build HTML with version suffix
./build-html.sh --version v0.2.0
```

Generated files (`dialog-protocol-*.pdf`, `dialog-protocol-*.html`) are gitignored and not committed to the repository.
