#!/bin/sh
# Runs the idempotent setup (config, migrations, first admin) and starts the server.
set -e
if [ "$1" = "serve" ]; then
  ewiki setup --non-interactive
fi
exec ewiki "$@"
