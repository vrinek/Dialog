---
title: GitHub Action for PDF Generation and Artifact Publishing
type: feat
status: active
date: 2026-02-25
origin: docs/brainstorms/2026-02-24-github-action-pdf-artifact-brainstorm.md
---

# GitHub Action for PDF Generation and Artifact Publishing

## Overview

Create a GitHub Actions workflow that automatically generates the Dialog protocol specification PDF and HTML on every push to any branch. The workflow publishes artifacts for all branches and creates release attachments for version tags. This provides immediate access to built specs during development and permanent archives for releases.

## Problem Statement / Motivation

Currently, building the PDF requires running `build-pdf.sh` locally with pandoc and Chromium installed. This creates friction for:
- Contributors wanting to preview spec changes
- Maintainers validating PRs
- Users accessing the latest spec without building locally

A CI workflow solves this by automating builds and making artifacts universally accessible.

## Proposed Solution

A single GitHub Actions workflow file (`.github/workflows/build-and-release.yml`) with two jobs:

1. **build-and-upload**: Runs on every push to any branch + manual trigger. Generates PDF (HTML is intermediate), uploads PDF as workflow artifact.
2. **release**: Runs only on tags matching `v*.*.*`. Downloads PDF artifact from build job, creates/updates GitHub release with PDF attachment.

**Job Dependency**: The release job depends on the build job completing successfully, ensuring the same artifacts are used for both workflow artifacts and release attachments (consistency).

## Technical Considerations

### Architecture

```yaml
Workflow: build-and-release.yml
├── Job: build-and-upload (always runs)
│   ├── Install dependencies (pandoc, chromium)
│   ├── Run build-pdf.sh
│   ├── Validate outputs exist and non-empty
│   └── Upload artifacts (PDF + HTML)
└── Job: release (runs on tags only)
    ├── needs: build-and-upload
    ├── Download artifacts
    ├── Create/update GitHub release
    └── Upload PDF/HTML to release
```

### Build Environment

- **Runner**: `ubuntu-22.04` (pinned for reproducibility, not `latest`)
- **Dependencies**:
  - `pandoc` (v3.x via apt)
  - `chromium-browser` (via apt)
- **Script**: `build-pdf.sh` (existing, requires no modifications)

### Trigger Strategy

| Event | Triggers Build | Triggers Release |
|-------|----------------|------------------|
| Push to any branch | ✅ Yes | ❌ No |
| Push tag `v*.*.*` | ✅ Yes | ✅ Yes |
| `workflow_dispatch` (manual) | ✅ Yes | ❌ No |
| Pull Request | ❌ No (intentionally skipped) | ❌ No |

**Rationale for skipping PR builds:** PDF generation is relatively expensive (Chromium + pandoc). PR changes can be validated via the push-to-branch build once merged. This saves CI minutes while still ensuring every merged commit has artifacts.

### Artifact Management

- **Workflow artifacts**: Named with `dialog-protocol-spec-${{ github.sha }}.pdf`
- **Retention**: 30 days (configured via `retention-days`)
- **Release attachments**: Named simply `dialog-protocol-spec.pdf`
- **HTML**: Generated but not uploaded (intermediate format for PDF generation)
- **Format**: PDF only uploaded

### Version Tag Pattern

**Pattern:** `v[0-9]+.[0-9]+.[0-9]+*` (semantic versioning with optional suffixes)

Matches:
- `v1.0.0` ✅
- `v1.0.0-beta.1` ✅
- `v1.0` ❌ (not full semver)
- `release-1.0.0` ❌ (no v prefix)

### Error Handling

**Build failures:**
- Exit codes from `build-pdf.sh` propagated to workflow
- Validation step checks PDF/HTML exist and are >1KB
- Failed builds block release job (via `needs` dependency)

**Release failures:**
- If release already exists for tag: skip attachment upload (artifacts still available)
- Network timeouts: GitHub Actions default retry logic applies

## System-Wide Impact

### Interaction Graph

```
Git Push Event
    ↓
GitHub Actions (workflow triggered)
    ↓
Job: build-and-upload
    ├── apt-get install pandoc chromium-browser
    ├── ./build-pdf.sh
│   ├── pandoc (markdown → HTML → intermediate)
│   └── chromium --headless --print-to-pdf (HTML → PDF)
└── upload-artifact action (PDF only)
    ↓
Job: release (conditional on tags)
    ├── download-artifact action (from previous job)
    └── softprops/action-gh-release (create/update release)
```

### Error & Failure Propagation

1. **Dependency installation failure** → Job fails, no artifacts, release job skipped
2. **pandoc failure** → build-pdf.sh exits non-zero → job fails, release skipped
3. **chromium failure** → Same as above
4. **Artifact upload failure** → Job fails, release job skipped
5. **Release creation failure** → Release job fails, but artifacts still available from workflow

