#!/usr/bin/env bash
set -euo pipefail

BACKEND_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLIENT_DIR="${HOD_CLIENT_DIR:-$BACKEND_DIR/../hoda-custom-client}"
TARGET="$BACKEND_DIR/internal/catalog/data"
FILES=(store_packs.json hero_pool.json runes.json gear.json tutorial_rewards.json)

if [[ ! -d "$CLIENT_DIR/data" ]]; then
    echo "Client checkout not found at $CLIENT_DIR; set HOD_CLIENT_DIR." >&2
    exit 1
fi
mkdir -p "$TARGET"
for file in "${FILES[@]}"; do
    cp "$CLIENT_DIR/data/$file" "$TARGET/$file"
done
echo "Copied ${FILES[*]} from $CLIENT_DIR/data"
