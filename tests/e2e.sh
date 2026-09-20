#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
project="distsys-e2e-$$"
compose() {
    docker compose --env-file /dev/null -p "$project" -f tests/compose.yaml "$@"
}
cleanup() {
    status=$?
    trap - EXIT
    if [ "$status" -ne 0 ]; then compose logs --no-color app db || true; fi
    compose down --volumes --remove-orphans > /dev/null 2>&1 || true
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
compose build app browser
compose up -d --wait db
compose run --rm app grant-admin 303
compose run --rm app grant-assistant 101
compose up -d --wait app
compose run --rm browser
