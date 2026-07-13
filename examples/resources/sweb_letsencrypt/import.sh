# Import an existing Let's Encrypt certificate by the domain it covers.
# The issuance inputs (wildcard/virtdom/ip/challenge) aren't recoverable from the
# API, so set them in your HCL afterwards.
terraform import sweb_letsencrypt.site example.com
