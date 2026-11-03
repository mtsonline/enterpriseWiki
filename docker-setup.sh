#!/usr/bin/env bash
# Prepares a Docker Compose installation of Enterprise Wiki in this directory.
# Safe to run again at any time: it only does what is still missing.
#
#   ./docker-setup.sh            (as the user the containers should run as;
#                                 with sudo, the calling user is used)
#
# - creates .env from .env.example (random passwords) if it does not exist
# - writes EWIKI_UID / EWIKI_GID of the calling user into .env
# - sets POSTGRES_VERSION to the version of existing data (if not set yet)
# - creates ./data/postgres and ./data/wiki and gives them to
#   that user (asks for sudo only if files belong to someone else)
set -euo pipefail
cd "$(dirname "$0")"

say() { printf '%s\n' "$*"; }
die() { printf 'docker-setup: %s\n' "$*" >&2; exit 1; }

uid="${SUDO_UID:-$(id -u)}"
gid="${SUDO_GID:-$(id -g)}"
[ "$uid" != 0 ] || die "please run this as the normal user who operates the wiki (not as root); the containers will run as that user"
owner="$(id -nu "$uid" 2>/dev/null || echo "$uid")"

# ---------------------------------------------------------------- .env
setvar() { # NAME VALUE – set or replace a line in .env
	if grep -q "^$1=" .env; then
		sed -i "s|^$1=.*|$1=$2|" .env
	else
		printf '%s=%s\n' "$1" "$2" >> .env
	fi
}
getvar() { sed -n "s/^$1=//p" .env | tail -n 1; }
random() { LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom | head -c "$1"; }

if [ ! -f .env ]; then
	[ -f .env.example ] || die ".env.example is missing"
	cp .env.example .env
	chmod 600 .env
	setvar POSTGRES_PASSWORD "$(random 32)"
	# admin password: upper, lower, digits and a symbol
	setvar EWIKI_ADMIN_PASSWORD "$(random 16)-Wk7"
	say "created .env with random passwords (admin password: see EWIKI_ADMIN_PASSWORD in .env)"
fi
[ "$(getvar EWIKI_UID)" = "$uid" ] && [ "$(getvar EWIKI_GID)" = "$gid" ] || {
	setvar EWIKI_UID "$uid"
	setvar EWIKI_GID "$gid"
	say "containers run as $owner (uid $uid, gid $gid)"
}

# PostgreSQL version: the newest existing data directory, else the default of docker-compose.yml
if [ -z "$(getvar POSTGRES_VERSION)" ]; then
	have="$(ls -d data/postgres/[0-9]*/docker 2>/dev/null | sed -E 's#data/postgres/([0-9]+)/docker#\1#' | sort -n | tail -n 1 || true)"
	recommended="$(sed -nE 's/.*image: postgres:\$\{POSTGRES_VERSION:-([0-9]+)\}.*/\1/p' docker-compose.yml | head -n 1)"
	setvar POSTGRES_VERSION "${have:-${recommended:-18}}"
	say "POSTGRES_VERSION=$(getvar POSTGRES_VERSION)"
fi
if [ -n "${SUDO_UID:-}" ]; then chown "$uid:$gid" .env; fi

# ---------------------------------------------------------------- folders
dirs=(data data/postgres data/wiki)
for d in "${dirs[@]}"; do
	[ -d "$d" ] || { mkdir -p "$d"; say "created $d"; }
done
# everything below ./data must belong to the user (e.g. after a move, or files of older containers)
foreign="$(find data \( ! -uid "$uid" -o ! -gid "$gid" \) -print -quit 2>/dev/null || true)"
if [ -n "$foreign" ]; then
	say "files in ./data belong to another user (e.g. $foreign) – changing the owner to $owner"
	if [ "$(id -u)" = 0 ]; then
		chown -R "$uid:$gid" data
	elif command -v sudo >/dev/null; then
		sudo chown -R "$uid:$gid" data
	else
		die "please run: chown -R $uid:$gid $(pwd)/data (as root)"
	fi
fi
chmod 700 data/postgres
if [ "$(id -u)" = 0 ]; then chown "$uid:$gid" data data/postgres data/wiki; fi

say "ready – start with: docker compose up -d"
