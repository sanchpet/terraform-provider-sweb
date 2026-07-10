# Manage a VPS's auto-backup schedule. Destroying the resource resets it to
# manual (auto-backups off).
resource "sweb_vps_backup_settings" "infra_01" {
  billing_id = "login_vps_10" # from `sweb vps list` (BILLING_ID)
  mode       = "auto"
  frequency  = 7 # every 7 days
  time       = 3 # at 03:00
}
