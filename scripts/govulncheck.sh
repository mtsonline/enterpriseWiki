#!/usr/bin/env bash
# Runs govulncheck. Findings in code we call fail, except those listed in
# .govulncheck-allow (with a mitigation in our code) until their date.
set -euo pipefail
cd "$(dirname "$0")/.."
out="$(mktemp)"
trap 'rm -f "$out"' EXIT
go run golang.org/x/vuln/cmd/govulncheck@latest -format json ./... >"$out"
# findings whose trace reaches a function (= our code calls the vulnerable symbol)
mapfile -t ids < <(jq -r 'select(.finding != null) | select(.finding.trace[0].function != null) | .finding.osv' "$out" | sort -u)
today="$(date -u +%F)"
fail=0
for id in "${ids[@]}"; do
	[ -n "$id" ] || continue
	line="$(grep -E "^$id[[:space:]]" .govulncheck-allow || true)"
	until="$(awk '{print $2}' <<<"$line")"
	if [ -n "$line" ] && [[ "$until" > "$today" || "$until" == "$today" ]]; then
		echo "allowed until $until: $line"
	else
		echo "VULNERABLE: $id (see https://pkg.go.dev/vuln/$id)"
		fail=1
	fi
done
if [ "$fail" = 1 ]; then
	go run golang.org/x/vuln/cmd/govulncheck@latest ./... || true
	exit 1
fi
echo "govulncheck: no unmitigated vulnerabilities"
