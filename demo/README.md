# The grounding demo

An AI assistant that answers from Dialog instead of from memory. Three fictional
authors publish a small knowledge domain — European countries and their capitals
— as real Dialog chains: signed, append-only blocks of content-addressed atoms,
bonds and molecules. The blocks are replayed through the reference
implementation's whole pipeline (L1 validation → the L2 graph → an L3 view), and
an MCP server exposes that view to an assistant as six tools. Every answer
carries the digest of each entity it rests on and the name of the author who
published it; where the authors disagree, the disagreement is what comes back,
because L3 surfaces conflicts and resolves none of them. This is the protocol's
founding use case, wired up end to end: grounding in facts that are addressed by
their content, attributed to whoever said them, and honest about being disputed.

## The dataset

Eleven countries, three author chains, 14 blocks, 94 operations, 93 entities in
L2. Generated from fixed seeds by `cmd/genchains`, committed under `chains/` as
raw block bytes, and byte-identical on every regeneration.

| Author | Blocks | Publishes |
|--------|-------:|-----------|
| `atlas` | 6 | The primary facts: countries, capitals, populations with a census period and a unit, areas. Asserts its own claim about Valdoria's capital. |
| `gazetteer` | 4 | Naming variants as equivalences (Holland ≡ Netherlands, Hellas ≡ Greece, Deutschland ≡ Germany, Lisboa ≡ Lisbon), its own wording of the capital bond, the rival claim about Valdoria's capital, a retraction of atlas's claim, and an explicit contradiction between the two. |
| `errata` | 4 | Corrections: a supersession chain over Poland's population, an assertion it later retracts itself, and an equivalence it gets wrong and withdraws. |

The disagreements are deliberate. Each one exists to make a meta-bond fire:

- **The capital dispute** — atlas says Miravel, gazetteer says Port Casta, and
  gazetteer also publishes "«Miravel is the capital of Valdoria» is untrue" and
  a contradiction between the two claims. Two conflicts, no resolution.
- **The correction chain** — errata replaces atlas's Poland figure, then
  replaces its own replacement. The older figures stay in the view, marked.
- **The withdrawn equivalence** — errata declares its two corrected Poland
  figures the same statement, which they are not, and retracts it a block
  later. While it stood it would have turned the correction chain into a
  supersession cycle.
- **The same-author flip** — errata asserts a Valdoria population figure true
  and retracts it two blocks later. Block order settles it: no conflict, just a
  retracted molecule.

**Valdoria is fictional, and so is everything published about it** — its
capital Miravel, gazetteer's rival Port Casta, its population and its area. The
demo needs one capital that two authors genuinely disagree about, and inventing
a country is the honest way to get one without publishing a false claim about a
real place. The other ten countries are real and their capitals are correct;
their population and area figures are approximate and are here to exercise the
scalar filler types, not to be cited. errata's revised Poland figures are
invented too.

## Build and run

Go is not needed system-wide; this repository's convention is `nix shell`. From
`demo/`:

```bash
nix shell nixpkgs#go --command go build ./cmd/dialog-mcp
./dialog-mcp                      # serves the chains built into the binary
./dialog-mcp -chains ./chains     # serves a chain directory on disk
DIALOG_MCP_CHAINS=./chains ./dialog-mcp
```

The chains are embedded in the binary (the `demo/chains` package), so the
command needs no working directory and no configuration to be useful — copy the
binary anywhere. `-chains DIR`, or `$DIALOG_MCP_CHAINS` when the flag is absent,
serves a directory instead: the one holding `index.json`. Either way the blocks
take the full loading path — decoded, validated against the ten rules of
[`spec/02-block-format.md`](../spec/02-block-format.md), and ingested into L2
only if their chain validated — so a tampered directory fails to start rather
than serving a lie.

The server speaks the MCP stdio transport: it is started by its client and talks
JSON-RPC over stdin and stdout. It never writes to stdout itself; the startup
line and every diagnostic go to stderr.

```
dialog-mcp: replayed 14 blocks from the chains built into this binary over 3 chains
(atlas, gazetteer and errata); L2 holds 93 entities
```

