#!/usr/bin/env bash
# Local check before a pull request: the checks of the CI, in the order that
# finds mistakes fastest. Quick checks first (about 3 minutes, stop at the
# first mistake), then the long ones, side by side where the machine allows.
#
#   scripts/precheck.sh                 # fast + go + e2e (what most changes need)
#   scripts/precheck.sh fast            # only the quick checks
#   scripts/precheck.sh fast go         # quick checks and all Go tests
#   scripts/precheck.sh all             # fast go e2e security docker
#
# Parts:
#   fast      gofmt, generated plugin imports, go vet, plugin contract,
#             licences, documentation, traps fit, npm release age, build,
#             typecheck and unit tests of the frontend
#   go        all Go tests with the race detector against PostgreSQL, the
#             server tests in the three shards of the CI (each its own database)
#   e2e       build, a fresh wiki, all browser tests with axe, every action
#             of the interface used (tools/uicoverage)
#   security  govulncheck, gosec, npm audit
#   docker    images and the Docker tests of the CI (needs a running Docker)
#
# Needs: Go, Node, a PostgreSQL user that may create databases:
#   EWIKI_PRECHECK_PG=postgres://wiki:wiki@localhost:5432   (default)
# Optional: EWIKI_PRECHECK_PORT (default 18080), EWIKI_PRECHECK_JOBS (parallel
# long parts, default: number of CPUs, at most 4).
set -euo pipefail
cd "$(dirname "$0")/.."

PG="${EWIKI_PRECHECK_PG:-postgres://wiki:wiki@localhost:5432}"
PORT="${EWIKI_PRECHECK_PORT:-18080}"
CPUS="$(nproc 2>/dev/null || echo 2)"
JOBS="${EWIKI_PRECHECK_JOBS:-$(( CPUS > 4 ? 4 : CPUS ))}"
WORK="$(mktemp -d /tmp/ewiki-precheck.XXXXXX)"
ADMIN_PW='Precheck-Passw0rd!'
export GOFLAGS="${GOFLAGS:-}"

