#!/usr/bin/env bash
# Starts real office servers for the examples of the action register that
# need them (office.collabora.test, office.onlyoffice.test, core.media.open):
#
#   - Collabora Online (CODE), http://127.0.0.1:9981 (WOPI discovery)
#   - OnlyOffice Docs, http://127.0.0.1:8091 (API with JWT and WOPI)
#
# The images and versions are the ones of docker-compose.yml (kept up to
# date by Renovate there). Only the servers named in EWIKI_OFFICE_SERVERS
# (default: both) are started. The full round trip (a browser edits a
# document and the server saves it back) is scripts/test-docker-office.sh.
#
#   eval "$(tests/office/start.sh)"    # starts them, prints the environment
#   tests/office/start.sh stop         # stops them
#
# The JWT secret is generated on every start and only printed as an
# environment variable.
set -Eeuo pipefail

here="$(cd "$(dirname "$0")/../.." && pwd)"
servers="${EWIKI_OFFICE_SERVERS:-collabora onlyoffice}"
log() { echo "$*" >&2; }

stop() {
	docker rm -f ewiki-test-collabora ewiki-test-onlyoffice >/dev/null 2>&1 || true
}
if [ "${1:-}" = stop ]; then
	stop
	exit 0
fi
stop

# image reads the image of a service from docker-compose.yml (with the
# default version of its variable).
image() {
	sed -nE "s#^\s+image: ($1)\:\\\$\{[A-Z_]+:-([^}]+)\}.*#\1:\2#p" "$here/docker-compose.yml" | head -n1
}

# wait_http waits until url answers with 200.
wait_http() { # name url
	for _ in $(seq 1 240); do
		if curl -fsS -o /dev/null "$2" 2>/dev/null; then
			return 0
		fi
		sleep 1
	done
	log "$1 did not start; last lines of its log:"
	docker logs --tail 40 "ewiki-test-$1" >&2 || true
	exit 1
}

case " $servers " in *" collabora "*)
	img=$(image collabora/code)
	[ -n "$img" ] || { log "collabora image not found in docker-compose.yml"; exit 1; }
	# as in docker-compose.yml: own proof key, only the capabilities it needs
	docker run -d --name ewiki-test-collabora -p 127.0.0.1:9981:9980 \
		--cap-drop ALL --cap-add CHOWN --cap-add FOWNER --cap-add MKNOD --cap-add SYS_CHROOT \
		--cap-add SETUID --cap-add SETGID --cap-add DAC_OVERRIDE \
		-e aliasgroup1=http://127.0.0.1 -e server_name=127.0.0.1:9981 -e DONT_GEN_SSL_CERT=1 \
		-e extra_params="--o:ssl.enable=false --o:ssl.termination=false --o:admin_console.enable=false --o:welcome.enable=false --o:remote_font_config.url= --o:home_mode.enable=true --o:mount_jail_tree=false" \
		--entrypoint /bin/sh "$img" -c '[ -s /etc/coolwsd/proof_key ] || coolconfig generate-proof-key; exec /start-collabora-online.sh' >/dev/null
	wait_http collabora http://127.0.0.1:9981/hosting/discovery
	echo "export EWIKI_TEST_COLLABORA_URL=http://127.0.0.1:9981"
	;;
esac

case " $servers " in *" onlyoffice "*)
	img=$(image onlyoffice/documentserver)
	[ -n "$img" ] || { log "onlyoffice image not found in docker-compose.yml"; exit 1; }
	secret=$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')
	docker run -d --name ewiki-test-onlyoffice -p 127.0.0.1:8091:80 --security-opt no-new-privileges:true \
		-e JWT_ENABLED=true -e JWT_SECRET="$secret" -e JWT_HEADER=Authorization -e WOPI_ENABLED=true \
		-e ALLOW_PRIVATE_IP_ADDRESS=true -e ALLOW_META_IP_ADDRESS=false "$img" >/dev/null
	wait_http onlyoffice http://127.0.0.1:8091/healthcheck
	echo "export EWIKI_TEST_ONLYOFFICE_URL=http://127.0.0.1:8091"
	echo "export EWIKI_TEST_ONLYOFFICE_SECRET=$secret"
	;;
esac