### State Lifecycle Risks

- **Partial build**: Validation step ensures PDF exists and is non-empty before upload
- **Stale artifacts**: 30-day retention prevents indefinite accumulation
- **Tag re-creation**: If tag deleted and re-created after artifact expiry, release job may fail (acceptable - user should create new tag)

### API Surface Parity

No API changes - this is purely CI/CD automation.

### Integration Test Scenarios

1. **Push to feature branch** → Verify artifacts appear in Actions tab
2. **Push tag v1.0.0** → Verify release created with PDF attachment
3. **Manual workflow_dispatch** → Verify artifacts generated, no release
4. **Build failure** → Verify workflow fails, no release created
5. **Tag push on existing release** → Verify attachments updated or skipped gracefully

## Acceptance Criteria

### Functional Requirements

- [ ] Workflow file `.github/workflows/build-and-release.yml` created and committed
- [ ] Workflow triggers on push to any branch
- [ ] Workflow triggers on tags matching `v*.*.*`
- [ ] Workflow triggers on `workflow_dispatch`
- [ ] Build job installs pandoc and chromium successfully
- [ ] Build job runs `build-pdf.sh` without errors
- [ ] PDF artifact uploaded (named with commit SHA)
- [ ] PDF is non-empty (validation step passes)
- [ ] Release job only runs on tags (not on regular branch pushes)
- [ ] Release job creates/updates GitHub release with PDF attachment
- [ ] Release attachments use simple naming (no SHA in filename)

### Non-Functional Requirements

- [ ] Artifact retention set to 30 days (not default 90)
- [ ] Ubuntu 22.04 runner pinned (not `latest`)
- [ ] Build completes in <5 minutes (typical runtime)
- [ ] Workflow uses minimal permissions (only `contents: write` for releases)

## Success Metrics

- **Availability**: Every push to main has accessible PDF artifact within 5 minutes
- **Reliability**: <5% workflow failure rate over first month
- **Adoption**: First release using automated workflow within 2 weeks of deployment

## Dependencies & Risks

### Dependencies

- `build-pdf.sh` must remain in repository root (existing)
- GitHub Actions ubuntu-22.04 runner must support apt-get packages
- `softprops/action-gh-release` action (community-maintained, stable)

### Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| pandoc/chromium not in apt repos | Low | High | Pin to specific Ubuntu version, monitor for changes |
| Build takes too long (>10 min) | Medium | Low | Parallelize if needed, cache dependencies |
| Release action unmaintained | Low | Medium | Can switch to `actions/create-release` or API calls |
| Large PDF exceeds limits | Low | Medium | Current PDF is ~570KB, far from 500MB limit |

## Implementation

### Files to Create

#### `.github/workflows/build-and-release.yml`

Main workflow file implementing the two-job design with proper triggers, dependencies, and artifact handling.

#### `.github/workflows/` (directory)

New directory to contain the workflow file.

### MVP Implementation Steps

1. Create `.github/workflows/` directory
2. Create `build-and-release.yml` with:
   - Triggers: push, workflow_dispatch
   - Job 1: build-and-upload (install deps, run script, validate, upload)
   - Job 2: release (needs job 1, conditional on tags, download, release)
3. Commit and push to trigger initial test run
4. Verify artifacts appear in Actions tab
5. Push a test tag to verify release job
6. Document usage in README (optional, can be separate PR)

## Sources & References

### Origin

- **Brainstorm document:** [docs/brainstorms/2026-02-24-github-action-pdf-artifact-brainstorm.md](docs/brainstorms/2026-02-24-github-action-pdf-artifact-brainstorm.md) — Key decisions carried forward:
  - Approach A (Simple Two-Job Workflow) selected for maintainability
  - Artifact generation triggers on any branch push
  - Release attachments only on version tags (`v*.*.*`)
  - Build dependencies: pandoc and chromium via apt-get
  - PDF generated via HTML intermediate (HTML not uploaded as artifact)

### Internal References

- Build script: `build-pdf.sh` — Combines markdown files, generates PDF via Chromium
- Spec files: `README.md`, `spec/00-overview.md` through `spec/06-meta-bonds.md`
- AGENTS.md conventions for commits and workflow

### External References

- GitHub Actions documentation: https://docs.github.com/en/actions
- `actions/upload-artifact`: https://github.com/actions/upload-artifact
- `actions/download-artifact`: https://github.com/actions/download-artifact
- `softprops/action-gh-release`: https://github.com/softprops/action-gh-release

### Related Work

None - this is a new CI/CD setup.
