# A cloud load balancer. Ordering bills; datacenter and plan_id force replacement,
# the algorithm/toggles and the backend servers/rules update in place.
resource "sweb_balancer" "web" {
  datacenter = 1
  type       = "roundrobin" # or "leastconn"
  plan_id    = 100

  server {
    ip     = "203.0.113.10"
    weight = 1
  }

  rule {
    protocol_balancer = "http"
    port_balancer     = "80"
    protocol_server   = "http"
    port_server       = "80"
  }
}
