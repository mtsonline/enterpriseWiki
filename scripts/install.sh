#!/usr/bin/env bash
# Installs Enterprise Wiki on a Linux server with systemd (bare metal / VM).
#
#   sudo ./scripts/install.sh [INSTALL_DIR]        (default /opt/enterprisewiki)
#
# Requirements: git, Go >= 1.24, Node.js >= 20 (with npm), PostgreSQL >= 14
# (reachable; the database must exist). The source tree stays in INSTALL_DIR so
# plugins can be installed and the binary rebuilt from the admin UI.
set -euo pipefail
DIR="${1:-/opt/enterprisewiki}"
SRC="$(cd "$(dirname "$0")/.." && pwd)"
SERVICE_USER="${EWIKI_USER:-ewiki}"

need() { command -v "$1" >/dev/null || { echo "missing requirement: $1" >&2; exit 1; }; }
need git; need go; need node; need npm; need systemctl

if [ "$(id -u)" -ne 0 ]; then echo "please run as root (sudo)" >&2; exit 1; fi

echo "==> installing into $DIR"
if [ "$SRC" != "$DIR" ]; then
  mkdir -p "$DIR"
  git -C "$SRC" ls-files -z | (cd "$SRC" && xargs -0 cp --parents -t "$DIR")
  [ -d "$SRC/.git" ] && cp -r "$SRC/.git" "$DIR/"
fi
id "$SERVICE_USER" >/dev/null 2>&1 || useradd --system --home "$DIR" --shell /usr/sbin/nologin "$SERVICE_USER"
chown -R "$SERVICE_USER" "$DIR"

echo "==> building"
sudo -u "$SERVICE_USER" env HOME="$DIR" bash "$DIR/scripts/build.sh"

echo "==> setup (config, database, first administrator)"
cd "$DIR"
sudo -u "$SERVICE_USER" env HOME="$DIR" "$DIR/bin/ewiki" --config "$DIR/config.yaml" setup
chmod 600 "$DIR/.env" 2>/dev/null || true

echo "==> systemd service"
cat > /etc/systemd/system/enterprisewiki.service <<UNIT
[Unit]
Description=Enterprise Wiki
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
User=$SERVICE_USER
WorkingDirectory=$DIR
Environment=EWIKI_CONFIG=$DIR/config.yaml
Environment=HOME=$DIR
ExecStart=$DIR/bin/ewiki serve
ExecReload=/bin/kill -HUP \$MAINPID
Restart=always
RestartSec=3
NoNewPrivileges=true
ProtectSystem=full
PrivateTmp=true
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable --now enterprisewiki
echo "==> done. Status: systemctl status enterprisewiki"
echo "    Automatic updates (optional): sudo $DIR/scripts/update.sh --enable-timer"
