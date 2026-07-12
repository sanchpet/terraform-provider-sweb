# Manage a domain's redirect URL for a domain the account already owns.
# Destroying the resource clears the redirect.
resource "sweb_domain_redirect" "example" {
  domain = "example.com"
  url    = "https://example.org"
}
