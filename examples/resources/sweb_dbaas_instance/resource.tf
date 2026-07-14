# A managed-database (DBaaS) cluster. Ordering bills; engine_type/engine_version
# force replacement, plan_id/display_name and the user set update in place.
resource "sweb_dbaas_instance" "app" {
  engine_type    = "postgresql"
  engine_version = "16"
  plan_id        = 200
  display_name   = "app-db"

  user {
    name     = "app"
    password = var.dbaas_password
  }
}
