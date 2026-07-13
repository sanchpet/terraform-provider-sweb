# Import an existing website by its document root (as shown by `sweb hosting site list`).
# The binding inputs (domain/machine/enable_redis_session) aren't per-site
# API-reported, so set them in your HCL afterwards.
terraform import sweb_site.shop shop
