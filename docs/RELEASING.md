# MiniWiki maintainer release process

This document describes the Git/GitHub release process **after** the first v0.24.2-beta release. The improvement kit advances the source version to **0.24.3-beta**, but does **not** create a new tag or publish a release. The existing v0.24.2-beta tag and downloads are preserved.

## How automation works

- `.github/workflows/ci.yml` continues to run on pushes and pull requests.
- `.github/workflows/release.yml` independently checks module integrity, runs Go tests with race detection and `go vet`, runs `govulncheck`, builds ten `CGO_ENABLED=0` executables, packages them with `scripts/package_release.py`, and validates SHA-256 checksums.
- **Manually starting the workflow produces a downloadable preview artifact but does not publish a GitHub Release.**
- **Pushing a new version tag** triggers the same checks and, only if they pass, publishes the archives and `SHA256SUMS` as a GitHub Release.
- Release jobs fail rather than overwriting an existing GitHub Release.

## 1. Develop and test changes

```sh
cd ~/Projects/miniwiki-beta-source/miniwiki_v24_2
git status
go test -race ./...
go vet ./...
# Make sure the working tree is clean before tagging.
```

Commit changes and push normally to `main`:

```sh
git add .
git diff --cached --check
git commit -m "Describe the change"
git push origin main
```

Review the regular CI run until it is green.

## 2. Preview the release pipeline without publishing

Choose the **next** version you plan to release. For example:

```sh
gh workflow run release.yml --repo Kevv-devv/miniwiki --ref main -f version=v0.24.3-beta
gh run list --repo Kevv-devv/miniwiki --limit 5
```

Once the run appears, watch it using its ID:

```sh
gh run watch RUN_ID --repo Kevv-devv/miniwiki
```

If successful, GitHub Actions will retain an artifact named `miniwiki-release-packages` for 14 days. This preview **does not make a public GitHub Release**.

## 3. Select a version and create its tag

For **v0.24.3-beta**, the improvement kit already updated `var version` in `cmd/wiki/main.go`. For subsequent releases, update that variable to the new value **without** the leading `v`. If the README has a version-specific download example, update it for the new release as part of the release commit. Ensure all changes are committed and pushed to `main`.

```sh
cd ~/Projects/miniwiki-beta-source/miniwiki_v24_2
# After editing the version and committing it:
go run ./cmd/wiki version
git status
```

Then tag the specific tested commit:

```sh
git tag -a v0.24.3-beta -m "MiniWiki v0.24.3-beta"
git push origin v0.24.3-beta
```

**Do not reuse or move `v0.24.2-beta`**, and do not push a release tag until its version matches `cmd/wiki/main.go`. The release workflow checks this before building. Make new tags for new releases, including security corrections.

## 4. Verify the published release

```sh
gh run list --repo Kevv-devv/miniwiki --limit 5
gh release view v0.24.3-beta --repo Kevv-devv/miniwiki \
  --json tagName,isPrerelease,isDraft,assets,url
```

Check for 11 assets (10 archives and `SHA256SUMS`), the expected tag, and `isDraft=false`. Download and checksum one platform archive, extract it into a **fresh directory**, and do a first-run browser smoke test. Cross-compilation alone isn't a runtime test.

## 5. Update documentation after publishing

Verify the README's release links and any version-specific instructions; commit and push any necessary changes to `main`. Add release-specific notes or a changelog if needed. Never edit or rebuild an already-published release in place when a new version tag is more appropriate.

## Troubleshooting and access

- If any build or test fails, the release-publishing step does not run. Fix the error on `main`, then tag a **new** commit with a new version.
- GitHub Actions must be enabled for this repository, and the workflow's publishing job needs permission to create releases (`contents: write`). The YAML explicitly requests this permission for the job.
- The workflow uses `GH_TOKEN` provided automatically by GitHub Actions; do **not** create, paste, or commit a personal access token.
- On a workflow `workflow_dispatch` preview, the requested version is embedded in the preview binaries, but no release tag is created.
- The repository's existing `v0.24.2-beta` release is not modified by adding the workflow. Workflows are not retroactively triggered for old tag pushes.
- The release workflow is deliberately self-contained and reruns checks before publication. This avoids publishing an untested tag just because the ordinary `main` branch passed earlier.

## Platform list

The supported automated packaging set is Linux AMD64/ARM64, Windows AMD64, macOS AMD64/ARM64, FreeBSD AMD64/ARM64, OpenBSD AMD64/ARM64, and NetBSD AMD64. NetBSD ARM64 is currently excluded because `modernc.org/libc` does not support that build target in the project's pinned dependency version.
