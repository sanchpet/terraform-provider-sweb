# Read a domain already on the account — expiry, registrar, auto-prolongation,
# docroot, and any configured redirect.
data "sweb_domain" "example" {
  domain = "example.com"
}

output "domain_expires" {
  value = data.sweb_domain.example.expired
}
