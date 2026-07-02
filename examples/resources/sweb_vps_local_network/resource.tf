# Attach an existing VPS to the account private (local) network. Declarative —
# no re-create. SpaceWeb assigns the local IP; configure the guest NIC with it.
resource "sweb_vps_local_network" "infra_01" {
  billing_id = "login_vps_10" # from `sweb vps list` (BILLING_ID)
}

output "infra_01_local_ip" {
  value = sweb_vps_local_network.infra_01.local_ip
}
