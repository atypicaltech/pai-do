# Persistent storage volume - survives droplet redeployments
resource "digitalocean_volume" "pai_data" {
  region                  = var.region
  name                    = "pai-data"
  size                    = 10 # GB
  initial_filesystem_type = "ext4"
  description             = "Persistent storage for PAI data"
  tags                    = ["pai", "prod"]
}

resource "digitalocean_droplet" "pai" {
  name   = "pai-prod"
  region = var.region
  size   = var.droplet_size
  image  = "ubuntu-24-04-x64"

  monitoring = true
  backups    = true
  backup_policy {
    plan = "daily"
    hour = 8 # 8 AM UTC (3-4 AM EST)
  }
  ipv6 = true

  ssh_keys = [var.ssh_fingerprint]
  tags     = ["pai", "prod", "automation"]

  timeouts {
    create = "20m"
    delete = "5m"
  }

  user_data = templatefile("${path.module}/cloud-init.yaml", {
    claude_oauth_token     = var.claude_oauth_token
    telegram_bot_token     = var.telegram_bot_token
    telegram_allowed_users = var.telegram_allowed_users
    tailscale_auth_key     = var.tailscale_auth_key
    volume_name            = digitalocean_volume.pai_data.name
    go_mod                 = file("${path.module}/../bridge-go/go.mod")
    go_sum                 = file("${path.module}/../bridge-go/go.sum")
    main_go                = file("${path.module}/../bridge-go/main.go")
    config_go              = file("${path.module}/../bridge-go/config.go")
    bot_go                 = file("${path.module}/../bridge-go/bot.go")
    session_go             = file("${path.module}/../bridge-go/session.go")
    format_go              = file("${path.module}/../bridge-go/format.go")
    memory_go              = file("${path.module}/../bridge-go/memory.go")
  })
}

# Attach volume to droplet
resource "digitalocean_volume_attachment" "pai_data" {
  droplet_id = digitalocean_droplet.pai.id
  volume_id  = digitalocean_volume.pai_data.id
}

# Minimal firewall - SSH only (Tailscale handles the rest)
resource "digitalocean_firewall" "pai" {
  name = "pai-firewall"

  droplet_ids = [digitalocean_droplet.pai.id]

  # SSH only
  inbound_rule {
    protocol         = "tcp"
    port_range       = "22"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }

  # All outbound (needed for Tailscale, Telegram API, npm/bun packages)
  outbound_rule {
    protocol              = "tcp"
    port_range            = "1-65535"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }

  outbound_rule {
    protocol              = "udp"
    port_range            = "1-65535"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }

  outbound_rule {
    protocol              = "icmp"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }
}
