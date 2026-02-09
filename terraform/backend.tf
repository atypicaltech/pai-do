terraform {
  required_version = ">= 1.0"

  backend "s3" {
    endpoint                    = "https://nyc3.digitaloceanspaces.com"
    key                         = "pai/terraform.tfstate"
    bucket                      = "atypical-tf"
    region                      = "us-east-1"
    skip_credentials_validation = true
    skip_metadata_api_check     = true
    skip_region_validation      = true
  }

  required_providers {
    digitalocean = {
      source  = "digitalocean/digitalocean"
      version = "~> 2.0"
    }
  }
}

provider "digitalocean" {
  token = var.do_token
}
