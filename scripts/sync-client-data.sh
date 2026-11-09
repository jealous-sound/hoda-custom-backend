#!/usr/bin/env bash
set -euo pipefail

BACKEND_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLIENT_DIR="${HOD_CLIENT_DIR:-$BACKEND_DIR/../hoda-custom-client}"
TARGET="$BACKEND_DIR/internal/catalog/data"
FILES=(store_packs.json hero_pool.json runes.json gear.json tutorial_rewards.json campaign_maps.json nexus_maps.json pvp_rules.json missions.json new_player.json
    custom_heroes.json intrinsic_passives.json ability_recipes.json consume_tables.json campaign_rosters.json nexus_rosters.json
    nexus_game_changers.json fortify_tables.json expeditions.json timed_quests.json stat_budget.json
    stat_budget_heroes.json)
BATTLE_TESTDATA="$BACKEND_DIR/internal/battle/testdata"

if [[ ! -d "$CLIENT_DIR/data" ]]; then
    echo "Client checkout not found at $CLIENT_DIR; set HOD_CLIENT_DIR." >&2
    exit 1
fi
mkdir -p "$TARGET"
for file in "${FILES[@]}"; do
    cp "$CLIENT_DIR/data/$file" "$TARGET/$file"
done
cp "$CLIENT_DIR/client-additional-data/characters.json" "$TARGET/characters.json"
cp "$CLIENT_DIR/tests/golden/battle_recipes.json" "$TARGET/battle_recipes.json"
mkdir -p "$BATTLE_TESTDATA"
cp "$CLIENT_DIR/tests/golden/battle_replays.json" "$BATTLE_TESTDATA/battle_replays.json"
echo "Copied ${FILES[*]}, characters.json and the battle goldens from $CLIENT_DIR"
