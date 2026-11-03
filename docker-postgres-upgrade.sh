#!/usr/bin/env bash
# Upgrades the PostgreSQL data of a Docker Compose installation
# (./data/postgres) to a new major version with pg_upgrade – using only the
# official postgres image of the new version.
#
#   ./docker-postgres-upgrade.sh             to the version recommended in docker-compose.yml
#   ./docker-postgres-upgrade.sh --to 19     to a given major version
#   ./docker-postgres-upgrade.sh --check     only report (exit 0: up to date, 10: upgrade recommended)
#   options: --yes (no questions, e.g. for cron), --link (pg_upgrade --link: fast,
#            almost no extra space, but the old data can no longer be used afterwards)
#
# Steps: stop the containers, start a one-off container of the official new
# image, install the binaries of the old version there (PostgreSQL apt
# repository, already configured in the image), pg_upgrade --check, then
# pg_upgrade into data/postgres/<new>/docker, compare the row counts of all
# tables, set POSTGRES_VERSION in .env and start everything again.
# The old data stays in data/postgres/<old> (delete it after checking).
# If anything fails nothing is switched and the old version starts again.
# Only PostgreSQL is upgraded; the wiki itself is updated by "docker compose up -d".
set -euo pipefail
cd "$(dirname "$0")"

die() { printf 'docker-postgres-upgrade: %s\n' "$*" >&2; exit 1; }
say() { printf '%s\n' "$*"; }

to="" yes=0 link=0 check=0
while [ $# -gt 0 ]; do
	case "$1" in
		--to) to="${2:-}"; shift ;;
		--to=*) to="${1#--to=}" ;;
		--yes|-y) yes=1 ;;
		--link) link=1 ;;
		--check) check=1 ;;
		-h|--help) sed -n '2,20p' "$0"; exit 0 ;;
		*) die "unknown option $1 (see --help)" ;;
	esac
	shift
done

[ -f .env ] || die ".env is missing – run ./docker-setup.sh first"
getvar() { sed -n "s/^$1=//p" .env | tail -n 1; }
setvar() { if grep -q "^$1=" .env; then sed -i "s|^$1=.*|$1=$2|" .env; else printf '%s=%s\n' "$1" "$2" >> .env; fi; }

compose_file="${COMPOSE_FILE:-docker-compose.yml}"
compose_file="${compose_file%%:*}"
cur="$(getvar POSTGRES_VERSION)"
[ -n "$cur" ] || die "POSTGRES_VERSION is not set in .env – run ./docker-setup.sh"
if [ -z "$to" ]; then
	to="$(sed -nE 's/.*image: postgres:\$\{POSTGRES_VERSION:-([0-9]+)\}.*/\1/p' "$compose_file" | head -n 1)"
	[ -n "$to" ] || die "no recommended version found in $compose_file (use --to)"
fi
case "$cur$to" in *[!0-9]*) die "versions must be numbers (POSTGRES_VERSION=$cur, target $to)" ;; esac
uid="$(getvar EWIKI_UID)" gid="$(getvar EWIKI_GID)"
pguser="$(sed -nE 's/^ *POSTGRES_USER: *"?([^" ]+)"?.*/\1/p' "$compose_file" | head -n 1)"
pguser="${pguser:-ewiki}"
old_dir="data/postgres/$cur/docker"
new_dir="data/postgres/$to/docker"

if [ "$cur" -ge "$to" ]; then
	say "PostgreSQL $cur is up to date (recommended: $to)."
	exit 0
fi
if [ "$check" = 1 ]; then
	say "PostgreSQL $cur → $to recommended: run ./docker-postgres-upgrade.sh"
	exit 10
fi
[ -s "$old_dir/PG_VERSION" ] || die "no PostgreSQL data in $old_dir"
[ "$(cat "$old_dir/PG_VERSION")" = "$cur" ] || die "$old_dir contains PostgreSQL $(cat "$old_dir/PG_VERSION"), but POSTGRES_VERSION is $cur"
[ -n "$uid" ] && [ -n "$gid" ] || die "EWIKI_UID/EWIKI_GID missing in .env – run ./docker-setup.sh"
if [ -e "$new_dir" ]; then
	if [ -e "$new_dir/.ewiki-upgraded" ]; then
		die "$new_dir already contains an upgraded database – set POSTGRES_VERSION=$to in .env, or remove $new_dir to upgrade again"
	fi
	say "$new_dir exists from an interrupted attempt and will be replaced."
fi

# free space: copy mode needs about the size of the old data, --link very little
size="$(du -sk "$old_dir" | cut -f1)"
avail="$(df -Pk data/postgres | awk 'NR==2 {print $4}')"
need=$(( link == 1 ? size / 10 + 102400 : size * 11 / 10 + 102400 ))
if [ "$avail" -lt "$need" ]; then
	die "not enough free space: about $((need / 1024)) MB needed, $((avail / 1024)) MB free (or use --link)"
fi

say "PostgreSQL upgrade $cur → $to"
say "  data:      $old_dir ($((size / 1024)) MB) → $new_dir$([ "$link" = 1 ] && echo ' (hard links)')"
say "  the wiki is stopped during the upgrade; the old data stays in data/postgres/$cur"
if [ "$yes" != 1 ]; then
	read -r -p "Continue? [y/N] " a
	case "$a" in y|Y|j|J|yes|ja) ;; *) die "cancelled" ;; esac
