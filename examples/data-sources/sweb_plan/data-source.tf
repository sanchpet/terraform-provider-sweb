# Resolve a configurator spec (CPU/RAM/disk) to a SpaceWeb plan id, so the VPS is
# described by readable resources instead of a magic plan number.
data "sweb_plan" "small" {
  cpu      = 2
  ram      = 6  # GB
  disk     = 15 # GB
  category = 1  # 1=NVMe (default), 2=HDD, 3=Turbo
}

resource "sweb_vps" "example" {
  alias        = "web-01"
  plan         = data.sweb_plan.small.id
  distributive = 164 # debian-13
  datacenter   = 1   # spb
}
