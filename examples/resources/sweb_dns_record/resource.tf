# Manage individual DNS records of a zone the account already owns. The record is
# identified by its content, so every attribute forces replacement — changing a
# value is a delete + create.

resource "sweb_dns_record" "www" {
  domain = "example.com"
  type   = "A"
  name   = "www" # empty or "@" for the apex
  value  = "203.0.113.10"
}

resource "sweb_dns_record" "mail" {
  domain   = "example.com"
  type     = "MX"
  value    = "mx1.example.com."
  priority = 10 # required for MX
}

resource "sweb_dns_record" "spf" {
  domain = "example.com"
  type   = "TXT"
  name   = "@"
  value  = "v=spf1 include:_spf.example.com ~all"
}
