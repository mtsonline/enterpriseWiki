#!/usr/bin/env bash
# Starts real sign-in systems for the tests in internal/server/realauth_test.go:
#
#   - Keycloak (official release from GitHub, runs with Java 21+) as OpenID
#     Connect and SAML 2.0 identity provider, http://localhost:8180
#   - OpenLDAP (slapd from the system packages) with the memberof overlay,
#     ldap://localhost:3890, suffix dc=example,dc=com
#   - with EWIKI_AUTH_AD=1: Samba as Active Directory domain controller
#     (package samba-ad-dc, needs sudo), ldaps://localhost:636, domain
#     EXAMPLE.TEST, certificate of an own test CA, strong authentication on
#   - with EWIKI_AUTH_SHIB=1: Apache with mod_shib (packages apache2 and
#     libapache2-mod-shib) at http://localhost:8280, a Shibboleth service
#     provider signed in at Keycloak (realm ewiki-shib); it forwards to the
#     wiki at 127.0.0.1:18080 (header mode of auth.shib)
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
set -Eeuo pipefail

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
	for p in apache shibd; do
		if [ -f "$work/shib/$p.pid" ]; then
			kill "$(cat "$work/shib/$p.pid")" 2>/dev/null || true
			rm -f "$work/shib/$p.pid"
		fi
	done
	if [ -d "$work/ad" ]; then
		sudo pkill -f "samba -s $work/ad/samba/etc/smb.conf" 2>/dev/null || true
	fi
}
if [ "${1:-}" = stop ]; then
	stop
	exit 0
fi
stop
mkdir -p "$work"
# logs of an earlier run would only confuse
rm -rf "$work"/*.log "$work/shib"
log() { echo "$*" >&2; }
# on an error: say where, and show the logs of both systems
failed() {
	log "tests/auth/start.sh failed in line $1"
	for f in "$work/slapd.log" "$work/ldapadd.log" "$work/keycloak.log" "$work/ad-provision.log" "$work/samba.log" "$work/shib/shibd.log" "$work/shib/apache-error.log"; do
		[ -s "$f" ] && { log "---- $f"; tail -n 60 "$f" >&2; }
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

# ---------------------------------------------------------------- Active Directory (Samba)
ad_env=""
if [ "${EWIKI_AUTH_AD:-}" = 1 ]; then
	command -v samba >/dev/null || { log "samba not found (package samba-ad-dc)"; false; }
	# samba-tool needs the Python version its modules were built for
	py=""
	for f in /usr/lib/python3/dist-packages/ldb.cpython-3*.so; do
		v="$(basename "$f" | sed -E 's/^ldb\.cpython-3([0-9]+)-.*/3.\1/')"
		command -v "python$v" >/dev/null && py="python$v"
	done
	[ -n "$py" ] || { log "no Python matching the samba modules"; false; }
	ad="$work/ad"
	sudo rm -rf "$ad"
	mkdir -p "$ad/tls"
	# own test CA and a server certificate with subject alternative names, as
	# an enterprise CA would issue it (the self-signed certificate of Samba
	# has no SAN, which Go rightly refuses)
	openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 2 -subj "/CN=ewiki test CA" \
		-keyout "$ad/tls/ca.key" -out "$ad/tls/ca.pem" 2>/dev/null
	openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj "/CN=localhost" \
		-keyout "$ad/tls/key.pem" -out "$ad/tls/req.pem" 2>/dev/null
	printf 'subjectAltName=DNS:localhost,IP:127.0.0.1\nextendedKeyUsage=serverAuth\n' >"$ad/tls/ext.cnf"
	openssl x509 -req -in "$ad/tls/req.pem" -CA "$ad/tls/ca.pem" -CAkey "$ad/tls/ca.key" -CAcreateserial -days 2 \
		-extfile "$ad/tls/ext.cnf" -out "$ad/tls/cert.pem" 2>/dev/null
	chmod 600 "$ad/tls/key.pem" "$ad/tls/ca.key"
	ad_pw="Aa1-$(secret)"
	ad_user_pw="Aa1-$(secret)"
	ad_bind_pw="Aa1-$(secret)"
	sudo "$py" "$(command -v samba-tool)" domain provision --realm=EXAMPLE.TEST --domain=EXAMPLE --server-role=dc \
		--dns-backend=NONE --adminpass="$ad_pw" --targetdir="$ad/samba" \
		--option="interfaces = lo" --option="bind interfaces only = yes" --option="server services = -dns" \
		--option="dns update command = /bin/true" --option="tls enabled = yes" \
		--option="tls keyfile = $ad/tls/key.pem" --option="tls certfile = $ad/tls/cert.pem" \
		--option="tls cafile = $ad/tls/ca.pem" >"$work/ad-provision.log" 2>&1
	# samba checks that only root can read the key
	sudo chown root "$ad/tls/key.pem"
	st() { sudo "$py" "$(command -v samba-tool)" "$@" -s "$ad/samba/etc/smb.conf" >>"$work/ad-provision.log" 2>&1; }
	st user create wikibind "$ad_bind_pw" --given-name Wiki --surname Bind
	st user create anna "$ad_user_pw" --given-name Anna --surname Example --mail-address anna@example.com
	st user create bernd "$ad_user_pw" --given-name Bernd --surname Example --mail-address bernd@example.com
	st user disable bernd
	st group add Staff
	st group add "Wiki Editors"
	st group addmembers "Wiki Editors" anna
	st group addmembers Staff "Wiki Editors"
	# users and groups are written before the start: one process at a time on the database
	(sudo nohup samba -s "$ad/samba/etc/smb.conf" -F --no-process-group -M single --debug-stdout -d 3 </dev/null >"$work/samba.log" 2>&1 &)
	ready=""
	# readiness only: the OpenLDAP tools compare the certificate with the host
	# name after a reverse lookup, which differs between machines. The Go test
	# checks the certificate strictly (and that an untrusted one is refused).
	for _ in $(seq 1 60); do
		if LDAPTLS_REQCERT=never ldapsearch -x -H ldaps://localhost:636 -D "wikibind@EXAMPLE.TEST" -w "$ad_bind_pw" \
			-b "DC=example,DC=test" -s base >/dev/null 2>&1; then
			ready=1
			break
		fi
		sleep 1
	done
	if [ -z "$ready" ]; then
		log "Samba did not answer:"
		LDAPTLS_REQCERT=never ldapsearch -x -H ldaps://localhost:636 -D "wikibind@EXAMPLE.TEST" -w "$ad_bind_pw" \
			-b "DC=example,DC=test" -s base 2>&1 | head -n 5 >&2 || true
		log "---- listening ports"
		sudo ss -ltnp 2>/dev/null | grep -E ':(389|636|88) ' >&2 || log "nothing on 389/636/88"
		log "---- AppArmor"
		sudo dmesg 2>/dev/null | grep -iE 'apparmor.*denied' | tail -n 10 >&2 || true
		false
	fi
	ad_env="export EWIKI_TEST_AD_URL=ldaps://localhost:636
