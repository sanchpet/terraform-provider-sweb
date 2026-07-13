# Manage a MySQL database on shared hosting.
# Identified by name; the API may store it under an account-prefixed full_name.
resource "sweb_database" "app" {
  name     = "appdb"
  password = var.database_password
  comment  = "application database" # optional; updates in place
  # version = "8.0"                  # optional; API default when omitted (forces replace)
}
