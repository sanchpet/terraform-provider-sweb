# terraform-provider-sweb

[![CI](https://github.com/sanchpet/terraform-provider-sweb/actions/workflows/ci.yml/badge.svg)](https://github.com/sanchpet/terraform-provider-sweb/actions/workflows/ci.yml)

Terraform provider for the [SpaceWeb](https://sweb.ru) (sweb.ru) hosting API.
Manage VPS instances declaratively. Built on the
[Terraform Plugin Framework](https://developer.hashicorp.com/terraform/plugin/framework)
over [`sweb-go-sdk`](https://github.com/sanchpet/sweb-go-sdk).

## Usage

```hcl
terraform {
  required_providers {
    sweb = {
      source = "sanchpet/sweb"
    }
  }
}

provider "sweb" {
  login    = var.sweb_login    # or $SWEB_LOGIN
  password = var.sweb_password # or $SWEB_PASSWORD
}

resource "sweb_vps" "infra_hub" {
  cpu          = 2
  ram          = 6
  disk         = 15
  category     = 1   # 1=nvme, 2=hdd, 3=turbo
  distributive = 164 # debian-13
  datacenter   = 1   # 1=spb, 2=msk, 3=ams
  alias        = "infra-hub"
}
```

### Authentication

SpaceWeb issues short-lived session tokens (no refresh-token flow). Two modes:

| Mode | Config | Env | Behaviour |
|------|--------|-----|-----------|
| Credentials (recommended) | `login` + `password` | `SWEB_LOGIN` / `SWEB_PASSWORD` | SDK re-exchanges for a fresh token transparently when the session expires |
| Token | `token` | `SWEB_TOKEN` | One-off; fails once the session expires |

`endpoint` (or `SWEB_ENDPOINT`) overrides the API root for staging/testing.

### The `sweb_vps` resource

Provision either via the **configurator** (`cpu` + `ram` + `disk` [+ `category`])
or a ready-made **`plan`** id — the two are mutually exclusive. Common inputs:
`distributive`, `datacenter`, `alias`, optional `ssh_key`, `ip_count`. Computed:
`billing_id` (= resource id), `uid`, `name`, `ip`, `running`.

### Importing

```sh
terraform import sweb_vps.infra_hub petrovpet2_vps_10
```

The id is the `billing_id` (`login_vps_N`) shown by `sweb vps list`. Import
reconstructs a **plan-mode** config (the resolved `plan` id is always available
from the API). Notes:

- Switching the imported resource to the configurator (`cpu`/`ram`/`disk`) is fine
  — those update in place (resize), so it does not force a replace.
- `ssh_key` is create-only and not recoverable from the API; re-state it in HCL.
- Use `terraform plan -generate-config-out=...` to materialise matching HCL.

## In-place updates & limitations

- **In-place:** `alias` (rename) and `plan` / `cpu` / `ram` / `disk` (resize via
  `changePlan`) update without a replacement. The resize is asynchronous — the
  provider waits until it settles (`Modify → ExtIpAdd → …`).
- **Disk grows only:** the API refuses shrinking a disk; the provider rejects a
  disk decrease at apply with a clear error.
- **Forces replacement:** `category` (storage tier), `distributive` (OS),
  `datacenter`, `ssh_key` and `ip_count`.
- **24h delete lock:** a freshly created VPS cannot be destroyed for 24h; the
  provider surfaces a clear error and keeps the resource in state.

## Development

```sh
mise install          # Go + golangci-lint + terraform + pre-commit (pinned)
mise run build        # go build ./...
mise run test         # unit tests
mise run testacc      # mock-acceptance: full TF lifecycle vs an httptest backend
mise run lint
pre-commit install && pre-commit run -a
```

Acceptance tests run against an in-memory mock of the SpaceWeb API — they never
touch the real service (which bills and locks deletes for 24h).

## License

MIT © Aleksandr Petrov
