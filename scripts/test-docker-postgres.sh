#!/usr/bin/env bash
# CI test of the Docker Compose installation (only uses the official
# PostgreSQL images):
#   1. ./docker-setup.sh as a normal user, PostgreSQL <from> with test data
#   2. ./docker-postgres-upgrade.sh to the recommended version (copy mode)
#   3. checks: data, roles and passwords, second database, ltree, not root,
#      old data kept, second run does nothing, --check
#   4. the same once more with --link, and a fresh installation
#   5. with --full: the wiki itself (docker-compose.build.yml) on the
#      upgraded database, running as the same user
#   scripts/test-docker-postgres.sh FROM [--full]
set -euo pipefail
from="$1" full="${2:-}"
src="$(cd "$(dirname "$0")/.." && pwd)"
fail() { echo "FAIL: $*" >&2; docker compose logs --no-color --tail 80 >&2 || true; exit 1; }
to="$(sed -nE 's/.*image: postgres:\$\{POSTGRES_VERSION:-([0-9]+)\}.*/\1/p' "$src/docker-compose.yml" | head -n 1)"
[ "$from" -lt "$to" ] || { echo "nothing to test: $from is not older than $to"; exit 0; }

prepare() { # name compose-file
	work="$(mktemp -d)/$1"; mkdir -p "$work"
	cp "$src"/{docker-compose.yml,docker-compose.build.yml,.env.example,docker-setup.sh,docker-postgres-upgrade.sh} "$work/"
	cd "$work"
	export COMPOSE_PROJECT_NAME="ewtest$1" COMPOSE_FILE="$2"
	./docker-setup.sh >/dev/null
	grep -q "^EWIKI_UID=$(id -u)$" .env || fail "EWIKI_UID not set"
	grep -q ready <<<"$(./docker-setup.sh)" || fail "second setup run"
}
setv() { sed -i "s|^$1=.*|$1=$2|" .env; }
wait_db() {
	for _ in $(seq 1 90); do
		docker compose exec -T postgres pg_isready -h 127.0.0.1 -U ewiki -d ewiki >/dev/null 2>&1 && return 0
		sleep 2
	done
	fail "database not ready"
}
pw() { sed -n 's/^POSTGRES_PASSWORD=//p' .env; }
q() { docker compose exec -T -e PGPASSWORD="$(pw)" postgres psql -h 127.0.0.1 -U ewiki -d "${2:-ewiki}" -Atc "$1"; }
seed() {
	q "CREATE EXTENSION IF NOT EXISTS ltree; CREATE TABLE t (id int PRIMARY KEY, path ltree, txt text);
	   INSERT INTO t SELECT g, ('a.b' || g)::ltree, repeat('x', g % 100) FROM generate_series(1, 20000) g;
	   CREATE ROLE reader LOGIN PASSWORD 'reader-pw'; GRANT SELECT ON t TO reader;" >/dev/null
	q "CREATE DATABASE other" postgres >/dev/null
	q "CREATE TABLE o AS SELECT generate_series(1, 42) AS n" other >/dev/null
}
verify() { # mode
	[ "$(sed -n 's/^POSTGRES_VERSION=//p' .env)" = "$to" ] || fail "POSTGRES_VERSION not updated"
	wait_db
	[ "$(q 'SHOW server_version_num' | cut -c1-2)" = "$to" ] || fail "server is not PostgreSQL $to"
	[ "$(q 'SELECT count(*) FROM t')" = 20000 ] || fail "rows of t"
	[ "$(q "SELECT count(*) FROM t WHERE path <@ 'a'")" = 20000 ] || fail "ltree"
	[ "$(q 'SELECT sum(n) FROM o' other)" = 903 ] || fail "second database"
	[ "$(docker compose exec -T -e PGPASSWORD=reader-pw postgres psql -h 127.0.0.1 -U reader -d ewiki -Atc 'SELECT count(*) FROM t')" = 20000 ] || fail "role and password of reader"
	[ "$(docker compose exec -T postgres id -u)" = "$(id -u)" ] || fail "postgres does not run as the user of .env"
	[ -s "data/postgres/$from/docker/PG_VERSION" ] || fail "old data not kept"
	[ -e "data/postgres/$to/docker/.ewiki-upgraded" ] || fail "marker missing"
	other="$(find data -path data/postgres/data -prune -o \( ! -uid "$(id -u)" -o ! -gid "$(id -g)" \) -printf "%p %u:%g\n" | head -5)"; [ -z "$other" ] || fail "files in ./data with another owner: $other"
	grep -q "up to date" <<<"$(./docker-postgres-upgrade.sh --yes)" || fail "second run"
	./docker-postgres-upgrade.sh --check >/dev/null || fail "--check after the upgrade"
	echo "ok: PostgreSQL $from → $to ($1)"
}

for mode in copy link; do
	prepare "$from$mode" docker-compose.yml
	setv POSTGRES_VERSION "$from"
	export PG_UPGRADE_SERVICES=postgres
	docker compose up -d postgres
	wait_db
	seed
	set +e; ./docker-postgres-upgrade.sh --check >/dev/null; rc=$?; set -e
	[ "$rc" = 10 ] || fail "--check before the upgrade (exit $rc)"
	if [ "$mode" = link ]; then ./docker-postgres-upgrade.sh --yes --link; else ./docker-postgres-upgrade.sh --yes; fi
	verify "$mode"
	docker compose down >/dev/null 2>&1
done

echo "== fresh installation"
prepare fresh docker-compose.yml
docker compose up -d postgres
wait_db
[ "$(q 'SELECT 1')" = 1 ] || fail "fresh installation"
[ -s "data/postgres/$to/docker/PG_VERSION" ] || fail "data not in data/postgres/$to/docker"
docker compose down >/dev/null 2>&1

if [ "$full" = --full ]; then
	echo "== wiki on the upgraded database (docker-compose.build.yml, own user)"
	prepare full docker-compose.build.yml
	# the build context is the repository
	sed "s|context: \.$|context: $src|" "$src/docker-compose.build.yml" > docker-compose.build.yml
	grep -q "context: $src" docker-compose.build.yml || fail "build context not replaced"
	setv POSTGRES_VERSION "$from"
	docker compose up -d --build
	wait_db
	for _ in $(seq 1 90); do curl -fs "http://localhost:$(sed -n 's/^EWIKI_PORT=//p' .env)/readyz" >/dev/null && break; sleep 2; done
	curl -fs "http://localhost:8080/readyz" >/dev/null || fail "wiki not ready on PostgreSQL $from"
	[ "$(docker compose exec -T wiki id -u)" = "$(id -u)" ] || fail "wiki does not run as the user of .env"
	[ -s data/wiki/config.yaml ] || fail "config.yaml not in ./data/wiki"
	unset PG_UPGRADE_SERVICES
	./docker-postgres-upgrade.sh --yes
	for _ in $(seq 1 90); do curl -fs http://localhost:8080/readyz >/dev/null && break; sleep 2; done
	grep -q '"name"' <<<"$(curl -fs http://localhost:8080/api/v1/site)" || fail "wiki after the upgrade"
	[ "$(q 'SHOW server_version_num' | cut -c1-2)" = "$to" ] || fail "wiki stack not on $to"
	docker compose down >/dev/null 2>&1
	echo "ok: wiki on PostgreSQL $from → $to"
fi
echo "all Docker Compose tests passed"
