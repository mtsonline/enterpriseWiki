#!/usr/bin/env bash
# The whole life of a core plugin from a private repository, against a real
# wiki built from source (no simulated parts):
#
#   access data → install (tag v1.0.0) → build → automatic restart → plugin
#   active → manual build and restart over the API → update check finds
#   v1.1.0 → update → build → restart → plugin active in 1.1.0
#
# The plugin is mtsonline/enterprisewiki-testplugin (private). Needs:
#   EWIKI_TESTPLUGIN_TOKEN  read-only token for that repository
#   EWIKI_DATABASE_URL      an empty PostgreSQL database
# The source tree is copied to $EWIKI_LIFECYCLE_WORK (default
# /tmp/ewiki-lifecycle), so the checkout itself is not changed. Go, Node.js
# and npm must be installed (the wiki builds itself).
set -Eeuo pipefail

# Without a token, EWIKI_LIFECYCLE_SOURCE=DIR checks install, build and
# restart with a local copy of the plugin (no access data, no update check).
local_source="${EWIKI_LIFECYCLE_SOURCE:-}"
[ -n "$local_source" ] || : "${EWIKI_TESTPLUGIN_TOKEN:?read token for mtsonline/enterprisewiki-testplugin}"
: "${EWIKI_DATABASE_URL:?an empty PostgreSQL database}"
here="$(cd "$(dirname "$0")/../.." && pwd)"
work="${EWIKI_LIFECYCLE_WORK:-/tmp/ewiki-lifecycle}"
port="${EWIKI_LIFECYCLE_PORT:-8097}"
base="http://127.0.0.1:$port"
repo="github.com/mtsonline/enterprisewiki-testplugin"
plugin="acme.testplugin"
password="Lifecycle-Passw0rd!"

log() { echo "== $*" >&2; }
fail() {
	echo "FAIL: $*" >&2
	echo "--- last lines of the server log:" >&2
	tail -n 60 "$work/server.log" >&2 || true
	exit 1
}

stop() {
	if [ -f "$work/server.pid" ]; then
		kill "$(cat "$work/server.pid")" 2>/dev/null || true
		rm -f "$work/server.pid"
	fi
}
trap stop EXIT

log "copy the source tree to $work"
stop
rm -rf "$work"
mkdir -p "$work/src"
tar -C "$here" --exclude=./.git --exclude=./web/node_modules --exclude=./bin -cf - . | tar -C "$work/src" -xf -
src="$work/src"
(cd "$src/web" && npm ci --no-audit --no-fund >/dev/null)

log "build the wiki"
(cd "$src" && ./scripts/build.sh >/dev/null)

export EWIKI_CONFIG="$work/config.yaml" EWIKI_SOURCE="$src" EWIKI_ADMIN_PASSWORD="$password"
"$src/bin/ewiki" setup --non-interactive >/dev/null 2>&1
"$src/bin/ewiki" config set server.listen "127.0.0.1:$port" >/dev/null
"$src/bin/ewiki" config set server.base_url "$base" >/dev/null

log "start the wiki"
nohup "$src/bin/ewiki" serve >"$work/server.log" 2>&1 &
echo $! >"$work/server.pid"
ready() {
	for _ in $(seq 1 "${1:-60}"); do
		curl -fsS -o /dev/null "$base/readyz" 2>/dev/null && return 0
		sleep 1
	done
	return 1
}
ready || fail "the wiki did not start"

jar="$work/cookies"
# api METHOD PATH [BODY or - for stdin]: prints the answer, fails on 4xx/5xx
api() {
	local args=(-sS -b "$jar" -c "$jar" -H 'X-Requested-With: ewiki' -H 'Content-Type: application/json' -X "$1" -w '\n%{http_code}')
	if [ "${3:-}" = - ]; then
		args+=(--data-binary @-)
	elif [ -n "${3:-}" ]; then
		args+=(--data-binary "$3")
	fi
	curl "${args[@]}" "$base$2"
}
status() { tail -n1 <<<"$1"; }
body() { sed '$d' <<<"$1"; }

# wait_version VERSION: waits (build and restart) until the running wiki has
# the plugin in that version
wait_version() {
	local want=$1
	for _ in $(seq 1 120); do
		sleep 5
		ready 5 || continue
		res=$(api GET /api/v1/admin/plugins) || continue
		[ "$(status "$res")" = 200 ] || continue
		local v
		v=$(body "$res" | jq -r --arg id "$plugin" '.installed[]? | select(.id==$id) | .version' 2>/dev/null || true)
		if [ "$v" = "$want" ]; then return 0; fi
		if [ "$(body "$res" | jq -r '.build.finished_at != null and .build.ok == false' 2>/dev/null)" = true ]; then
			fail "the build failed: $(body "$res" | jq -r '.build.log')"
		fi
	done
	fail "$plugin $want did not become active"
}

