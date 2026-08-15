# L2/L3 processing layers — design and plan

**Status:** in-progress
**Date:** 2026-08-15

## Purpose

Implement the L2 (ontology graph accumulation) and L3 (truth distillation) layers of
spec/05-processing-model.md in the Go reference implementation, completing the
L1 → L2 → L3 pipeline. L1 (block storage/validation) already exists in `go/block`.

## Decisions

- **Two packages:** `go/graph` (L2) and `go/accept` (L3). Names mirror the spec's
  "what we know" / "what we accept" framing.
- **L2 is an in-memory, append-only store** of entities keyed by digest, each carrying
  one or more authorship records `(author pub key, block digest)`. Same-CID
  re-publication appends an authorship record, never a duplicate entity (spec/05
  "Accumulation rules"). No persistence in v1 — persistence is storage-engine choice,
  not protocol.
- **Ingestion consumes validated blocks only.** `graph.Graph.Ingest` takes a
  `*block.Block` that the caller has validated (and, for private blocks, a decrypted
  `block.Payload` via the existing `privacy`/`block.Decrypter` machinery). The graph
  trusts its caller on validity — enforcing the "never ingest stored-but-unvalidated"
  rule is the L1 orchestration's job; the reference wiring for that lives in tests
  and doc comments, not a new node daemon.
- **L3 is a derived, recompute-on-demand view** (`accept.View`) built from
  `(graph, subscription set)`. Correctness over performance: v1 recomputes; an
  incremental engine is future work. Views are cheap to rebuild and never mutate L2.
- **Meta-bond semantics in L3** (the five standard meta-bonds from `entity`):
  - `is the same as` — transitive, symmetric equivalence closure (union-find) over
    atoms, bonds, and molecules, built only from subscribed authors' equivalence
    molecules.
  - `is true` / `is untrue` — per-molecule truth state: Asserted / Retracted /
    **Conflicted** when subscribed authors disagree (spec/05 MUST: surface, don't
    resolve).
  - `contradicts` — explicit conflict pairs, surfaced.
  - `supersedes` — supersession chains; superseded molecules remain queryable but
    are marked. Cycles are surfaced as conflicts.
  - Equivalence interacts with the others: assertions about a molecule apply to its
    equivalence class. Where spec/06 is ambiguous about an interaction, the
    implementation picks the defensible reading and files a todo.
- **Conflict model:** `accept.Conflict` is a first-class value (kind, the molecules
  involved, the authors on each side). The reference implementation only surfaces;
  resolution strategies are pluggable by the application on top of the surfaced data.
- **No conformance vectors for L2/L3.** Vectors pin wire bytes; L2/L3 is node
  behavior. Behavior is pinned by scenario tests instead (multi-author fixtures
  driving assert/retract/supersede/equivalence cases end to end from signed blocks).

## Phases

1. `go/graph` (L2): accumulation, authorship tagging, private-payload ingestion,
   idempotent re-ingestion, queries (by digest, by kind, by author, provenance),
   scenario tests from real signed blocks. Spec/05 "Accumulation rules", "No
   interpretation".
   — **done:** `Graph` with `Ingest`/`IngestPayload` over caller-validated
   blocks, `Authorship` records deduplicated on (author, block), every query
   answering in digest order (the determinism guard now covers `graph`), and
   scenario, spec-example and 100-rebuild determinism tests. Three ambiguities
   filed as `todos/049`, `050` and `051`.
2. `go/accept` (L3): subscriptions, filtering, meta-bond application, conflict
   surfacing, queries. Spec/05 "Layer 3", spec/06 application rules.
   — **done:** `View` built by `Build(graph, block.Source, *Subscriptions)`, a
   pure snapshot recomputed rather than maintained; per-entity filtering; all
   five meta-bonds read from subscribed authors' molecules only — equivalence
   closure by union-find, truth by per-lineage latest-wins over a block-order
   index walked from the L1 source (never `ts`), contradiction and supersession
   with transitive chains and cycle detection; four `Conflict` kinds surfaced
   and none resolved; every query in digest order, with the determinism guard
   extended to cover `accept`. Todos 049, 050 and 051 ratified and applied to
   spec/05 and spec/06 first; four new ambiguities filed as `todos/052` to
   `055`.
3. Docs: README architecture section gains implementation pointers; AGENTS.md
   package list; this plan marked complete.

## Rules for implementing agents

Same as docs/plans/2026-08-12-go-reference-implementation.md: spec is normative,
file todos (never silently resolve), gofmt/vet/test/golangci-lint clean before every
commit, granular commits, no pushes.
