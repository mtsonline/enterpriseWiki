#!/bin/sh
# CI test of the automatic upgrade: for every older major version whose
# binaries are in the image, create data with the official image of that
# version (layout of the plain postgres image), start the Enterprise Wiki
# image on the same volume and check that everything arrived.
#   docker/postgres/test-upgrade.sh IMAGE
set -eu
IMG="$1"
fail() { echo "FAIL: $*" >&2; exit 1; }
wait_ready() { # container
  for i in $(seq 1 120); do
    docker exec "$1" pg_isready -h 127.0.0.1 -U ewiki -d ewiki >/dev/null 2>&1 && return 0
    docker ps -q -f "name=^$1$" | grep -q . || { docker logs "$1" >&2; fail "$1 stopped"; }
    sleep 2
  done
  docker logs "$1" >&2; fail "$1 not ready"
}
q() { docker exec -e PGPASSWORD=secret "$1" psql -h 127.0.0.1 -U ewiki -d ewiki -Atc "$2"; }

olds=$(docker run --rm --entrypoint sh "$IMG" -c 'ls -d /usr/libexec/postgresql[0-9]* 2>/dev/null | sed "s#.*postgresql##"')
[ -n "$olds" ] || fail "no old binaries in the image"
for old in $olds; do
  echo "== upgrade from PostgreSQL $old"
  vol="ewiki-pgtest-$old"
  docker volume rm -f "$vol" "$vol-backup" >/dev/null 2>&1 || true
  docker run -d --name pgold -e POSTGRES_USER=ewiki -e POSTGRES_PASSWORD=secret -e POSTGRES_DB=ewiki -v "$vol:/var/lib/postgresql/data" "postgres:$old-alpine" >/dev/null
  wait_ready pgold
  q pgold "CREATE EXTENSION IF NOT EXISTS ltree; CREATE TABLE t (id int PRIMARY KEY, path ltree, txt text); INSERT INTO t SELECT g, ('a.b' || g)::ltree, repeat('x', g % 100) FROM generate_series(1, 5000) g; CREATE ROLE reader LOGIN PASSWORD 'r'; GRANT SELECT ON t TO reader;" >/dev/null
  docker exec pgold psql -U ewiki -d postgres -c "CREATE DATABASE other" >/dev/null
  docker exec pgold psql -U ewiki -d other -c "CREATE TABLE o AS SELECT generate_series(1, 42) AS n" >/dev/null
  docker stop pgold >/dev/null && docker rm pgold >/dev/null

  docker run -d --name pgnew -e POSTGRES_USER=ewiki -e POSTGRES_PASSWORD=ignored-on-upgrade -e POSTGRES_DB=ewiki \
    -v "$vol:/var/lib/postgresql/ewiki" -v "$vol-backup:/backups" "$IMG" >/dev/null
  wait_ready pgnew
  docker logs pgnew 2>&1 | grep -q "finished and verified" || { docker logs pgnew >&2; fail "no upgrade message"; }
  [ "$(q pgnew 'SELECT count(*) FROM t')" = 5000 ] || fail "rows of t"
  [ "$(q pgnew "SELECT count(*) FROM t WHERE path <@ 'a'")" = 5000 ] || fail "ltree"
  [ "$(docker exec -e PGPASSWORD=secret pgnew psql -h 127.0.0.1 -U ewiki -d other -Atc 'SELECT sum(n) FROM o')" = 903 ] || fail "second database"
  [ "$(docker exec -e PGPASSWORD=r pgnew psql -h 127.0.0.1 -U reader -d ewiki -Atc 'SELECT count(*) FROM t')" = 5000 ] || fail "role/password of reader"
  major=$(q pgnew "SHOW server_version_num" | cut -c1-2)
  docker exec pgnew sh -c "ls /backups/pg$old-before-pg$major-*.sql.gz" >/dev/null || fail "dump missing"
  docker exec pgnew test -f /var/lib/postgresql/ewiki/PG_VERSION || fail "old data not kept"
  # a restart does not upgrade again
  docker restart pgnew >/dev/null && wait_ready pgnew
  [ "$(docker logs pgnew 2>&1 | grep -c 'finished and verified')" = 1 ] || fail "upgraded twice"
  docker rm -f pgnew >/dev/null
  docker volume rm -f "$vol" "$vol-backup" >/dev/null
  echo "ok: $old -> $major"
done

echo "== fresh volume"
docker run -d --name pgfresh -e POSTGRES_USER=ewiki -e POSTGRES_PASSWORD=secret -e POSTGRES_DB=ewiki -v ewiki-pgtest-fresh:/var/lib/postgresql/ewiki "$IMG" >/dev/null
wait_ready pgfresh
[ "$(q pgfresh 'SELECT 1')" = 1 ] || fail "fresh"
docker rm -f pgfresh >/dev/null; docker volume rm -f ewiki-pgtest-fresh >/dev/null
echo "all upgrade tests passed"
