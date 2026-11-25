#!/usr/bin/env bash
# Installs new releases of Enterprise Wiki in a Docker Compose installation
# (published image). Runs on the host, as the user who runs the wiki
# (EWIKI_UID in .env, member of the group docker); the wiki itself never
# gets access to Docker.
#
#   scripts/update-docker.sh                     one run: update if the wiki reported a new release
#   scripts/update-docker.sh --dry-run           show what a run would do, change nothing
#   scripts/update-docker.sh --enable-timer [HH:MM] [--cron]
#                                                daily run (default 03:30): a systemd timer with
#                                                root or sudo, else (or with --cron) a crontab entry
#   scripts/update-docker.sh --disable-timer     remove the timer
#   scripts/update-docker.sh --status            show timer and last run
#
# A run:
#   1. reads ./data/wiki/update-available, written by the wiki when its
#      update check found a newer published release. The file is treated as
#      untrusted: only a version of the form 1.2.3 is taken from it, nothing
#      else is used. No file: nothing to do.
#   2. does nothing but report for a self-built image (docker-compose.build.yml):
#      update it with git pull && docker compose -f docker-compose.build.yml up -d --build
#   3. backs up the database (pg_dump into ./data/backups, the newest 10 are kept);
#      if that fails, nothing is updated
#   4. docker compose pull wiki && docker compose up -d, then waits until the
#      wiki is ready and compares its version
#   5. writes ./data/wiki/update-timer.json (next run, last run, result) for the
#      overview of the administration, and logs to ./data/update-docker.log
# At most one update per run; runs never overlap.
set -euo pipefail

DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$DIR"
# for tests: another docker command, another place of the unit files, a fixed clock
DOCKER="${EWIKI_UPDATE_DOCKER:-docker}"
UNITS="${EWIKI_UPDATE_UNITS:-/etc/systemd/system}"
NAME=enterprisewiki-docker-update
DATA="$DIR/data"
MARKER="$DATA/wiki/update-available"
STATE="$DATA/wiki/update-timer.json"
CONF="$DATA/update-docker.conf"
LOG="$DATA/update-docker.log"
TAG="# $NAME $DIR"

die() { printf 'update-docker: %s\n' "$*" >&2; exit 1; }
say() { printf '%s\n' "$*"; }
log() {
	say "$*"
	[ -n "${dry:-}" ] && return 0
	printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >> "$LOG"
	# keep the log small
	if [ "$(wc -l < "$LOG")" -gt 2000 ]; then tail -n 1000 "$LOG" > "$LOG.tmp" && mv "$LOG.tmp" "$LOG"; fi
}
getvar() { [ -f .env ] && sed -n "s/^$1=//p" .env | tail -n 1 || true; }
valid_time() { [[ "$1" =~ ^([01][0-9]|2[0-3]):[0-5][0-9]$ ]]; }
valid_version() { [[ "$1" =~ ^[0-9]{1,4}\.[0-9]{1,4}\.[0-9]{1,4}$ ]]; }

[ -f docker-compose.yml ] || die "run this in a Docker Compose installation of Enterprise Wiki (no docker-compose.yml in $DIR)"
uid="$(getvar EWIKI_UID)"
[[ "$uid" =~ ^[0-9]+$ ]] || die "EWIKI_UID is not set in .env – run ./docker-setup.sh first"
owner="$(id -nu "$uid" 2>/dev/null || true)"
[ -n "$owner" ] || die "no user with the id $uid (EWIKI_UID in .env)"

# ---------------------------------------------------------------- settings of the timer
# data/update-docker.conf: mode=systemd|cron and time=HH:MM (written by --enable-timer)
conf_get() { [ -f "$CONF" ] && sed -n "s/^$1=//p" "$CONF" | tail -n 1 || true; }
mode="$(conf_get mode)"; time_="$(conf_get time)"
case "$mode" in systemd|cron) valid_time "$time_" || mode= ;; *) mode= ;; esac

