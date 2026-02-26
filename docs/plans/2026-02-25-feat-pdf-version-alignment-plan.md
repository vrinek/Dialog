---
title: PDF Version Alignment with Git Tags
type: feat
status: active
date: 2026-02-25
origin: docs/brainstorms/2026-02-25-pdf-version-alignment-with-tags-brainstorm.md
---

# PDF Version Alignment with Git Tags

## Overview

Create a preprocessing script that injects the git tag version into all Dialog protocol specification markdown files before PDF generation. This ensures the PDF document's version header matches the tagged release version (e.g., v0.1.0) rather than showing the static spec version (1.0), aligning the release tag with the visible document version.

## Problem Statement / Motivation

Currently, when pushing a git tag like `v0.1.0`, the generated PDF still displays `**Version:** 1.0 (2026-02-20)` in the header of each spec document. This creates a disconnect between:
- The version indicated by the git tag (source of truth for releases)
- The version shown in the generated PDF document

**Impact:** Users downloading the PDF from a `v0.1.0` release see "Version: 1.0" in the document, causing confusion about which protocol version the document describes. The protocol specification version (1.0) and the release version (v0.1.0) are different concepts that need clear presentation.

## Proposed Solution

A standalone shell script (`scripts/inject-version.sh`) that:
1. Detects the current git tag from `GITHUB_REF` (CI) or `git describe --tags` (local)
2. Replaces the version string in all 8 spec files (README.md + spec/00-06)
3. Keeps the original spec date intact (the date is part of the spec, not the build)

The workflow is modified to call this script **only for tagged builds**, ensuring branch builds continue to show the development spec version.

### Workflow Integration

```
Push tag v0.1.0
    ↓
GitHub Actions triggered
    ↓
Check: Is this a tagged build? (GITHUB_REF starts with refs/tags/v)
    ↓
YES: Run scripts/inject-version.sh
    - Extract version from GITHUB_REF (v0.1.0)
    - Update all 8 files: **Version:** v0.1.0 (2026-02-20)
    ↓
Run build-pdf.sh (existing)
    - Files now have injected version
    - Generated PDF shows v0.1.0 in headers
    ↓
Upload artifacts / Create release
```

## Technical Considerations

### Script Architecture

```bash
scripts/inject-version.sh
├── Detect version source (priority order)
│   ├── GITHUB_REF env var (CI context)
│   └── git describe --tags (local fallback)
├── Validate version format (refs/tags/v* or v*)
├── Extract clean version (strip refs/tags/ prefix)
├── Validate VERSION env var or fail with error
├── For each target file (8 total)
│   └── Replace: <<VERSION>> → $TAG_VERSION
└── Exit 0 on success, 1 on failure
```

### Target Files (8 total)

All files will contain placeholder `**Version:** <<VERSION>>` which gets replaced during CI builds.

| # | File                        | Line to Replace                                         |
|---|-----------------------------|---------------------------------------------------------|
| 1 | README.md                   | Version line with `<<VERSION>>` placeholder             |
| 2 | spec/00-overview.md         | Line 3: `**Version:** <<VERSION>> \| **Status:** Draft` |
| 3 | spec/01-data-model.md       | Line 3: Same format                                     |
| 4 | spec/02-block-format.md     | Line 3: Same format                                     |
| 5 | spec/03-encoding.md         | Line 3: Same format                                     |
| 6 | spec/04-cryptography.md     | Line 3: Same format                                     |
| 7 | spec/05-processing-model.md | Line 3: Same format                                     |
| 8 | spec/06-meta-bonds.md       | Line 3: Same format                                     |

### Version String Pattern

**Format in all spec files (placeholder):**
```markdown
**Version:** <<VERSION>> | **Status:** Draft
```

**After injection (example with tag v0.1.0):**
```markdown
**Version:** v0.1.0 | **Status:** Draft
```

**Replacement pattern:** Simple string substitution of `<<VERSION>>` with the tag version. No regex needed.
```bash
sed -i "s/<<VERSION>>/$VERSION/g" "$file"
```

**Alternative (if VERSION contains slashes):**
```bash
sed -i "s|<<VERSION>>|$VERSION|g" "$file"
```

### Version Detection Logic

