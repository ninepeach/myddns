#!/bin/sh
set -eu

[ "$(id -u)" -eq 0 ] || { echo "Run as root." >&2; exit 1; }
[ "$(uname -s)" = Linux ] || { echo "Linux is required." >&2; exit 1; }
case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "Unsupported architecture." >&2; exit 1 ;;
esac
if [ -f /etc/alpine-release ]; then
  init=openrc
elif command -v systemctl >/dev/null 2>&1; then
  init=systemd
else
  echo "systemd or Alpine OpenRC is required." >&2
  exit 1
fi
command -v curl >/dev/null 2>&1 || { echo "Install curl and ca-certificates first." >&2; exit 1; }
command -v sha256sum >/dev/null 2>&1 || { echo "sha256sum is required." >&2; exit 1; }

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
base=https://github.com/ninepeach/myddns/releases/latest/download
asset=myddns_linux_$arch
curl -fLsS --proto '=https' --proto-redir '=https' "$base/$asset" -o "$tmp/$asset"
curl -fLsS --proto '=https' --proto-redir '=https' "$base/SHA256SUMS" -o "$tmp/SHA256SUMS"
expected=$(awk -v file="$asset" '$2 == file {print $1}' "$tmp/SHA256SUMS")
[ "${#expected}" -eq 64 ] || { echo "Missing checksum." >&2; exit 1; }
actual=$(sha256sum "$tmp/$asset")
[ "${actual%% *}" = "$expected" ] || { echo "Checksum mismatch." >&2; exit 1; }

if ! id myddns >/dev/null 2>&1; then
  if [ "$init" = openrc ]; then
    addgroup -S myddns
    adduser -S -D -H -s /sbin/nologin -G myddns myddns
  else
    useradd --system --no-create-home --shell /usr/sbin/nologin myddns
  fi
fi
mkdir -p /etc/myddns /usr/local/bin
chmod 750 /etc/myddns
chown root:myddns /etc/myddns
chmod 755 "$tmp/$asset"
mv "$tmp/$asset" /usr/local/bin/myddns
if [ ! -f /etc/myddns/config.json ]; then
  printf '%s\n' '{"interval":"5m","cycle_timeout":"1m","ipv4":true,"ipv6":true,"zones":[{"id":"YOUR_ZONE_ID","records":["home.example.com"]}]}' > /etc/myddns/config.json
fi
if [ ! -f /etc/myddns/token.env ]; then
  printf '%s\n' 'CLOUDFLARE_API_TOKEN="YOUR_TOKEN"' > /etc/myddns/token.env
fi
chown root:myddns /etc/myddns/config.json
chmod 640 /etc/myddns/config.json
chown root:root /etc/myddns/token.env
chmod 600 /etc/myddns/token.env

if [ "$init" = systemd ]; then
  cat > /etc/systemd/system/myddns.service <<'SERVICE'
[Unit]
Description=Cloudflare Dynamic DNS
Wants=network-online.target
After=network-online.target

[Service]
User=myddns
Group=myddns
EnvironmentFile=/etc/myddns/token.env
ExecStart=/usr/local/bin/myddns -config /etc/myddns/config.json
Restart=on-failure
RestartSec=10
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
SERVICE
  systemctl daemon-reload
else
  cat > /etc/init.d/myddns <<'SERVICE'
#!/sbin/openrc-run
description="Cloudflare Dynamic DNS"
command=/usr/local/bin/myddns
command_args="-config /etc/myddns/config.json"
command_user="myddns:myddns"
supervisor=supervise-daemon
depend() { need net; }
start_pre() {
  set -a
  . /etc/myddns/token.env
  set +a
}
SERVICE
  chmod 755 /etc/init.d/myddns
fi
echo "Installed. Edit /etc/myddns/config.json and /etc/myddns/token.env."
if [ "$init" = systemd ]; then
  echo "Then run: systemctl enable --now myddns"
else
  echo "Then run: rc-update add myddns default && rc-service myddns start"
fi
