# Changelog

## [0.14.0](https://github.com/sanchpet/terraform-provider-sweb/compare/v0.13.0...v0.14.0) (2026-07-14)


### Features

* make mailbox and database passwords write-only (importable) ([#51](https://github.com/sanchpet/terraform-provider-sweb/issues/51)) ([83e21ad](https://github.com/sanchpet/terraform-provider-sweb/commit/83e21add90a7a5a89fee301fa68d8de26937c767))

## [0.13.0](https://github.com/sanchpet/terraform-provider-sweb/compare/v0.12.0...v0.13.0) (2026-07-14)


### Features

* cloud-tier resources — balancer, dbaas_instance, monitoring_check, monitoring_contact ([#49](https://github.com/sanchpet/terraform-provider-sweb/issues/49)) ([66b94ee](https://github.com/sanchpet/terraform-provider-sweb/commit/66b94ee9150a7c6a4efb8c0d11abe456219e5831))

## [0.12.0](https://github.com/sanchpet/terraform-provider-sweb/compare/v0.11.0...v0.12.0) (2026-07-13)


### Features

* add sweb_letsencrypt resource ([#48](https://github.com/sanchpet/terraform-provider-sweb/issues/48)) ([d0804c7](https://github.com/sanchpet/terraform-provider-sweb/commit/d0804c7f87fbfae6b341954182834b44181c8dc0))
* hosting database, site and cron resources ([#47](https://github.com/sanchpet/terraform-provider-sweb/issues/47)) ([a088c41](https://github.com/sanchpet/terraform-provider-sweb/commit/a088c41b0e3bc3d6345e0f12df69d1679b8a04f4))
* **mailbox:** add sweb_mailbox resource ([#45](https://github.com/sanchpet/terraform-provider-sweb/issues/45)) ([86459e0](https://github.com/sanchpet/terraform-provider-sweb/commit/86459e0e2fc79f2d812c9c7de15bca3826609638))

## [0.11.0](https://github.com/sanchpet/terraform-provider-sweb/compare/v0.10.0...v0.11.0) (2026-07-13)


### Features

* tf-dns-import helper to adopt an existing DNS zone ([#42](https://github.com/sanchpet/terraform-provider-sweb/issues/42)) ([1a43064](https://github.com/sanchpet/terraform-provider-sweb/commit/1a43064d0bf487b4f0d0345cb0670b809e0b74e2))

## [0.10.0](https://github.com/sanchpet/terraform-provider-sweb/compare/v0.9.0...v0.10.0) (2026-07-12)


### Features

* add sweb_dns_srv_record for SRV records ([#40](https://github.com/sanchpet/terraform-provider-sweb/issues/40)) ([1b00e84](https://github.com/sanchpet/terraform-provider-sweb/commit/1b00e840ce96a6e3a3cf95595b7327a25d85fe76))

## [0.9.0](https://github.com/sanchpet/terraform-provider-sweb/compare/v0.8.0...v0.9.0) (2026-07-12)


### Features

* add sweb_dns_record for A/AAAA/CNAME/MX/TXT/NS records ([#38](https://github.com/sanchpet/terraform-provider-sweb/issues/38)) ([f7c7a92](https://github.com/sanchpet/terraform-provider-sweb/commit/f7c7a92ca21caeca567669f86b321bd03dbc1348))

## [0.8.0](https://github.com/sanchpet/terraform-provider-sweb/compare/v0.7.0...v0.8.0) (2026-07-12)


### Features

* add domain subdomain/redirect resources and a domain data source ([#36](https://github.com/sanchpet/terraform-provider-sweb/issues/36)) ([c4ae45f](https://github.com/sanchpet/terraform-provider-sweb/commit/c4ae45fe525651e0c7c694d49984ed99edc6d12c))

## [0.7.0](https://github.com/sanchpet/terraform-provider-sweb/compare/v0.6.0...v0.7.0) (2026-07-10)


### Features

* add sweb_vps_backup_settings resource ([#34](https://github.com/sanchpet/terraform-provider-sweb/issues/34)) ([a698c3d](https://github.com/sanchpet/terraform-provider-sweb/commit/a698c3d5de21f4cc9dbe20edfcf4262e0fca7a09))

## [0.6.0](https://github.com/sanchpet/terraform-provider-sweb/compare/v0.5.3...v0.6.0) (2026-07-10)


### Features

* add sweb_ptr_record resource ([#32](https://github.com/sanchpet/terraform-provider-sweb/issues/32)) ([a113965](https://github.com/sanchpet/terraform-provider-sweb/commit/a1139656480423872b67904050469b2ebac2cb22))

## [0.5.3](https://github.com/sanchpet/terraform-provider-sweb/compare/v0.5.2...v0.5.3) (2026-07-02)


### Bug Fixes

* correct ssh_key description (public key content, not a key id) ([#27](https://github.com/sanchpet/terraform-provider-sweb/issues/27)) ([cc7c7c7](https://github.com/sanchpet/terraform-provider-sweb/commit/cc7c7c761e67e12ad66e90cca45e9698dc0982c2))

## [0.5.2](https://github.com/sanchpet/terraform-provider-sweb/compare/v0.5.1...v0.5.2) (2026-07-02)


### Bug Fixes

* bump sweb-go-sdk to v0.8.2 (local_ip object-shape decode) ([#25](https://github.com/sanchpet/terraform-provider-sweb/issues/25)) ([0db49c4](https://github.com/sanchpet/terraform-provider-sweb/commit/0db49c4f78bbf2d24610da049026552b88b77dbc))

## [0.5.1](https://github.com/sanchpet/terraform-provider-sweb/compare/v0.5.0...v0.5.1) (2026-07-02)


### Bug Fixes

* bump sweb-go-sdk to v0.8.1 (IP price FlexFloat) ([#23](https://github.com/sanchpet/terraform-provider-sweb/issues/23)) ([1f186df](https://github.com/sanchpet/terraform-provider-sweb/commit/1f186df440dd00e4b51ca3faa0fb1ca8fbdf71f7))

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
