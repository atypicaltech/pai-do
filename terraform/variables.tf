variable "do_token" {
  description = "DigitalOcean API token"
  type        = string
  sensitive   = true
}

variable "ssh_fingerprint" {
  description = "SSH key fingerprint for droplet access"
  type        = string
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

# Claude Code Authentication
variable "claude_oauth_token" {
  description = "Claude Code OAuth token from 'claude setup-token' (uses your subscription)"
  type        = string
  sensitive   = true
}

# Telegram Bot
variable "telegram_bot_token" {
  description = "Telegram bot token from @BotFather"
  type        = string
  sensitive   = true
}

variable "telegram_allowed_users" {
  description = "Comma-separated Telegram user IDs allowed to use the bot"
  type        = string
  default     = ""
}

# Tailscale
variable "tailscale_auth_key" {
  description = "Tailscale pre-auth key for joining tailnet"
  type        = string
  sensitive   = true
}

# ElevenLabs TTS
variable "elevenlabs_api_key" {
  description = "ElevenLabs API key for TTS voice synthesis"
  type        = string
  sensitive   = true
  default     = ""
}

# Packer base image
variable "snapshot_id" {
  description = "DigitalOcean snapshot ID from Packer build (pai-base-*)"
  type        = string
}
