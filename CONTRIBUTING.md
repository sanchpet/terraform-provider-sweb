# Contributing

## Commits & PR titles (Conventional Commits)

This repo uses [Conventional Commits](https://www.conventionalcommits.org/) with
[release-please](https://github.com/googleapis/release-please) for versioning and
the changelog. PRs are **squash-merged**, so the **PR title** becomes the commit
on `main` and drives the release — it MUST be a valid Conventional Commit (CI
enforces this):

```
<type>[optional scope]: <description>
```

- `feat:` → minor bump · `fix:` → patch bump
- `feat!:` / `fix!:` or a `BREAKING CHANGE:` footer → major bump
- `docs:` `chore:` `refactor:` `test:` `ci:` `perf:` → no release on their own

Examples: `feat: add in-place resize`, `fix: bump sweb-go-sdk to v0.6.1`.

## Branches

Branch from `main`; name it `<type>/<slug>`. Branches are deleted automatically
on merge.

## Releases — automated, do not hand-edit

Do **not** tag manually or edit `CHANGELOG.md`. release-please opens a *release
PR* that maintains the version and changelog from merged commits; merging it tags
the release and GoReleaser builds, GPG-signs and uploads the provider artifacts
(+ the registry manifest). The Terraform Registry ingests that release.

## Local checks

`mise run ci` (lint + test) and `mise run testacc` (mock-acceptance) must pass.
Never write tests that hit the real API — `create` bills and 24h-locks deletes.