export EWIKI_TEST_AD_CA=$ad/tls/ca.pem
export EWIKI_TEST_AD_BIND_DN=wikibind@EXAMPLE.TEST
export EWIKI_TEST_AD_BIND_PASSWORD=$ad_bind_pw
export EWIKI_TEST_AD_USER_PASSWORD=$ad_user_pw"
fi

# ---------------------------------------------------------------- ready?
for _ in $(seq 1 120); do
	curl -fs "http://localhost:$kc_port/realms/master" >/dev/null 2>&1 && break
	sleep 1
done
curl -fs "http://localhost:$kc_port/realms/master" >/dev/null || { log "Keycloak did not start:"; tail -n 40 "$work/keycloak.log" >&2; exit 1; }

# ---------------------------------------------------------------- Shibboleth (Apache with mod_shib)
shib_env=""
if [ "${EWIKI_AUTH_SHIB:-}" = 1 ]; then
	command -v shibd >/dev/null && command -v apache2 >/dev/null || { log "shibd or apache2 not found (packages apache2 libapache2-mod-shib)"; false; }
	shib="$work/shib"
	shib_port=8280
	backend=127.0.0.1:18080
	sp="http://localhost:$shib_port"
	kcr="http://localhost:$kc_port"
	realm=ewiki-shib
	mkdir -p "$shib"
	tok="$(curl -fsS -d grant_type=password -d client_id=admin-cli -d username=kcadmin --data-urlencode "password=$kc_pw" \
		"$kcr/realms/master/protocol/openid-connect/token" | jq -r .access_token)"
	kca() { curl -sS --fail-with-body -H "Authorization: Bearer $tok" -H 'Content-Type: application/json' "$@"; }
	kca -X POST "$kcr/admin/realms" -d "{\"realm\":\"$realm\",\"enabled\":true}"
	kca -X POST "$kcr/admin/realms/$realm/groups" -d '{"name":"Professors"}'
	shib_pw="$(secret)"
	jq -n --arg pw "$shib_pw" '{username:"erika", email:"erika@example.com", emailVerified:true, enabled:true,
		firstName:"Erika", lastName:"Example", groups:["/Professors"], credentials:[{type:"password", value:$pw, temporary:false}]}' |
		kca -X POST "$kcr/admin/realms/$realm/users" -d @-
	curl -fsS "$kcr/realms/$realm/protocol/saml/descriptor" >"$shib/idp-metadata.xml"
	openssl req -x509 -newkey rsa:3072 -nodes -days 2 -subj "/CN=localhost" \
		-keyout "$shib/sp-key.pem" -out "$shib/sp-cert.pem" 2>/dev/null
	sed -e "s#@WORK@#$shib#g" -e "s#@SP@#$sp#g" -e "s#@IDP@#$kcr/realms/$realm#g" \
		"$here/shib/shibboleth2.xml.in" >"$shib/shibboleth2.xml"
	cp "$here/shib/attribute-map.xml" "$shib/attribute-map.xml"
	printf '%s\n' 'log4j.rootCategory=INFO, f' 'log4j.appender.f=org.apache.log4j.FileAppender' \
		"log4j.appender.f.fileName=$shib/shibd.log" 'log4j.appender.f.layout=org.apache.log4j.PatternLayout' \
		'log4j.appender.f.layout.ConversionPattern=%d %p %c: %m%n' >"$shib/shibd.logger"
	user=""
	if [ "$(id -u)" = 0 ]; then
		# Apache does not serve as root: its workers run as nobody, who must
		# reach the socket of shibd
		user="User nobody
