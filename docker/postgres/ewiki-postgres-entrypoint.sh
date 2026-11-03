#!/bin/sh
# Entrypoint of the Enterprise Wiki PostgreSQL image: automatic, verified
# upgrade between major versions (dump of the old version into the new
# one), then the normal entrypoint of the official image.
set -eu

ROOT="${PGDATA_ROOT:-/var/lib/postgresql/ewiki}"
MAJOR="${PG_MAJOR%%.*}"
NEW="$ROOT/pg$MAJOR"
BACKUPS="${PG_UPGRADE_BACKUP_DIR:-/backups}"
USER_="${POSTGRES_USER:-postgres}"
export PGDATA="$NEW"

log() { echo "ewiki-postgres: $*" >&2; }

# The newest cluster of an older major version: the volume root (layout of
# the plain postgres image) or $ROOT/pg<major> (this image).
find_old() {
  best=""; bestv=0
  for d in "$ROOT" "$ROOT"/pg*; do
    [ -f "$d/PG_VERSION" ] || continue
    v=$(cat "$d/PG_VERSION")
    case "$v" in *[!0-9]*) continue ;; esac
    if [ "$v" -lt "$MAJOR" ] && [ "$v" -gt "$bestv" ]; then best="$d"; bestv="$v"; fi
  done
  [ -n "$best" ] && echo "$bestv $best"
}

# "database.schema.table rows" for all tables of all databases
inventory() { # socket dir
  for db in $(psql -h "$1" -U "$USER_" -d postgres -Atc "SELECT datname FROM pg_database WHERE NOT datistemplate AND datname <> 'postgres' ORDER BY 1"); do
    psql -h "$1" -U "$USER_" -d "$db" -Atc "SELECT quote_ident(schemaname)||'.'||quote_ident(relname) FROM pg_stat_user_tables ORDER BY 1" |
    while read -r t; do
      echo "$db.$t $(psql -h "$1" -U "$USER_" -d "$db" -Atc "SELECT count(*) FROM $t")"
    done
  done
}

as_pg() { su-exec postgres "$@"; }

upgrade() {
  oldv="$1"; old="$2"
  bin="/usr/libexec/postgresql$oldv"
  if [ ! -x "$bin/pg_ctl" ]; then
    log "data of PostgreSQL $oldv found in $old, but this image has no binaries of version $oldv."
    log "Use an image of an intermediate version first or upgrade manually (pg_dumpall / restore). Nothing was changed."
    sleep 60; exit 1
  fi
  stamp=$(date +%Y%m%d-%H%M%S)
  mkdir -p "$BACKUPS" && chown postgres "$BACKUPS"
  dump="$BACKUPS/pg$oldv-before-pg$MAJOR-$stamp.sql.gz"
  oldsock=$(mktemp -d) newsock=$(mktemp -d) tmp="$ROOT/.pg$MAJOR.upgrading"
  chown postgres "$oldsock" "$newsock"
  rm -rf "$tmp"
  log "upgrading PostgreSQL $oldv ($old) to $MAJOR – dump: $dump"

  # 1. start the old cluster locally (no network) and dump it
  as_pg "$bin/pg_ctl" -D "$old" -o "-c listen_addresses='' -k $oldsock" -w -t 300 -l "$BACKUPS/pg$oldv-$stamp.log" start
  trap 'as_pg "$bin/pg_ctl" -D "$old" -m fast -w stop >/dev/null 2>&1 || true' EXIT
  inventory "$oldsock" > "$BACKUPS/pg$oldv-$stamp.inventory"
  as_pg pg_dumpall -h "$oldsock" -U "$USER_" | gzip > "$dump"
  as_pg "$bin/pg_ctl" -D "$old" -m fast -w stop
  trap - EXIT
  log "dump written ($(du -h "$dump" | cut -f1)), $(wc -l < "$BACKUPS/pg$oldv-$stamp.inventory") tables"

  # 2. new cluster in a temporary directory, restore, compare
  mkdir -p "$tmp" && chown postgres "$tmp" && chmod 700 "$tmp"
  as_pg initdb -D "$tmp" -U "$USER_" --auth-local=trust --auth-host=scram-sha-256 ${POSTGRES_INITDB_ARGS:-} >/dev/null
  cp "$old/pg_hba.conf" "$tmp/pg_hba.conf"
  echo "listen_addresses = '*'" >> "$tmp/postgresql.auto.conf"
  as_pg pg_ctl -D "$tmp" -o "-c listen_addresses='' -k $newsock" -w -t 300 start
  trap 'as_pg pg_ctl -D "$tmp" -m fast -w stop >/dev/null 2>&1 || true' EXIT
  errors="$BACKUPS/pg$MAJOR-restore-$stamp.log"
  gunzip -c "$dump" | as_pg psql -h "$newsock" -U "$USER_" -d postgres -v ON_ERROR_STOP=0 -q >/dev/null 2>"$errors" || true
  # the only expected error: the superuser created by initdb exists already
  if grep -v "role \"$USER_\" already exists" "$errors" | grep -q "ERROR"; then
    log "restore reported errors (see $errors) – keeping PostgreSQL $oldv, nothing was switched"
    as_pg pg_ctl -D "$tmp" -m fast -w stop; trap - EXIT; rm -rf "$tmp"; sleep 60; exit 1
  fi
  inventory "$newsock" > "$BACKUPS/pg$MAJOR-$stamp.inventory"
  as_pg pg_ctl -D "$tmp" -m fast -w stop
  trap - EXIT
  if ! cmp -s "$BACKUPS/pg$oldv-$stamp.inventory" "$BACKUPS/pg$MAJOR-$stamp.inventory"; then
    log "row counts differ after the restore – keeping PostgreSQL $oldv, nothing was switched:"
    diff "$BACKUPS/pg$oldv-$stamp.inventory" "$BACKUPS/pg$MAJOR-$stamp.inventory" >&2 || true
    rm -rf "$tmp"; sleep 60; exit 1
  fi

  # 3. switch over (the old data directory stays as it is)
  mv "$tmp" "$NEW"
  echo "upgraded from PostgreSQL $oldv ($old) on $stamp, dump $dump" > "$NEW/EWIKI_UPGRADED_FROM"
  log "upgrade to PostgreSQL $MAJOR finished and verified. Old data kept in $old, dump in $dump."
}

if [ "${1:-}" = "postgres" ] && [ "$(id -u)" = 0 ]; then
  mkdir -p "$ROOT" && chown postgres "$ROOT"
  if [ ! -s "$NEW/PG_VERSION" ]; then
    rm -rf "$ROOT/.pg$MAJOR.upgrading"
    if old=$(find_old); then
      upgrade $old
    fi
  fi
fi
# normal start (initializes a new, empty cluster on a fresh volume)
exec docker-entrypoint.sh "$@"
