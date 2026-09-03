# One additional public IP on a VPS. SpaceWeb picks the address, so `ip` is
# computed and rotating a burnt exit IP is a replacement of this resource:
#
#   terraform apply -replace=sweb_vps_ip.exit_ru     # or: terraform taint …
#
# Ordering bills, and an IP cannot be released within 24h of being ordered —
# create_before_destroy orders the new address first, so a rotation does not
# depend on the old one being releasable at that moment.
resource "sweb_vps_ip" "exit_ru" {
  billing_id = "login_vps_18" # from `sweb vps list` (BILLING_ID)

  lifecycle {
    create_before_destroy = true
  }
}

# The guest OS still needs the address on its interface (netplan/ifcfg).
output "exit_ru_ip" {
  value = sweb_vps_ip.exit_ru.ip
}

output "exit_ru_netmask" {
  value = sweb_vps_ip.exit_ru.netmask
}
