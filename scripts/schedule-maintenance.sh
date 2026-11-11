#!/usr/bin/env bash
set -euo pipefail

SERVER_URL="${HOD_SERVER_URL:-https://api.hodareborn.com}"
ROUTE="$SERVER_URL/admin/maintenance"
USAGE="Usage: $(basename "$0") [status | cancel | <wait such as 30m or 2h> | <time such as 2026-01-02T15:04:05Z>]"

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    echo "$USAGE"
    exit 0
fi
if [[ -z "${HOD_ADMIN_TOKEN:-}" ]]; then
    echo "Set HOD_ADMIN_TOKEN to the server's admin token." >&2
    exit 1
fi

call() {
    curl --silent --show-error --fail-with-body --header @- "$@" <<<"Authorization: Bearer $HOD_ADMIN_TOKEN"
    echo
}

case "${1:-status}" in
    status)
        call "$ROUTE"
        ;;
    cancel)
        call --request DELETE "$ROUTE"
        ;;
    *T*)
        call --request PUT --data "{\"at\":\"$1\"}" "$ROUTE"
        ;;
    *)
        call --request PUT --data "{\"in\":\"$1\"}" "$ROUTE"
        ;;
esac