## Wiring it into an MCP client

With Claude Code, from anywhere:

```bash
claude mcp add dialog -- /absolute/path/to/demo/dialog-mcp
```

With a client that takes the usual JSON configuration file:

```json
{
  "mcpServers": {
    "dialog": {
      "command": "/absolute/path/to/demo/dialog-mcp",
      "args": [],
      "env": {}
    }
  }
}
```

Use an absolute path: the client chooses the working directory, and a relative
one will not resolve. No environment is required — `DIALOG_MCP_CHAINS` is only
for serving a directory instead of the embedded chains.

The six tools:

| Tool | Answers |
|------|---------|
| `dialog_lookup` | Find entities by the words they contain — the way in, since every other tool takes a digest. |
| `dialog_truth` | The truth state of a molecule and the whole record behind it: assertions, supersession, contradictions, equivalents, and who declared each. |
| `dialog_conflicts` | Every disagreement the view surfaces, by kind, with both sides attributed. |
| `dialog_equivalents` | The equivalence class of an entity, and the meta-molecules that declared it. |
| `dialog_provenance` | The L2 authorship records of an entity — who published it and in which block, unsubscribed authors included. |
| `dialog_subscriptions` | Read or replace the subscribed authors. Replacing rebuilds the view and reports what changed. |

The subscription set belongs to the server process and outlives a call. The
stdio transport gives one client one process, so that process is the session; a
server reached over a transport that multiplexes sessions would have to key this
state per session, and this one does not.

## A tour

Five questions, in order, with what the tools actually answer. The outputs are
verbatim, with long lines elided at a `…`; every digest is real, since
`cmd/genchains` produces these bytes and the tests recompute them.

### 1. "What is the capital of France?" — a grounded answer

The assistant calls `dialog_lookup` with `Paris`, then `dialog_truth` on the
molecule it finds.

```
2 entities in the view say "Paris", with atlas, gazetteer and errata subscribed.

1. atom: Paris
    published by atlas
    digest 9eea1d9493afe28c1eaf7d14ac55d1d9a2a257005961d89d7aed5ac1e43e8e7b
    cid    bafyreie65iozje5p4kgb5l35cswfluozukrfoaczmhmj26xnlla6ipuopm

2. molecule: Paris is the capital of France [truth: unasserted]
    published by atlas
    digest eabb3b3de9f95ca25921b23bad96ef30e12eb02c55af5771e582a3590d7efc44
    cid    bafyreihkxm5t32pzlsrfsinshowzn3zq4exlalcvv5lxdzmcunmq27x4iq
```

```
Truth: unasserted.
No subscribed author has published a truth meta-molecule about it. Publication is
not an assertion of truth; it is a statement made.

Assertions: none.

Superseded: no.

Equivalence: nobody has declared it the same as anything else.

Subscribed: atlas, gazetteer and errata.
```

The answer an assistant should give is "Paris — atlas published that, and nobody
subscribed has disputed it", with the digest available to check. Note what
`unasserted` means and does not mean: almost every molecule in any Dialog graph
is unasserted, because publishing a statement is not the same act as asserting
it true. Silence is not denial.

### 2. "What is the capital of Valdoria?" — a dispute, surfaced

`dialog_truth` on atlas's claim:

```
molecule: Miravel is the capital of Valdoria [truth: conflicted]
    published by atlas
    digest 2d8619fffd0c9335e9b24e148681c86b3aec95853a7545a97c4aea381b021b68

Truth: conflicted.
Subscribed authors disagree, and Dialog does not resolve that: both positions
stand, and neither has been discarded. See dialog_conflicts.

Assertions (2):
  - gazetteer says it is untrue, in gazetteer block 4 of 4 (their last word)
    meta-molecule 0d0390c3f33230dd8c7d25e10a4e37ec292c608669288f62212a967282a20cac
  - atlas says it is true, in atlas block 6 of 6 (their last word)
    meta-molecule ed7a56986593d5a8c34bb7ae4dbde347bc9255d4d8845dbefe848969e6969439

Declared to contradict:
  - «Port Casta is the capital of Valdoria» (76c072a49644b2d2…)

The contradiction was declared by:
  - ««Port Casta is the capital of Valdoria» contradicts «Miravel is the capital of Valdoria»»
    declared by gazetteer, in gazetteer block 4 of 4
    digest 32e63d872a14fa05cc5cf169b0c3de64489e75adf5fe35806ff0a2ffa61dd67d
```

