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
docs/
  brainstorms/           # Design decision records
  plans/                 # Implementation plans
archive/                 # Superseded documents
todos/                   # Pending spec fixes (numbered)
```

## Build/Test Commands

This is a documentation project. No build/test commands exist yet.

When implementing reference code in the future:
- Run linting before committing
- Run full test suite before committing

## Code Style Guidelines (For Future Implementation)

### Language & Framework
- Protocol is language-agnostic
- Reference implementations: TBD
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

### Error Handling (For Future Code)
- Validation errors: Clear, actionable messages
- Content addressing failures: Distinguish encoding vs hashing errors
- Cryptographic failures: Fail secure, don't leak partial state

## Git Workflow

- Commit messages: Use conventional format
- Co-authored commits: Include `Co-Authored-By: Claude...` when appropriate
- Spec changes: Update version and status headers
- TODOs: Create numbered files in `todos/` directory

## Writing New Spec Sections

1. Follow existing document structure (Abstract → Terminology → Overview → Specification)
2. Use CDDL for all data structure definitions
3. Provide at least one worked example with actual values
4. Document all fixed parameters in a table
5. Cross-reference related spec documents

## When Adding Code

1. Add appropriate build/lint/test commands to this file
2. Follow existing code style in the codebase
3. Use CBOR libraries that support dCBOR deterministic encoding
4. Implement content-addressing exactly per spec
