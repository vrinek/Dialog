# Dialog Protocol — Agent Guidelines

This is a protocol specification project. The `spec/` directory contains the formal Dialog protocol specification.

## Project Structure

```
spec/           # Protocol specification documents (Markdown)
  00-overview.md         # Entry point to the spec
  01-data-model.md       # Atoms, bonds, molecules, fillers
  02-block-format.md     # Block structure, operations, validation
  03-encoding.md         # CBOR, CID, dCBOR profile
  04-cryptography.md     # Ed25519, X25519, encryption
  05-processing-model.md   # L1/L2/L3 architecture
  06-meta-bonds.md       # Standard meta-bond library
go/                      # Go reference implementation (module github.com/vrinek/Dialog/go)
  dcbor/ cid/ entity/ block/ privacy/   # the protocol, one package per layer
  cmd/genvectors/        # writes the conformance vectors to vectors/
  internal/              # the vector generator and its JSON schema
  conformance_test.go    # checks the implementation against committed vectors/
vectors/                 # Conformance test vectors (generated, language-agnostic)
docs/
  brainstorms/           # Design decision records
  plans/                 # Implementation plans
archive/                 # Superseded documents
todos/                   # Pending spec fixes (numbered)
```

## Build/Test Commands

The specification is prose and needs no build. The Go reference implementation
in `go/` does. Run all three from `go/`, and all three before every commit:

```bash
nix shell nixpkgs#go --command gofmt -l .        # must print nothing
nix shell nixpkgs#go --command go vet ./...
nix shell nixpkgs#go --command go test -count=1 ./...
```

Go is not installed system-wide; `nix shell nixpkgs#go --command` is how it is
invoked. The toolchain version comes from `go/go.mod`.

The PDF is built with `./build-pdf.sh` (needs pandoc and chromium) and the HTML
with `./build-html.sh`; both take an optional `--version vX.Y.Z`.

### Conformance vectors

`vectors/` holds the conformance test vectors: the canonical bytes, digests,
CIDs, signatures and ciphertexts that every implementation must reproduce. They
are generated, never hand-edited:

```bash
cd go && nix shell nixpkgs#go --command go run ./cmd/genvectors
```

**Any change that alters canonical bytes MUST regenerate `vectors/`, and the
resulting diff MUST be reviewed as a breaking-change signal.** A diff there
means every implementation that matched the old bytes is now wrong. If a change
was not meant to move any byte and the diff is non-empty, the change is a bug,
not the vectors. `go test ./...` fails when the two drift apart, and so does
CI.

## Code Style Guidelines

### Language & Framework
- Protocol is language-agnostic; the specification is normative, not the code
- Reference implementation: Go, in `go/` (Go 1.24, module `github.com/vrinek/Dialog/go`)
- Dependencies: standard library plus `golang.org/x/crypto`; the CBOR codec is hand-rolled on purpose
- Prefer deterministic, widely-supported languages for reference code

### Specification Writing Style

**Document Format:**
- Use RFC 2119 keywords: MUST, MUST NOT, SHOULD, SHOULD NOT, MAY
- Each spec file starts with: Version, Status, Abstract
- Use CDDL (RFC 8610) for CBOR schema definitions
- Include worked examples for all data types

**Conventions:**
- **Terminology**: Define terms in "Terminology" section before use
- **Fixed parameters**: Document in tables with "Parameter | Value" format
- **Examples**: Use hex encoding for binary data, include calculations
- **CDDL**: Comment with `;` for inline notes
- **Diagrams**: Use ASCII art for architecture diagrams

**Formatting:**
- Headings: Title Case for H1, sentence case for H2+
- Code blocks: Use ` ```cddl ` for CDDL, ` ``` ` for examples
- Tables: Use GFM table syntax with aligned columns
- Line length: ~80-100 characters for readability

**Naming:**
- Filenames: `NN-descriptive-name.md` (zero-padded numbers)
- CDDL types: `lowercase-with-dashes` or `camelCase`
- Field names: Use CBOR string keys in CDDL maps

**Cross-references:**
- Link to other spec docs: `[01-data-model.md](01-data-model.md)`
- Link to external RFCs: `[RFC 2119](https://...)`

### Error Handling
- Validation errors: Clear, actionable messages naming the rule that failed
- Content addressing failures: Distinguish encoding vs hashing errors
- Cryptographic failures: Fail secure, don't leak partial state

## Git Workflow

- Commit messages: Use conventional format
- Co-authored commits: Include `Co-Authored-By: Claude...` when appropriate
- Spec changes: Update version and status headers
- TODOs: Create numbered files in `todos/` directory
- Releases: tag **twice** at the same commit — `vX.Y.Z` for the spec and its
  PDF, `go/vX.Y.Z` for the Go module. A subdirectory module resolves for
  `go get` only through a tag carrying its directory prefix, so a bare
  `vX.Y.Z` releases nothing to Go users. See README, "Releases"

## Writing New Spec Sections

1. Follow existing document structure (Abstract → Terminology → Overview → Specification)
2. Use CDDL for all data structure definitions
3. Provide at least one worked example with actual values
4. Document all fixed parameters in a table
5. Cross-reference related spec documents

## When Adding Code

1. Follow existing code style in the codebase; document each type and function
   with the spec section it implements
2. Implement content-addressing exactly per spec — bytes first, then digest,
   then CID
3. Encode with the `dcbor` package; a general-purpose CBOR library does not
   implement Dialog's profile and MUST NOT be substituted for it
4. Cover the new behaviour with tests, and add a conformance vector if it fixes
   bytes another implementation would have to reproduce
5. Keep this file's commands accurate if the toolchain changes
