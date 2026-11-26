#!/usr/bin/env bash
# Prepares a Docker Compose installation of Enterprise Wiki in this directory.
# Safe to run again at any time: it only does what is still missing.
#
#   ./docker-setup.sh                (as the user who runs the wiki)
#   sudo ./docker-setup.sh           (the calling user runs the wiki)
#   ./docker-setup.sh --user NAME    (as root: NAME runs the wiki)
#
# Who runs the wiki:
# - started by a normal user (also with sudo): that user. Without access to
#   Docker it stops with a hint (group docker or rootless Docker).
# - started by root: asks for the user (--user NAME without a terminal;
#   default "ewiki"), creates the user if missing and adds it to the group
#   docker. All files of this directory then belong to that user (except
#   ./data/crowdsec, which CrowdSec keeps as root in its container).
# The containers of the wiki, the database and the proxy always run as this
# user (EWIKI_UID / EWIKI_GID in .env), also when root starts them with
# docker compose.
#
# Also:
# - creates .env from .env.example (random passwords) if it does not exist
# - sets POSTGRES_VERSION to the version of existing data (if not set yet)
# - asks for the image variant (lite or full) on a new installation
# - asks how the wiki is reached: HTTPS through the bundled proxy (domain,
#   certificate authority, e-mail) or plain HTTP on an IP address
# - with HTTPS: asks for an office server (Collabora or OnlyOffice) on its
#   own host name
# - asks whether new releases are installed automatically (daily timer of
#   scripts/update-docker.sh: systemd with root or sudo, else crontab)
# - creates ./data/postgres and ./data/wiki and gives them to that user
set -euo pipefail
cd "$(dirname "$0")"

say() { printf '%s\n' "$*"; }
die() { printf 'docker-setup: %s\n' "$*" >&2; exit 1; }
# ask PROMPT DEFAULT – prints the answer (the default if empty)
ask() {
	local a
	printf '%s [%s] ' "$1" "$2" >&2
	read -r a || die "no answer (end of input)"
	printf '%s' "${a:-$2}"
}
getvar() { [ -f .env ] || return 0; sed -n "s/^$1=//p" .env | tail -n 1; }

user_arg=
while [ $# -gt 0 ]; do
	case "$1" in
		--user) [ $# -ge 2 ] || die "--user needs a name"; user_arg="$2"; shift 2 ;;
		--user=*) user_arg="${1#--user=}"; shift ;;
		-h|--help) sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
		*) die "unknown option $1 (see --help)" ;;
	esac
done

# ---------------------------------------------------------------- user
# as_root: run by root itself (not through sudo by a normal user)
as_root=
if [ "$(id -u)" = 0 ] && [ "${SUDO_UID:-0}" = 0 ]; then as_root=1; fi
if [ -n "$user_arg" ] && [ -z "$as_root" ]; then
	die "--user is only for root; as a normal user, the wiki runs as you"
fi

