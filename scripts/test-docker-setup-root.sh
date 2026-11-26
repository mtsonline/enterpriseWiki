#!/usr/bin/env bash
# Test of docker-setup.sh run by root (run this as root, e.g. with sudo):
#   1. without a terminal, root must name the user (--user NAME)
#   2. root, --user NAME: creates the user, adds it to the group docker,
#      writes its ids into .env and gives it every file of the directory
#      (also files root creates later, on the next run)
#   3. an existing user outside the group docker is added to it
#   4. asked in a terminal (default "ewiki" offered)
#   5. refused: the user root, a directory below /root, --user for a
#      normal user
#   6. run by the wiki's user itself: works; a user outside the group docker
#      only gets the hint (no change)
#   7. with a real Docker and an image: root starts docker compose, and
#      still the wiki and the database run as that user, not as root
#
#   sudo scripts/test-docker-setup-root.sh [IMAGE]
#
# Parts 1-6 use a stand-in for docker that works only for members of the
# group docker; part 7 runs only with an image and a running Docker.
# Creates and finally removes users named ewsetup*.
set -euo pipefail
src="$(cd "$(dirname "$0")/.." && pwd)"
image="${1:-}"
[ "$(id -u)" = 0 ] || { echo "run this as root (sudo $0)" >&2; exit 1; }
for c in useradd userdel usermod runuser script; do command -v "$c" >/dev/null || { echo "$c is missing" >&2; exit 1; }; done

base="$(mktemp -d /tmp/ewiki-setup-root.XXXXXX)"
chmod 755 "$base"
users=()
cleanup() {
	set +e
	if [ -n "${compose_dir:-}" ]; then (cd "$compose_dir" && docker compose down -v >/dev/null 2>&1); fi
	for u in "${users[@]}"; do userdel -r "$u" >/dev/null 2>&1; done
	[ -z "${made_group:-}" ] || groupdel docker
	rm -rf "$base" /root/ewiki-setup-test
}
trap cleanup EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }
newuser() { local u="ewsetup$1$$"; users+=("$u"); printf '%s' "$u"; }
prepare() { # NAME – a copy of the installation files in $base/NAME
	dir="$base/$1"
	mkdir -p "$dir"
	cp "$src"/{docker-compose.yml,.env.example,docker-setup.sh} "$dir/"
	chmod 755 "$dir"
	cd "$dir"
}
val() { sed -n "s/^$1=//p" .env | tail -n 1; }
in_docker_group() { id -nG "$1" | tr ' ' '\n' | grep -qx docker; }
# files of the directory that do not belong to the user (none expected)
foreign() { find . \( -path ./data/crowdsec -o -path ./data/postgres/data \) -prune -o \( ! -user "$1" -o ! -group "$(id -gn "$1")" \) -print | head -n 3; }

if ! getent group docker >/dev/null; then groupadd docker; made_group=1; fi
# the stand-in for docker: "docker info" works only in the group docker
fake="$base/fake-docker"
cat > "$fake" <<'EOF'
#!/bin/sh
id -nG | tr ' ' '\n' | grep -qx docker
EOF
chmod 755 "$fake"
export EWIKI_SETUP_DOCKER="$fake"
export EWIKI_UPDATE_UNITS="$base/units" # never a real systemd timer
mkdir -p "$EWIKI_UPDATE_UNITS"

echo "== 1. without a terminal, root must name the user"
prepare name
out="$(./docker-setup.sh </dev/null 2>&1)" && fail "ran without a user: $out"
grep -q -- '--user NAME' <<<"$out" || fail "no hint to --user: $out"
[ ! -f .env ] || fail ".env written without a user"

echo "== 2. --user NAME: user created, group docker, ids in .env, files belong to the user"
u="$(newuser a)"
prepare create
out="$(./docker-setup.sh --user "$u" </dev/null 2>&1)" || fail "setup as root: $out"
id -u "$u" >/dev/null || fail "user $u not created"
in_docker_group "$u" || fail "$u not in the group docker"
[ "$(val EWIKI_UID)" = "$(id -u "$u")" ] && [ "$(val EWIKI_GID)" = "$(id -g "$u")" ] || fail "EWIKI_UID/GID are not those of $u"
[ -z "$(foreign "$u")" ] || fail "files not of $u: $(foreign "$u")"
[ "$(stat -c %a .env)" = 600 ] || fail ".env is readable by others"
grep -q "sudo -u $u docker compose up -d" <<<"$out" || fail "no start hint for $u: $out"
# the next run (the user from .env) gives files root created to the user
touch data/wiki/made-by-root .made-by-root
./docker-setup.sh </dev/null >/dev/null 2>&1 || fail "second run as root"
[ -z "$(foreign "$u")" ] || fail "second run left files of root: $(foreign "$u")"
[ "$(val EWIKI_UID)" = "$(id -u "$u")" ] || fail "second run changed the user"
# runs as that user too, without root
runuser -u "$u" -- ./docker-setup.sh </dev/null >/dev/null 2>&1 || fail "run by $u itself"