log "sign in (access data need a fresh sign-in)"
res=$(api POST /api/v1/auth/login "{\"username\":\"admin\",\"password\":\"$password\"}")
[ "$(status "$res")" = 200 ] || fail "sign-in: $res"

if [ -n "$local_source" ]; then
	log "install the local copy: build and restart automatically"
	res=$(api POST /api/v1/admin/plugins/install "$(jq -nc --arg s "$local_source" '{source:$s, accept_external:true}')")
	[ "$(status "$res")" = 200 ] || fail "install: $(body "$res")"
fi

if [ -z "$local_source" ]; then
log "installing without access data is refused"
res=$(api POST /api/v1/admin/plugins/install "{\"source\":\"$repo\",\"ref\":\"v1.0.0\",\"accept_external\":true}")
[ "$(status "$res")" = 400 ] && grep -q "git clone failed" <<<"$res" || fail "install without access data: $res"

log "set the access data (token through stdin only)"
res=$(jq -n --arg t "$EWIKI_TESTPLUGIN_TOKEN" '{prefix:"https://github.com/mtsonline/enterprisewiki-testplugin", token:$t, note:"lifecycle test"}' |
	api PUT /api/v1/admin/plugins/credentials -)
[ "$(status "$res")" = 200 ] || fail "access data: $(body "$res")"
for where in "$(api GET /api/v1/admin/plugins/credentials)" "$(cat "$work/server.log")" "$(api GET '/api/v1/admin/audit/search?limit=50')"; do
	if grep -qF "$EWIKI_TESTPLUGIN_TOKEN" <<<"$where"; then fail "the token shows in an answer or the log"; fi
done


log "install v1.0.0: build and restart automatically"
res=$(api POST /api/v1/admin/plugins/install "{\"source\":\"$repo\",\"ref\":\"v1.0.0\",\"accept_external\":true}")
[ "$(status "$res")" = 200 ] || fail "install: $(body "$res")"
[ "$(body "$res" | jq -r '.origin.level')" = external ] || fail "origin: $(body "$res")"
fi
wait_version 1.0.0

log "build over the API, then restart over the API"
res=$(api POST /api/v1/admin/plugins/build)
[ "$(status "$res")" = 202 ] || fail "build: $(body "$res")"
for _ in $(seq 1 120); do
	sleep 5
	res=$(api GET /api/v1/admin/plugins)
	[ "$(body "$res" | jq -r '.build.running')" = false ] && break
done
[ "$(body "$res" | jq -r '.build.ok')" = true ] || fail "manual build: $(body "$res" | jq -r '.build.log')"
[ "$(body "$res" | jq -r '.restart_pending')" = true ] || fail "no restart pending after a manual build: $(body "$res")"
res=$(api POST /api/v1/admin/restart)
[ "$(status "$res")" = 202 ] || fail "restart: $(body "$res")"
sleep 3
ready 120 || fail "no restart"
res=$(api GET /api/v1/admin/plugins)
[ "$(body "$res" | jq -r '.restart_pending')" = false ] || fail "still pending after the restart: $(body "$res")"

if [ -n "$local_source" ]; then
	log "ok: install, build and restart (local source; access data and update need the token)"
	exit 0
fi

log "the update check finds v1.1.0 (with the access data)"
res=$(api GET '/api/v1/admin/updates?force=1')
item=$(body "$res" | jq -c --arg id "$plugin" '.items[] | select(.id==$id)')
[ "$(jq -r '.available' <<<"$item")" = true ] && [ "$(jq -r '.latest' <<<"$item")" = 1.1.0 ] || fail "update check: $item"

log "install the update: build and restart automatically"
res=$(api POST "/api/v1/admin/updates/$plugin" '{"accept_external":true}')
[ "$(status "$res")" = 200 ] || fail "update: $(body "$res")"
wait_version 1.1.0

log "remove the access data"
res=$(api DELETE '/api/v1/admin/plugins/credentials?prefix=https://github.com/mtsonline/enterprisewiki-testplugin')
[ "$(status "$res")" = 204 ] || fail "remove access data: $res"
if grep -qF "$EWIKI_TESTPLUGIN_TOKEN" "$work/config.yaml" "$work/server.log"; then fail "the token is still stored or logged"; fi
log "ok: install, build, restart and update of a private core plugin"