if [ -n "$as_root" ]; then
	# which user: the option, else the one of an earlier run, else ask
	owner="$user_arg"
	if [ -z "$owner" ]; then
		prev="$(getvar EWIKI_UID || true)"
		if [ -n "$prev" ] && [ "$prev" != 0 ]; then owner="$(getent passwd "$prev" | cut -d: -f1 || true)"; fi
	fi
	if [ -z "$owner" ]; then
		[ -t 0 ] || die "run as root, the wiki needs a user of its own: ./docker-setup.sh --user NAME"
		say "The wiki does not run as root. It runs as a normal user, who owns all its files."
		owner="$(ask 'User who runs the wiki (created if missing):' ewiki)"
	fi
	[[ "$owner" =~ ^[a-z_][a-z0-9_-]{0,31}$ ]] || die "not a valid user name: $owner (lower case letters, digits, _ and -)"
	[ "$owner" != root ] || die "the wiki must not run as root: choose another user"
	if ! id -u "$owner" >/dev/null 2>&1; then
		command -v useradd >/dev/null || die "useradd is missing: please create the user $owner yourself"
		useradd --create-home --user-group --shell /bin/bash "$owner"
		say "created the user $owner"
	fi
	uid="$(id -u "$owner")"
	gid="$(id -g "$owner")"
	[ "$uid" != 0 ] || die "$owner has the user id 0 (root): choose another user"
	# Docker without root rights: the group docker
	if getent group docker >/dev/null; then
		if ! id -nG "$owner" | tr ' ' '\n' | grep -qx docker; then
			usermod -aG docker "$owner"
			say "added $owner to the group docker (in an open login of $owner, it applies after logging in again)"
		fi
	else
		say "there is no group docker (rootless Docker?): $owner must be able to use Docker"
	fi
	# commands as that user (with its new groups)
	if command -v runuser >/dev/null; then
		as_owner() { runuser -u "$owner" -- "$@"; }
	elif command -v sudo >/dev/null; then
		as_owner() { sudo -u "$owner" -- "$@"; }
	else
		die "neither runuser nor sudo is available to check Docker as $owner"
	fi
	# the user must reach this directory (not below /root, for example)
	case "$PWD" in /|/root|/root/*) die "the wiki must not live in $PWD: move this directory (e.g. to /opt/enterprise-wiki or /home/$owner/wiki) and run it there" ;; esac
	chown "$uid:$gid" .
	as_owner test -x "$PWD" -a -w "$PWD" || die "$owner cannot reach $PWD (a folder above it is closed): move this directory (e.g. to /opt/enterprise-wiki or /home/$owner/wiki)"
else
	uid="${SUDO_UID:-$(id -u)}"
	gid="${SUDO_GID:-$(id -g)}"
	owner="$(id -nu "$uid" 2>/dev/null || echo "$uid")"
	if [ -n "${SUDO_UID:-}" ]; then
		as_owner() { sudo -u "#$uid" -- "$@"; }
	else
		as_owner() { "$@"; }
	fi
fi

# the user who runs the wiki must be able to use Docker (the group docker,
# or rootless Docker): docker compose and the update helper
# (scripts/update-docker.sh) run as this user
docker_cmd="${EWIKI_SETUP_DOCKER:-docker}" # (tests: a fake command)
command -v "$docker_cmd" >/dev/null || die "Docker is not installed (https://docs.docker.com/engine/install/)"
if ! as_owner "$docker_cmd" info >/dev/null 2>&1; then
	die "$owner cannot use Docker. Either add the user to the group docker, then log out and in again
(or start a new login shell) and run ./docker-setup.sh again:
  sudo usermod -aG docker $owner
or set up rootless Docker for $owner (https://docs.docker.com/engine/security/rootless/).
Is the Docker service running? (systemctl status docker)"
fi

# ---------------------------------------------------------------- .env
setvar() { # NAME VALUE – set or replace a line in .env
	if grep -q "^$1=" .env; then
		sed -i "s|^$1=.*|$1=$2|" .env
	else
		printf '%s=%s\n' "$1" "$2" >> .env
	fi
}
# (a fixed amount of random bytes, read to the end: no "broken pipe" with pipefail)
random() { head -c 4096 /dev/urandom | LC_ALL=C tr -dc 'A-Za-z0-9' | cut -c "1-$1"; }

fresh=
if [ ! -f .env ]; then
	fresh=1
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
	setvar POSTGRES_VERSION "${have:-${recommended:-19}}"
	say "POSTGRES_VERSION=$(getvar POSTGRES_VERSION)"
fi
# image variant (asked once; non-interactive: lite)
if [ -z "$(getvar EWIKI_IMAGE_TAG)" ] || [ -n "$fresh" ]; then
	tag=latest
	if [ -t 0 ]; then
		printf 'Image: [l]ite (small, no plugin installation) or [f]ull (installs and builds plugins)? [l] '
		read -r answer || answer=
		case "$answer" in f|F|full) tag=full ;; esac
	fi
	setvar EWIKI_IMAGE_TAG "$tag"
	say "EWIKI_IMAGE_TAG=$tag"
fi

# access (asked once; non-interactive: plain HTTP on 127.0.0.1)
valid_domain() { [[ "$1" =~ ^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$ ]] && [ "${#1}" -le 253 ]; }
valid_email() { [[ "$1" =~ ^[A-Za-z0-9._%+-]{1,64}@([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}$ ]]; }
valid_ipv4() {
	[[ "$1" =~ ^([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})$ ]] || return 1
	local o; for o in "${BASH_REMATCH[@]:1}"; do [ "$((10#$o))" -le 255 ] || return 1; done
}
url_re='^https?://[]A-Za-z0-9.:[-]+(/[A-Za-z0-9._~/-]*)?$'
valid_ipv6() { [[ "$1" =~ ^[0-9a-fA-F:]{2,39}$ ]] && [[ "$1" == *:* ]]; }
port="$(getvar EWIKI_PORT)"; port="${port##*:}"
[[ "$port" =~ ^[0-9]{1,5}$ ]] && [ "$port" -ge 1 ] && [ "$port" -le 65535 ] || port=8080
if [ -z "$(getvar EWIKI_HTTPS)" ] || [ -n "$fresh" ]; then
	https=no
	if [ -t 0 ]; then
		case "$(ask 'HTTPS with the bundled reverse proxy (Caddy, automatic certificates)? [y/n]' n)" in y|Y|yes|j|J|ja) https=yes ;; esac
	fi
	if [ "$https" = yes ]; then
		while :; do
			domain="$(ask 'Domain of the wiki (DNS must point to this server), e.g. wiki.example.com:' '')"
			domain="${domain,,}"
			valid_domain "$domain" && break
			say "  not a valid domain name"
		done
		say "Certificate authority:"
		say "  1) Let's Encrypt"
		say "  2) ZeroSSL"
		say "  3) Let's Encrypt staging (test certificates, for trying out)"
		while :; do
			case "$(ask 'Choice' 1)" in
				1) acme=letsencrypt; break ;;
				2) acme=zerossl; break ;;
				3) acme=letsencrypt-staging; break ;;
			esac
		done
		while :; do
			email="$(ask 'E-mail address for the certificate authority (expiry notices):' '')"
			valid_email "$email" && break
			say "  not a valid e-mail address"
		done
		setvar EWIKI_HTTPS proxy
		setvar COMPOSE_PROFILES proxy
		setvar EWIKI_DOMAIN "$domain"
		setvar EWIKI_BASE_URL "https://$domain"
		setvar EWIKI_ACME "$acme"
		setvar EWIKI_ACME_EMAIL "$email"
		# the wiki itself is only reachable through the proxy
		setvar EWIKI_PORT "127.0.0.1:$port"
		setvar EWIKI_TRUSTED_PROXIES 172.30.80.250
		say "HTTPS via the bundled proxy: https://$domain ($acme); ports 80 and 443 must be reachable from the internet"
	else
		ip=127.0.0.1
		if [ -t 0 ]; then
			while :; do
				ip="$(ask 'IP address the wiki listens on (127.0.0.1 = only this computer, 0.0.0.0 = all):' 127.0.0.1)"
				valid_ipv4 "$ip" || valid_ipv6 "$ip" && break
				say "  not a valid IP address"
			done
		fi
		bind="$ip"
		if valid_ipv6 "$ip"; then bind="[$ip]"; fi
		setvar EWIKI_HTTPS off
		setvar EWIKI_PORT "$bind:$port"
		base="http://localhost:$port"
		case "$ip" in 127.*|::1) ;; *)
			say "  without HTTPS, passwords are sent unencrypted: put the wiki behind a reverse proxy with TLS"
			if [ -t 0 ]; then
				while :; do
					base="$(ask 'Address users open (base URL):' "http://$(hostname -f 2>/dev/null || hostname):$port")"
					[[ "$base" =~ $url_re ]] && break
					say "  not a valid URL"
				done
			fi
		esac
		setvar EWIKI_BASE_URL "$base"
		if [ -t 0 ]; then
			case "$(ask 'Does an own reverse proxy (nginx, Apache) on this computer forward to the wiki? [y/n]' n)" in
				y|Y|yes|j|J|ja) setvar EWIKI_TRUSTED_PROXIES 172.30.80.1; say "  trusted proxy: 172.30.80.1 (the host, as seen from the container)" ;;
			esac
		fi
		say "HTTP on $bind:$port ($base)"
	fi
fi
# office server (asked once, only with the bundled proxy; non-interactive: none)
if [ "$(getvar EWIKI_HTTPS)" = proxy ] && { [ -z "$(getvar EWIKI_OFFICE)" ] || [ -n "$fresh" ]; }; then
	office=none
	if [ -t 0 ]; then
		say "Office server for documents, spreadsheets and presentations:"
		say "  1) none"
		say "  2) Collabora Online (CODE)"
		say "  3) OnlyOffice Docs (needs about 4 GB RAM)"
		while :; do
			case "$(ask 'Choice' 1)" in
				1) break ;;
				2) office=collabora; break ;;
				3) office=onlyoffice; break ;;
			esac
		done
	fi
	if [ "$office" != none ]; then
		domain="$(getvar EWIKI_DOMAIN)"
		parent="${domain#*.}"
		def=office
		[ "$office" = onlyoffice ] && def=docs
		while :; do
			ohost="$(ask "Host name of the office server (own DNS name pointing to this server):" "$def.$parent")"
			ohost="${ohost,,}"
			if ! valid_domain "$ohost"; then say "  not a valid domain name"; continue; fi
			[ "$ohost" != "$domain" ] && break
			say "  must differ from the wiki's domain (an own origin keeps the wiki's cookies safe)"
		done
		setvar COMPOSE_PROFILES "proxy,$office"
		setvar EWIKI_MEDIA_VIEWER "office.$office"
		if [ "$office" = collabora ]; then
			setvar EWIKI_COLLABORA_HOST "$ohost"
			setvar EWIKI_COLLABORA_URL http://collabora:9980
		else
			setvar EWIKI_ONLYOFFICE_HOST "$ohost"
			setvar EWIKI_ONLYOFFICE_URL "https://$ohost"
			setvar EWIKI_ONLYOFFICE_INTERNAL_URL http://onlyoffice
			[ -n "$(getvar EWIKI_ONLYOFFICE_JWT_SECRET)" ] || setvar EWIKI_ONLYOFFICE_JWT_SECRET "$(random 48)"
		fi
		say "office: $office at https://$ohost (default viewer for office files)"
	fi
	setvar EWIKI_OFFICE "$office"
fi
# CrowdSec with the bundled proxy (asked once; non-interactive: none)
if [ "$(getvar EWIKI_HTTPS)" = proxy ] && { [ -z "$(getvar EWIKI_SECURITY)" ] || [ -n "$fresh" ]; }; then
	security=none
	if [ -t 0 ]; then
		say "CrowdSec blocks addresses that scan or attack the wiki and requests against known"
		say "vulnerabilities. It runs locally only: nothing is sent to CrowdSec."
		case "$(ask 'Protect the proxy with CrowdSec? [y/n]' y)" in y|Y|yes|j|J|ja) security=crowdsec ;; esac
	fi
	setvar EWIKI_SECURITY "$security"
	if [ "$security" = crowdsec ]; then
		setvar COMPOSE_FILE docker-compose.yml:docker-compose.crowdsec.yml
		[ -n "$(getvar CROWDSEC_BOUNCER_KEY)" ] || setvar CROWDSEC_BOUNCER_KEY "$(random 48)"
		[ -n "$(getvar EWIKI_CROWDSEC_PASSWORD)" ] || setvar EWIKI_CROWDSEC_PASSWORD "$(random 48)"
		say "CrowdSec: on (docker-compose.crowdsec.yml; the proxy is built on the first start)"
	else
		setvar COMPOSE_FILE docker-compose.yml
		say "CrowdSec: off"
	fi
fi
# automatic updates (asked once; non-interactive: not set up). A self-built
# image is updated from its source, so the question does not come up.
if [ -t 0 ] && [ -z "$(getvar EWIKI_AUTO_UPDATE)" ] && [[ "${COMPOSE_FILE:-$(getvar COMPOSE_FILE)}" != *build* ]]; then
	say "Automatic updates: once a day the update helper (scripts/update-docker.sh) checks whether the wiki"
	say "reported a new release; only then it backs up the database and installs the new image."
	auto=off
	case "$(ask 'Install new releases automatically? [y/n]' y)" in y|Y|yes|j|J|ja) auto=on ;; esac
	if [ "$auto" = on ]; then
		while :; do
			at="$(ask 'Time of day (HH:MM, plus up to one hour):' 03:30)"
			[[ "$at" =~ ^([01][0-9]|2[0-3]):[0-5][0-9]$ ]] && break
			say "  please enter a time like 03:30"
		done
	fi
	setvar EWIKI_AUTO_UPDATE "$auto"
fi
if [ "$(id -u)" = 0 ]; then chown "$uid:$gid" .env; fi

# ---------------------------------------------------------------- folders
dirs=(data data/postgres data/wiki data/caddy data/caddy/data data/caddy/config data/caddy/logs)
for d in "${dirs[@]}"; do
	[ -d "$d" ] || { mkdir -p "$d"; say "created $d"; }
done
# everything below ./data must belong to the user (e.g. after a move, or files of older containers)
# (data/postgres/data: empty mount point that Docker creates for the images up to PostgreSQL 17;
# data/crowdsec: CrowdSec runs as root in its container and keeps its own files)
own_data() { find data \( -path data/postgres/data -o -path data/crowdsec \) -prune -o \( ! -uid "$uid" -o ! -gid "$gid" \) "$@"; }
foreign="$(own_data -print -quit 2>/dev/null || true)"
if [ -n "$foreign" ]; then
	say "files in ./data belong to another user (e.g. $foreign) – changing the owner to $owner"
	if [ "$(id -u)" = 0 ]; then
		own_data -exec chown -h "$uid:$gid" {} +
	elif command -v sudo >/dev/null; then
		sudo find data \( -path data/postgres/data -o -path data/crowdsec \) -prune -o \( ! -uid "$uid" -o ! -gid "$gid" \) -exec chown -h "$uid:$gid" {} +
	else
		die "please run (as root): chown -R $uid:$gid $(pwd)/data"
	fi
fi
chmod 700 data/postgres data/caddy
if [ "$(getvar EWIKI_SECURITY)" = crowdsec ]; then
	# CrowdSec runs as root in its container (its own files)
	mkdir -p data/crowdsec/data data/crowdsec/config
	chmod 700 data/crowdsec
	# the password of the machine "ewiki" (installations set up before the
	# CrowdSec page of the administration)
	[ -n "$(getvar EWIKI_CROWDSEC_PASSWORD)" ] || setvar EWIKI_CROWDSEC_PASSWORD "$(random 48)"
	# the wiki writes the allowed addresses here, CrowdSec reads them
	mkdir -p data/wiki/crowdsec-exchange
	chmod 755 data/wiki/crowdsec-exchange
fi
if [ "$(id -u)" = 0 ]; then
	chown "$uid:$gid" data data/postgres data/wiki
	if [ -d data/wiki/crowdsec-exchange ]; then chown "$uid:$gid" data/wiki/crowdsec-exchange; fi
fi

# run by root: everything here belongs to the user who runs the wiki (files
# root created now or earlier); not ./data/crowdsec (CrowdSec runs as root
# in its container) and the empty mount point of older PostgreSQL images
if [ -n "$as_root" ]; then
	find . \( -path ./data/crowdsec -o -path ./data/postgres/data \) -prune -o \( ! -uid "$uid" -o ! -gid "$gid" \) -exec chown -h "$uid:$gid" {} +
fi

# the timer needs .env and ./data: set up last (systemd with root or sudo, else crontab)
if [ -n "${at:-}" ] && [ "$(getvar EWIKI_AUTO_UPDATE)" = on ]; then
	scripts/update-docker.sh --enable-timer "$at" || say "the update timer could not be set up; later: scripts/update-docker.sh --enable-timer $at"
fi

if [ -n "$as_root" ]; then
	say "ready – start as $owner: sudo -u $owner docker compose up -d"
	say "(started by root, the containers run as $owner too)"
else
	say "ready – start with: docker compose up -d"
fi
