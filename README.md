# PAI on DigitalOcean

Self-hosted Claude Code + Telegram Bridge on DigitalOcean, deployed via OpenTofu and GitHub Actions.

## Architecture

- **Compute**: DigitalOcean Droplet (s-2vcpu-4gb, Ubuntu 24.04)
- **Storage**: 10GB persistent DO Volume at `/mnt/pai-data`
- **Network**: Tailscale VPN mesh (no public exposure except SSH)
- **Security**: UFW firewall (SSH only), fail2ban, SSH key-only auth
- **Runtime**: Claude Code (native binary) + PAI Bridge (Go static binary)
- **Interface**: Telegram bot via TelegramBridge daemon
- **Auth**: Claude subscription via OAuth token (no metered API billing)
- **State**: OpenTofu state stored in DO Spaces

## How It Works

```
You (Telegram) → Bot API → TelegramBridge daemon → Claude Code CLI → Anthropic API
                                                  ↓
                                           VPS sandbox (~/projects)
```

The bridge is a lightweight Go binary (~30MB) that:
1. Long-polls Telegram for messages
2. Spawns `claude -p` subprocesses per message
3. Streams responses back to Telegram with HTML formatting
4. Manages conversation sessions with `--resume`
5. Logs conversations, generates summaries, and injects prior context into new sessions

Claude Code runs against your subscription (Pro/Max), not metered API.

## Memory System

The bridge implements multi-layer memory for session continuity:

```
/mnt/pai-data/memory/
  conversations/{userID}/{sessionID}.jsonl   # Every turn logged
  summaries/{userID}/{date}-{sessionID}.md   # Claude-generated session summaries
  daily/{userID}/{YYYY-MM-DD}.md             # Daily append-only notes
```

- **Conversation logging** — every message exchange is written to JSONL on the persistent volume
- **Pre-death flush** — when sessions timeout, get `/clear`ed, or the bridge shuts down, Claude summarizes the conversation into a durable markdown file
- **Cross-session context** — new sessions load the last 5 summaries + today's/yesterday's daily notes, so Claude knows what happened before
- **Daily reset** — sessions reset at 4 AM (configurable) instead of short idle timeouts

## Required GitHub Secrets

| Secret | Description | Where to Get |
|--------|-------------|--------------|
| `DO_TOKEN` | DigitalOcean API token | DO Dashboard → API → Generate New Token |
| `DO_SPACES_ACCESS_KEY` | Spaces access key for Terraform state | DO Dashboard → Spaces → Manage Keys |
| `DO_SPACES_SECRET_KEY` | Spaces secret key for Terraform state | DO Dashboard → Spaces → Manage Keys |
| `SSH_FINGERPRINT` | SSH key fingerprint (MD5 format) | DO Dashboard → Settings → Security → SSH Keys |
| `CLAUDE_OAUTH_TOKEN` | Claude Code OAuth token | Run `claude setup-token` locally |
| `TELEGRAM_BOT_TOKEN` | Telegram bot token | @BotFather on Telegram |
| `TELEGRAM_ALLOWED_USERS` | Telegram user IDs (quoted, comma-separated) | Send `/start` to @userinfobot |
| `TAILSCALE_AUTH_KEY` | Tailscale pre-auth key | login.tailscale.com/admin/settings/keys |

### Getting the Claude OAuth Token

```bash
# On your local machine (requires browser)
claude setup-token

# This outputs a token like: sk-ant-oat01-...
# Save it as the CLAUDE_OAUTH_TOKEN secret
```

### Telegram Allowed Users Format

The `TELEGRAM_ALLOWED_USERS` secret should be quoted user IDs, comma-separated:
```
"123456789", "987654321"
```

## Deployment

Push to `main` branch and trigger the workflow manually (workflow_dispatch).

### What Gets Deployed

1. DigitalOcean Droplet with Ubuntu 24.04
2. Persistent volume attached and mounted
3. Tailscale VPN connection (hostname: `pai-prod`)
4. Claude Code native binary (auto-updates)
5. PAI Bridge binary (downloaded from latest GitHub release)
6. Bridge running as systemd service
7. Health check server on port 7777

## Post-Deployment

### Verify Setup

```bash
# SSH via Tailscale
ssh root@pai-prod

# Check cloud-init completed
cloud-init status
cat /var/log/pai-setup.log

# Check bridge service
systemctl status pai-telegram-bridge
journalctl -u pai-telegram-bridge -f

# Check health
curl http://localhost:7777/health

# Check Tailscale
tailscale status
```

### Service Management

```bash
# Restart bridge
systemctl restart pai-telegram-bridge

# View logs
journalctl -u pai-telegram-bridge -f

# Stop bridge
systemctl stop pai-telegram-bridge
```

### Telegram Bot Commands

The bot registers these commands automatically:

| Command | Description |
|---------|-------------|
| `/start` | Show bridge info |
| `/status` | Current session status |
| `/clear` | End current session |
| `/cd /path` | Change working directory |
| `/sessions` | List active sessions |

### Supported Input

- **Text messages** — regular chat
- **Photos** — image analysis
- **PDFs** — document analysis
- **Text files** — code, markdown, CSV, JSON, etc.

## Cost

~$30/month infrastructure + your Claude subscription:
- Droplet (s-2vcpu-4gb): $24/mo
- Volume (10GB): $1/mo
- Backups: ~$5/mo

## Versioning

This project uses [CalVer](https://calver.org/) with the format `vYYYY.MM.PATCH`.

To create a release:
```bash
git tag v2026.02.1 && git push --tags
```

The patch number increments within a month: `v2026.02.1`, `v2026.02.2`, `v2026.02.3`, etc.

## Local Development

```bash
cd terraform
tofu init
tofu plan
tofu apply
```
