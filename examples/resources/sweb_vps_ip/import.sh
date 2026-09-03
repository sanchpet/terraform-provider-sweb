# Import an IP already attached to a VPS. The id is "<billing_id>:<ip>" — a VPS
# can hold several addresses, so the billing id alone does not identify one.
terraform import sweb_vps_ip.exit_ru login_vps_18:203.0.113.7
