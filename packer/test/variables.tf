variable "do_token" {
  description = "DigitalOcean API token"
  type        = string
  sensitive   = true
}

variable "snapshot_id" {
  description = "Packer snapshot ID to deploy"
  type        = string
}

variable "ssh_fingerprint" {
  description = "SSH key fingerprint for droplet access"
  type        = string
}

variable "tailscale_auth_key" {
  description = "Tailscale pre-auth key for joining tailnet"
  type        = string
  sensitive   = true
}

variable "region" {
  description = "DigitalOcean region"
  type        = string
  default     = "nyc3"
}

variable "droplet_size" {
  description = "Droplet size"
  type        = string
  default     = "s-1vcpu-2gb"
}
