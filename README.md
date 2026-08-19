# Dialog

Distributed, append-only ontology graph protocol.

**Goal:** Define a protocol first and foremost. Different software built by different people that interoperates — think Ethereum network vs Ethereum clients.

## What the protocol defines

1. **Block format and validation rules** — how data is structured and verified
2. **Content-addressed ontology model** — atoms, bonds, molecules
3. **Three-layer processing model** — L1 (heard) → L2 (known) → L3 (accepted)
4. **Standard meta-bond library** — a minimal set of meta-bonds all implementations should support

## What the protocol does NOT define

- Transport (how nodes discover each other and exchange blocks) — [`spec/07`](spec/07-transport.md) is an *optional* profile, not a requirement
- Conflict resolution strategy (implementation decides)
- Query interface (SQL, GraphQL, etc. are implementation choices)
- L3 processing rules beyond author filtering, meta-molecule application, and mandatory conflict detection

## Specification

The full protocol specification is in [`spec/`](spec/00-overview.md).

| # | Document | Covers |
|---|----------|--------|
| 00 | [Overview](spec/00-overview.md) | Scope, architecture, fixed parameters, document index |
| 01 | [Data Model](spec/01-data-model.md) | Atoms, bonds, molecules, filler types |
| 02 | [Block Format](spec/02-block-format.md) | Block structure, operations, the ten validation rules |
| 03 | [Encoding](spec/03-encoding.md) | dCBOR, CIDs, the text forms of a CID and an author key |
| 04 | [Cryptography](spec/04-cryptography.md) | Ed25519 signatures, X25519 encryption, key rotation |
| 05 | [Processing Model](spec/05-processing-model.md) | L1/L2/L3, subscriptions, demand-driven resolution |
| 06 | [Meta-Bonds](spec/06-meta-bonds.md) | Standard meta-bond library, extension process |
| 07 | [Transport Profile](spec/07-transport.md) | **Optional profile.** One serialization for wire and file, six sync operations, an HTTP binding |

Documents 00–06 are the protocol. Document 07 is normative for an
implementation that chooses to speak it and binding on nothing else: no block,
chain or implementation is invalid for not speaking it, and exchanging files is
a complete conforming transport.

## Implementations

Two implementations exist. Neither is the protocol — the specification is —
and they exist for the same reason a two-implementation project always does:
to prove the specification (together with the conformance vectors it fixes)
is a sufficient interop contract, not just a document one team's code happens
to agree with itself about.

**[`vectors/`](vectors/README.md) is the interop contract.** Four
language-agnostic JSON files pin the canonical bytes, digests, CIDs, signing
inputs, signatures and ciphertexts of a fixed set of entities and blocks, plus
the byte strings a conforming implementation must reject. Both implementations
below are verified against them, and the test suite of each fails if it ever
drifts from the committed files.

### Go — reference (`go/`)

