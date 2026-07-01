# Changelog

## [0.3.0](https://github.com/sanchpet/terraform-provider-sweb/compare/v0.2.4...v0.3.0) (2026-07-01)


### Features

* in-place resize of plan/cpu/ram/disk via changePlan ([#12](https://github.com/sanchpet/terraform-provider-sweb/issues/12)) ([99cb1d9](https://github.com/sanchpet/terraform-provider-sweb/commit/99cb1d9a13b56798a764e25176fd9b0b213d0c5c))

## Changelog

From v0.2.5 on, this file is maintained automatically by
[release-please](https://github.com/googleapis/release-please) from
[Conventional Commit](https://www.conventionalcommits.org/) messages — see
[CONTRIBUTING.md](CONTRIBUTING.md).

## Releases up to 0.2.4 (pre-automation)

- **0.2.4** — bump sweb-go-sdk to v0.6.0 (configurator sold-out guard).
- **0.2.3** — bump sweb-go-sdk to v0.3.0 (hardened `List` decode: FlexInt/FlexFloat, reconciled index).
- **0.2.2** — bump sweb-go-sdk to v0.2.1 (float64 money fields; fixes the fractional-price decode crash).
- **0.2.1** — clean rename plan (stable computed fields kept out of the diff).
- **0.2.0** — in-place alias rename (`alias` updatable via the API `rename`).
- **0.1.2** — manifest listed in `SHA256SUMS` (Terraform Registry-ingestable).
