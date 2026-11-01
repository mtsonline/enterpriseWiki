#!/usr/bin/env bash
# Safely updates an installation created with scripts/install.sh.
#
#   sudo scripts/update.sh [--ref TAG]     update to the latest release tag (or TAG)
#   sudo scripts/update.sh --enable-timer  install a weekly systemd timer for automatic updates
#
# Steps: fetch -> build into bin/ewiki.new -> self-test -> database backup ->
# swap binary -> restart -> health check. If the health check fails, the
# previous binary is restored and restarted automatically. Database migrations
# only ever add structures; the backup allows a full restore if needed.
set -euo pipefail
DIR="$(cd "$(dirname "$0")/.." && pwd)"
SERVICE=enterprisewiki
REF=""
while [ $# -gt 0 ]; do
  case "$1" in
    --ref) REF="$2"; shift 2 ;;
    --enable-timer)
      cat > /etc/systemd/system/enterprisewiki-update.service <<UNIT
[Unit]
Description=Enterprise Wiki automatic update
[Service]
Type=oneshot
ExecStart=$DIR/scripts/update.sh
UNIT
      cat > /etc/systemd/system/enterprisewiki-update.timer <<UNIT
[Unit]
Description=Weekly Enterprise Wiki update
[Timer]
OnCalendar=Sun 03:30
RandomizedDelaySec=1h
Persistent=true
[Install]
WantedBy=timers.target
UNIT
      systemctl daemon-reload && systemctl enable --now enterprisewiki-update.timer
      echo "automatic updates enabled (systemctl list-timers enterprisewiki-update.timer)"; exit 0 ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
done

cd "$DIR"
OWNER="$(stat -c %U "$DIR")"
as_owner() { sudo -u "$OWNER" env HOME="$DIR" "$@"; }

echo "==> fetching"
as_owner git fetch --tags --quiet origin
if [ -z "$REF" ]; then
  REF="$(as_owner git tag --list 'v*' --sort=-v:refname | head -n1)"
  [ -z "$REF" ] && REF="origin/main"
fi
CURRENT="$(as_owner git rev-parse HEAD)"
TARGET="$(as_owner git rev-parse "$REF^{commit}")"
if [ "$CURRENT" = "$TARGET" ]; then echo "already up to date ($REF)"; exit 0; fi
as_owner git -c advice.detachedHead=false checkout --quiet "$TARGET"

echo "==> building $REF"
if ! as_owner env EWIKI_BUILD_OUT="$DIR/bin/ewiki.new" bash scripts/build.sh; then
  echo "build failed - keeping the running version" >&2
  as_owner git checkout --quiet "$CURRENT"; exit 1
fi
as_owner "$DIR/bin/ewiki.new" version

echo "==> database backup"
mkdir -p "$DIR/backups"
if command -v pg_dump >/dev/null; then
  set -a; [ -f "$DIR/.env" ] && . "$DIR/.env"; set +a
  pg_dump --format=custom --file "$DIR/backups/before-$(date +%Y%m%d-%H%M%S).dump" "${EWIKI_DATABASE_URL:?EWIKI_DATABASE_URL not set}"
else
  echo "warning: pg_dump not found, skipping database backup" >&2
fi

echo "==> switching binary"
cp -p bin/ewiki bin/ewiki.prev
mv bin/ewiki.new bin/ewiki
systemctl restart "$SERVICE"

LISTEN="$(as_owner bin/ewiki --config "$DIR/config.yaml" config get server.listen 2>/dev/null | tr -d '"')"
PORT="${LISTEN##*:}"
for i in $(seq 1 30); do
  if curl -fs "http://127.0.0.1:${PORT:-8080}/readyz" >/dev/null; then
    echo "==> update to $REF successful"; exit 0
  fi
  sleep 2
done

echo "health check failed - rolling back" >&2
mv bin/ewiki.prev bin/ewiki
as_owner git checkout --quiet "$CURRENT"
systemctl restart "$SERVICE"
exit 1
