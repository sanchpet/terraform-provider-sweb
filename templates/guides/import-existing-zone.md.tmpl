---
page_title: "Importing an existing DNS zone"
subcategory: "Guides"
description: |-
  Adopt an account's existing DNS zone into declarative Terraform, drift-free, in one pass.
---

# Importing an existing DNS zone

You do not have to recreate an existing zone by hand. The `tf-dns-import` helper
turns a live zone dump into Terraform `import {}` blocks, and Terraform then
synthesizes the resource HCL for you. The whole zone lands in declarative config
with a `terraform plan` that reports **no changes**.

## Prerequisites

- The [`sweb` CLI](https://github.com/sanchpet/sweb) (for the zone dump and token).
- Terraform 1.5+ (for `import {}` blocks and `-generate-config-out`).

## 1. Point Terraform at the right account

SpaceWeb serves multiple accounts (a cloud/VPS panel and a hosting/mail+domains
panel) from one API. **Domains live on the hosting account.** If you configure the
provider with the wrong account's credentials, every read fails with
`-32500 Нет доступа к домену` ("no access to domain") — the request is
authenticated but that account cannot see the zone.

Mint a token for the account that owns the domain and hand it to the provider —
via a variable, not a swapped environment variable:

```terraform
variable "sweb_token" {
  type      = string
  sensitive = true
}

provider "sweb" {
  token = var.sweb_token
}
```

```sh
export TF_VAR_sweb_token="$(sweb token --profile hosting)"
```

Managing zones on more than one account? Give each its own provider instance with
an `alias` and set `provider = sweb.<alias>` on each resource.

## 2. Generate the import blocks

Install the helper (it ships in this repo) and pipe the zone into it:

```sh
go install github.com/sanchpet/terraform-provider-sweb/cmd/tf-dns-import@latest

sweb dns records example.com -o json \
  | tf-dns-import example.com > imports.tf
```

Each record becomes one block. The helper knows the provider's content-addressed
id grammar, so round-robin records (same host, several values), a TXT whose host
lives in the wire `domain` field (DKIM), apex records, and SRV all get correct,
unique ids:

```terraform
import {
  to = sweb_dns_record.a_apex_222_43_142
  id = "example.com/A//77.222.43.142"
}

import {
  to = sweb_dns_srv_record.autodiscover_tcp_apex
  id = "example.com/autodiscover/tcp//autodiscover.spaceweb.ru./443"
}
```

## 3. Synthesize the HCL and verify

Let Terraform write the resource configuration from the live state:

```sh
terraform plan -generate-config-out=generated.tf
```

Review `generated.tf`, then apply the import and confirm a clean follow-up plan:

```sh
terraform apply     # imports into state; import reads the API, it does not mutate it
terraform plan      # -> "No changes. Your infrastructure matches the configuration."
```

A second plan reporting **No changes** is the proof that the zone now lives in
Terraform byte-for-byte. From here, edit `generated.tf` and manage the zone
declaratively.

## Notes

- **Identity is content, not index.** SpaceWeb addresses records by a per-type
  index that shifts as the zone changes, so the resources key on
  `type + host + value` (SRV on `service + protocol + host + target + port`).
  Every attribute forces replacement — changing a value is a delete + create.
- **MX priority** is not encoded in the import id. Set it in config; `Read`
  reconciles it after import.