fi

image="postgres:$to"
docker pull -q "$image" >/dev/null || die "could not pull $image"
docker compose stop
# services to start again (all by default; the CI test sets only "postgres")
services=(${PG_UPGRADE_SERVICES:-})
restart_old() { say "starting the previous version again"; docker compose up -d "${services[@]}" || true; }

rc=0
docker run --rm -i --user root -e OLD="$cur" -e NEW="$to" -e PUID="$uid" -e PGID="$gid" -e PGUSER_NAME="$pguser" -e LINK="$link" \
	-v "$(pwd)/data/postgres:/var/lib/postgresql" --entrypoint bash "$image" -s <<'INNER' || rc=$?
set -euo pipefail
log() { echo "pg-upgrade: $*" >&2; }
OLDD="/var/lib/postgresql/$OLD/docker" NEWD="/var/lib/postgresql/$NEW/docker"
OLDB="/usr/lib/postgresql/$OLD/bin" NEWB="/usr/lib/postgresql/$NEW/bin"
sock=/tmp/pgsock
[ ! -e "$OLDD/postmaster.pid" ] || { log "the old server seems to be running ($OLDD/postmaster.pid) – stop it first"; exit 1; }

# binaries of the old version from the PostgreSQL apt repository of the image
if [ ! -x "$OLDB/pg_ctl" ]; then
	log "installing PostgreSQL $OLD binaries"
	list=/etc/apt/sources.list.d/pgdg.list
	if [ -f "$list" ]; then sed -i -E "s/^(deb .*)$/\1 $OLD/" "$list"; fi
	apt-get update -qq >/dev/null
	DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends "postgresql-$OLD" >/dev/null
fi
[ -x "$OLDB/pg_ctl" ] || { log "PostgreSQL $OLD binaries are not available"; exit 1; }

# run as the owner of the data (EWIKI_UID/EWIKI_GID)
groupmod -o -g "$PGID" postgres; usermod -o -u "$PUID" -g "$PGID" postgres >/dev/null
mkdir -p "$sock" /tmp/work && chown postgres:postgres "$sock" /tmp/work
as() { gosu postgres "$@"; }
cd /tmp/work

inventory() { # port -> "db.schema.table rows" lines
	local db t
	for db in $(as psql -h "$sock" -p "$1" -U "$PGUSER_NAME" -d postgres -Atc "SELECT datname FROM pg_database WHERE datallowconn AND datname <> 'template1' ORDER BY 1"); do
		as psql -h "$sock" -p "$1" -U "$PGUSER_NAME" -d "$db" -Atc "SELECT format('%I.%I', schemaname, relname) FROM pg_stat_user_tables ORDER BY 1" |
		while read -r t; do echo "$db.$t $(as psql -h "$sock" -p "$1" -U "$PGUSER_NAME" -d "$db" -Atc "SELECT count(*) FROM $t")"; done
	done
}
cleanup() {
	local rc=$?
	as "$OLDB/pg_ctl" -D "$OLDD" -m fast -w stop >/dev/null 2>&1 || true
	as "$NEWB/pg_ctl" -D "$NEWD" -m fast -w stop >/dev/null 2>&1 || true
	if [ $rc -ne 0 ]; then log "failed – removing the incomplete $NEWD"; rm -rf "$NEWD"; fi
}
trap cleanup EXIT

