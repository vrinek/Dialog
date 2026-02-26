---
date: 2026-02-25
topic: pdf-version-alignment-with-tags
---

# PDF Version Alignment with Git Tags

## What We're Building

A preprocessing script that injects the git tag version into all Dialog protocol specification markdown files before PDF generation. This ensures the PDF document's version header matches the tagged release version (e.g., v0.1.0) rather than showing the static spec version (1.0).

**Problem:** Currently, when pushing tag `v0.1.0`, the generated PDF still displays `**Version:** 1.0 (2026-02-20)` in the header of each spec document. The git tag and document version are misaligned.

**Solution:** Create `scripts/inject-version.sh` that:
1. Detects if running from a git tag
2. Replaces the version string in all spec files (README.md + 00-06)
3. Is called by the GitHub Actions workflow before `build-pdf.sh`

## Why This Approach

**Approaches Considered:**

1. **Approach A (Workflow preprocessing):** Add sed commands directly in the workflow YAML. Pros: Simple, no extra files. Cons: Clutters workflow, harder to test locally, not reusable.

2. **Approach B (Modify build-pdf.sh):** Add version injection to the existing build script. Pros: Single script does everything. Cons: Changes existing tested script, adds complexity to core build logic, less flexible.

3. **Approach C - Separate preprocessing script (Selected):** Create dedicated `scripts/inject-version.sh`. Pros: Clean separation of concerns, testable independently, reusable for local builds, doesn't modify existing scripts, clear intent. Cons: One additional small file (acceptable trade-off).

**Decision:** Approach C selected for clean architecture and maintainability.

## Key Decisions

- **Script location:** `scripts/inject-version.sh` (new `scripts/` directory)
- **Files to update:** README.md, spec/00-overview.md through spec/06-meta-bonds.md (all 8 files)
- **Version detection:** Check `GITHUB_REF` env var for tag pattern (`refs/tags/v*`), fallback to `git describe --tags` for local use
- **String pattern:** Replace `**Version:** 1.0 (2026-02-20)` with `**Version:** $TAG_VERSION`
- **Date handling:** Keep original date from spec (it's the spec date, not the build date)
- **Non-tagged builds:** Script is not called; if invoked anyway, it fails with clear error message
- **Script behavior:** Requires tag version via env var or git; exits with error if missing
- **Workflow integration:** Only call script when `GITHUB_REF` matches tag pattern; skip entirely for branch builds
- **Backup strategy:** No backup needed - changes are in CI only (fresh checkout), files are gitignored outputs anyway

## Open Questions

None - all requirements are clear.

## Next Steps

→ `/workflows:plan` for implementation details
