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

variable "claude_oauth_token" {
  description = "Claude Code OAuth token"
  type        = string
  sensitive   = true
}

variable "telegram_bot_token" {
  description = "Telegram bot token"
  type        = string
  sensitive   = true
}

variable "telegram_allowed_users" {
  description = "Comma-separated Telegram user IDs"
  type        = string
  default     = ""
}

variable "elevenlabs_api_key" {
  description = "ElevenLabs API key for TTS"
  type        = string
  sensitive   = true
  default     = ""
}

variable "region" {
  description = "DigitalOcean region"
  type        = string
  default     = "nyc3"
}

variable "droplet_size" {
  description = "Droplet size"
  type        = string
  default     = "s-2vcpu-4gb"
}
