# Import an existing MySQL database by its name (as shown by `sweb hosting db list`).
# The password is write-only and not API-reported, so set it in your HCL afterwards.
terraform import sweb_database.app appdb
