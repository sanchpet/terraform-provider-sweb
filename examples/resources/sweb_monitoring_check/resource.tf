# An uptime-monitoring check. type forces replacement; target/name/interval,
# contacts and options update in place; enabled toggles activation.
resource "sweb_monitoring_check" "site" {
  type        = "http"
  target      = "https://example.com"
  name        = "example.com uptime"
  interval    = 60
  contact_ids = [sweb_monitoring_contact.ops.id]
  enabled     = true
}
