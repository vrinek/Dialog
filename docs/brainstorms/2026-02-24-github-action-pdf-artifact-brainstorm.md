---
date: 2026-02-24
topic: github-action-pdf-artifact
---

# GitHub Action for PDF Generation and Artifact Publishing

## What We're Building

A GitHub Actions workflow that automatically generates the Dialog protocol specification PDF on every push to `origin/main` and publishes it as both a workflow artifact (for immediate access) and as a release attachment (for permanent, versioned storage). The workflow will use the existing `build-pdf.sh` script, which combines all markdown spec files and converts them to PDF using Chromium headless mode.

## Why This Approach

**Approaches Considered:**

1. **Approach A (Selected): Simple Two-Job Workflow** - Single workflow file with two jobs: one for artifact upload on main branch pushes, one for release attachments on version tags. Pros: Simple, maintainable, follows GitHub Actions conventions. Cons: None significant for this use case.

2. **Approach B: Modular Multi-File Workflows** - Separate workflow files for build, main, and release. Pros: Clear separation of concerns. Cons: Unnecessary complexity for a documentation repo, creates duplication.

3. **Approach C: Artifact + Git Commit** - Uploads artifact and commits PDF to a separate branch. Pros: Permanent history in git. Cons: Requires write permissions, pollutes git history with generated files.

**Decision:** Approach A was chosen because it provides the right balance of functionality and simplicity. The two-job design cleanly separates continuous integration (artifact upload) from release management (versioned attachments) while keeping everything in one maintainable file.

## Key Decisions

- **Trigger Strategy**: Workflow triggers on `push` to any branch (generates artifacts for all branches) and on `workflow_dispatch` for manual runs. Release job triggers only on tags matching `v*.*.*` pattern (creates permanent release attachments).
- **Artifact Retention**: Workflow artifacts use default 90-day retention (sufficient for CI purposes). Release attachments are permanent.
- **Build Environment**: Use `ubuntu-latest` runner with `pandoc` installed via `apt-get` (not pre-installed) and system Chromium (also via `apt-get`).
- **Artifact Naming**: PDF named with commit SHA for workflow artifacts (e.g., `dialog-protocol-spec-abc1234.pdf`), simple name for releases.
- **Parallel HTML Generation**: Also generate and upload HTML version alongside PDF for web access.

## Open Questions

None - all requirements are clear.

## Next Steps

→ `/workflows:plan` for implementation details