The whole L1 → L2 → L3 pipeline: the wire format, the block-level validation
rules, and the two layers above them that the vectors do not cover (the
accumulated graph and one user's filtered, meta-bond-applied view of it).
It generates `vectors/`.

| Package | What it implements |
|---------|--------------------|
| [`dcbor`](go/dcbor) | The deterministic CBOR profile: a canonical encoder and a strict decoder that rejects every non-canonical input |
| [`cid`](go/cid) | The 32-byte digest used inside structures and the 36-byte CIDv1 used at the API boundary, with its base32 text form |
| [`entity`](go/entity) | Atoms, bonds, molecules and the five filler types, with the standard meta-bond library |
| [`block`](go/block) | Blocks, the four operations, the signing input, chain and reachability validation, key rotation and fork detection |
| [`privacy`](go/privacy) | Private-block encryption (XChaCha20-Poly1305 with AAD) and per-recipient key wrapping (X25519 + HKDF-SHA-256) |
| [`graph`](go/graph) | Layer 2: the append-only ontology graph, accumulating validated blocks' entities with their authorship tags and answering provenance queries |
| [`accept`](go/accept) | Layer 3: one user's view of L2 — filtered by subscription, with the five standard meta-bonds applied and every conflict surfaced and none resolved |
| [`transport`](go/transport) | The optional [transport profile](spec/07-transport.md): the block sequence that is both a wire body and a file, an HTTP server over any block store, and a sync client that validates on receipt and obtains each chain from more than one source |

One third-party dependency, `golang.org/x/crypto`. (`go.mod` names a second,
the ruleguard DSL; no build ever compiles it — it exists so the static-analysis
rules in `go/ruleguard/` type-check.) The CBOR codec is
hand-rolled, because Dialog's profile is a small restricted subset and writing
it audits the specification more honestly than a general-purpose library would.

```bash
cd go
nix shell nixpkgs#go --command go test ./...          # the whole suite, vectors included
nix shell nixpkgs#go --command go run ./cmd/genvectors # regenerate ../vectors
```

(`go test ./...` works directly with any Go 1.21 or later installed: `go/go.mod`
pins `toolchain go1.26.6`, and the `go` command downloads and switches to it on
its own. The `nix shell` prefix is this project's convention for not requiring
a system-wide Go at all.)

Any change that alters canonical bytes must regenerate `vectors/`, and that
diff is a breaking change for every implementation that matched the old bytes.

**`go get` caveat.** The module path is `github.com/vrinek/Dialog/go`, and no
`go/vX.Y.Z` tag exists yet, so there is no released version to ask for. Until
one does, depend on a branch or a pseudo-version:

```bash
go get github.com/vrinek/Dialog/go@main
```

See [Releases](#releases) for why the tag needs the `go/` prefix.

### TypeScript — wire format and transport (`ts/`)

The wire format — `dcbor`, `cid`, `entity`, `block` and `privacy`, the four
vector files' worth of the protocol — and the optional [transport
profile](spec/07-transport.md): the block sequence, an HTTP server over any
block store, and a sync client. All of it written **clean-room**: built against
`spec/` and `vectors/` alone, with no access to `go/`'s source, so that its
agreement with the Go implementation is evidence the specification and the
vectors are sufficient on their own, not evidence that one implementation
copied the other's design decisions. That is not a slogan — writing the
transport a second time this way found six places where `spec/07` was silent or
said two things, filed as todos 090 to 095 and now settled in the text. L2 and
L3 are node behavior rather than interop surface, and are not part of it.

Zero-transitive-dependency runtime dependencies from the audited `@noble/*`
family (`curves` for Ed25519 and X25519, `ciphers` for XChaCha20-Poly1305,
`hashes` for SHA-256, SHA-512 and HKDF); dCBOR is hand-rolled, same rationale
as the Go implementation. Runs on Node 24 with no build step (native
TypeScript type-stripping); library code uses no Node-only APIs, so it also
runs in a browser.

```bash
cd ts
nix shell nixpkgs#nodejs_24 --command npm ci
nix shell nixpkgs#nodejs_24 --command npx tsc --noEmit
nix shell nixpkgs#nodejs_24 --command node --test
```

See [`docs/plans/2026-08-18-typescript-implementation.md`](docs/plans/2026-08-18-typescript-implementation.md)
for the plan and the phase-by-phase account of every gap the clean-room
process found in `spec/` and `vectors/` along the way.

## Demo

[`demo/`](demo/README.md) is the founding use case wired up: three fictional
authors publish a small knowledge domain — European countries and their
capitals, with deliberate disagreements — as real Dialog chains, and an MCP
server exposes the L3 view of them to an AI assistant. The assistant answers
from content-addressed, author-attributed facts, cites the digest and author of
every claim, and reports a dispute as a dispute rather than picking a side.

## Architecture

### Layer 1 — "What we heard"
Raw blockchain data. Each author has their own chain of signed, append-only blocks. Users subscribe to authors, determining which chains the node fetches and stores. The `prev` field links each block to its predecessor (ordering only); the `refs` field optionally references specific CID-providing blocks in other chains, forming a DAG. *Implemented by [`block`](go/block).*

### Layer 2 — "What we know"
Accumulated ontology graph. Operations are extracted from all stored blocks and added to a single graph, tagged with authorship. Foreign-referenced blocks are demand-pulled for validation context. No interpretation occurs — meta-molecules are stored as regular molecules. *Implemented by [`graph`](go/graph).*

### Layer 3 — "What we accept"
L2 distilled by filtering to subscribed authors, applying meta-molecule semantics, and surfacing conflicts. Subscriptions are a cross-cutting concern: at L1 they determine which chains to fetch; at L3 they determine which authors' data to accept. This is the application's source of truth. *Implemented by [`accept`](go/accept).*

## Ontology model

All entities are content-addressed and author-independent:

- **Atom:** `digest(description)` — a unique entity (e.g., "Paris, the capital of France")
- **Bond:** `digest(template)` — a sentence template (e.g., "_A_ is the capital of _B_")
- **Molecule:** `digest(bond_digest, fillers...)` — a complete statement (e.g., "[Paris] is the capital of [France]")

Fillers can be: atom references, bond references, molecule references, IPFS URIs (files), or scalar literals (numbers, units, datetime ranges).

References *inside* Dialog structures (`prev`, `refs`, a molecule's `bond`, and atom/bond/molecule fillers) are raw 32-byte SHA-256 digests. The full 36-byte CIDv1 is used only externally — APIs, logs, and human-readable identifiers. IPFS URI fillers are unaffected: they carry IPFS's own identifier as a string.

## Block format

Three block types: `public`, `private`, `rotation`.

A **public block** contains: protocol version, block type, author's public key, signature, previous block digest, foreign block references (digests of CID-providing blocks), timestamp, and an ordered list of operations.

A **private block** encrypts `refs`, `ts`, and `ops` together into a single `enc` field. Only chain management fields (`v`, `type`, `pub`, `sig`, `prev`) remain in plaintext, minimizing metadata leakage. A `nonce` field provides the 192-bit XChaCha20 nonce.

A **rotation block** signals the end of the current key's chain. It contains exactly one `rotate_key` operation. The new key begins a fresh chain whose genesis block references the rotation block via `refs`.

Four operation types: `create_atom`, `create_bond`, `create_molecule`, `rotate_key`.

The `prev` field is strictly for chain ordering (append-only semantics, fork detection) — NOT for CID resolution. The `refs` field lists specific CID-providing blocks whose operations define entities needed by the current block. A block is valid if all its operations reference entity digests reachable from the same block, ancestor blocks (via `prev`), or referenced blocks (via `refs`, resolved transitively). Fork detection is a normative requirement.

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
- [Conformance vectors](vectors/README.md) — the interop contract in bytes, and how to build against it
- [Protocol design brainstorm](docs/brainstorms/2026-02-20-dialog-protocol-design-brainstorm.md) — full design session with all decisions and rationale
- [Original architecture notes](archive/Dialog%20architecture.md) — early design notes (archived, superseded by spec)

## Releases

Every release is tagged **twice**, at the same commit:

| Tag | Releases | Consumed by |
|-----|----------|-------------|
| `vX.Y.Z` | The specification and the PDF built from it | GitHub Releases; the `<<VERSION>>` placeholder in the spec files |
| `go/vX.Y.Z` | The Go module `github.com/vrinek/Dialog/go` | `go get` |

The `go/` prefix is not a convention chosen here. The Go module lives in the
`go/` subdirectory, and the module proxy resolves a subdirectory module only
through a tag carrying that directory prefix. A bare `vX.Y.Z` tag therefore
publishes the specification and does nothing whatsoever for `go get`, which
reports that no matching version exists — so a release that tags only `vX.Y.Z`
is a release the Go module never had.

```bash
git tag v0.3.0 && git tag go/v0.3.0
git push origin v0.3.0 go/v0.3.0
```

The `vX.Y.Z` tag builds the PDF and creates the GitHub Release
([`build-and-release.yml`](.github/workflows/build-and-release.yml)). The
`go/vX.Y.Z` tag runs the Go checks ([`go.yml`](.github/workflows/go.yml)) and
publishes nothing: the module proxy picks the tag up on the first `go get`
that asks for it. A `go/` tag never builds a PDF — the PDF workflow's tag
filter is anchored at `v` and excludes `go/**` explicitly.

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
