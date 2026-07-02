# Changelog

## [0.5.0](https://github.com/sanchpet/terraform-provider-sweb/compare/v0.4.1...v0.5.0) (2026-07-02)


### Features

* add sweb_vps_local_network resource (private network attach) ([#21](https://github.com/sanchpet/terraform-provider-sweb/issues/21)) ([cad325e](https://github.com/sanchpet/terraform-provider-sweb/commit/cad325e0bb58bae8b9e68bfa378e01cf31bb9642))

## [0.4.1](https://github.com/sanchpet/terraform-provider-sweb/compare/v0.4.0...v0.4.1) (2026-07-02)


### Bug Fixes

* serialize VPS creation to keep the List-diff correlation correct ([#19](https://github.com/sanchpet/terraform-provider-sweb/issues/19)) ([a415336](https://github.com/sanchpet/terraform-provider-sweb/commit/a415336262fdbd4c77a8d5266c6c52d0403d754f))

## [0.4.0](https://github.com/sanchpet/terraform-provider-sweb/compare/v0.3.0...v0.4.0) (2026-07-02)


### Features

* add sweb_plan data source resolving configurator spec to plan id ([#17](https://github.com/sanchpet/terraform-provider-sweb/issues/17)) ([d3020d3](https://github.com/sanchpet/terraform-provider-sweb/commit/d3020d341227096814f75f568de6216e06491e5b))

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
