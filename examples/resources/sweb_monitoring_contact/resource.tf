# A monitoring notification contact. type and value force replacement; name
# updates in place. type is one of email, phone, telegram.
resource "sweb_monitoring_contact" "ops" {
  type  = "email"
  value = "ops@example.com"
  name  = "Ops mailbox"
}