# 1. state of the old cluster: settings for the new one, row counts
as "$OLDB/pg_ctl" -D "$OLDD" -l /tmp/work/old.log -o "-c listen_addresses='' -k $sock -p 50432" -w -t 600 start >/dev/null
read -r enc collate ctype < <(as psql -h "$sock" -p 50432 -U "$PGUSER_NAME" -d postgres -AtF ' ' -c "SELECT pg_encoding_to_char(encoding), datcollate, datctype FROM pg_database WHERE datname = 'template1'")
inventory 50432 > /tmp/work/before
as "$OLDB/pg_ctl" -D "$OLDD" -m fast -w stop >/dev/null
log "PostgreSQL $OLD: $(wc -l < /tmp/work/before) tables, encoding $enc, locale $collate"

# 2. new cluster with the same settings (data checksums as in the old one)
args=(--encoding="$enc" --lc-collate="$collate" --lc-ctype="$ctype")
if as "$OLDB/pg_controldata" "$OLDD" | grep -Eq 'checksum version: *0$'; then
	[ "$NEW" -lt 18 ] || args+=(--no-data-checksums)
else
	args+=(--data-checksums)
fi
rm -rf "$NEWD"; mkdir -p "$NEWD"; chown -R postgres:postgres "/var/lib/postgresql/$NEW"; chmod 700 "$NEWD"
as "$NEWB/initdb" -D "$NEWD" -U "$PGUSER_NAME" "${args[@]}" >/tmp/work/initdb.log
cp "$OLDD/pg_hba.conf" "$NEWD/pg_hba.conf" && chown postgres:postgres "$NEWD/pg_hba.conf"

# 3. pg_upgrade: check first, then upgrade
mode=(--copy); [ "$LINK" = 1 ] && mode=(--link)
up=(as "$NEWB/pg_upgrade" -b "$OLDB" -B "$NEWB" -d "$OLDD" -D "$NEWD" -U "$PGUSER_NAME" --socketdir="$sock" --jobs="$(nproc)")
"${up[@]}" --check || { log "pg_upgrade --check failed – nothing was changed"; exit 1; }
"${up[@]}" "${mode[@]}"

# 4. verify: the same tables with the same row counts; refresh statistics
as "$NEWB/pg_ctl" -D "$NEWD" -l /tmp/work/new.log -o "-c listen_addresses='' -k $sock -p 50433" -w -t 600 start >/dev/null
inventory 50433 > /tmp/work/after
if ! cmp -s /tmp/work/before /tmp/work/after; then
	log "row counts differ after the upgrade:"; diff /tmp/work/before /tmp/work/after >&2 || true; exit 1
fi
stats=(--analyze-in-stages); [ "$NEW" -lt 18 ] || stats=(--analyze-in-stages --missing-stats-only)
as "$NEWB/vacuumdb" -h "$sock" -p 50433 -U "$PGUSER_NAME" --all "${stats[@]}" >/dev/null
as "$NEWB/pg_ctl" -D "$NEWD" -m fast -w stop >/dev/null
echo "upgraded from PostgreSQL $OLD on $(date -u +%Y-%m-%dT%H:%M:%SZ) ($(wc -l < /tmp/work/before) tables verified)" > "$NEWD/.ewiki-upgraded"
chown postgres:postgres "$NEWD/.ewiki-upgraded"
trap - EXIT
log "done: $(wc -l < /tmp/work/after) tables, row counts identical"
INNER

if [ "$rc" -ne 0 ]; then
	restart_old
	die "the upgrade failed – PostgreSQL $cur is unchanged and running again"
fi
setvar POSTGRES_VERSION "$to"
docker compose up -d "${services[@]}"
say ""
say "PostgreSQL $to is running. The old data is still in data/postgres/$cur$([ "$link" = 1 ] && echo ' (no longer usable after --link)')."
say "After checking the wiki you can delete it:  rm -rf data/postgres/$cur"
