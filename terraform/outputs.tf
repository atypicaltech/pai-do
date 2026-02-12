output "droplet_ip" {
  description = "Public IP (no SSH — Tailscale only)"
  value       = digitalocean_droplet.pai.ipv4_address
}

output "ssh_command" {
  description = "SSH command via Tailscale"
  value       = "ssh root@pai-prod"
}

output "tailscale_hostname" {
  description = "Access PAI via Tailscale at this hostname"
  value       = "pai-prod (on your tailnet)"
}

output "health_url" {
  description = "Health check URL (via Tailscale)"
  value       = "http://pai-prod:7777/health"
}
