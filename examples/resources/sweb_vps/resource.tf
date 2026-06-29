# Configurator mode: specify resources, the provider resolves the plan id.
resource "sweb_vps" "infra_hub" {
  cpu          = 2
  ram          = 6  # GB
  disk         = 15 # GB
  category     = 1  # 1=nvme, 2=hdd, 3=turbo
  distributive = 164 # debian-13
  datacenter   = 1   # 1=spb, 2=msk, 3=ams
  alias        = "infra-hub"
  ssh_key      = var.ssh_key_id

  timeouts {
    create = "15m"
  }
}

# Ready-made plan mode (mutually exclusive with cpu/ram/disk):
# resource "sweb_vps" "node" {
#   plan         = 379
#   distributive = 164
#   datacenter   = 1
#   alias        = "node-1"
# }

output "infra_hub_ip" {
  value = sweb_vps.infra_hub.ip
}
