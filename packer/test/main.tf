terraform {
  required_version = ">= 1.0"

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

resource "digitalocean_droplet" "pai_test" {
  name   = "pai-test"
  region = var.region
  size   = var.droplet_size
  image  = var.snapshot_id

  monitoring = true
  ipv6       = true

  ssh_keys = [var.ssh_fingerprint]
  tags     = ["pai", "test", "packer"]

  user_data = templatefile("${path.module}/cloud-init-test.yaml", {
    claude_oauth_token     = var.claude_oauth_token
    telegram_bot_token     = var.telegram_bot_token
    telegram_allowed_users = var.telegram_allowed_users
    tailscale_auth_key     = var.tailscale_auth_key
    elevenlabs_api_key     = var.elevenlabs_api_key
  })
}

output "droplet_ip" {
  value = digitalocean_droplet.pai_test.ipv4_address
}

output "droplet_id" {
  value = digitalocean_droplet.pai_test.id
}
