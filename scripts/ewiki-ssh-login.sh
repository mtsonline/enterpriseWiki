#!/usr/bin/env bash
# Log in to the Enterprise Wiki API with an SSH key (like GitHub) and print a
# short-lived bearer token.
#
#   TOKEN=$(scripts/ewiki-ssh-login.sh https://wiki.example.com alice ~/.ssh/id_ed25519)
#   curl -H "Authorization: Bearer $TOKEN" https://wiki.example.com/api/v1/auth/me
#
# The public key must be registered in the profile (My profile -> SSH keys).
# Requires curl, ssh-keygen and python3 (for JSON handling).
set -euo pipefail
BASE="${1:?usage: $0 BASE_URL USERNAME [KEY_FILE]}"
USER_NAME="${2:?usage: $0 BASE_URL USERNAME [KEY_FILE]}"
KEY="${3:-$HOME/.ssh/id_ed25519}"
json() { python3 -c "import json,sys; print(json.load(sys.stdin)['$1'])"; }

CHALLENGE=$(curl -fsS -X POST -H 'Content-Type: application/json' \
  -d "$(python3 -c 'import json,sys; print(json.dumps({"username": sys.argv[1]}))' "$USER_NAME")" \
  "$BASE/api/v1/auth/ssh/challenge" | json challenge)

SIG=$(printf '%s' "$CHALLENGE" | ssh-keygen -Y sign -q -f "$KEY" -n ewiki-login -)

curl -fsS -X POST -H 'Content-Type: application/json' \
  -d "$(python3 -c 'import json,sys; print(json.dumps({"challenge": sys.argv[1], "signature": sys.argv[2]}))' "$CHALLENGE" "$SIG")" \
  "$BASE/api/v1/auth/ssh/verify" | json token
