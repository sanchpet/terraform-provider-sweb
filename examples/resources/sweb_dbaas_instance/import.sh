# Import an existing cluster by its billing id (as shown by `sweb dbaas list`).
# User passwords are write-only; set them in your HCL afterwards.
terraform import sweb_dbaas_instance.app petrovpet2_dbaas_10
