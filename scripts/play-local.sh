#!/usr/bin/env bash
set -euo pipefail

BACKEND_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLIENT_DIR="${HOD_CLIENT_DIR:-$BACKEND_DIR/../hoda-custom-client}"
ADDR="${HOD_BACKEND_ADDR:-127.0.0.1:8787}"
DATA_DIR="${HOD_BACKEND_DATA:-$BACKEND_DIR/runtime}"
PLAYER_ID="${HOD_PLAYER_ID:-}"
BACKEND_URL="http://$ADDR"
HEALTH_ATTEMPTS=50

if [[ ! -f "$CLIENT_DIR/Makefile" ]]; then
    echo "Client checkout not found at $CLIENT_DIR; set HOD_CLIENT_DIR." >&2
    exit 1
fi
CLIENT_DIR="$(cd "$CLIENT_DIR" && pwd)"

backend_healthy() {
    curl -fsS "$BACKEND_URL/healthz" >/dev/null 2>&1
}

echo "Building backend..."
(cd "$BACKEND_DIR" && go build -o bin/hoda-server ./cmd/hoda-server)
echo "Building client..."
make -C "$CLIENT_DIR" game

SERVER_PID=""
stop_backend() {
    if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
        kill "$SERVER_PID"
        wait "$SERVER_PID" 2>/dev/null || true
    fi
}
trap stop_backend EXIT INT TERM

if backend_healthy; then
    echo "Reusing backend already running at $BACKEND_URL"
else
    mkdir -p "$DATA_DIR"
    SEED_ARGS=()
    if [[ -n "$PLAYER_ID" ]]; then
        SEED_ARGS=(-seed-player "$PLAYER_ID" -seed-profile "$CLIENT_DIR/player_save.dat")
    fi
    "$BACKEND_DIR/bin/hoda-server" \
        -addr "$ADDR" \
        -data "$DATA_DIR" \
        ${SEED_ARGS[@]+"${SEED_ARGS[@]}"} \
        >>"$DATA_DIR/server.log" 2>&1 &
    SERVER_PID=$!
    for _ in $(seq "$HEALTH_ATTEMPTS"); do
        backend_healthy && break
        if ! kill -0 "$SERVER_PID" 2>/dev/null; then
            echo "Backend exited during startup; see $DATA_DIR/server.log" >&2
            tail -n 20 "$DATA_DIR/server.log" >&2
            exit 1
        fi
        sleep 0.1
    done
    backend_healthy || { echo "Backend did not become healthy at $BACKEND_URL" >&2; exit 1; }
    echo "Backend running at $BACKEND_URL (pid $SERVER_PID, log $DATA_DIR/server.log)"
fi

cd "$CLIENT_DIR"
if [[ -n "$PLAYER_ID" ]]; then
    export HOD_PLAYER_ID="$PLAYER_ID"
fi
HOD_BACKEND_URL="$BACKEND_URL" ./game "$@"
