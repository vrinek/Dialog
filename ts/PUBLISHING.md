# Publishing `dialog-protocol`

This package ships compiled JavaScript with `.d.ts` declarations, built from
`src/**/*.ts` by `npm run build` (`tsc -p tsconfig.build.json`) into `dist/`.
`dist/` is gitignored — it is generated fresh at publish time by the
`prepublishOnly` script, never committed.

## Pre-flight checklist

- [x] **LICENSE file.** `package.json` declares `"license": "Apache-2.0"` and
      `ts/LICENSE` carries the full license text (a copy of the repo-root
      `LICENSE`). npm auto-includes a root-level `LICENSE` file in the
      tarball without needing an entry in `files`; confirmed with
      `npm pack --dry-run`, which lists `LICENSE` alongside `dist/` and
      `README.md`.
- [ ] `version` in `ts/package.json` matches the release you intend
      (currently `0.8.0`).
- [ ] Working tree is clean and this commit is the one you want published
      (npm publishes exactly what's on disk, not what's committed, but keep
      them in sync).
- [ ] `npm run typecheck && npm test` pass (456 tests as of this writing).
- [ ] `npm run build` succeeds and `dist/` looks right
      (`find dist -type f`).
- [ ] `npm pack --dry-run` shows only `dist/`, `README.md`, `LICENSE`, and
      `package.json` — no `test/`, `scripts/`, or `.ts` sources.
- [ ] You are logged in to npm as an account/org with publish rights to the
      `dialog-protocol` name (unclaimed as of this writing — first publish
      registers it).

## Commands

From `ts/`:

```sh
# One-time: log in to npm (opens a browser, or prompts for a token)
npm login

# Sanity check what will ship
npm run typecheck
npm test
npm run build
npm pack --dry-run

# Publish. `prepublishOnly` re-runs typecheck, test, and build automatically.
npm publish --access public
```

`dialog-protocol` is an unscoped package name, so `--access public` is not
strictly required (unscoped packages default to public), but pass it anyway —
it fails loudly instead of silently attempting a private publish if the name
is ever changed to a scoped one (e.g. `@vrinek/dialog-protocol`).

### Provenance

If publishing from GitHub Actions with OIDC configured (not currently set up
for this repo), add `--provenance` to `npm publish` to attach a signed
attestation linking the published artifact to the CI run and commit that
built it. Publishing from a local machine, as this checklist assumes, cannot
produce provenance — that requires npm's trusted-publisher CI integration.

### After publishing

- Verify: `npm view dialog-protocol`
- Tag the release in git if not already tagged (the main repo's release
  tagging convention is documented in the root `README.md`).