```bash
# Priority 1: GitHub Actions environment
if [ -n "$GITHUB_REF" ]; then
    if [[ "$GITHUB_REF" == refs/tags/v* ]]; then
        VERSION="${GITHUB_REF#refs/tags/}"
    else
        echo "Error: GITHUB_REF is set but does not point to a version tag"
        exit 1
    fi
# Priority 2: Local git tags
elif git describe --tags --exact-match HEAD 2>/dev/null; then
    VERSION=$(git describe --tags --exact-match HEAD)
    # Validate it starts with v
    if [[ ! "$VERSION" == v* ]]; then
        echo "Error: Current tag does not start with 'v'"
        exit 1
    fi
else
    echo "Error: No tag version available"
    echo "This script requires either:"
    echo "  - GITHUB_REF=refs/tags/v*.*.* (in CI)"
    echo "  - Current HEAD to be at a v*.*.* tag (locally)"
    exit 1
fi
```

### Workflow Conditional Logic

The workflow should only call `inject-version.sh` for tagged builds:

```yaml
- name: Inject version from tag
  if: startsWith(github.ref, 'refs/tags/v')
  run: |
    chmod +x scripts/inject-version.sh
    ./scripts/inject-version.sh
```

**Why conditional in workflow rather than script?**
- Clear intent: "version injection happens for tags only"
- Script can be strict: fails if no tag available
- Easier to debug workflow runs (step appears vs skipped)
- Non-tagged builds don't even attempt version detection

### Error Handling

| Scenario                         | Behavior                                                                  |
|----------------------------------|---------------------------------------------------------------------------|
| Tagged build, GITHUB_REF valid   | Script runs, version injected, continues                                  |
| Tagged build, GITHUB_REF invalid | Script fails, workflow stops                                              |
| Non-tagged build                 | Step skipped entirely, uses original versions                             |
| Script fails mid-injection       | Exit non-zero, workflow stops, partial changes discarded (fresh checkout) |
| File not found                   | Script fails with clear "File not found: spec/02-block-format.md"         |
| Pattern not found                | Script warns but continues (files may already be updated)                 |

### sed vs perl Decision

**Selected: sed with extended regex (`sed -E`)**

- **Pros:** Standard on all Unix systems, no additional dependencies
- **Cons:** Escaping can be tricky with special characters
- **Alternative considered:** perl -pe (more powerful regex but less portable)
- **Rationale:** The pattern is simple enough for sed; all target systems (GitHub Actions ubuntu-22.04, macOS dev machines) have GNU sed or BSD sed with -E support

**BSD vs GNU sed compatibility:**
- Both support `-E` for extended regex (standard since POSIX.1-2017)
- In-place editing: `-i ''` (BSD) vs `-i` (GNU)
- **Solution:** Use `-i.bak` then remove `.bak` files, works on both

## System-Wide Impact

### Interaction Graph

```
Git Tag Push (v0.1.0)
    ↓
GitHub Actions workflow
    ↓
Job: build-and-upload
    ├── Checkout repository
    ├── Install dependencies
    ├── [CONDITIONAL] Run scripts/inject-version.sh
    │   ├── Read GITHUB_REF=refs/tags/v0.1.0
    │   ├── VERSION=v0.1.0
    │   ├── sed -E -i.bak 's/\*\*Version:\*\* [0-9.]+/**Version:** v0.1.0/g' README.md
    │   ├── Same for spec/00-06 (8 files total)
    │   └── rm -f *.bak spec/*.bak
    ├── Run build-pdf.sh
    │   ├── Files now have injected version
    │   └── Generated PDF shows v0.1.0
    ├── Validate PDF
    └── Upload artifacts
    ↓
Job: release (conditional on tags)
    ├── Download artifacts
    └── Create release with PDF showing v0.1.0
```

### Error & Failure Propagation

1. **Version detection failure** (no tag, invalid tag) → Script exits with error message, workflow stops
2. **File not found** → Script exits with "File not found: spec/XX-*.md", workflow stops
3. **Pattern not found** → Script warns but continues (file may already have version, or format changed)
4. **sed failure** → Script exits, workflow stops
5. **Partial injection** (some files succeed, one fails) → Files remain in mixed state, but since CI uses fresh checkout, next run starts clean

### State Lifecycle Risks

