# Publishing `dialog-protocol`

This package ships compiled JavaScript with `.d.ts` declarations, built from
`src/**/*.ts` by `npm run build` (`tsc -p tsconfig.build.json`) into `dist/`.
`dist/` is gitignored — it is generated fresh at publish time by the
`prepublishOnly` script, never committed.

## Pre-flight checklist

- [ ] **LICENSE file.** `package.json` declares `"license": "MIT"` but there is
      no `LICENSE` file anywhere in the repository (checked both `ts/` and the
      repo root). Add one before publishing — npm will publish without it, but
      a declared license with no license file is a real gap for consumers and
      for `npm`/OSS-scanner tooling. Once added at the repo root, either copy
      it into `ts/LICENSE` or add `"license"`-adjacent text to `ts/README.md`;
      `files` in `package.json` does not currently list a `LICENSE` entry
      because there is nothing to list yet — add one if you place the file at
      `ts/LICENSE`.
- [ ] `version` in `ts/package.json` matches the release you intend
      (currently `0.8.0`).
- [ ] Working tree is clean and this commit is the one you want published
      (npm publishes exactly what's on disk, not what's committed, but keep
      them in sync).
- [ ] `npm run typecheck && npm test` pass (454 tests as of this writing).
- [ ] `npm run build` succeeds and `dist/` looks right
      (`find dist -type f`).
- [ ] `npm pack --dry-run` shows only `dist/`, `README.md`, and
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
