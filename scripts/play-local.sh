#!/usr/bin/env bash
set -euo pipefail

BACKEND_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLIENT_DIR="${HOD_CLIENT_DIR:-$BACKEND_DIR/../hoda-custom-client}"
CLIENT_EXECUTABLE="${HOD_CLIENT_EXECUTABLE:-Heroes of Dragon Age.app/Contents/MacOS/Heroes of Dragon Age}"
ADDR="${HOD_BACKEND_ADDR:-127.0.0.1:8787}"
DATA_DIR="${HOD_BACKEND_DATA:-$BACKEND_DIR/runtime}"
PLAYER_ID="${HOD_PLAYER_ID:-}"
ACCOUNT_FILE="${HOD_ACCOUNT_PATH:-$DATA_DIR/client-account.dat}"
SEEDED_ACCOUNT_MARK="$DATA_DIR/seeded-account"
BACKEND_URL="http://$ADDR"
HEALTH_ATTEMPTS=50

if [[ ! -f "$CLIENT_DIR/Makefile" ]]; then
    echo "Client checkout not found at $CLIENT_DIR; set HOD_CLIENT_DIR." >&2
    exit 1
fi
CLIENT_DIR="$(cd "$CLIENT_DIR" && pwd)"
LOCAL_SAVE="$CLIENT_DIR/player_save.dat"

backend_healthy() {
    curl -fsS "$BACKEND_URL/healthz" >/dev/null 2>&1
}

SERVER_PID=""
stop_backend() {
    if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
        kill "$SERVER_PID"
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    SERVER_PID=""
}
trap stop_backend EXIT INT TERM

start_backend() {
    "$BACKEND_DIR/bin/hoda-server" -addr "$ADDR" -data "$DATA_DIR" "$@" >>"$DATA_DIR/server.log" 2>&1 &
    SERVER_PID=$!
    for _ in $(seq "$HEALTH_ATTEMPTS"); do
        backend_healthy && return 0
        kill -0 "$SERVER_PID" 2>/dev/null || break
        sleep 0.1
    done
    stop_backend
    return 1
}

report_failed_start() {
    echo "Backend did not start at $BACKEND_URL; see $DATA_DIR/server.log" >&2
    tail -n 20 "$DATA_DIR/server.log" >&2
    exit 1
}

json_text_field() {
    sed -n 's/.*"'"$1"'"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1
}

create_seeded_account() {
    local reply player secret
    start_backend || report_failed_start
    reply="$(curl -fsS -X POST "$BACKEND_URL/v1/players")"
    stop_backend
    player="$(json_text_field player_id <<<"$reply")"
    secret="$(json_text_field secret <<<"$reply")"
    if [[ -z "$player" || -z "$secret" ]]; then
        echo "The backend did not hand out a local account." >&2
        exit 1
    fi
    mkdir -p "$(dirname "$ACCOUNT_FILE")"
    (umask 077 && printf '{"player_id": "%s", "secret": "%s"}\n' "$player" "$secret" >"$ACCOUNT_FILE")
    echo "$player" >"$SEEDED_ACCOUNT_MARK"
    if start_backend -seed-player "$player" -seed-profile "$LOCAL_SAVE"; then
        echo "Online Game starts as $player, a copy of $LOCAL_SAVE"
        return
    fi
    echo "Could not import $LOCAL_SAVE (see $DATA_DIR/server.log); $player starts as a new player." >&2
    start_backend || report_failed_start
}

wants_seeded_account() {
    [[ -z "$PLAYER_ID" && ! -f "$SEEDED_ACCOUNT_MARK" && ! -f "$ACCOUNT_FILE" && -f "$LOCAL_SAVE" ]]
}

echo "Building backend..."
(cd "$BACKEND_DIR" && go build -o bin/hoda-server ./cmd/hoda-server)
echo "Building client..."
make -C "$CLIENT_DIR"

if backend_healthy; then
    echo "Reusing backend already running at $BACKEND_URL"
else
    mkdir -p "$DATA_DIR"
    if [[ -n "$PLAYER_ID" ]]; then
        start_backend -seed-player "$PLAYER_ID" -seed-profile "$LOCAL_SAVE" -open-unregistered || report_failed_start
    elif wants_seeded_account; then
        create_seeded_account
    else
        start_backend || report_failed_start
    fi
    echo "Backend running at $BACKEND_URL (pid $SERVER_PID, log $DATA_DIR/server.log)"
fi

cd "$CLIENT_DIR"
if [[ -n "$PLAYER_ID" ]]; then
    export HOD_PLAYER_ID="$PLAYER_ID"
fi
echo "Local Game plays $LOCAL_SAVE; Online Game plays on $BACKEND_URL with the account in $ACCOUNT_FILE"
HOD_SERVER_URL="$BACKEND_URL" HOD_ACCOUNT_PATH="$ACCOUNT_FILE" "./$CLIENT_EXECUTABLE" "$@"