Two subscribed authors, two positions, no winner. `dialog_conflicts` says the
same thing from the other direction and finds two separate disagreements — the
truth disagreement, and the contradiction gazetteer declared between the two
molecules:

```
2 conflicts, with atlas, gazetteer and errata subscribed. Dialog surfaces these
and resolves none of them; the choice is the application's.

truth disagreement (1):
  1. truth disagreement over «Miravel is the capital of Valdoria», between gazetteer and atlas
     is true: atlas
     is untrue: gazetteer

contradiction (1):
  1. contradiction between «Miravel is the capital of Valdoria» and «Port Casta is
     the capital of Valdoria», declared by gazetteer
```

An assistant grounded here must report the dispute and name both sides. Picking
a winner is the assistant's judgement, not the data's — the protocol forbids the
implementation from making that choice on the application's behalf
([`spec/06-meta-bonds.md`](../spec/06-meta-bonds.md), "Conflict handling").

### 3. "Ignore gazetteer" — truth is relative to a subscription set

`dialog_subscriptions` with `action: set` and `authors: [atlas, errata]`:

```
Subscribed to atlas and errata; was atlas, gazetteer and errata.
Entities: 93 → 72 (-21). Conflicts: 2 → 0. 2 conflicts disappeared.

Gone:
  - truth disagreement over «Miravel is the capital of Valdoria», between gazetteer and atlas
  - contradiction between «Miravel is the capital of Valdoria» and «Port Casta is
    the capital of Valdoria», declared by gazetteer
  Nothing was resolved. The authors who disagreed are no longer both in the view,
  so there is no longer a disagreement to surface — the assertions are still in
  L2, and re-subscribing brings them back.
```

Asking again about Valdoria's capital now gets a flat answer:

```
molecule: Miravel is the capital of Valdoria [truth: asserted]

Truth: asserted.
A subscribed author stands behind it, and nobody subscribed contradicts them.

Assertions (1):
  - atlas says it is true, in atlas block 6 of 6 (their last word)
```

Twenty-one entities left the view with gazetteer, and both conflicts went with
them. Nothing was deleted and nothing was decided: L2 still holds every one of
gazetteer's blocks, and `dialog_provenance` will still report them. This is the
demo's point about L3 — a view is one subscriber's, and "true" here always means
"true given who you are listening to".

### 4. "How many people live in Poland?" — the newest figure, with its history

`dialog_truth` on atlas's original figure (re-subscribe to all three authors
first, or leave errata subscribed as above):

```
molecule: Poland had a population of 36600000 people during 2024-01-01T00:00:00Z
to 2024-12-31T23:59:59Z [truth: unasserted, superseded]
    published by atlas
    digest 5c03613d23daf91f2129c878117cc060394164a3847948dab0902d476c5c658c

Superseded: yes.
  - replaced by «Poland had a population of 36620000 people during …» (285b55ef55bf36cd…)
  - the current version is «Poland had a population of 36621000 people during …» (9e322ddca6cbdb59…)
  A superseded molecule is deprecated, not deleted; it is still here.

The supersession was declared by:
  - ««Poland had a population of 36620000 people during …» supersedes «Poland had a
    population of 36600000 people during …»»
    declared by errata, in errata block 1 of 4
    digest de9c58d6c6795ea58df9c218c0154412cdc8b4c3975400a87d3e51422b8bb90d
```

The answer is 36,621,000, and the chain that got there is visible: atlas's
figure, errata's first correction, errata's second. The correction is attributed
to the author who made it and to the block they made it in, so "the current
figure" is always answerable with "current according to whom".

### 5. "Are the two corrected figures the same statement?" — a withdrawn declaration

