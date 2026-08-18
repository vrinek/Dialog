---
status: complete
priority: p2
issue_id: "077"
tags: [encoding, grounding-demo, api, documentation, interoperability]
dependencies: ["076"]
---

# Author Keys Are Still Rendered as Hex Outside the Protocol

## Problem Statement

`todos/076` gave an author's Ed25519 public key a canonical text form and made
it binding: `spec/04-cryptography.md`, "Key encoding", now says that when a key
is "displayed to users, written to a log, named in a configuration file, or used
as the identifier of a chain" it MUST be written as the multibase base32 form of
`spec/03-encoding.md`, "Text representation of author keys".

Several places in this repository still write a key as hex, and one of them is
exactly the case the rule names: the grounding demo's committed chain index
identifies each chain by a hex `public_key` field. Hex is a byte dump; the
specification says a byte dump is not an identifier and MUST NOT be treated as
one. The rule was written after the code, so nothing was broken — but the demo
is the project's worked example of a node, and it is now the project's worked
example of ignoring a MUST.

## Findings

- `demo/internal/chainfile/chainfile.go`: `ChainEntry.PublicKey` is documented
  as "their raw Ed25519 public key, hex-encoded", written by
  `hex.EncodeToString(c.Pub)` and read back by `hex.DecodeString`. It is the
  identifier under which a chain is filed, which is the case
  `spec/04-cryptography.md` names.
- `demo/chains/index.json`: three `public_key` values, all hex. This is
  committed generated output, so a change here moves bytes the demo's tests and
  README quote and must be regenerated with `go run ./cmd/genchains`, never
  hand-edited.
- `demo/cmd/dialog-mcp/server.go`: an unknown author is labelled with the first
  16 hex characters of the key — a display to a user, over MCP.
- `demo/cmd/dialog-mcp/subscriptions.go`: prints `c.Pub[:4]`, likewise.
- `go/block/block.go` and `go/block/chain.go`: `Block.String`, `Chain.String`
  and several error messages render a key or a key prefix with `%x`. These are
  debug renderings of a truncated prefix, not identifiers, so they are arguably
  the "byte dump" case the specification permits — but `String` methods reach
  logs, and logs are named in the rule. Whether they should change is the
  judgement call in this todo, not an obvious yes.
- `vectors/*.json` keep `public_key` in hex deliberately and are not affected:
  every byte string in the vectors is hex by the file format's own convention,
  and the text form now sits beside it as `public_key_text`.

## Proposed Solutions

### Option 1: Convert every identifier, leave the debug dumps (Recommended)

Change what *names* a key — the demo's chain index and the MCP server's
user-facing labels — to the canonical text form, and leave the truncated `%x`
prefixes in the Go library's `String` methods and error messages, adding a
comment where one is not obviously a byte dump.

- **Pros**: fixes the case the rule was written for; the index becomes readable
  as an identifier and pastes into anything that speaks multibase; the library's
  debug output stays short, which is why it is truncated in the first place.
- **Cons**: moves `demo/chains/index.json`, which means regenerating the demo's
  committed bytes and updating whatever the README quotes; the line between
  "identifier" and "debug dump" is a judgement each call site needs.
- **Effort**: Small (demo), Small (library comments)
- **Risk**: Low — no protocol byte moves; `demo/chains/*.bin` are unaffected,
  since a block's `pub` field is raw bytes and always was.

### Option 2: Convert everything, including the library's `String` methods

- **Pros**: one rule, no judgement calls, nothing to explain.
- **Cons**: a 56-character identifier in every debug line where 8 hex characters
  were enough; error messages that name two keys become unreadable.

### Option 3: Leave it and scope the specification's rule

Narrow the `spec/04-cryptography.md` sentence to identifiers, explicitly
permitting truncated hex in diagnostics.

- **Pros**: the smallest change, and it admits what implementations will do
  anyway.
- **Cons**: weakens a rule one day after it was written; the demo's index is an
  identifier under any reading, so this option does not actually cover it.

## Recommended Action

Option 1, with the demo regenerated in the same commit as the code that changes
its shape.

