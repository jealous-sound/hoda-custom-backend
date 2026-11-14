#!/usr/bin/env bash
set -euo pipefail

SERVER_URL="${HOD_SERVER_URL:-https://api.hodareborn.com}"
USAGE="Usage: $(basename "$0") <login>"

if [[ $# -ne 1 || "$1" == "-h" || "$1" == "--help" ]]; then
    echo "$USAGE"
    exit 0
fi
if [[ -z "${HOD_ADMIN_TOKEN:-}" ]]; then
    echo "Set HOD_ADMIN_TOKEN to the server's admin token." >&2
    exit 1
fi
if [[ ! "$1" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
    echo "A login is letters, digits, '.', '_' or '-'." >&2
    exit 1
fi

curl --silent --show-error --fail-with-body --header @- --request PUT \
    "$SERVER_URL/admin/logins/$1/password" <<<"Authorization: Bearer $HOD_ADMIN_TOKEN"
echo
