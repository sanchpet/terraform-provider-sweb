# Manage a shared-hosting website (document root) on a domain the account owns.
# Identified by doc_root; alias updates in place, the binding forces replacement.
resource "sweb_site" "shop" {
  alias    = "shop"
  doc_root = "shop"
  domain   = "example.com"
  # machine              = "www"  # optional subdomain label (forces replace)
  # enable_redis_session = true   # store PHP sessions in Redis (forces replace)
}
