# Dialog

Distributed, append-only ontology graph to replace the Internet.

**Goal:** Define a protocol first and foremost. Different software built by different people that interoperates — think Ethereum network vs Ethereum clients.

## Architecture

See [Dialog architecture.md](Dialog%20architecture.md) for the full three-layer design (blockchain → ontology graph → distilled truth).

## Key Design Decisions

### Schema-less by design
Dialog has no defined schema. Each application publishes their own using meta molecules. Since it's a graph, a few meta molecules suffice to connect subgraphs of different schemas.

### Validation
Validation is intentionally lightweight — mostly ensuring referenced data is accessible and conflicts are flagged. Some conflicts can be mechanically resolved; others need AI or human attention.

### Scalability
Open question: how does Layer 2 scale as author subscriptions grow? Needs research.

## Transport Layer Candidates

### Nostr
- Tried first, worked well enough
- Decentralized relay-based protocol

### Jazz (jazz.tools)
- https://jazz.tools/
- Discovered Feb 2026
- Local-first sync engine with CoJSON (Collaborative JSON)
- Built-in auth, permissions, encryption
- CoJSON could map well to Layer 2 — collaborative, append-only JSON with native sync
- Subscription model (CoMap permissions) could handle author subscriptions
- **Question:** Can Jazz's CRDTs replace the blockchain consensus for Layer 1, or is the signed-block model essential?
- **TODO**: Evaluate as transport layer (reminder set for Feb 23)

## Open Questions
- Layer 2 scalability as subscriptions grow
- Jazz CRDTs vs signed-block model for Layer 1
- Bond schema evolution across applications (meta molecules handle this, but edge cases?)
- Conflict resolution: mechanical vs AI vs human — where are the boundaries?

## Status
- Defining the protocol
- Exploring transport layer options
