# Heroes of Dragon Age backend

A Go server, standard library only, for the native client in `../hoda-custom-client`. It owns what a player must not
be able to edit locally: the profile, the economy, the clock, and battle outcomes, which it decides by replaying the
client's simulation. Production runs on Railway at `https://api.hodareborn.com`.

## Run

With both repositories side by side:

```sh
./scripts/play-local.sh
```

The script builds both projects, starts the server on `127.0.0.1:8787`, runs the game against it, and stops the
server when the game exits. Server logs go to `runtime/server.log`.

| Variable | Default | |
| --- | --- | --- |
| `HOD_CLIENT_DIR` | `../hoda-custom-client` | client checkout |
| `HOD_BACKEND_ADDR` | `127.0.0.1:8787` | listen address |
| `HOD_BACKEND_DATA` | `runtime` | data directory |
| `HOD_PLAYER_ID` | unset (new guest) | plays as this id, such as `local`, importing the client's `player_save.dat` on first start |

`make test` runs `go vet` and `go test`. `make run` starts the server alone, and `make format-check` checks `gofmt`.

## Flags

| Flag | Default | |
| --- | --- | --- |
| `-addr` | `127.0.0.1:8787` | listen address |
| `-data` | `runtime` | accounts, profiles, `guilds.json`, `reports.jsonl` |
| `-seed-profile`, `-seed-player` | unset, `local` | imports a client save for one player |
| `-open-unregistered` | off | lets players without an account, such as a seeded `local`, open sessions |
| `-session-idle` | `24h` | unused session tokens expire after this |
| `-guest-idle`, `-prune-every` | `336h`, `1h` | deletes guests that never named themselves or finished the tutorial; `0` keeps them |
| `-trust-cf-connecting-ip` | off | takes the visitor address from `CF-Connecting-IP`; only behind Railway's edge |
| `-min-client-version` | empty | sends `426 Upgrade Required` to older clients |
| `-client-releases`, `-client-releases-every` | the client repo's latest release, `15m` | source of `latest` in `/v1/client` |
| `-client-download-url` | `https://hodareborn.com/#download` | the page the client opens to update |

## Deploy

Railway builds the `Dockerfile` (see `railway.json`, health check `/healthz`). The server listens on `$PORT`, keeps its
data in `/data`, and runs with `-trust-cf-connecting-ip`. The Railway variable `HOD_MIN_CLIENT_VERSION` feeds
`-min-client-version`. Raise it only for a client release that older clients cannot play alongside, such as a change
to the simulation or to an action's shape.

## Client data

The client's rule tables, `characters.json` and golden battle corpus are embedded from `internal/catalog/data` and
`internal/battle/testdata`. `scripts/sync-client-data.sh` copies them from the client, from `HOD_CLIENT_DIR` when set.
`go test` fails when the copies drift from the client checkout next to this one. Run the script after the client
changes `data/` or runs `make golden`.

The wire contract is shared with the client's `source/backend.c`; change both together.

## API

| Method | Path | |
| --- | --- | --- |
| `GET` | `/healthz` | liveness |
| `GET` | `/v1/time` | `{"server_time"}` in unix seconds |
| `GET` | `/v1/client` | `{"latest", "minimum", "download_url"}` |
| `POST` | `/v1/players` | creates a guest, answering `201 {"player_id", "secret", "server_time"}` |
| `POST` | `/v1/sessions` | `{"player_id", "secret"}` answers `{"token", "player_id", "server_time"}` |
| `GET` | `/v1/players/{id}/profile` | the save JSON, revision in `X-Profile-Revision` |
| `PUT` | `/v1/players/{id}/profile` | uploads the save; `If-Match: 0` creates it |
| `POST` | `/v1/players/{id}/store/purchases` | `{"request_id", "pack", "currency", "price", "scripted"}` |
| `POST` | `/v1/players/{id}/refills` | `{"request_id", "kind": "energy" or "stamina"}` |
| `POST` | `/v1/players/{id}/actions` | `{"request_id", "action", ...}` |
| | `/v1/players/{id}/guild…`, `/guilds…`, `/reports` | guilds, chat, Onslaught, duels and abuse reports; see `internal/api/server.go` |

