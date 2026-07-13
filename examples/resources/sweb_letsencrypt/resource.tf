# Issue a free Let's Encrypt certificate for a domain on shared hosting.
# Identified by the domain it covers; issuance is asynchronous (Create waits).
resource "sweb_letsencrypt" "site" {
  domain      = "example.com"
  autoprolong = true # renew automatically before expiry (updates in place)
  # wildcard  = true          # request a wildcard cert (forces replacement)
  # virtdom   = "sub.example.com"
  # challenge = "acme"        # "acme" | "dns"
}
