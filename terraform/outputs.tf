output "droplet_ip" {
  description = "Public IP address (for SSH fallback)"
  value       = digitalocean_droplet.pai.ipv4_address
}

output "ssh_command" {
  description = "SSH command to connect to the droplet"
  value       = "ssh root@${digitalocean_droplet.pai.ipv4_address}"
}

output "tailscale_hostname" {
  description = "Access PAI via Tailscale at this hostname"
  value       = "pai-prod (on your tailnet)"
}

output "health_url" {
  description = "Health check URL (via Tailscale)"
  value       = "http://pai-prod:7777/health"
}
