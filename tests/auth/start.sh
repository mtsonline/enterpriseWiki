#!/usr/bin/env bash
# Starts real sign-in systems for the tests in internal/server/realauth_test.go:
#
#   - Keycloak (official release from GitHub, runs with Java 21+) as OpenID
#     Connect and SAML 2.0 identity provider, http://localhost:8180
#   - OpenLDAP (slapd from the system packages) with the memberof overlay,
#     ldap://localhost:3890, suffix dc=example,dc=com
#
# Both run as normal processes, without containers, so the test works the
# same locally and in the CI. The tests create their own realms, clients and
# users through the Keycloak admin API; the LDAP directory is filled here.
#
#   eval "$(tests/auth/start.sh)"     # starts both, prints the environment
#   tests/auth/start.sh stop          # stops both
#
# Work directory: $EWIKI_AUTH_WORK (default /tmp/ewiki-auth). Passwords are
# generated on every start and only printed as environment variables.
set -euo pipefail

# renovate: datasource=github-releases depName=keycloak/keycloak
KEYCLOAK_VERSION=26.8.2

work="${EWIKI_AUTH_WORK:-/tmp/ewiki-auth}"
here="$(cd "$(dirname "$0")" && pwd)"
kc_port=8180
ldap_port=3890

stop() {
	for p in keycloak slapd; do
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
# on an error: say where, and show the logs of both systems
failed() {
	log "tests/auth/start.sh failed in line $1"
	for f in "$work/slapd.log" "$work/ldapadd.log" "$work/keycloak.log"; do
		[ -s "$f" ] && { log "---- $f"; tail -n 30 "$f" >&2; }
	done
	stop
}
trap 'failed $LINENO' ERR
secret() { od -An -N18 -tx1 /dev/urandom | tr -d ' \n'; }

# ---------------------------------------------------------------- Keycloak
kc="$work/keycloak-$KEYCLOAK_VERSION"
if [ ! -x "$kc/bin/kc.sh" ]; then
	base="https://github.com/keycloak/keycloak/releases/download/$KEYCLOAK_VERSION"
	log "downloading Keycloak $KEYCLOAK_VERSION"
	curl -fsSL -o "$work/keycloak.tgz" "$base/keycloak-$KEYCLOAK_VERSION.tar.gz"
	# Keycloak publishes only a SHA-1 checksum next to the archive: it detects
	# broken downloads. The archive comes over TLS from the project's releases.
	echo "$(curl -fsSL "$base/keycloak-$KEYCLOAK_VERSION.tar.gz.sha1")  $work/keycloak.tgz" | sha1sum -c --quiet >&2
	tar -xzf "$work/keycloak.tgz" -C "$work"
	rm -f "$work/keycloak.tgz"
fi
kc_pw="$(secret)"
# every start begins empty: the admin account is created with the new password
rm -rf "$kc/data"
# start-dev: embedded database, plain HTTP on localhost – a throw-away test system
(
	cd "$kc"
	KC_BOOTSTRAP_ADMIN_USERNAME=kcadmin KC_BOOTSTRAP_ADMIN_PASSWORD="$kc_pw" JAVA_TOOL_OPTIONS='' \
		nohup bin/kc.sh start-dev --http-port "$kc_port" --hostname "http://localhost:$kc_port" >"$work/keycloak.log" 2>&1 &
	echo $! >"$work/keycloak.pid"
)

# ---------------------------------------------------------------- OpenLDAP
slapd_bin="$(command -v slapd || echo /usr/sbin/slapd)"
[ -x "$slapd_bin" ] || { log "slapd not found (package slapd)"; exit 1; }
modules=/usr/lib/ldap
[ -d "$modules" ] || modules="$(dirname "$(find /usr/lib* -name 'back_mdb*.so*' -print -quit 2>/dev/null)")"
ldap_pw="$(secret)"
user_pw="$(secret)"
rm -rf "$work/ldap" && mkdir -p "$work/ldap/db"
cat >"$work/ldap/slapd.conf" <<EOF
include /etc/ldap/schema/core.schema
include /etc/ldap/schema/cosine.schema
include /etc/ldap/schema/inetorgperson.schema
include /etc/ldap/schema/nis.schema
modulepath $modules
moduleload back_mdb
moduleload memberof
pidfile $work/slapd.pid
database mdb
suffix "dc=example,dc=com"
rootdn "cn=admin,dc=example,dc=com"
rootpw $("$(dirname "$slapd_bin")/slappasswd" -s "$ldap_pw" 2>/dev/null || slappasswd -s "$ldap_pw")
directory $work/ldap/db
overlay memberof
access to attrs=userPassword by self write by anonymous auth by * none
access to * by users read by * none
EOF
# a copy of the program: the AppArmor profile of /usr/sbin/slapd (Ubuntu) only
# allows the system directories, not this work directory
cp "$slapd_bin" "$work/slapd"
"$work/slapd" -f "$work/ldap/slapd.conf" -h "ldap://localhost:$ldap_port/" -d 0 >"$work/slapd.log" 2>&1 &
echo $! >"$work/slapd.pid"
ready=""
for _ in $(seq 1 50); do
	if ldapsearch -x -H "ldap://localhost:$ldap_port" -D "cn=admin,dc=example,dc=com" -w "$ldap_pw" -b "" -s base >/dev/null 2>&1; then
		ready=1
		break
	fi
	sleep 0.2
done
[ -n "$ready" ] || { log "OpenLDAP did not start"; false; }
sed "s/@USER_PASSWORD@/$user_pw/g" "$here/directory.ldif" |
	ldapadd -x -H "ldap://localhost:$ldap_port" -D "cn=admin,dc=example,dc=com" -w "$ldap_pw" >"$work/ldapadd.log"

# ---------------------------------------------------------------- ready?
for _ in $(seq 1 120); do
	curl -fs "http://localhost:$kc_port/realms/master" >/dev/null 2>&1 && break
	sleep 1
done
curl -fs "http://localhost:$kc_port/realms/master" >/dev/null || { log "Keycloak did not start:"; tail -n 40 "$work/keycloak.log" >&2; exit 1; }

cat <<EOF
export EWIKI_TEST_KEYCLOAK_URL=http://localhost:$kc_port
export EWIKI_TEST_KEYCLOAK_ADMIN=kcadmin
export EWIKI_TEST_KEYCLOAK_PASSWORD=$kc_pw
export EWIKI_TEST_LDAP_URL=ldap://localhost:$ldap_port
export EWIKI_TEST_LDAP_BIND_DN=cn=admin,dc=example,dc=com
export EWIKI_TEST_LDAP_BIND_PASSWORD=$ldap_pw
export EWIKI_TEST_LDAP_USER_PASSWORD=$user_pw
EOF