Player routes need `Authorization: Bearer <token>`. Profile uploads, purchases, refills, actions and guild creation
also need `If-Match: <revision>`. Calls that grant or spend answer `{"revision", "profile", "granted"}`, and the client
loads that profile in place of its own. A repeated `request_id` replays the first answer without charging again.

| Status | Meaning |
| --- | --- |
| `401` | wrong secret or expired token; the client opens a new session |
| `404` | unknown player; the client creates a new guest |
| `409` | stale revision; carries the current one |
| `422` | refused: `{"error", "code"}`, plus `revision` and `profile` on economy calls; codes such as `cant_afford`, `inventory_full`, `squad_changed` or `outcome_mismatch`, and the original `GuildResponseCode` names for guilds |
| `428` | missing `If-Match` |
| `426` | client older than `-min-client-version`; never sent to builds without a version tag |
| `429` | rate limited, with `Retry-After`; limits are in `internal/api/limits.go` |

## Economy

The server is the only writer of the economy. A profile `PUT` is merged: the client's copy is kept, but the server's
values win for account currencies, levels and timers, store counters, Nexus, campaign, missions, expeditions,
`timed_quests`, PvP, gear, runes, and the roster's heroes with their progression. Anything that grants or spends is an
action, handled in `internal/economy/actions.go`:

- Battles: `battle_start`, `battle`, `pvp_match`, `pvp_defend`
- Heroes: `consume`, `fortify`, `remove_fortify`, `ascend`
- Expeditions: `expedition_start`, `expedition_claim`, `expedition_recall`
- Bounties: `quest_claim`, `quest_swap`, `quest_chest`
- Nexus: `nexus_unlock`, `nexus_award`, `nexus_reset`
- Rewards: `mission_claim`, `pvp_event_claim`, `tutorial_reward`
- Items: `gear_sell`, `gear_prestige`, `rune_prestige`, `rune_craft`

Pack rolls, PvP matches and battle seeds are rolled on the server from random state it never sends, so reloading a
save cannot predict or reroll them.

## Battle replay

`internal/battle` is a Go port of the client's `source/battle_sim.c`. It keeps float32 math with each product rounded
before it is added, so results do not depend on the CPU fusing multiply-adds.

1. `battle_start` sends the squad, its active runes, the enemy and the game changer. The server checks them against its
   own state and data, refusing a mismatch with `squad_changed` or `opponent_changed`. It then spends the energy or
   stamina and answers `{"battle_id", "seed"}`. PvP opponents come only from `pvp_match`.
2. `battle` replays the battle and pays the server's result. If the client's claim differs, the battle still settles
   on the replay and the answer is `422 outcome_mismatch`. A battle that is never finished settles at the next
   `battle_start`.

PvP battles replay under the stat budget (`stat_budget.json` and `stat_budget_heroes.json`) when `battle.stat_budget`
in `pvp_rules.json` is on; it ships off. Turning it on changes the PvP simulation, so it needs a client release that
carries the same switch and a raised `HOD_MIN_CLIENT_VERSION`.

The golden tests replay `internal/battle/testdata/battle_replays.json`, recorded by the C simulation. They require the
same outcome, crits and SHA-256 of the action timeline.

## License

The code in this repository is licensed under the GNU Affero General Public License v3.0 (AGPL-3.0-only); see `LICENSE`.

Heroes of Dragon Age is a trademark of Electronic Arts. The original game data, art, audio, fonts and APK that the
game reads or downloads are © Electronic Arts Inc. and are not covered by this license. This is a non-commercial fan
project and is not affiliated with or endorsed by Electronic Arts or BioWare.