errata published an equivalence between its own two Poland figures and took it
back one block later. `dialog_truth` on that meta-molecule:

```
molecule: «Poland had a population of 36620000 people …» is the same as
«Poland had a population of 36621000 people …» [truth: retracted, withdrawn]
    published by errata
    digest 0ea64add42f0c6001ce40a72fbf9a863aa745126a5ba1d2c9af7d6911a6f4dcf

Truth: retracted.
A subscribed author's last word is that it is untrue, with nobody subscribed
holding the opposite.

Assertions (1):
  - errata says it is untrue, in errata block 4 of 4 (their last word)

This is itself a meta-molecule, on the standard bond "_A_ is the same as _B_".
  Every subscribed author who published it has since retracted it, so it declares
  nothing. It is still an entity of the view, and its truth state records the
  retraction.
```

A withdrawn meta-molecule is not deleted — nothing in Dialog is. It stays an
entity of the view with its own authorship and its own truth state, and it
simply declares nothing, so the two figures are two classes and the correction
chain reads as a chain. Had it stood, the supersession between the two figures
would have been a class superseding itself: a cycle with no current version at
the end of it, which is exactly the conflict L3 would then have surfaced
([`spec/06-meta-bonds.md`](../spec/06-meta-bonds.md), "Withdrawing
meta-molecules").

## How it is put together

```
internal/content    the dataset, and every statement of it as a Dialog entity
internal/publish    signs the three chains, validating each block as it is signed
internal/chainfile  reads and writes the chain directory (index.json + one .block file per block)
internal/replay     the loading path a node takes: chainfile → ValidateChain → graph.Ingest → accept.Build
internal/render     turns an entity back into the English sentence its bond spells
cmd/genchains       writes chains/, and -checks that it is still current
cmd/dialog-mcp      the MCP server: six tools over an accept.View
chains/             the committed blocks, byte for byte
```

The demo is its own Go module (`github.com/vrinek/Dialog/demo`) requiring the
library module through a `replace` onto `../go`. That is deliberate: the library
keeps its `x/crypto`-only dependency surface, and the MCP SDK
(`github.com/modelcontextprotocol/go-sdk`) and its transitive dependencies live
here, where nobody importing the protocol has to resolve them.

Nothing in this directory reimplements any part of the protocol. The chains are
signed by `go/block`, validated by `go/block`, accumulated by `go/graph` and
distilled by `go/accept`; what the demo adds is a dataset, a renderer and a
tool surface.

## Checks

From `demo/`:

```bash
nix shell nixpkgs#go --command gofmt -l .                       # must print nothing
nix shell nixpkgs#go --command go vet ./...
nix shell nixpkgs#go --command go test -count=1 -shuffle=on ./...
nix shell nixpkgs#go --command go run ./cmd/genchains -check    # committed chains are current
nix shell nixpkgs#go --command go mod tidy -diff
```

`genchains -check` is the discipline `vectors/` has: the chains are generated,
never hand-edited, and regeneration must be byte-identical. Rebuild them with
`go run ./cmd/genchains` and review the diff — a change there means every digest
in this README moved, and every test that recomputes one will say so.

## What it found

The demo is also the specification's "prototype phase": real content published
through the real pipeline, with every gap it surfaced filed as a todo rather
than patched around. Six were found, `todos/063` through `todos/068`.

Three changed the specification: equivalence is declared and never derived
through a molecule's parts (`063`), a retraction withdraws the meta-molecule its
author published (`064`, which changed L3's behaviour and made the withdrawn
equivalence of step 5 worth publishing), and the standard meta-bonds are
entities somebody has to publish before using (`065`). Two changed the library's
API: L3 must be able to say who declared an equivalence or a supersession
(`067`) and where in their chain they said it (`068`) — both found by writing
this server and having to answer "who says so" without a way to ask. One is
still open: `066`, a conformance vector that publishes a meta-molecule together
with the meta-bond it uses.

See [`docs/plans/2026-08-18-grounding-demo.md`](../docs/plans/2026-08-18-grounding-demo.md)
for the plan and the phase-by-phase account.
