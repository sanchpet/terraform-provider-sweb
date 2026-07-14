# Manage a mailbox on a mail domain the account already owns.
# The mailbox is identified by domain + local part (the label before @).
resource "sweb_mailbox" "info" {
  domain   = "example.com"
  name     = "info"           # -> info@example.com
  password            = var.mailbox_password
  password_wo_version = 1 # bump with a new password to rotate
  antispam = "medium"         # hard | medium | soft | off (default off)
  spf      = true             # enable SPF filtering
  comment  = "shared inbox"   # optional free-text note
}
