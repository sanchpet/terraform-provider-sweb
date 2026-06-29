terraform {
  required_providers {
    sweb = {
      source = "sanchpet/sweb"
    }
  }
}

# Credentials resolve from config or the environment:
#   token            -> SWEB_TOKEN     (one-off, no refresh)
#   login + password -> SWEB_LOGIN / SWEB_PASSWORD (transparent token refresh)
provider "sweb" {
  login    = var.sweb_login
  password = var.sweb_password
}
