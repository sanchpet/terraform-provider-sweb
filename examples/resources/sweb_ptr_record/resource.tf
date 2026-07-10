# Manage the reverse-DNS (PTR) record of a public IP the account already owns.
# Destroying the resource resets the PTR to the provider default.
resource "sweb_ptr_record" "mail" {
  ip  = "203.0.113.7" # from `sweb vps ip list <vps>`
  ptr = "mail.example.com"
}