# the next run as UTC (RFC 3339), empty without a timer
next_run() {
	local t
	case "$mode" in
	systemd)
		t="$(systemctl show "$NAME.timer" -p NextElapseUSecRealtime --value 2>/dev/null || true)"
		if [ -n "$t" ] && [ "$t" != n/a ] && date -u -d "$t" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null; then return 0; fi
		;;&
	systemd|cron)
		# the next HH:MM in local time (also the fallback for systemd)
		t="$(date -d "today $time_" +%s)"
		[ "$t" -gt "$(date +%s)" ] || t="$(date -d "tomorrow $time_" +%s)"
		date -u -d "@$t" +%Y-%m-%dT%H:%M:%SZ
		;;
	esac
}

# write_state RESULT VERSION: data/wiki/update-timer.json for the overview
write_state() {
	[ -n "${dry:-}" ] && return 0
	[ -n "$mode" ] || { rm -f "$STATE"; return 0; }
	local next last result version window=0 tmp
	next="$(next_run)"
	[ "$mode" = systemd ] && window=60
	# what an earlier run reported stays until the next run
	old() { sed -n "s/.*\"$1\": *\"\([0-9A-Za-z:.-]*\)\".*/\1/p" "$STATE" 2>/dev/null | head -n 1 || true; }
	last="$(old last_run)" result="$(old last_result)" version="$(old last_version)"
	if [ -n "${1:-}" ]; then last="$(date -u +%Y-%m-%dT%H:%M:%SZ)" result="$1" version="${2:-}"; fi
	quote() { if [ -n "$1" ]; then printf '"%s"' "$1"; else printf null; fi; }
	tmp="$(mktemp "$DATA/wiki/.update-timer.XXXXXX")"
	printf '{"schema":1,"mode":"%s","next_run":%s,"window_minutes":%d,"last_run":%s,"last_result":"%s","last_version":"%s","updated_at":"%s"}\n' \
		"$mode" "$(quote "$next")" "$window" "$(quote "$last")" "$result" "$version" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$tmp"
	chmod 644 "$tmp"
	mv "$tmp" "$STATE"
}

# ---------------------------------------------------------------- one run
# run: the docker commands; in a dry run only printed
dk() {
	if [ -n "${dry:-}" ]; then say "  would run: docker $*"; return 0; fi
	"$DOCKER" "$@"
}

