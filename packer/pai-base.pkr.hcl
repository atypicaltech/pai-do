packer {
  required_plugins {
    digitalocean = {
      version = ">= 1.4.0"
      source  = "github.com/digitalocean/digitalocean"
    }
  }
}

variable "do_token" {
  type      = string
  sensitive = true
}

variable "region" {
  type    = string
  default = "nyc3"
}

variable "base_image" {
  type    = string
  default = "ubuntu-24-04-x64"
}

variable "droplet_size" {
  type    = string
  default = "s-2vcpu-4gb"
}

source "digitalocean" "pai-base" {
  api_token     = var.do_token
  image         = var.base_image
  region        = var.region
  size          = var.droplet_size
  ssh_username  = "root"
  snapshot_name = "pai-base-{{timestamp}}"
  snapshot_regions = [var.region]
  tags          = ["pai", "packer"]
}

build {
  sources = ["source.digitalocean.pai-base"]

  # 1. System packages (no golang-go — we install Go from official tarball below)
  provisioner "shell" {
    inline = [
      "export DEBIAN_FRONTEND=noninteractive",
      # Wait for any existing apt/dpkg processes to finish (unattended-upgrades, cloud-init, etc.)
      "echo 'Waiting for apt lock...'",
      "while fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1 || fuser /var/lib/apt/lists/lock >/dev/null 2>&1; do echo 'apt locked, sleeping 5s...'; sleep 5; done",
      "echo 'apt lock acquired'",
      "apt-get update",
      "apt-get upgrade -y",
      "apt-get install -y ufw fail2ban curl jq ffmpeg unzip nmap masscan nikto sqlmap dnsrecon hydra git libpcap-dev",
    ]
  }

  # 2. Install Go from official tarball
  provisioner "shell" {
    inline = [
      # Pin to Go 1.23.x — Go 1.24 breaks bytedance/sonic (used by nuclei via gin)
      "GO_VERSION=$(curl -fsSL 'https://go.dev/dl/?mode=json&include=all' | jq -r '[.[].version | select(startswith(\"go1.23\"))] | first')",
      "curl -fsSL \"https://go.dev/dl/$${GO_VERSION}.linux-amd64.tar.gz\" -o /tmp/go.tar.gz",
      "tar -C /usr/local -xzf /tmp/go.tar.gz",
      "rm -f /tmp/go.tar.gz",
      "ln -sf /usr/local/go/bin/go /usr/local/bin/go",
      "ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt",
      "go version",
    ]
  }

  # 3. Create pai user
  provisioner "shell" {
    inline = [
      "useradd --system --shell /bin/bash --home-dir /home/pai --create-home pai",
      "mkdir -p /home/pai/.local/bin /home/pai/go/bin",
      "chown -R pai:pai /home/pai",
    ]
  }

  # 4. Fail2ban config
  provisioner "file" {
    source      = "files/jail.local"
    destination = "/etc/fail2ban/jail.local"
  }

  # 5. GitHub CLI
  provisioner "shell" {
    inline = [
      "GH_VERSION=$(curl -fsSL https://api.github.com/repos/cli/cli/releases/latest | jq -r '.tag_name' | sed 's/^v//')",
      "curl -fsSL \"https://github.com/cli/cli/releases/download/v$${GH_VERSION}/gh_$${GH_VERSION}_linux_amd64.tar.gz\" -o /tmp/gh.tar.gz",
      "tar -xzf /tmp/gh.tar.gz -C /tmp/",
      "cp /tmp/gh_$${GH_VERSION}_linux_amd64/bin/gh /home/pai/.local/bin/gh",
      "chmod +x /home/pai/.local/bin/gh",
      "rm -rf /tmp/gh.tar.gz /tmp/gh_*",
    ]
  }

  # 6. DigitalOcean CLI
  provisioner "shell" {
    inline = [
      "DOCTL_VERSION=$(curl -fsSL https://api.github.com/repos/digitalocean/doctl/releases/latest | jq -r '.tag_name' | sed 's/^v//')",
      "curl -fsSL \"https://github.com/digitalocean/doctl/releases/download/v$${DOCTL_VERSION}/doctl-$${DOCTL_VERSION}-linux-amd64.tar.gz\" -o /tmp/doctl.tar.gz",
      "tar -xzf /tmp/doctl.tar.gz -C /home/pai/.local/bin/",
      "chmod +x /home/pai/.local/bin/doctl",
      "rm -f /tmp/doctl.tar.gz",
    ]
  }

  # 7. Bun runtime
  provisioner "shell" {
    inline = [
      "su - pai -c 'curl -fsSL https://bun.sh/install | bash'",
    ]
  }

  # 8. ProjectDiscovery suite (Go tools — the big time saver)
  provisioner "shell" {
    inline = [
      "su - pai -c 'export GOPATH=/home/pai/go && export PATH=\"$PATH:/usr/local/go/bin:/home/pai/go/bin\" && go install github.com/projectdiscovery/subfinder/v2/cmd/subfinder@latest'",
      "su - pai -c 'export GOPATH=/home/pai/go && export PATH=\"$PATH:/usr/local/go/bin:/home/pai/go/bin\" && go install github.com/projectdiscovery/httpx/cmd/httpx@latest'",
      "su - pai -c 'export GOPATH=/home/pai/go && export PATH=\"$PATH:/usr/local/go/bin:/home/pai/go/bin\" && go install github.com/projectdiscovery/nuclei/v3/cmd/nuclei@latest'",
      "su - pai -c 'export GOPATH=/home/pai/go && export PATH=\"$PATH:/usr/local/go/bin:/home/pai/go/bin\" && go install github.com/projectdiscovery/naabu/v2/cmd/naabu@latest'",
      "su - pai -c 'export GOPATH=/home/pai/go && export PATH=\"$PATH:/usr/local/go/bin:/home/pai/go/bin\" && go install github.com/projectdiscovery/katana/cmd/katana@latest'",
      "su - pai -c 'export GOPATH=/home/pai/go && export PATH=\"$PATH:/usr/local/go/bin:/home/pai/go/bin\" && go install github.com/projectdiscovery/dnsx/cmd/dnsx@latest'",
      "su - pai -c 'export GOPATH=/home/pai/go && export PATH=\"$PATH:/usr/local/go/bin:/home/pai/go/bin\" && go install github.com/projectdiscovery/tlsx/cmd/tlsx@latest'",
      "su - pai -c 'export GOPATH=/home/pai/go && export PATH=\"$PATH:/usr/local/go/bin:/home/pai/go/bin\" && go install github.com/projectdiscovery/cdncheck/cmd/cdncheck@latest'",
      "su - pai -c 'export GOPATH=/home/pai/go && export PATH=\"$PATH:/usr/local/go/bin:/home/pai/go/bin\" && go install github.com/projectdiscovery/asnmap/cmd/asnmap@latest'",
      "su - pai -c 'export GOPATH=/home/pai/go && export PATH=\"$PATH:/usr/local/go/bin:/home/pai/go/bin\" && go install github.com/projectdiscovery/mapcidr/cmd/mapcidr@latest'",
      "su - pai -c 'export GOPATH=/home/pai/go && export PATH=\"$PATH:/usr/local/go/bin:/home/pai/go/bin\" && go install github.com/projectdiscovery/pdtm/cmd/pdtm@latest'",
    ]
  }

  # 9. Fuzzing and content discovery
  provisioner "shell" {
    inline = [
      "su - pai -c 'export GOPATH=/home/pai/go && export PATH=\"$PATH:/usr/local/go/bin:/home/pai/go/bin\" && go install github.com/ffuf/ffuf/v2@latest'",
    ]
  }

  # 10. URL harvesting
  provisioner "shell" {
    inline = [
      "su - pai -c 'export GOPATH=/home/pai/go && export PATH=\"$PATH:/usr/local/go/bin:/home/pai/go/bin\" && go install github.com/tomnomnom/waybackurls@latest'",
      "su - pai -c 'export GOPATH=/home/pai/go && export PATH=\"$PATH:/usr/local/go/bin:/home/pai/go/bin\" && go install github.com/lc/gau/v2/cmd/gau@latest'",
    ]
  }

  # 11. Secret detection
  provisioner "shell" {
    inline = [
      # trufflehog: go install fails due to replace directives in go.mod, use pre-built binary
      "TRUFFLEHOG_VERSION=$(curl -fsSL https://api.github.com/repos/trufflesecurity/trufflehog/releases/latest | jq -r '.tag_name' | sed 's/^v//')",
      "curl -fsSL \"https://github.com/trufflesecurity/trufflehog/releases/download/v$${TRUFFLEHOG_VERSION}/trufflehog_$${TRUFFLEHOG_VERSION}_linux_amd64.tar.gz\" -o /tmp/trufflehog.tar.gz",
      "tar -xzf /tmp/trufflehog.tar.gz -C /home/pai/.local/bin/ trufflehog",
      "chmod +x /home/pai/.local/bin/trufflehog",
      "rm -f /tmp/trufflehog.tar.gz",
      "su - pai -c 'export GOPATH=/home/pai/go && export PATH=\"$PATH:/usr/local/go/bin:/home/pai/go/bin\" && go install github.com/gitleaks/gitleaks/v8@latest'",
    ]
  }

  # 12. Web crawling and utilities
  provisioner "shell" {
    inline = [
      "su - pai -c 'export GOPATH=/home/pai/go && export PATH=\"$PATH:/usr/local/go/bin:/home/pai/go/bin\" && go install github.com/jaeles-project/gospider@latest'",
      "su - pai -c 'export GOPATH=/home/pai/go && export PATH=\"$PATH:/usr/local/go/bin:/home/pai/go/bin\" && go install github.com/tomnomnom/anew@latest'",
      "su - pai -c 'export GOPATH=/home/pai/go && export PATH=\"$PATH:/usr/local/go/bin:/home/pai/go/bin\" && go install github.com/tomnomnom/qsreplace@latest'",
      "su - pai -c 'export GOPATH=/home/pai/go && export PATH=\"$PATH:/usr/local/go/bin:/home/pai/go/bin\" && go install github.com/tomnomnom/unfurl@latest'",
    ]
  }

  # 13. testssl.sh
  provisioner "shell" {
    inline = [
      "git clone --depth 1 https://github.com/drwetter/testssl.sh.git /opt/testssl",
      "ln -sf /opt/testssl/testssl.sh /usr/local/bin/testssl",
    ]
  }

  # 14. Tailscale (install binary only, auth happens at deploy time)
  provisioner "shell" {
    inline = [
      "curl -fsSL https://tailscale.com/install.sh | sh",
    ]
  }

  # 15. Nuclei templates (warm cache so first scan isn't slow)
  provisioner "shell" {
    inline = [
      "su - pai -c 'export PATH=\"$PATH:/home/pai/go/bin\" && nuclei -update-templates -silent' || true",
    ]
  }

  # 16. Configure PATH for pai user
  provisioner "shell" {
    inline = [
      "echo 'export PATH=\"/home/pai/.bun/bin:/home/pai/go/bin:/home/pai/.local/bin:/usr/local/bin:$PATH\"' >> /home/pai/.bashrc",
      "chown pai:pai /home/pai/.bashrc",
    ]
  }

  # 17. Clean up to reduce snapshot size
  provisioner "shell" {
    inline = [
      "apt-get clean",
      "rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*",
      "rm -rf /home/pai/go/pkg/mod/cache",
      "rm -rf /root/.cache",
    ]
  }
}
