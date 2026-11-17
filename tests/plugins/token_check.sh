#!/usr/bin/env bash
# Checks that the token of the plugin lifecycle test can do no more than it
# must: read mtsonline/enterprisewiki-testplugin. Every request only reads or
# is refused; nothing is changed (the write probe points a branch at a
# commit that does not exist: a token allowed to write gets 422 "invalid",
# one without write access 403 or 404).
#
#   EWIKI_TESTPLUGIN_TOKEN=… tests/plugins/token_check.sh
set -Eeuo pipefail

: "${EWIKI_TESTPLUGIN_TOKEN:?read token for mtsonline/enterprisewiki-testplugin}"
api="${GITHUB_API_URL:-https://api.github.com}"
repo="mtsonline/enterprisewiki-testplugin"
failed=0

# call METHOD PATH [BODY]: prints the HTTP status (the token goes through a
# header file, never the command line)
hdr=$(mktemp)
trap 'rm -f "$hdr"' EXIT
chmod 600 "$hdr"
printf 'Authorization: Bearer %s\nAccept: application/vnd.github+json\nX-GitHub-Api-Version: 2022-11-28\n' "$EWIKI_TESTPLUGIN_TOKEN" >"$hdr"
call() {
	curl -sS -o /tmp/token_check.json -w '%{http_code}' -H @"$hdr" -X "$1" "$api$2" ${3:+--data "$3"}
}
expect() { # description wanted-status-pattern actual
	if [[ "$3" =~ ^($2)$ ]]; then
		echo "ok   $1 ($3)"
	else
		echo "FAIL $1: HTTP $3, expected $2"
		failed=1
	fi
}

expect "read the test plugin repository" 200 "$(call GET "/repos/$repo")"
expect "read its files" 200 "$(call GET "/repos/$repo/contents/plugin.json")"
expect "read its tags" 200 "$(call GET "/repos/$repo/tags")"

# no write access: pointing a new branch at a commit that does not exist
expect "no write access to the repository (code)" "403|404" \
	"$(call POST "/repos/$repo/git/refs" '{"ref":"refs/heads/token-check-probe","sha":"0000000000000000000000000000000000000001"}')"
expect "no access to issues" "403|404|410" "$(call POST "/repos/$repo/issues" '{}')"
# (the list of collaborators is readable with "Metadata: read", which
# GitHub gives every token; the Actions settings need "Administration")
expect "no access to the settings of the repository" "403|404" "$(call GET "/repos/$repo/actions/permissions")"
expect "no access to its deploy keys" "403|404" "$(call GET "/repos/$repo/keys")"
expect "no access to its secrets" "403|404" "$(call GET "/repos/$repo/actions/secrets")"
expect "no access to webhooks" "403|404" "$(call GET "/repos/$repo/hooks")"

# no other private repository is reachable
if [ "$(call GET "/user/repos?per_page=100&visibility=private")" = 200 ]; then
	others=$(jq -r --arg r "$repo" '.[] | select(.full_name != $r) | .full_name' /tmp/token_check.json)
	if [ -n "$others" ]; then
		echo "FAIL the token reaches other private repositories: $others"
		failed=1
	else
		echo "ok   no other private repository"
	fi
else
	echo "ok   the token cannot list repositories"
fi
rm -f /tmp/token_check.json
exit "$failed"
