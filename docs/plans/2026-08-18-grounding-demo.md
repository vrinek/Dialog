# Grounding demo — design and plan

**Status:** in-progress
**Date:** 2026-08-18

## Purpose

The first application of Dialog: a curated knowledge domain published as real
Dialog chains, distilled through L3, and exposed to an AI assistant via an MCP
server — demonstrating the founding use case of grounding an AI's answers in
content-addressed, author-attributed, conflict-aware facts. This is also the
"prototype phase" the spec defers its open questions to (L2 growth, conflict
resolution boundaries, schema evolution): real content is expected to surface
protocol findings, which get filed as todos like every other phase's.

## Decisions

- **Location:** `demo/` at repo root, its OWN Go module (`github.com/vrinek/Dialog/demo`)
  requiring the library module — the library's go.mod keeps its x/crypto-only
  dependency surface; the demo module may take the official MCP Go SDK
  (`github.com/modelcontextprotocol/go-sdk`) and nothing else beyond the library.
- **Domain:** European countries and capitals — small, verifiable, and matching
  the spec's own worked examples. Three fictional authors with deliberate,
  documented disagreements so every meta-bond fires:
  - **atlas** (primary facts): countries, capitals, populations with datetime
    ranges and units.
  - **gazetteer** (naming): equivalence molecules for naming variants
    ("Holland" ≡ "the Netherlands"), plus one truth claim that contradicts
    atlas (a capital dispute) and an explicit `contradicts` declaration.
  - **errata** (corrections): supersession chains updating population figures,
    and a retraction of one of its own earlier assertions (latest-wins demo).
- **Determinism:** chains generated from fixed seeds by `demo/cmd/genchains`,
  committed as binary block files under `demo/chains/` with an index; loading
  them replays L1 validation → graph → accept exactly as a real node would.
  Regeneration must be byte-identical (same discipline as vectors/).
- **MCP server:** `demo/cmd/dialog-mcp`, stdio transport, tools over an
  `accept.View`:
  - `dialog_lookup` — find entities by description/template substring
  - `dialog_truth` — truth state + assertions (with authors) for a molecule
  - `dialog_conflicts` — surfaced conflicts, by kind
  - `dialog_equivalents` — equivalence class of an entity
  - `dialog_provenance` — authorship records straight from L2
  - `dialog_subscriptions` — list/set the session's subscribed authors
    (rebuilds the view; demonstrates that truth is subscription-relative)
  Responses cite digests and authors so the assistant can attribute claims.
- **Walkthrough:** `demo/README.md` — how to build, wire into an MCP client,
  and a scripted tour: ask about a capital (grounded answer), hit the dispute
  (conflict surfaced, not resolved), change subscriptions (answer changes),
  see a superseded population figure flagged.
- **Non-goals:** no transport (chains are files), no persistence beyond the
  committed files, no npm/registry publication.

## Phases

1. `demo/` module + content model + `genchains` + committed chains + loader,
   with tests (chains validate, graph/accept produce the expected states,
   regeneration byte-identical).
   — **done:** `demo/` is its own module (`replace` onto `../go`, no
   dependencies beyond the library). `internal/content` holds the dataset — ten
   real European countries plus one documented-fictional one, keys from fixed
   SHA-256 seeds, block timestamps from a fixed base — and encodes every
   statement of it as an entity, so tests recompute a digest rather than being
   told it. `internal/publish` signs the three chains (14 blocks, 94
   operations), validating each block as it is signed; `internal/chainfile`
   renders and reads the directory (`index.json` plus one `.block` file per
   block, the raw canonical bytes); `cmd/genchains` writes it and `-check`s it.
   `internal/replay` is the loading path a node takes —
   `chainfile.Read` → `block.ValidateChain` → `graph.Ingest` → `accept.Build` —
   with views built per subscription set. Tests cover replay and validation
   from the committed bytes, L2 entity counts and authorship (93 entities, 94
   authorship records, one entity with two authors), both conflicts of the
   capital dispute, equivalence at all three levels, the supersession chain, the
   same-author flip, the collapse of both when `gazetteer` is dropped, tampering
   rejection, and byte-identical regeneration. Three findings filed as
   `todos/063`, `064` and `065`, all three since settled in the specification —
   `064` changed L3 behaviour, so `errata` also publishes an equivalence it
   withdraws a block later and the chains were regenerated for it.
2. `dialog-mcp` server + tools + tests (tool-level, over the committed chains).
3. Walkthrough README + CI (demo module build+test job) + plan closure.

## Rules for implementing agents

Library modules (`go/`, `ts/`) are read-only for this track except where a
genuine library gap blocks the demo — file a todo instead of patching around
it, and surface the gap in the report. Spec is normative; protocol findings
become todos (next free: 067). Checks: demo module `gofmt -l`, `go vet`,
`go test` clean before every commit; the repo's full Go battery must stay
green (the library is untouched, so this is a smoke check). Granular commits,
required trailers, no pushes.
