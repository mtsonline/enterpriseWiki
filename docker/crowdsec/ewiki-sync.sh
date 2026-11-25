#!/bin/bash
# Runs inside the CrowdSec container (docker-compose.crowdsec.yml) next to
# CrowdSec. It does two things for Enterprise Wiki:
#
# 1. It registers the machine "ewiki" with the password of
#    EWIKI_CROWDSEC_PASSWORD, so the wiki can read alerts and lift blocks
#    through the Local API. The wiki never gets access to Docker.
# 2. It applies the allowed addresses the wiki writes to
#    $EWIKI_SYNC_DIR/allowlist.txt to the CrowdSec allowlist "ewiki" (the
#    Local API cannot write allowlists, cscli can). Addresses that become
#    allowed lose their current blocks.
#
# The file comes from another container, so every line is checked: only IP
# addresses and networks pass (they cannot start with "-" and contain no
# spaces, so they cannot become options or further arguments of cscli).
# Lines starting with # and empty lines are skipped. Without the file
# nothing is changed. Also used by tests/crowdsec/start.sh with a local
# CrowdSec (CSCLI, EWIKI_SYNC_DIR and EWIKI_SYNC_INTERVAL set there).
set -u

dir="${EWIKI_SYNC_DIR:-/ewiki}"
interval="${EWIKI_SYNC_INTERVAL:-10}"
list=ewiki
read -r -a cscli <<< "${CSCLI:-cscli}"

log() { echo "ewiki-sync: $*" >&2; }

# wait until CrowdSec has created its database and answers cscli
until "${cscli[@]}" machines list -o raw > /dev/null 2>&1; do
	sleep 2
done

if [ -n "${EWIKI_CROWDSEC_PASSWORD:-}" ]; then
	# --force sets the password again if the machine exists; -f /dev/null
	# keeps the credentials of CrowdSec itself untouched
	if "${cscli[@]}" machines add ewiki --password "$EWIKI_CROWDSEC_PASSWORD" -f /dev/null --force > /dev/null 2>&1; then
		log "machine ewiki registered"
	else
		log "could not register the machine ewiki"
	fi
fi

valid() {
	[[ ${#1} -le 64 && $1 =~ ^[0-9A-Fa-f:.]{2,45}(/[0-9]{1,3})?$ ]]
}

# minus A B prints the lines of A that are not in B
minus() {
	awk 'NR == FNR { if ($0 != "") b[$0] = 1; next } $0 != "" && !($0 in b)' <(printf '%s\n' "$2") <(printf '%s\n' "$1")
}

applied() {
	"${cscli[@]}" allowlists inspect "$list" -o raw 2> /dev/null | awk -F, -v n="$list" 'NR > 1 && $1 == n { print $3 }' | sort -u
}

last=""
while :; do
	file="$dir/allowlist.txt"
	if [ -f "$file" ] && [ ! -L "$file" ]; then
		raw="$(head -c 1048576 "$file")"
		want="" bad=0
		while IFS= read -r line || [ -n "$line" ]; do
			line="${line%$'\r'}"
			[[ -z $line || $line == \#* ]] && continue
			if valid "$line"; then
				want+="$line"$'\n'
			else
				bad=$((bad + 1))
			fi
		done <<< "$raw"
		if [ "$raw" != "${lastraw-}" ]; then
			[ $bad = 0 ] || log "skipped $bad invalid line(s)"
			lastraw="$raw"
		fi
		want="$(printf '%s' "$want" | sort -u)"
		if ! "${cscli[@]}" allowlists inspect "$list" -o raw > /dev/null 2>&1; then
			"${cscli[@]}" allowlists create "$list" -d "Enterprise Wiki" > /dev/null 2>&1 || log "could not create the allowlist $list"
		fi
		have="$(applied)"
		if [ "$want" != "$have" ] || [ "$want" != "$last" ]; then
			mapfile -t add < <(minus "$want" "$have")
			mapfile -t del < <(minus "$have" "$want")
			ok=1
			if [ ${#add[@]} -gt 0 ]; then
				"${cscli[@]}" allowlists add "$list" "${add[@]}" -d "Enterprise Wiki" > /dev/null 2>&1 || ok=0
				for v in "${add[@]}"; do
					if [[ $v == */* ]]; then
						"${cscli[@]}" decisions delete -r "$v" --contained > /dev/null 2>&1
					else
						"${cscli[@]}" decisions delete -i "$v" > /dev/null 2>&1
					fi
				done
			fi
			if [ ${#del[@]} -gt 0 ]; then
				"${cscli[@]}" allowlists remove "$list" "${del[@]}" > /dev/null 2>&1 || ok=0
			fi
			if [ $ok = 1 ]; then
				[ ${#add[@]} -gt 0 ] || [ ${#del[@]} -gt 0 ] && log "allowlist: ${#add[@]} added, ${#del[@]} removed"
				last="$want"
			else
				log "allowlist could not be applied completely; trying again"
			fi
		fi
	fi
	sleep "$interval"
done
