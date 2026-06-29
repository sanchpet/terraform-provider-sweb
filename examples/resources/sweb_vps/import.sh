# Import an existing VPS by its billing id (the "login_vps_N" service id, as
# shown by `sweb vps list`). The provider reconstructs a plan-mode config; verify
# the result against your HCL (and adjust to configurator mode if desired).
terraform import sweb_vps.infra_hub petrovpet2_vps_10