- **No backup needed:** CI environments use fresh checkouts; modified files are never committed back
- **No rollback needed:** If injection fails, workflow stops before PDF generation
- **Local usage risk:** If developer runs script locally without committing, they may have modified files. Script prints clear warning at start.
- **Idempotency:** Running script twice produces same result (replacement is idempotent)

### API Surface Parity

No API changes - this is purely CI/CD preprocessing. The spec files on disk are modified in-memory during CI; the repository remains unchanged.

### Integration Test Scenarios

1. **Push tag v1.0.0** → Verify PDF shows "Version: v1.0.0" in headers
2. **Push to main branch** → Verify PDF still shows "Version: 1.0" (no injection)
3. **Push tag invalid-1.0** → Verify workflow fails at injection step
4. **Local run with no tag** → Script fails with clear error about missing tag
5. **Local run at tag v0.5.0** → Script succeeds, files modified locally (revert with git checkout)

## Acceptance Criteria

### Functional Requirements

- [ ] `scripts/inject-version.sh` created and executable
- [ ] Script detects version from `GITHUB_REF` when available (CI mode)
- [ ] Script detects version from `git describe --tags` when available (local mode)
- [ ] Script fails with clear error if no tag version available
- [ ] Script updates all 8 target files: README.md + spec/00-06
- [ ] Script preserves the spec date in parentheses (e.g., 2026-02-20)
- [ ] Script uses sed (or perl) with proper regex to match version pattern
- [ ] Script is idempotent (running twice produces same result)
- [ ] Workflow updated to call script only for tagged builds
- [ ] Workflow uses `if: startsWith(github.ref, 'refs/tags/v')` condition
- [ ] Workflow skips version injection for non-tagged builds
- [ ] PDF generated from tagged build shows injected version in all headers
- [ ] PDF generated from branch build shows original spec version

### Non-Functional Requirements

- [ ] Script has clear error messages for all failure modes
- [ ] Script works on both Ubuntu (CI) and macOS (local development)
- [ ] Script exits with code 0 on success, non-zero on failure
- [ ] Script handles spaces in paths correctly (quote variables)
- [ ] Script prints progress messages (e.g., "Injecting version v0.1.0 into 8 files...")
- [ ] Version injection adds <1 second to build time

## Success Metrics

- **Correctness:** 100% of tagged releases show the tag version in PDF headers
- **Reliability:** 0% of non-tagged builds have version injected
- **Usability:** Developer can run script locally and understand success/failure
- **Transparency:** Workflow logs clearly show when version injection runs vs skipped

## Dependencies & Risks

### Dependencies

- `sed` must support extended regex (`-E` flag) - available on macOS and Linux
- GitHub Actions `ubuntu-22.04` runner must have `git` installed (standard)
- Target files must exist at expected paths (README.md, spec/00-06)

### Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| sed regex doesn't match (format change) | Low | High | Script warns if no matches found; fails fast in CI |
| File paths change (spec files renamed) | Low | Medium | Script validates files exist before processing |
| Tag format changes (no longer v*.*.*) | Low | Medium | Document convention in AGENTS.md; validation logic adaptable |
| BSD/GNU sed incompatibility | Low | Low | Use `-i.bak` pattern, works on both |

## Implementation

### Files to Create

#### `scripts/inject-version.sh`

Main script implementing version detection and file injection:

