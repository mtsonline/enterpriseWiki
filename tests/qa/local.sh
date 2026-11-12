#!/usr/bin/env bash
# Runs the whole test routine against a throw-away instance on this machine:
# own PostgreSQL cluster, fresh database, the wiki built from this checkout.
#
#   tests/qa/local.sh                 everything (about 25 minutes)
#   tests/qa/local.sh functional      one part: functional | load | pentest (after a seed)
#
# Environment:
#   EWIKI_QA_WORK      working directory (default /tmp/ewiki-qa); results in $EWIKI_QA_WORK/results
#   EWIKI_QA_DATABASE_URL  use this (empty!) database instead of an own cluster
#   EWIKI_QA_PORT      port of the wiki (default 18080)
#   EWIKI_QA_STAGES    users per load stage (default 100,500,1000,2500,5000,10000)
#   EWIKI_QA_HOLD      measured time per stage (default 40s)
#   EWIKI_QA_SPIKE     simultaneous requests of the spike test (default 10000, 0: none)
#   EWIKI_QA_USERS     QA accounts (default 100)
#   EWIKI_QA_SKIP_BUILD=1  use the existing bin/ewiki
#   EWIKI_QA_KEEP=1    leave wiki and database running afterwards
set -euo pipefail
REPO="$(cd "$(dirname "$0")/../.." && pwd)"
WORK="${EWIKI_QA_WORK:-/tmp/ewiki-qa}"
PORT="${EWIKI_QA_PORT:-18080}"
PGPORT="${EWIKI_QA_PGPORT:-54329}"
CMD="${1:-all}"
mkdir -p "$WORK"
PGPID=""; WIKIPID=""

cleanup() {
  if [ "${EWIKI_QA_KEEP:-}" = 1 ]; then
    echo "==> left running: wiki pid $WIKIPID on port $PORT (admin password in $WORK/admin-password)"
    return
  fi
  [ -n "$WIKIPID" ] && kill "$WIKIPID" 2>/dev/null || true
  if [ -n "$PGPID" ]; then as_pg "$PGBIN/pg_ctl -D $WORK/pg -m immediate stop" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT

# PostgreSQL refuses to run as root: use the account "postgres" then
as_pg() { if [ "$(id -u)" = 0 ]; then su postgres -c "$1"; else bash -c "$1"; fi; }

if [ -z "${EWIKI_QA_DATABASE_URL:-}" ]; then
  PGBIN="$(pg_config --bindir 2>/dev/null || true)"
  [ -x "$PGBIN/initdb" ] || PGBIN="$(ls -d /usr/lib/postgresql/*/bin 2>/dev/null | sort -V | tail -1)"
  [ -x "$PGBIN/initdb" ] || { echo "PostgreSQL (initdb) not found: install it or set EWIKI_QA_DATABASE_URL" >&2; exit 1; }
  if [ "$(id -u)" = 0 ]; then id postgres >/dev/null 2>&1 || useradd -m postgres; fi
  as_pg "$PGBIN/pg_ctl -D $WORK/pg -m immediate stop" >/dev/null 2>&1 || true
  rm -rf "$WORK/pg" && mkdir -p "$WORK/pg" && chmod 755 "$WORK"
  [ "$(id -u)" = 0 ] && chown postgres "$WORK/pg"
  echo "==> PostgreSQL cluster in $WORK/pg (port $PGPORT)"
  as_pg "$PGBIN/initdb -D $WORK/pg -A trust -U postgres >/dev/null"
  as_pg "$PGBIN/pg_ctl -D $WORK/pg -w -l $WORK/pg/log -o '-c listen_addresses=127.0.0.1 -c port=$PGPORT -c max_connections=200 -c shared_buffers=256MB -c unix_socket_directories=$WORK/pg' start >/dev/null"
  PGPID=1
  as_pg "$PGBIN/createdb -h 127.0.0.1 -p $PGPORT -U postgres wiki"
  EWIKI_QA_DATABASE_URL="postgres://postgres@127.0.0.1:$PGPORT/wiki?sslmode=disable"
fi

if [ "${EWIKI_QA_SKIP_BUILD:-}" != 1 ] || [ ! -x "$REPO/bin/ewiki" ]; then
  echo "==> building the wiki"
  (cd "$REPO" && ./scripts/build.sh >"$WORK/build.log" 2>&1) || { tail -20 "$WORK/build.log"; exit 1; }
fi
echo "==> building the test routine"
(cd "$REPO/tests/qa" && go build -o "$WORK/qa" .)

INST="$WORK/instance"
rm -rf "$INST" && mkdir -p "$INST"
export EWIKI_QA_ADMIN_PASSWORD="Qa-$(head -c 12 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 14)-9a!"
( umask 077; printf '%s\n' "$EWIKI_QA_ADMIN_PASSWORD" > "$WORK/admin-password" )
echo "==> installing the instance in $INST"
(cd "$INST" && EWIKI_DATABASE_URL="$EWIKI_QA_DATABASE_URL" EWIKI_ADMIN_USER=admin EWIKI_ADMIN_PASSWORD="$EWIKI_QA_ADMIN_PASSWORD" EWIKI_BASE_URL="http://127.0.0.1:$PORT" \
  EWIKI_LISTEN="127.0.0.1:$PORT" EWIKI_LANGUAGES=de,en EWIKI_SITE_NAME="QA Wiki" "$REPO/bin/ewiki" setup --non-interactive >"$WORK/setup.log" 2>&1) || { tail -20 "$WORK/setup.log"; exit 1; }
# every virtual user gets its own client address (X-Forwarded-For), as behind a reverse proxy
sed -i 's/trusted_proxies: \[\]/trusted_proxies: [127.0.0.1]/' "$INST/config.yaml"
ulimit -n "$(ulimit -Hn)" 2>/dev/null || true
(cd "$INST" && EWIKI_DATABASE_URL="$EWIKI_QA_DATABASE_URL" exec "$REPO/bin/ewiki" serve >"$WORK/server.log" 2>&1) &
WIKIPID=$!
for _ in $(seq 60); do curl -fs "http://127.0.0.1:$PORT/readyz" >/dev/null 2>&1 && break; sleep 0.5; done
curl -fs "http://127.0.0.1:$PORT/readyz" >/dev/null || { echo "the wiki does not start:" >&2; tail -20 "$WORK/server.log" >&2; exit 1; }

VERSION="$(cd "$REPO" && git describe --tags --always --dirty 2>/dev/null || echo dev)"
ARGS=(-base "http://127.0.0.1:$PORT" -out "$WORK/results" -spoof-ips -server-pid "$WIKIPID" -users "${EWIKI_QA_USERS:-100}"
  -routes "$REPO/docs/developer/api.md" -repo "$REPO" -version "$VERSION"
  -stages "${EWIKI_QA_STAGES:-100,500,1000,2500,5000,10000}" -hold "${EWIKI_QA_HOLD:-40s}" -spike "${EWIKI_QA_SPIKE:-10000}")
echo "==> qa $CMD (wiki pid $WIKIPID, version $VERSION)"
if [ "$CMD" != all ] && [ "$CMD" != seed ]; then "$WORK/qa" seed "${ARGS[@]}"; fi
"$WORK/qa" "$CMD" "${ARGS[@]}"
if [ "$CMD" != all ]; then "$WORK/qa" report "${ARGS[@]}"; fi
echo "==> results: $WORK/results (report.md, *.json), server log: $WORK/server.log"
