# Manage a subdomain of a domain the account already owns.
# Destroying the resource removes the subdomain.
resource "sweb_subdomain" "shop" {
  domain  = "example.com"
  machine = "shop" # -> shop.example.com ("*" for a wildcard)
  dir     = "/shop" # optional site directory
}