```bash
#!/bin/bash
set -euo pipefail

# Detect version from GITHUB_REF or git tag
detect_version() {
    if [ -n "${GITHUB_REF:-}" ]; then
        if [[ "$GITHUB_REF" == refs/tags/v* ]]; then
            echo "${GITHUB_REF#refs/tags/}"
            return 0
        fi
        echo "Error: GITHUB_REF does not point to a version tag: $GITHUB_REF" >&2
        return 1
    fi
    
    if git describe --tags --exact-match HEAD 2>/dev/null | grep -q '^v'; then
        git describe --tags --exact-match HEAD
        return 0
    fi
    
    echo "Error: No version tag available. Expected GITHUB_REF=refs/tags/v* or HEAD at v* tag." >&2
    return 1
}

# Update version in a single file
update_file() {
    local file="$1"
    local version="$2"
    
    if [ ! -f "$file" ]; then
        echo "Error: File not found: $file" >&2
        return 1
    fi
    
    # Check if placeholder exists
    if ! grep -q "<<VERSION>>" "$file"; then
        echo "Error: Placeholder <<VERSION>> not found in $file" >&2
        return 1
    fi
    
    # Replace placeholder with actual version
    sed -i.bak "s|<<VERSION>>|$version|g" "$file"
    rm -f "$file.bak"
    
    echo "  ✓ Updated $file"
}

main() {
    local version
    version=$(detect_version) || exit 1
    
    echo "Injecting version $version into spec files..."
    
    local files=(
        "README.md"
        "spec/00-overview.md"
        "spec/01-data-model.md"
        "spec/02-block-format.md"
        "spec/03-encoding.md"
        "spec/04-cryptography.md"
        "spec/05-processing-model.md"
        "spec/06-meta-bonds.md"
    )
    
    for file in "${files[@]}"; do
        update_file "$file" "$version"
    done
    
    echo "✓ Version injection complete"
}

main "$@"
```

#### `scripts/` (directory)

New directory to contain utility scripts for the project.

### Files to Modify

#### `.github/workflows/build-and-release.yml`

Add conditional step between "Install dependencies" and "Build PDF":

```yaml
      - name: Inject version from tag
        if: startsWith(github.ref, 'refs/tags/v')
        run: |
          chmod +x scripts/inject-version.sh
          ./scripts/inject-version.sh
```

### MVP Implementation Steps

1. **Update version lines in all 8 files** to use placeholder:
   - Replace `**Version:** 1.0 (2026-02-20)` with `**Version:** <<VERSION>>`
   - Files: README.md, spec/00-overview.md through spec/06-meta-bonds.md
   
2. Create `scripts/` directory

3. Create `scripts/inject-version.sh` with:
   - Version detection (GITHUB_REF or git describe)
   - File validation and loop through 8 targets
   - sed-based placeholder replacement (`<<VERSION>>` → actual version)
   - Proper error handling and exit codes
   - Progress messages

4. Make script executable: `chmod +x scripts/inject-version.sh`

5. Update `.github/workflows/build-and-release.yml`:
   - Add conditional step before "Build PDF"
   - Use `if: startsWith(github.ref, 'refs/tags/v')`
   - Call script with proper permissions

6. Commit and push to test on a branch (script should be skipped)

7. Push a test tag (e.g., `v0.0.0-test`) to verify injection works

8. Verify PDF shows injected version in document headers

9. Delete test tag after verification

## Sources & References

### Origin

- **Brainstorm document:** [docs/brainstorms/2026-02-25-pdf-version-alignment-with-tags-brainstorm.md](docs/brainstorms/2026-02-25-pdf-version-alignment-with-tags-brainstorm.md) — Key decisions carried forward:
  - Approach C (Separate preprocessing script) selected for clean architecture
  - Script location: `scripts/inject-version.sh`
  - Files to update: README.md + spec/00-06 (8 total)
  - Version detection: GITHUB_REF first, git describe fallback
  - Placeholder pattern: `<<VERSION>>` for simple replacement
  - Non-tagged builds: skip injection entirely

### Internal References

- Workflow file: `.github/workflows/build-and-release.yml` — To be modified
- Build script: `build-pdf.sh` — No changes required, runs after injection
- Spec files: `spec/00-overview.md` through `spec/06-meta-bonds.md` — Target files for injection
- README.md: `README.md` — Also receives version injection
- AGENTS.md: Project conventions and commit guidelines

### External References

- GitHub Actions context docs: https://docs.github.com/en/actions/learn-github-actions/contexts
- sed manual (POSIX): https://pubs.opengroup.org/onlinepubs/9699919799/utilities/sed.html
- POSIX extended regex: https://pubs.opengroup.org/onlinepubs/9699919799/basedefs/V1_chap09.html

### Related Work

- Previous plan: [2026-02-25-feat-github-action-pdf-artifact-plan.md](2026-02-25-feat-github-action-pdf-artifact-plan.md) — The workflow being modified
- Brainstorm: [2026-02-24-github-action-pdf-artifact-brainstorm.md](../brainstorms/2026-02-24-github-action-pdf-artifact-brainstorm.md) — Original workflow design
