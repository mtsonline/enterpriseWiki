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
# - asks for the image variant (lite or full) on a new installation
# - asks how the wiki is reached: HTTPS through the bundled proxy (domain,
#   certificate authority, e-mail) or plain HTTP on an IP address
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
	setvar POSTGRES_VERSION "${have:-${recommended:-18}}"
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
ask() { # PROMPT DEFAULT – prints the answer (the default if empty)
	local a
	printf '%s [%s] ' "$1" "$2" >&2
	read -r a || die "no answer (end of input)"
	printf '%s' "${a:-$2}"
}
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
if [ -n "${SUDO_UID:-}" ]; then chown "$uid:$gid" .env; fi

# ---------------------------------------------------------------- folders
dirs=(data data/postgres data/wiki data/caddy data/caddy/data data/caddy/config)
for d in "${dirs[@]}"; do
	[ -d "$d" ] || { mkdir -p "$d"; say "created $d"; }
done
# everything below ./data must belong to the user (e.g. after a move, or files of older containers)
# (data/postgres/data: empty mount point that Docker creates for the images up to PostgreSQL 17)
foreign="$(find data -path data/postgres/data -prune -o \( ! -uid "$uid" -o ! -gid "$gid" \) -print -quit 2>/dev/null || true)"
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
chmod 700 data/postgres data/caddy
if [ "$(id -u)" = 0 ]; then chown "$uid:$gid" data data/postgres data/wiki; fi

say "ready – start with: docker compose up -d"