## Technical Details

- **Affected Files**: `demo/internal/chainfile/chainfile.go`,
  `demo/chains/index.json` (regenerated), `demo/cmd/dialog-mcp/server.go`,
  `demo/cmd/dialog-mcp/subscriptions.go`, `demo/README.md` if it quotes a key,
  possibly comments in `go/block/block.go` and `go/block/chain.go`
- **Related Components**: the grounding demo's chain files, the MCP server's
  presentation layer
- **Database Changes**: No

## Acceptance Criteria

- [x] No file that *names* an author renders the key as hex
- [x] `demo/chains/index.json` is regenerated, not edited, and the demo's five
      checks pass
- [x] Where a truncated hex prefix survives, a comment says it is a byte dump
      and not an identifier

## Work Log

### 2026-08-18 - Filed While Applying 076

**By:** Claude

Found while carrying the transport design document's Q1 into the specification:
the new rule in `spec/04-cryptography.md` had a violation in the repository
before the ink was dry. The demo was left alone deliberately — it is a separate
module whose committed chain bytes move when its index shape changes, which is a
change worth making on its own.

### 2026-08-18 - Ratified and Applied

**By:** Claude

**Decision (project lead):** Option 1, with the boundary drawn explicitly:
*external artifacts* — anything this repository emits for another program or
another person to read — use the canonical text form; *debug output* may keep a
truncated hex prefix.

**Changes:**

- `demo/internal/chainfile/chainfile.go`: `ChainEntry.PublicKey` is now the
  56-character text form. `Build` writes it with `cid.AuthorKeyText` and
  `readChain` parses it with `cid.ParseAuthorKeyText`, so a hex key — or a
  padded, uppercase or mis-prefixed one — now fails to load rather than being
  accepted as an alias. The doc comment says which spec section defines the
  form and notes that the `pub` field on the wire stays 32 raw bytes. The
  "signed by X, filed under Y" error names both keys in the text form through a
  small `keyText` helper rather than as two eight-byte dumps.
- `demo/chains/index.json`: regenerated with `go run ./cmd/genchains`. Three
  `public_key` values moved; no `.block` file moved, because a block's `pub`
  field is raw bytes and always was. The five demo checks pass, including
  `genchains -check`.
- `demo/internal/render/render.go`: a new `AuthorKey(pub)` — the one place the
  demo turns a key into text for display, beside `Short` which does the same
  job for a digest. It falls back to a stated byte count for a key that is not
  32 bytes, which no decoded block can produce, so that a display path never
  has to return an error.
- `demo/cmd/dialog-mcp/server.go`: an author outside the demo is now labelled
  with the text form instead of the first 16 hex characters. Author *names*
  remain the primary label everywhere they exist; the key appears where the hex
  did, which is only the fallback for an author the demo cannot name.
- `demo/cmd/dialog-mcp/subscriptions.go`: `authorLine` prints the full text form
  in place of `key %x…` over `c.Pub[:4]`.
- `demo/internal/replay/replay.go`: the chain/index key-mismatch error names both
  keys in the text form.

**In-scope-exempt debug output.** `go/block/block.go` and `go/block/chain.go`
keep their `%x` renderings of a truncated key prefix in `Block.String`,
`Chain.String` and several error messages, and this is the ruling rather than an
oversight: they are diagnostics, not identifiers, they are deliberately
truncated so that a debug line stays readable, and nothing consumes them as a
name. The rule the specification states is about the case where a key *names*
something, and no `String` method in the library does. `vectors/*.json` likewise
keep `public_key` in hex — every byte string in the vectors is hex by that file
format's convention — with the text form beside it as `public_key_text`. The
third acceptance criterion is satisfied by this paragraph in place of a comment
at each site: the decision is one decision, and it belongs where it can be read
once.

**Verification:** the demo's five checks — `gofmt -l .`, `go vet ./...`,
`go test -count=1 -shuffle=on ./...`, `go run ./cmd/genchains -check`,
`go mod tidy -diff`. The library module is untouched.

## Notes

Source: applying `todos/076`.