echo "== 3. an existing user outside the group docker is added"
u2="$(newuser b)"
useradd --create-home --user-group --shell /bin/bash "$u2"
! in_docker_group "$u2" || fail "test user already in the group docker"
prepare existing
./docker-setup.sh --user "$u2" </dev/null >/dev/null 2>&1 || fail "setup for an existing user"
in_docker_group "$u2" || fail "$u2 not added to the group docker"
[ "$(val EWIKI_UID)" = "$(id -u "$u2")" ] || fail "EWIKI_UID is not that of $u2"

echo "== 4. asked in a terminal"
u3="$(newuser c)"
prepare ask
# user; image lite; no HTTPS; 127.0.0.1; no own proxy; no automatic updates
printf '%s\n' "$u3" l n '' n n | script -qec ./docker-setup.sh /dev/null >setup.log 2>&1 || { cat setup.log >&2; fail "interactive setup"; }
grep -q 'User who runs the wiki (created if missing): \[ewiki\]' setup.log || fail "no question for the user"
id -u "$u3" >/dev/null || fail "$u3 not created"
in_docker_group "$u3" || fail "$u3 not in the group docker"
[ -z "$(foreign "$u3")" ] || fail "files not of $u3: $(foreign "$u3")"

echo "== 5. refused: root, invalid names, /root, --user for a normal user"
prepare refuse
for bad in root 'x;id' '../etc' 'Upper'; do
	./docker-setup.sh --user "$bad" </dev/null >/dev/null 2>&1 && fail "accepted --user $bad"
done
[ ! -f .env ] || fail ".env written for a refused user"
mkdir -p /root/ewiki-setup-test
cp "$src"/{docker-compose.yml,.env.example,docker-setup.sh} /root/ewiki-setup-test/
out="$(/root/ewiki-setup-test/docker-setup.sh --user "$u" </dev/null 2>&1)" && fail "set up below /root"
grep -q 'must not live in' <<<"$out" || fail "no hint to move the directory: $out"
cd "$base/create"
out="$(runuser -u "$u" -- ./docker-setup.sh --user "$u2" </dev/null 2>&1)" && fail "--user accepted from a normal user"
grep -q 'only for root' <<<"$out" || fail "no hint for --user: $out"

echo "== 6. the wiki's user outside the group docker: only the hint"
u4="$(newuser d)"
useradd --create-home --user-group --shell /bin/bash "$u4"
prepare self
chown "$u4:$(id -gn "$u4")" .
out="$(runuser -u "$u4" -- ./docker-setup.sh </dev/null 2>&1)" && fail "ran without Docker access"
grep -q "usermod -aG docker $u4" <<<"$out" || fail "no hint for the group docker: $out"
! in_docker_group "$u4" || fail "a normal run changed the groups"

if [ -z "$image" ] || ! docker info >/dev/null 2>&1; then
	echo "OK (without part 7: no image or no Docker)"
	exit 0
fi

echo "== 7. root starts docker compose: the containers still run as the user"
unset EWIKI_SETUP_DOCKER
u5="$(newuser e)"
prepare compose
compose_dir="$dir"
export COMPOSE_PROJECT_NAME=ewsetuproot
./docker-setup.sh --user "$u5" </dev/null >/dev/null 2>&1 || fail "setup with Docker"
sed -i -E "s#^(\s+image:) ghcr.io/mtsonline/enterprisewiki:.*#\1 $image#; s#pull_policy: always#pull_policy: never#" docker-compose.yml
docker compose up -d --wait >/dev/null 2>&1 || { docker compose logs --no-color --tail 50 >&2; fail "docker compose up as root"; }
for svc in wiki postgres; do
	got="$(docker compose exec -T "$svc" id -u)"
	[ "$got" = "$(id -u "$u5")" ] || fail "$svc runs as uid $got, not as $u5"
done
[ -z "$(foreign "$u5")" ] || fail "files not of $u5 after the start: $(foreign "$u5")"
echo "OK"
