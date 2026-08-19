# Grounding demo — design and plan

**Status:** complete
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
   — **done:** `demo/cmd/dialog-mcp` serves the six tools over an `accept.View`
   on the stdio transport, using the official Go SDK
   (`github.com/modelcontextprotocol/go-sdk` v1.7.0, the module's only direct
   dependency beyond the library; it brings eight indirect ones of its own —
   a JSON Schema implementation, a JSON codec, `x/oauth2`, `x/sync`, `x/time`
   and a URI-template package. The library module is untouched and keeps its
   x/crypto-only surface). The chains are embedded in the binary by
   default, so the command needs no working directory; `-chains DIR` or
   `$DIALOG_MCP_CHAINS` serves a directory instead, and either way the blocks
   take the full validating load path of `internal/replay`.
   `internal/render` is the demo's voice: it resolves a molecule's bond and
   fillers and spells the sentence — scalars with their unit atoms, datetime
   ranges as their endpoints, molecule fillers in guillemets so a meta-molecule
   reads as a statement about a statement — with its template scan pinned to
   `entity.ParseTemplateVariables` by a test, and reading from L2 rather than
   the view because filtering is per entity and not transitive
   (spec/05-processing-model.md, "Filtering rules"). Every response cites full
   digests, CID text forms and author names; every error distinguishes a
   malformed identifier, a digest L2 never held, and an entity L2 holds that
   this subscription set does not admit. The subscription set is process state
   and documented as such. Tests call the handlers directly over the committed
   chains — lookup by words, the conflicted Valdoria claim with both authors,
   both conflicts of the dispute, Holland's class with the equivalence that
   declared it, the twice-published truth bond's two authorship records, the
   withdrawn errata equivalence, the supersession chain, and the delta message
   when `gazetteer` is dropped — plus an in-memory transport test that the six
   tools list with inferred schemas. Two findings filed: `todos/067` (an applied
   equivalence or supersession cannot be attributed through the L3 API, so the
   server scans for the declaring meta-molecules itself) and `todos/068` (L3
   computes an assertion's position in its author's chain and reports only the
   block digest).
3. Walkthrough README + CI (demo module build+test job) + plan closure.
   — **done:** `demo/README.md` is the demo's front door: what it is and why,
   the three authors with every disagreement they were given and the fictional
   country flagged as fictional, how to build and run it, how to wire it into
   Claude Code (`claude mcp add`) or any stdio MCP client, and a five-step tour
   whose outputs are verbatim tool answers — a grounded citation, the Valdoria
   dispute surfaced with both authors and no winner, that dispute vanishing
   (with 21 entities) when gazetteer is unsubscribed, the Poland correction
   chain read back to the author and block that declared it, and the withdrawn
   equivalence that declares nothing and is still there. CI is
   `.github/workflows/demo.yml`, its own workflow rather than a step in
   `go.yml`, running gofmt, vet, `go test -race -shuffle=on`,
   `genchains -check` and `go mod tidy -diff` on every push and pull request —
   the demo builds against `go/` from the working tree, so a library change is
   exactly what breaks it. The root README gained a Demo section and AGENTS.md
   the module's place in the tree and its five commands.

   Before that, the two findings phase 2 filed were ratified rather than
   deferred, because both were about the demo's whole point. `todos/067`:
   `accept.Declaration` and `accept.Backing`, read by
   `View.EquivalenceDeclarations`, `View.SupersessionDeclarations` and
   `View.ContradictionDeclarations` — which meta-molecule declared a reading,
   and which subscribed authors still stand behind it, the backing now decided
   per author so that one author's retraction removes them and leaves the
   declaration standing on the others (spec/06-meta-bonds.md, "Withdrawing
   meta-molecules"). `todos/068`: `accept.ChainPosition`, carried by
   `Assertion.Position` and answered for any block the view places by
   `View.BlockPosition` — height counted through key rotations, and a length
   that is honest about being the chain as far as this view reads it. The
   server deleted its `metaDeclarations` scan and its block index and reads
   both APIs; `dialog_truth` now attributes the supersession and the
   contradiction as well. No wire byte moved: `vectors/` is unchanged.

## Outcome

The demo works and is the repository's front door to what Dialog is for: an
assistant grounded in it cites a digest and an author for every claim, and
reports the Valdoria capital dispute as a dispute because L3 hands it one and
refuses to resolve it. Three chains, 14 blocks, 94 operations, 93 entities, and
a six-tool MCP surface over them, with the chains committed as bytes and
regenerated byte-identically.

What it yielded, beyond the demo itself:

- **Six protocol findings, `todos/063`–`068`, all from real content rather than
  from reading the specification again.** Three changed the specification:
  molecule equivalence is declared and never derived through a molecule's parts
  (`063`), a retraction withdraws the meta-molecule its author published
  (`064`), and the standard meta-bonds are entities somebody has to publish
  before using them (`065`). Two changed the library's API (`067`, `068`,
  above). The sixth, `066`, moved `vectors/blocks.json`: a chain block
  publishing a meta-molecule together with its meta-bond, and the same block
  without the `create_bond` as a rule 4 rejection.

- **Withdrawal semantics, forced into existence by a dataset.** `064` is the
  one that changed behaviour: before it, a retracted equivalence went on
  unifying, and the demo's own content is what made that visible — errata
  publishes an equivalence between its two corrected Poland figures and takes
  it back a block later, and while it stood the correction chain was a
  supersession cycle with no current figure at the end of it. The rule that
  came out of it — publication is backing, a publishing author's later "«M» is
  untrue" withdraws theirs, another author's retraction withdraws nothing, and
  the truth meta-bonds are exempt so that the regress has a bottom — is now
  `spec/06-meta-bonds.md`, "Withdrawing meta-molecules", with `applyStanding`
  implementing it and `todos/067`'s declaration records reporting it.

- **The MCP surface as a design pressure on L3.** Both API findings came from
  the same place: a tool that must answer "who says so" cannot be built on an
  API that only answers "what is so". L3 computed the declaring meta-molecule,
  the backing authors and the block positions in the course of reaching its
  answers, and reported the answers without the working; the server was
  re-deriving all three, including the withdrawal rule, which it could have got
  wrong quietly. Exposing what was already computed cost one type, three
  accessors and one field, and deleted more application code than it added.
  The lesson generalises: an L3 API that reports readings without provenance is
  not usable for grounding, which is the protocol's founding use case.

- **A second module, and what it cost.** `demo/` requires `go/` through a
  `replace` and carries the MCP SDK and its eight indirect dependencies, so the
  library's `x/crypto`-only surface survived contact with a real application.
  The price is a module `go/`'s own suite never builds, which is why
  `demo.yml` runs on every push and AGENTS.md says to run the demo's tests when
  L2 or L3 behaviour moves.

## Rules for implementing agents

Library modules (`go/`, `ts/`) are read-only for this track except where a
genuine library gap blocks the demo — file a todo instead of patching around
it, and surface the gap in the report. Spec is normative; protocol findings
become todos (next free: 069). Checks: demo module `gofmt -l`, `go vet`,
`go test` clean before every commit; the repo's full Go battery must stay
green (the library is untouched, so this is a smoke check). Granular commits,
required trailers, no pushes.
