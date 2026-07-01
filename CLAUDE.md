# terraform-provider-sweb — instructions for Claude Code

Terraform provider for the SpaceWeb (sweb.ru) hosting API. The third layer of the
stack `sweb-go-sdk ← { sweb CLI, this provider }`. Built on the **Terraform Plugin
Framework** (not legacy SDKv2) over `github.com/sanchpet/sweb-go-sdk`.

## Architecture (boundary discipline)

The provider is a thin adapter between Terraform's declarative model and the
imperative JSON-RPC API. All transport/auth lives in the SDK; the provider only
maps the resource lifecycle and smooths three API leaks:

- `main.go` — `providerserver.Serve` at `registry.terraform.io/sanchpet/sweb`.
- `internal/provider/provider.go` — provider schema + `Configure` (builds the
  `*sweb.Client` from `token` or `login`+`password`, `endpoint` override).
- `internal/provider/vps_resource.go` — the `sweb_vps` resource (CRUD + import).

### Three API leaks the provider closes

1. **`Create` returns nothing usable** → correlate the new node via a **List-diff**
   (snapshot billing ids before create, find the new one after). Never trust the
   raw create body. Assumes a single writer (personal infra).
2. **Async provisioning** (`is_running`/`current_action`) → **poll until ready**
   with a `timeouts` block (create default 15m).
3. **24h post-create delete lock** (`-32500`) → hard diagnostic, keep the resource
   in state. Never silently orphan a billed VPS.

### Resource identity & import

- Identity = `BillingID` (`login_vps_N`); it is the Terraform id and the delete /
  import key. `uid` is a computed reference.
- Import reconstructs **plan-mode** config (a resolved `plan_id` is always
  available). `Read` is **mode-aware**: it refreshes only inputs already set in
  state, so configurator and plan resources don't adopt each other's fields.
  `category` is not API-reported, so its drift is not detected.

## Build & test (mise-first)

```sh
mise install
mise run build
mise run test       # unit
mise run testacc    # TF_ACC=1: full lifecycle vs the httptest mock (provider_test.go)
mise run lint
```

**Testing is mock-acceptance:** `terraform-plugin-testing` drives the real TF
lifecycle (plan/apply/import/destroy) against an in-memory SpaceWeb mock. Do **not**
write tests that hit the real API — `create` bills and locks deletes for 24h.

## Conventions

- **English** for all repo artifacts (code, comments, docs, commits, PRs).
- Commits: small and focused; `--signoff` + `Co-Authored-By: Claude` (personal repo).
- **Branch + PR**; do not self-merge — merging is the owner's call.
- **Conventional Commits + release-please (BLOCKING):** commit / PR-title format is
  `<type>[scope]: <desc>` (`feat`→minor, `fix`→patch, `!` or `BREAKING CHANGE`→major).
  PRs are squash-merged, so the **PR title is the release commit** — CI enforces its
  format (`pr-title` workflow). Versioning and `CHANGELOG.md` are automated by
  **release-please** (merging its release PR tags + runs GoReleaser → registry) —
  never `git tag` or edit the changelog by hand. See `CONTRIBUTING.md`.
- Keep the resource schema and `README`/`examples/` in sync; the registry renders
  `docs/` (generate with `tfplugindocs` when added).

## Security / opsec (BLOCKING)

- **No real account data in the repo.** Test fixtures are synthetic (TEST-NET IPs
  `203.0.113.0/24`, fake billing ids). Never commit tokens/credentials.
- Releases are GPG-signed for the Terraform Registry (`GPG_PRIVATE_KEY` /
  `PASSPHRASE` secrets; `GPG_FINGERPRINT` wired in the release workflow).
