#!/usr/bin/env bash
# Starts real search engines for the tests in
# internal/server/realsearch_test.go (and the examples of the action
# register that need them):
#
#   - OpenSearch (official release, bundled JDK), http://localhost:9201
#   - Elasticsearch (official release, bundled JDK, with a password for the
#     user "elastic"), http://localhost:9202
#   - Meilisearch (official release from GitHub), http://localhost:7701
#
# All run as normal processes, without containers, like tests/auth/start.sh,
# so the tests work the same locally and in the CI. OpenSearch and
# Elasticsearch refuse to run as root. Only the engines named in
# EWIKI_SEARCH_ENGINES (default: all three) are started.
#
#   eval "$(tests/search/start.sh)"    # starts them, prints the environment
#   tests/search/start.sh stop         # stops them
#
# Work directory: $EWIKI_SEARCH_WORK (default /tmp/ewiki-search). Keys and
# passwords are generated on every start and only printed as environment
# variables. Elasticsearch runs here as a test system only; it is not part
# of the product.
set -Eeuo pipefail

# renovate: datasource=github-releases depName=opensearch-project/OpenSearch
OPENSEARCH_VERSION=3.9.0
# renovate: datasource=github-releases depName=elastic/elasticsearch extractVersion=^v(?<version>.+)$
ELASTICSEARCH_VERSION=9.5.5
# renovate: datasource=github-releases depName=meilisearch/meilisearch extractVersion=^v(?<version>.+)$
MEILISEARCH_VERSION=1.56.0

work="${EWIKI_SEARCH_WORK:-/tmp/ewiki-search}"
engines="${EWIKI_SEARCH_ENGINES:-opensearch elasticsearch meilisearch}"
os_port=9201
es_port=9202
meili_port=7701

stop() {
	for p in opensearch elasticsearch meilisearch; do
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
rm -rf "$work"/*.log
log() { echo "$*" >&2; }
secret() { head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n'; }

# fetch downloads a release once (kept in the work directory).
fetch() { # url file
	if [ ! -s "$work/$2" ]; then
		log "downloading $1"
		curl -fsSL --retry 3 -o "$work/$2.part" "$1"
		mv "$work/$2.part" "$work/$2"
	fi
}

# wait_http waits until url answers (with optional curl arguments).
wait_http() { # name url [curl args]
	local name=$1 url=$2
	shift 2
	for _ in $(seq 1 180); do
		if curl -fsS -o /dev/null "$@" "$url" 2>/dev/null; then
			return 0
		fi
		sleep 1
	done
	log "$name did not start; last lines of its log:"
	tail -n 40 "$work/$name.log" >&2 || true
	exit 1
}

case " $engines " in *" opensearch "*)
	fetch "https://artifacts.opensearch.org/releases/bundle/opensearch/$OPENSEARCH_VERSION/opensearch-$OPENSEARCH_VERSION-linux-x64.tar.gz" opensearch.tgz
	rm -rf "$work/opensearch" && mkdir -p "$work/opensearch"
	tar -xzf "$work/opensearch.tgz" -C "$work/opensearch" --strip-components=1
	# a single node without the security plugin (the wiki talks plain HTTP here)
	OPENSEARCH_JAVA_OPTS="-Xms512m -Xmx512m" "$work/opensearch/bin/opensearch" \
		-Ediscovery.type=single-node -Ehttp.port=$os_port -Enetwork.host=127.0.0.1 \
		-Eplugins.security.disabled=true -Epath.data="$work/opensearch/data" \
		>"$work/opensearch.log" 2>&1 &
	echo $! >"$work/opensearch.pid"
	wait_http opensearch "http://127.0.0.1:$os_port/"
	echo "export EWIKI_TEST_OPENSEARCH_URL=http://127.0.0.1:$os_port"
	;;
esac

case " $engines " in *" elasticsearch "*)
	fetch "https://artifacts.elastic.co/downloads/elasticsearch/elasticsearch-$ELASTICSEARCH_VERSION-linux-x86_64.tar.gz" elasticsearch.tgz
	rm -rf "$work/elasticsearch" && mkdir -p "$work/elasticsearch"
	tar -xzf "$work/elasticsearch.tgz" -C "$work/elasticsearch" --strip-components=1
	es_pw=$(secret)
	# security on (password of "elastic"), HTTP without TLS on localhost
	printf '%s' "$es_pw" | "$work/elasticsearch/bin/elasticsearch-keystore" add -x -f bootstrap.password >&2
	ES_JAVA_OPTS="-Xms512m -Xmx512m" "$work/elasticsearch/bin/elasticsearch" \
		-Ediscovery.type=single-node -Ehttp.port=$es_port -Enetwork.host=127.0.0.1 \
		-Expack.security.enabled=true -Expack.security.http.ssl.enabled=false \
		-Expack.security.transport.ssl.enabled=false -Expack.ml.enabled=false \
		-Epath.data="$work/elasticsearch/data" >"$work/elasticsearch.log" 2>&1 &
	echo $! >"$work/elasticsearch.pid"
	wait_http elasticsearch "http://127.0.0.1:$es_port/" -u "elastic:$es_pw"
	echo "export EWIKI_TEST_ELASTICSEARCH_URL=http://127.0.0.1:$es_port"
	echo "export EWIKI_TEST_ELASTICSEARCH_PASSWORD=$es_pw"
	;;
esac

case " $engines " in *" meilisearch "*)
	fetch "https://github.com/meilisearch/meilisearch/releases/download/v$MEILISEARCH_VERSION/meilisearch-linux-amd64" "meilisearch-$MEILISEARCH_VERSION"
	chmod +x "$work/meilisearch-$MEILISEARCH_VERSION"
	meili_key=$(secret)
	rm -rf "$work/meili-data"
	"$work/meilisearch-$MEILISEARCH_VERSION" --http-addr "127.0.0.1:$meili_port" --master-key "$meili_key" \
		--db-path "$work/meili-data" --no-analytics --env development >"$work/meilisearch.log" 2>&1 &
	echo $! >"$work/meilisearch.pid"
	wait_http meilisearch "http://127.0.0.1:$meili_port/health"
	echo "export EWIKI_TEST_MEILISEARCH_URL=http://127.0.0.1:$meili_port"
	echo "export EWIKI_TEST_MEILISEARCH_KEY=$meili_key"
	;;
esac