update() {
	[ "$(id -u)" = "$uid" ] || die "run this as $owner (EWIKI_UID), not as $(id -nu)"
	mkdir -p "$DATA/backups"
	exec 9> "$DATA/.update-docker.lock"
	if command -v flock >/dev/null && ! flock -n 9; then
		log "another run is still going on"; write_state busy ""; return 0
	fi
	if [ ! -e "$MARKER" ]; then
		log "no new release reported by the wiki"; write_state no-update ""; return 0
	fi
	# the marker is untrusted: a small regular file, only the version is used
	local version size
	[ -f "$MARKER" ] && [ ! -L "$MARKER" ] || { log "invalid $MARKER (not a regular file)"; write_state invalid-marker ""; return 1; }
	size="$(wc -c < "$MARKER")"
	[ "$size" -le 4096 ] || { log "invalid $MARKER (too large)"; write_state invalid-marker ""; return 1; }
	[ "$(wc -l < "$MARKER")" -le 1 ] || { log "invalid $MARKER (more than one line)"; write_state invalid-marker ""; return 1; }
	version="$(tr -d '\n' < "$MARKER" | sed -n 's/^{"version":"\([^"]*\)".*}$/\1/p')"
	valid_version "$version" || { log "invalid $MARKER (no version of the form 1.2.3)"; write_state invalid-marker ""; return 1; }
	if grep -q '"security":true' "$MARKER"; then log "new release $version (security update)"; else log "new release $version"; fi

	# a self-built image is updated from its source, not here
	local built=""
	# (read whole: grep -q would end the pipe early, a failure with pipefail)
	local cfg
	cfg="$("$DOCKER" compose config wiki 2>/dev/null || true)"
	if grep -Eq '^ {4}build:' <<< "$cfg"; then built=1; fi
	if [ "$("$DOCKER" compose exec -T wiki printenv EWIKI_IMAGE_ORIGIN 2>/dev/null || true)" = self-built ]; then built=1; fi
	if [ -n "$built" ]; then
		log "self-built image: nothing done. Update with: git pull && docker compose -f docker-compose.build.yml up -d --build"
		write_state self-built "$version"; return 0
	fi

	# 1. backup of the database
	local pguser pgdb dump
	pguser="$(sed -nE 's/^ *POSTGRES_USER: *"?([A-Za-z0-9_]+)"?.*/\1/p' docker-compose.yml | head -n 1)"
	pgdb="$(sed -nE 's/^ *POSTGRES_DB: *"?([A-Za-z0-9_]+)"?.*/\1/p' docker-compose.yml | head -n 1)"
	dump="$DATA/backups/before-update-$(date +%Y%m%d-%H%M%S).dump"
	if [ -n "${dry:-}" ]; then
		say "  would back up the database to $dump"
	elif ! "$DOCKER" compose exec -T postgres pg_dump -U "${pguser:-ewiki}" -d "${pgdb:-ewiki}" -Fc > "$dump.part" || [ ! -s "$dump.part" ]; then
		rm -f "$dump.part"
		log "database backup failed – nothing updated"; write_state backup-failed "$version"; return 1
	else
		mv "$dump.part" "$dump"
		log "database backup: $dump"
		# keep the newest 10
		find "$DATA/backups" -maxdepth 1 -name 'before-update-*.dump' -printf '%f\n' | sort -r | tail -n +11 |
			while read -r f; do rm -f "$DATA/backups/$f"; done
	fi

	# 2. new image, restart
	if ! dk compose pull wiki || ! dk compose up -d; then
		log "docker compose failed – see the output above"; write_state failed "$version"; return 1
	fi
	[ -n "${dry:-}" ] && { say "dry run: nothing changed"; return 0; }

	# 3. ready and the expected version?
	local ready="" installed
	for _ in $(seq 1 60); do
		if "$DOCKER" compose exec -T wiki wget -qO- http://127.0.0.1:8080/readyz >/dev/null 2>&1; then ready=1; break; fi
		sleep 5
	done
	[ -n "$ready" ] || { log "the wiki did not become ready within 5 minutes (docker compose logs wiki); backup: $dump"; write_state failed "$version"; return 1; }
	installed="$("$DOCKER" compose exec -T wiki ewiki version 2>/dev/null | awk '{print $2}' | sed 's/^v//')"
	if [ "$installed" != "$version" ]; then
		log "running version ${installed:-unknown}, expected $version (is a fixed version set in EWIKI_IMAGE_TAG in .env?)"
		write_state not-reached "$version"; return 1
	fi
	rm -f "$MARKER"
	log "updated to $version"; write_state updated "$version"
}

# ---------------------------------------------------------------- timer
# as root (or with sudo): a systemd timer; else a crontab entry of the user
enable_timer() {
	local at="${1:-03:30}" force_cron="${2:-}"
	valid_time "$at" || die "time must be HH:MM (e.g. 03:30), not $at"
	# systemd: on a host that runs it (tests: EWIKI_UPDATE_UNITS)
	local systemd=
	if [ -n "${EWIKI_UPDATE_UNITS:-}" ] || { command -v systemctl >/dev/null && [ -d /run/systemd/system ]; }; then systemd=1; fi
	if [ -z "$force_cron" ] && [ -n "$systemd" ]; then
		if [ "$(id -u)" = 0 ]; then
			cat > "$UNITS/$NAME.service" <<UNIT
[Unit]
Description=Enterprise Wiki update (Docker, $DIR)
After=docker.service
[Service]
Type=oneshot
User=$owner
WorkingDirectory=$DIR
ExecStart=$DIR/scripts/update-docker.sh
UNIT
			cat > "$UNITS/$NAME.timer" <<UNIT
[Unit]
Description=Daily Enterprise Wiki update (Docker, $DIR)
[Timer]
OnCalendar=*-*-* $at
RandomizedDelaySec=1h
Persistent=true
[Install]
WantedBy=timers.target
UNIT
			if [ -z "${EWIKI_UPDATE_UNITS:-}" ]; then systemctl daemon-reload && systemctl enable --now "$NAME.timer" >/dev/null; fi
			remove_cron
			set_conf systemd "$at"
			say "daily update timer (systemd): $NAME.timer, $at plus up to one hour (systemctl list-timers $NAME.timer)"
			return 0
		fi
		if command -v sudo >/dev/null && [ -t 0 ]; then
			say "root rights are needed for a systemd timer; sudo asks for your password (Ctrl+C: crontab instead)"
			if sudo "$0" --enable-timer "$at"; then return 0; fi
			say "sudo did not work – using a crontab entry instead"
		fi
	fi
	[ "$(id -u)" = "$uid" ] || die "for a crontab entry, run this as $owner (EWIKI_UID)"
	command -v crontab >/dev/null || die "neither root rights for a systemd timer nor crontab are available"
	# cron has no random delay: a random minute within the hour after the time, chosen once
	local h m
	h="${at%:*}" m="${at#*:}"
	m=$(( 10#$m + RANDOM % 60 )); h=$(( (10#$h + m / 60) % 24 )); m=$(( m % 60 ))
	at="$(printf '%02d:%02d' "$h" "$m")"
	{ crontab -l 2>/dev/null | grep -vF "$TAG" || true; printf '%d %d * * * cd %q && scripts/update-docker.sh >/dev/null 2>&1 %s\n' "$m" "$h" "$DIR" "$TAG"; } | crontab -
	set_conf cron "$at"
	say "daily update (crontab of $owner) at $at"
}
remove_cron() {
	command -v crontab >/dev/null || return 0
	if [ "$(id -u)" = "$uid" ]; then
		local tab
		tab="$(crontab -l 2>/dev/null || true)"
		grep -qF "$TAG" <<< "$tab" || return 0
		grep -vF "$TAG" <<< "$tab" | crontab -
	elif [ "$(id -u)" = 0 ]; then
		local tab
		tab="$(crontab -u "$owner" -l 2>/dev/null || true)"
		grep -qF "$TAG" <<< "$tab" || return 0
		grep -vF "$TAG" <<< "$tab" | crontab -u "$owner" -
	fi
}
set_conf() {
	mode="$1" time_="$2"
	printf 'mode=%s\ntime=%s\n' "$1" "$2" > "$CONF"
	chown "$uid:$(getvar EWIKI_GID)" "$CONF" 2>/dev/null || true
	write_state "" ""
	[ "$(id -u)" != 0 ] || chown "$uid:$(getvar EWIKI_GID)" "$STATE" 2>/dev/null || true
}
disable_timer() {
	if [ -f "$UNITS/$NAME.timer" ]; then
		if [ "$(id -u)" != 0 ]; then
			command -v sudo >/dev/null || die "the systemd timer needs root rights to remove"
			sudo "$0" --disable-timer; return 0
		fi
		[ -n "${EWIKI_UPDATE_UNITS:-}" ] || systemctl disable --now "$NAME.timer" >/dev/null 2>&1 || true
		rm -f "$UNITS/$NAME.timer" "$UNITS/$NAME.service"
		[ -n "${EWIKI_UPDATE_UNITS:-}" ] || systemctl daemon-reload
	fi
	remove_cron
	rm -f "$CONF" "$STATE"
	mode=
	say "update timer removed"
}

case "${1:-}" in
	"") update ;;
	--dry-run) dry=1; update ;;
	--enable-timer)
		shift
		at=03:30 cron=
		for a in "$@"; do case "$a" in --cron) cron=1 ;; *) at="$a" ;; esac; done
		enable_timer "$at" "$cron" ;;
	--disable-timer) disable_timer ;;
	--status)
		if [ -n "$mode" ]; then say "timer: $mode, daily at $time_, next run $(next_run) (UTC)"; else say "timer: off"; fi
		[ -f "$STATE" ] && say "state: $(cat "$STATE")"
		[ -e "$MARKER" ] && say "reported by the wiki: $(head -c 300 "$MARKER")"
		true ;;
	*) die "unknown option $1 (see the head of $0)" ;;
esac