Group nogroup"
		chmod 755 "$work" "$shib"
	fi
	awk -v user="$user" '{ if ($0 == "@USER@") print user; else print }' "$here/shib/apache.conf.in" |
		sed -e "s#@WORK@#$shib#g" -e "s#@PORT@#$shib_port#g" -e "s#@BACKEND@#$backend#g" >"$shib/apache.conf"
	shibd -t -c "$shib/shibboleth2.xml" >"$shib/shibd-check.log" 2>&1 || { log "shibd configuration:"; cat "$shib/shibd-check.log" >&2; false; }
	nohup shibd -F -f -c "$shib/shibboleth2.xml" >>"$shib/shibd.log" 2>&1 &
	# in the foreground shibd writes no pid file: remember it here
	echo $! >"$shib/shibd.pid"
	for _ in $(seq 1 60); do [ -S "$shib/shibd.sock" ] && break; sleep 0.5; done
	[ -S "$shib/shibd.sock" ] || { log "shibd did not start"; false; }
	chmod 666 "$shib/shibd.sock"
	apache2 -f "$shib/apache.conf" -k start
	for _ in $(seq 1 60); do curl -fs "$sp/Shibboleth.sso/Metadata" >"$shib/sp-metadata.xml" 2>/dev/null && break; sleep 0.5; done
	[ -s "$shib/sp-metadata.xml" ] || { log "Apache with mod_shib did not start"; false; }
	# Keycloak learns the service provider from its metadata, plus the attributes it needs
	kca -X POST "$kcr/admin/realms/$realm/client-description-converter" -H 'Content-Type: application/xml' \
		--data-binary @"$shib/sp-metadata.xml" |
		jq '.protocolMappers = [
			{name:"uid", protocol:"saml", protocolMapper:"saml-user-property-mapper", config:{"user.attribute":"username", "attribute.name":"uid", "attribute.nameformat":"Basic"}},
			{name:"email", protocol:"saml", protocolMapper:"saml-user-property-mapper", config:{"user.attribute":"email", "attribute.name":"email", "attribute.nameformat":"Basic"}},
			{name:"displayName", protocol:"saml", protocolMapper:"saml-user-property-mapper", config:{"user.attribute":"firstName", "attribute.name":"displayName", "attribute.nameformat":"Basic"}},
			{name:"groups", protocol:"saml", protocolMapper:"saml-group-membership-mapper", config:{"attribute.name":"groups", "full.path":"false", "single":"false", "attribute.nameformat":"Basic"}}]' |
		kca -X POST "$kcr/admin/realms/$realm/clients" -d @-
	shib_env="export EWIKI_TEST_SHIB_URL=$sp
export EWIKI_TEST_SHIB_BACKEND=$backend
export EWIKI_TEST_SHIB_PASSWORD=$shib_pw"
fi

cat <<EOF
export EWIKI_TEST_KEYCLOAK_URL=http://localhost:$kc_port
export EWIKI_TEST_KEYCLOAK_ADMIN=kcadmin
export EWIKI_TEST_KEYCLOAK_PASSWORD=$kc_pw
export EWIKI_TEST_LDAP_URL=ldap://localhost:$ldap_port
export EWIKI_TEST_LDAP_BIND_DN=cn=admin,dc=example,dc=com
export EWIKI_TEST_LDAP_BIND_PASSWORD=$ldap_pw
export EWIKI_TEST_LDAP_USER_PASSWORD=$user_pw
EOF
[ -z "$ad_env" ] || echo "$ad_env"
[ -z "$shib_env" ] || echo "$shib_env"