parts=("$@")
[ ${#parts[@]} -eq 0 ] && parts=(fast go e2e)
[ "${parts[*]}" = all ] && parts=(fast go e2e security docker)
want() { local p; for p in "${parts[@]}"; do [ "$p" = "$1" ] && return 0; done; return 1; }

start=$(date +%s)
say() { printf '\n== %s (%ds)\n' "$*" $(( $(date +%s) - start )); }
fail() { printf '\nFAILED: %s\n' "$*" >&2; exit 1; }

# psql against the maintenance database of the PostgreSQL server
pgq() { psql -q "$PG/postgres?sslmode=disable" -c "$1" >/dev/null; }
newdb() { pgq "DROP DATABASE IF EXISTS $1" && pgq "CREATE DATABASE $1"; }
pids=()
# stops a part with everything it started (go test runs test binaries)
killtree() {
	local c
	for c in $(pgrep -P "$1" 2>/dev/null); do killtree "$c"; done
	kill "$1" 2>/dev/null || true
}
cleanup() {
	local p
	for p in "${pids[@]}"; do killtree "$p"; done
	# databases of parts that were stopped early
	for p in $(psql -Atq "$PG/postgres?sslmode=disable" -c "SELECT datname FROM pg_database WHERE datname LIKE 'ewiki_precheck_%_$$'" 2>/dev/null); do
		pgq "DROP DATABASE IF EXISTS $p" || true
	done
}
trap cleanup EXIT

# ---------------------------------------------------------------- fast
if want fast; then
	say "quick checks"
	out="$(gofmt -l . | grep -v '^web/node_modules/' || true)"
	[ -z "$out" ] || fail "gofmt: $out"
	go run ./tools/genplugins && git diff --quiet -- plugins/all || fail "generated plugin imports changed (go run ./tools/genplugins; commit plugins/all)"
	go vet ./... || fail "go vet"
	(cd sdk && go vet ./... && go run ./tools/protocheck) || fail "plugin contract (sdk/tools/protocheck)"
	go run ./tools/notices -check || fail "licences (tools/notices)"
	go run ./tools/docscheck || fail "documentation (tools/docscheck)"
	go run ./tools/traps -fit || fail "a trap no longer fits the code (tools/traps -fit)"
	if git rev-parse -q --verify origin/main >/dev/null; then
		git show origin/main:web/package-lock.json > "$WORK/old-lock.json"
		go run ./tools/releaseage -old "$WORK/old-lock.json" -new web/package-lock.json || fail "npm versions younger than 3 days (tools/releaseage)"
	fi
	(cd web && node build.mjs > "$WORK/web-build.log" 2>&1) || { tail -n 30 "$WORK/web-build.log"; fail "build of the frontend"; }
	(cd web && npm run -s typecheck) || fail "typecheck of the frontend"
	(cd web && npm run -s test:unit > "$WORK/unit.log" 2>&1) || { tail -n 40 "$WORK/unit.log"; fail "unit tests of the frontend"; }
fi

# ---------------------------------------------------------------- long parts, side by side
# each runs in the background with its own log; at most $JOBS at a time
long=()
run() { # name, command...
	local name="$1"; shift
	while [ "$(jobs -rp | wc -l)" -ge "$JOBS" ]; do sleep 2; done
	( "$@" > "$WORK/$name.log" 2>&1; echo $? > "$WORK/$name.status" ) &
	pids+=($!)
	long+=("$name")
}

gotest() { # shard
	local shard="$1" db="ewiki_precheck_s$1_$$" rc=0
	newdb "$db"
	export EWIKI_TEST_DATABASE_URL="$PG/$db?sslmode=disable"
	if [ "$shard" = 0 ]; then
		# shellcheck disable=SC2046
		go test -race -count=1 -timeout 60m $(go list ./... | grep -v '/internal/server$') || rc=$?
	else
		go test -race -count=1 -timeout 60m -run "$(go run ./tools/testshard -shard "$shard" -of 3 ./internal/server)" ./internal/server || rc=$?
	fi
	pgq "DROP DATABASE IF EXISTS $db" || true
	return "$rc"
}

e2e() {
	local db="ewiki_precheck_e2e_$$" dir="$WORK/wiki"
	newdb "$db"
	mkdir -p "$dir"
	export EWIKI_CONFIG="$dir/config.yaml" EWIKI_DATABASE_URL="$PG/$db?sslmode=disable" EWIKI_ADMIN_PASSWORD="$ADMIN_PW"
	./bin/ewiki setup --non-interactive
	./bin/ewiki config set server.listen ":$PORT"
	./bin/ewiki config set server.base_url "http://localhost:$PORT"
	./bin/ewiki config set webhooks.allow_private true
	./bin/ewiki config set feeds.allow_private true
	./bin/ewiki serve > "$dir/server.log" 2>&1 &
	local srv=$!
	for _ in $(seq 1 60); do curl -fs "http://localhost:$PORT/readyz" >/dev/null && break; sleep 1; done
	local rc=0
	(cd web && EWIKI_E2E_URL="http://localhost:$PORT" EWIKI_E2E_USER=admin EWIKI_E2E_PASSWORD="$ADMIN_PW" \
		EWIKI_E2E_REQUIRE_AXE=1 EWIKI_E2E_UI_REQUESTS="$dir/ui-requests.json" npm run -s test:e2e) || rc=$?
	[ "$rc" = 0 ] && { go run ./tools/uicoverage -requests "$dir/ui-requests.json" || rc=$?; }
	kill "$srv" 2>/dev/null || true
	[ "$rc" = 0 ] || { echo "--- server log"; tail -n 80 "$dir/server.log"; }
	pgq "DROP DATABASE IF EXISTS $db" || true
	return "$rc"
}

security() {
	scripts/govulncheck.sh
	go run github.com/securego/gosec/v2/cmd/gosec@latest -quiet -exclude-generated -exclude-dir=web -conf .gosec.json ./...
	(cd web && npm audit --audit-level=low)
}

dockertests() {
	docker info >/dev/null 2>&1 || { echo "Docker is not running"; return 1; }
	docker build --target lite -t enterprisewiki:ci .
	scripts/test-docker-plugins.sh
	scripts/test-docker-proxy.sh enterprisewiki:ci
}

# build once before the long parts: a build while Go tests compile (the
# frontend is embedded) would break them
if want go || want e2e; then
	say "build"
	./scripts/build.sh > "$WORK/build.log" 2>&1 || { tail -n 40 "$WORK/build.log"; fail "build"; }
fi

if want go; then
	say "Go tests (race detector), 4 parts, $JOBS at a time"
	# the browser tests are the longest part: start them first
	want e2e && run e2e e2e
	for s in 1 2 3 0; do run "go-$s" gotest "$s"; done
elif want e2e; then
	run e2e e2e
fi
want security && run security security
want docker && run docker dockertests

if [ ${#long[@]} -gt 0 ]; then
	say "waiting for: ${long[*]} (logs in $WORK)"
	wait || true
	bad=()
	for n in "${long[@]}"; do
		[ "$(cat "$WORK/$n.status" 2>/dev/null || echo 1)" = 0 ] || bad+=("$n")
	done
	for n in "${bad[@]}"; do
		printf '\n--- %s (last lines of %s)\n' "$n" "$WORK/$n.log"
		grep -E -- '^(--- FAIL|FAIL|not ok|panic)' "$WORK/$n.log" | head -n 30 || true
		tail -n 30 "$WORK/$n.log"
	done
	[ ${#bad[@]} -eq 0 ] || fail "${bad[*]}"
fi

say "all checks passed: ${parts[*]}"
