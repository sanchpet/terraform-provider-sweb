# Manage an SRV record of a zone the account already owns. Like sweb_dns_record
# it is content-addressed, and every attribute forces replacement.
resource "sweb_dns_srv_record" "autodiscover" {
  domain   = "example.com"
  service  = "autodiscover"
  protocol = "tcp"
  target   = "autodiscover.example.com."
  port     = 443
  priority = 5
  weight   = 0
  ttl      = 86400
}
