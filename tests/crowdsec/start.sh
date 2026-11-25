#!/usr/bin/env bash
# Starts a real CrowdSec Local API for the tests of the plugin
# security.crowdsec (internal/server/crowdsec_test.go and the examples of
# the action register that need it):
#
#   - CrowdSec (official release from GitHub), Local API only (-no-cs: no
#     log processing), SQLite, without the Central API,
#     http://127.0.0.1:18081
#   - docker/crowdsec/ewiki-sync.sh next to it, as in the container of
#     docker-compose.crowdsec.yml: it registers the machine "ewiki" and
#     applies the allowlist the wiki writes to the exchange folder
#
# Normal processes, without containers, like tests/search/start.sh.
#
#   eval "$(tests/crowdsec/start.sh)"    # starts it, prints the environment
#   tests/crowdsec/start.sh stop         # stops it
#
# Work directory: $EWIKI_CROWDSEC_WORK (default /tmp/ewiki-crowdsec), the
# downloaded programs in $EWIKI_CROWDSEC_CACHE (default: the work
# directory), the port $EWIKI_CROWDSEC_PORT (default 18081). The
# password of the machine is generated on every start and only printed as
# environment variable. A second machine "cli" lets the tests add decisions
# with cscli (printed as EWIKI_TEST_CROWDSEC_CSCLI).
set -Eeuo pipefail

# renovate: datasource=github-releases depName=crowdsecurity/crowdsec
CROWDSEC_VERSION=v1.8.2

work="${EWIKI_CROWDSEC_WORK:-/tmp/ewiki-crowdsec}"
cache="${EWIKI_CROWDSEC_CACHE:-$work}"
port="${EWIKI_CROWDSEC_PORT:-18081}"
here="$(cd "$(dirname "$0")/../.." && pwd)"

stop() {
	for p in crowdsec sync; do
		if [ -f "$work/$p.pid" ]; then
			kill "$(cat "$work/$p.pid")" 2>/dev/null || true
			rm -f "$work/$p.pid"
		fi
	done
}
if [ "${1:-}" = stop ]; then
	stop
	exit 0
fi
stop
mkdir -p "$work"
log() { echo "$*" >&2; }
secret() { head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n'; }

# only the two programs and the default configuration are kept (the
# release is about 120 MB)
mkdir -p "$cache"
bin="$cache/bin-$CROWDSEC_VERSION"
if [ ! -x "$bin/crowdsec" ]; then
	tgz="$cache/crowdsec-$CROWDSEC_VERSION.tgz"
	log "downloading CrowdSec $CROWDSEC_VERSION"
	curl -fsSL --retry 5 --retry-all-errors -o "$tgz" "https://github.com/crowdsecurity/crowdsec/releases/download/$CROWDSEC_VERSION/crowdsec-release.tgz"
	rm -rf "$cache/unpack" && mkdir -p "$cache/unpack" "$bin"
	tar -xzf "$tgz" -C "$cache/unpack" --strip-components=1
	cp "$cache/unpack/cmd/crowdsec/crowdsec" "$cache/unpack/cmd/crowdsec-cli/cscli" "$bin/"
	cp -r "$cache/unpack/config" "$bin/config"
	rm -rf "$cache/unpack" "$tgz"
fi

# a fresh configuration and database on every start
run="$work/run"
rm -rf "$run" && mkdir -p "$run/data" "$run/config/hub" "$run/config/notifications" "$run/plugins" "$run/exchange"
cp "$bin/config/profiles.yaml" "$bin/config/simulation.yaml" "$run/config/"
cp -r "$bin/config/patterns" "$run/config/"
touch "$run/config/acquis.yaml" "$run/config/local_api_credentials.yaml"
echo '{}' > "$run/config/hub/.index.json"
cat > "$run/config.yaml" <<EOF
common: {log_media: stdout, log_level: warning}
config_paths: {config_dir: $run/config, data_dir: $run/data, hub_dir: $run/config/hub, index_path: $run/config/hub/.index.json, notification_dir: $run/config/notifications, plugin_dir: $run/plugins}
crowdsec_service: {acquisition_path: $run/config/acquis.yaml}
db_config: {type: sqlite, db_path: $run/data/crowdsec.db}
api:
  client: {credentials_path: $run/config/local_api_credentials.yaml}
  server:
    listen_uri: 127.0.0.1:$port
    profiles_path: $run/config/profiles.yaml
prometheus: {enabled: false}
EOF
cscli="$bin/cscli -c $run/config.yaml"
# the machine of cscli itself (writes local_api_credentials.yaml)
$cscli machines add cli --password "$(secret)" -f "$run/config/local_api_credentials.yaml" --force > /dev/null 2>&1

(cd "$run" && exec "$bin/crowdsec" -c "$run/config.yaml" -no-cs) > "$work/crowdsec.log" 2>&1 &
echo $! > "$work/crowdsec.pid"
for _ in $(seq 1 60); do
	if curl -fsS -o /dev/null "http://127.0.0.1:$port/health" 2>/dev/null; then
		break
	fi
	sleep 1
done
# the answer must come from this CrowdSec (not from another one on the port)
if ! kill -0 "$(cat "$work/crowdsec.pid")" 2>/dev/null || ! curl -fsS -o /dev/null "http://127.0.0.1:$port/health"; then
	log "CrowdSec did not start:"
	tail -n 40 "$work/crowdsec.log" >&2
	exit 1
fi

pw=$(secret)
CSCLI="$cscli" EWIKI_SYNC_DIR="$run/exchange" EWIKI_SYNC_INTERVAL=1 EWIKI_CROWDSEC_PASSWORD="$pw" \
	"$here/docker/crowdsec/ewiki-sync.sh" > "$work/sync.log" 2>&1 &
echo $! > "$work/sync.pid"
# the machine is registered when it can sign in
for _ in $(seq 1 60); do
	if curl -fsS -o /dev/null -X POST -H 'Content-Type: application/json' \
		-d "{\"machine_id\":\"ewiki\",\"password\":\"$pw\",\"scenarios\":[]}" "http://127.0.0.1:$port/v1/watchers/login" 2>/dev/null; then
		break
	fi
	sleep 1
done
if ! curl -fsS -o /dev/null -X POST -H 'Content-Type: application/json' \
	-d "{\"machine_id\":\"ewiki\",\"password\":\"$pw\",\"scenarios\":[]}" "http://127.0.0.1:$port/v1/watchers/login"; then
	log "the machine ewiki cannot sign in; log of ewiki-sync.sh:"
	cat "$work/sync.log" >&2
	exit 1
fi

echo "export EWIKI_TEST_CROWDSEC_URL=http://127.0.0.1:$port"
echo "export EWIKI_TEST_CROWDSEC_PASSWORD=$pw"
echo "export EWIKI_TEST_CROWDSEC_CSCLI='$cscli'"
echo "export EWIKI_TEST_CROWDSEC_EXCHANGE=$run/exchange"
echo "export EWIKI_CROWDSEC_EXCHANGE=$run/exchange"
