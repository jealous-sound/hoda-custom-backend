# Heroes of Dragon Age client backend

Go backend for the native raylib client in `hoda-custom-client`. It owns what a player must not be able
to edit locally: the player profile (the client's save) and the clock the game's timers run on.

The Python server in `hoda-original-backend` is a different thing: it speaks the original Android 4.4.0
client's protobuf protocol.

## Play locally

With both repositories side by side:

```sh
./scripts/play-local.sh
```

It builds the backend and the client, starts the backend on `127.0.0.1:8787`, runs the game against it and stops
the backend when the game exits. The first launch creates a guest account and starts the tutorial; the client keeps
the guest's id and secret in `account.dat`. To keep playing an old save instead, set `HOD_PLAYER_ID=local`: the
backend then imports the client's `player_save.dat` as `local` on first start. Server logs go to
`runtime/server.log`.

| Variable | Default |
| --- | --- |
| `HOD_CLIENT_DIR` | `../hoda-custom-client` |
| `HOD_BACKEND_ADDR` | `127.0.0.1:8787` |
| `HOD_BACKEND_DATA` | `runtime` |
| `HOD_PLAYER_ID` | unset (guest account from `account.dat`) |

`HOD_BACKEND_URL` sends the client straight to that server. Without it, the client asks at startup whether to play
locally, with `player_save.dat`, or online against the server baked into it, `https://api.hodareborn.com`: this
service on Railway, behind the `api.hodareborn.com` custom domain.

## Commands

```sh
make test
make run
make format-check
```

## API

| Method | Path | |
| --- | --- | --- |
| `GET` | `/healthz` | liveness |
| `GET` | `/v1/time` | `{"server_time": <unix seconds>}` |
| `GET` | `/v1/client` | `{"latest", "minimum", "download_url"}`; see [Client versions](#client-versions) |
| `POST` | `/v1/players` | creates a guest → `201 {"player_id", "secret", "server_time"}` |
| `POST` | `/v1/sessions` | `{"player_id", "secret"}` → `{"token", "player_id", "server_time"}` |
| `GET` | `/v1/players/{id}/profile` | the save JSON; revision in `X-Profile-Revision`, 404 before the first save |
| `PUT` | `/v1/players/{id}/profile` | requires `Authorization: Bearer <token>` and `If-Match: <revision>` (`0` creates) |
| `POST` | `/v1/players/{id}/store/purchases` | `{"request_id", "pack", "currency", "price", "scripted"}` → `{"revision", "profile", "granted"}` |
| `POST` | `/v1/players/{id}/refills` | `{"request_id", "kind": "energy" \| "stamina"}` → `{"revision", "profile", "granted"}` |
| `POST` | `/v1/players/{id}/actions` | `{"request_id", "action", ...}` → `{"revision", "profile", "granted"}`; see [Economy](#economy) |

A session needs a registered account and its secret: an unknown player gets `404` (the client then creates a new
guest) and a wrong secret `401`. Only with `-open-unregistered`, which `scripts/play-local.sh` passes when it seeds a
save for `HOD_PLAYER_ID`, do players without an account, like a seeded `local`, open without one. Session tokens
expire after `-session-idle` (default `24h`) without use, and a player keeps at most 8; the client opens a new one
on `401`. A write with a stale revision gets `409` and the current revision, so a client that started from an old copy
cannot overwrite newer progress. Profiles are validated (at most 1 MiB, object, `account`/`roster`/`squads` present,
account counters non-negative, `account.name` if present is 1-25 letters, digits, spaces or `-`, as in 5.4.4) and
stored as `runtime/players/<id>.json`. Accounts live in `runtime/accounts/<id>.json` with only a hash of the secret.

Requests are rate-limited (token buckets in `internal/api/limits.go`); a limited request gets `429` with
`Retry-After`. New accounts: 10 per visitor address per hour and 360 per hour in all; sessions: 30 per address
then one per 10 s; any player route: 60 then 10 per second per player; chat lines: 5 then one per 3 s; abuse
reports: 5 per hour. The visitor address is the last `X-Forwarded-For` hop (else `X-Real-IP`, else the connection),
replaced by `CF-Connecting-IP` when that hop is a Cloudflare address. On Railway the last hop is Railway's own edge
node, so the Dockerfile passes `-trust-cf-connecting-ip`, which takes `CF-Connecting-IP` whenever it is present:
Railway's edge answers `403` to a request that carries that header without coming from Cloudflare, so it cannot be
spoofed by calling the origin directly. Without the header (a direct call) the edge hop is the key, which lumps such
callers together. The request log records the address as `client`.

Guests that leave in the first minutes would pile up as empty accounts, so the server deletes a guest and its
profile once it has gone `-guest-idle` (default `336h`, 14 days; `0` keeps everyone) without opening a session,
counted from its creation, if its profile has no `account.name` and `tutorial.complete` is false, or it never saved
a profile. Profiles without a `tutorial` section predate the tutorial and are kept. The check runs on start and every
`-prune-every` (default `1h`). Each session stamps `last_session_at` in the account file.

## Client versions

The client sends `User-Agent: HeroesOfDragonAge/<version>`, its `git describe --tags` output (`v0.5.0`,
`v0.5.0-rc.1`, `v0.5.0-3-gabc1234`). `GET /v1/client` tells it the newest release and the oldest one the server still
serves:

```json
{"latest": "v0.5.1", "minimum": "v0.5.0", "download_url": "https://hodareborn.com/#download"}
```

`latest` is the tag of the client repository's latest GitHub release (`-client-releases`), looked up on start and
every `-client-releases-every` (default `15m`) with `If-None-Match`, so unchanged answers do not count against
GitHub's rate limit; a failed lookup keeps the last tag. It is never lower than `minimum`, and empty while neither is
known. `minimum` comes from `-min-client-version` (on Railway, the `HOD_MIN_CLIENT_VERSION` variable) and is empty by
default. Raise it only for a release the server cannot serve older clients alongside, such as a change to the battle
simulation or to an action's shape: from then on every other `/v1/` request from an older client gets `426 Upgrade
Required` with the same three fields and an `error`, and the client blocks online play until the player downloads
the new version. Release candidates sort before their release and `git describe` builds after it. A request without
the client's `User-Agent`, or from a build without a version tag (`dev`, a bare commit hash), is never refused.
`download_url` is `-client-download-url`, the website's download section by default.

## Store

Pack purchases and energy/stamina refills run on the server. The client sends the pack and the price it shows;
the server checks the pack is on sale (UTC schedules and flash rotation), that the roster or rune inventory has room
and the player can pay, then rolls the pack, charges it and writes the heroes, runes, gear, counters and the
`summoned` mission counter into the stored profile. The reply carries the whole new profile, which the client loads
in place of its own, and `granted`: the heroes with their instance ids, runes, gear and whether a tome dropped.

- The roll state lives only in the server's part of `runtime/players/<id>.json` and is seeded randomly per player,
  so pulls cannot be predicted or rerolled by reloading a save.
- Both actions need `If-Match` with the current revision (`409` otherwise). A repeated `request_id` replays the
  first answer without charging again, so a client can retry after a dropped reply.
- Refusals are `422 {"error", "code", "revision", "profile"}` with `code` one of `cant_afford`, `inventory_full`, `unavailable`,
  `counter_limit`, `offer_changed`, `already_full`, `bad_profile`, and for battles `squad_changed`, `opponent_changed`,
  `outcome_mismatch`.
- Tutorial packs from `tutorial_rewards.json` are scripted once per player and only while the tutorial runs.

## Economy

The server is the only writer of the player's economy. A profile `PUT` is merged: the client's copy is kept, but
these fields keep the server's values:

- `account`: level, experience, energy, stamina, their maximums and regen timers, gold, gems.
- `store`: opened counters, legendary pity, vouchers. `nexus`: potions, keys, event unlocks and runs, and the run's
  progress and escrow (the event and difficulty stay the client's until the run is committed).
- `campaign`: completions and losses.
- `missions`: `claimed` and `counters`; `progress` is rewritten from the server's own high-water marks.
- `roster`: heroes the server has not granted are dropped, a hero the upload leaves out is put back (heroes leave
  only through `consume` and `fortify`), and every hero keeps the server's tier, level, `xp`, `consume_xp`,
  `ascended` flag and `fortify` slots (absent when none is filled).
- `pvp`: trophies, best trophies, wins, losses, streaks, trophies won, defense results and timer, the trophy hold
  timers, the day's next-opponent count (`nexts_used`, `nexts_day`) and the battle history. The client keeps
  `trophies_seen` and its event display.
- `gear`: tomes, prestige and ascension flags; the inventory may move into hero slots but a gear id may not grow.
- `runes`: unknown runes are dropped, `inventory_expire_at` and running activations are the server's, and a new
  activation may last at most the rune's duration.

Energy and stamina regenerate from server time (`regen_at` in the server state), one point per 300 s. A new
player's first upload starts from the new-player economy (1725 gold, 41 gems, level 1, the three starter heroes and
starter runes) whatever it claims.

Everything that grants or spends goes through `POST /v1/players/{id}/actions`:

| `action` | Fields | |
| --- | --- | --- |
| `battle_start` | `mode: "campaign"`, `map`, `node`, battle inputs | checks the node is unlocked, spends its energy, issues the seed |
| `pvp_match` | `event`, optional `bot`, `next` | matches a ladder or event opponent, or the chosen `bot`, from the server's roll, trophies, banners, level and clock; `next` spends one of the day's next opponents and rolls again; answers `match` |
| `battle_start` | `mode: "pvp"`, `match`, `event`, or `duel` or `revenge_at`, battle inputs | spends 1 stamina; fights the server's current match and uses it up (a duel from the member's stored squad, level and trophies, a revenge from a lost defense in the server's history) |
| `battle_start` | `mode: "nexus"`, `step`, `revive`, battle inputs | checks the step and runs left, spends energy (and a potion to revive) |
| `battle` | `battle_id`, `won`, `crit_gold` | replays the started battle and pays the server's result: first win, mastery, challenge or grind rewards from `campaign_maps.json`; PvP gold and XP by the opponent's level, trophies, streaks and history, then due defenses, event banners per win and milestones once; Nexus escrow of gold, XP, potions and a unit roll |
| `nexus_award` / `nexus_reset` | | pays the escrow and rolls the units / abandons the run |
| `consume` | `target`, `seeds` | consumes seed heroes that are in no squad into the target: hero XP, consume XP and tier-ups from `consume_tables.json`; the seeds and their gear are gone |
| `fortify` | `parent`, `slots: [{"index", "unit"}]` | sacrifices heroes that are in no squad into the parent's open fortify slots (the progression's fortify column below its consume level); the same hero combines into a higher tier; the sacrificed heroes and their gear are gone |
| `remove_fortify` | `parent`, `index` (`-1` for every filled slot) | charges `remove_gem_cost` from `fortify_tables.json` per child and gives each back as a new level 1 hero, in slot order |
| `pvp_defend` | | runs the simulated defenses due since the last check (at most three); the client sends it at startup |
| `nexus_unlock` | `event` | spends the event's keys |
| `mission_claim` | `mission` | once per mission; campaign, level and map stats are checked against the server's own state |
| `pvp_event_claim` | `cycle` | once per finished event, ranked from the banners the server counted |
| `gear_sell`, `gear_prestige`, `ascend` | `gear` / `instance_id` | sell value, prestige slot gems, a tome |
| `rune_prestige`, `rune_craft` | `squad` / `pairs` | prestige slot gems; each pair of identical idle runes becomes one of the next tier |
| `tutorial_reward` | `reward: "giant" \| "gem_pack" \| "parting_gift"` | once each |

Rune gold and XP bonuses come from the active runes of `current_squad`, and every battle's XP also goes to each hero
that fought, up to the hero's level cap for its tier (five more once ascended). Crit gold is credited up to 100 crits.

Missions are counted on the server. Campaign, level, hero, collection, squad and PvP stats are read from the
server's own state; the other counters grow only from actions: summons from purchases, gold from everything but
mission rewards, consumes and combines from `consume`, rune activations from uploads, challenge attempts, crits,
battle days (UTC) and event banners from battles. A battle's biggest damage, whether it landed a faction hit and,
in an event, whether the whole squad was buffed come from the server's replay. A mission is claimable once the
server has seen its stat reach the target, even if it later drops.

## Battle replay

The server decides every battle by replaying the client's deterministic simulation. `internal/battle` is a Go port of
the client's `source/battle_sim.c` with the data it reads: hero stats by tier and level, consume crit chance,
ascension, fortify bonuses from `fortify_tables.json`, intrinsic passives, gear and rune effects, ability recipes (the built-in table comes from the client's
`tests/golden/battle_recipes.json`), campaign and Nexus rosters, game changers and PvP opponents. Float math stays in
float32 with every product rounded before it is added, so results do not depend on the CPU fusing multiply-adds.

Golden tests in `internal/battle` replay `testdata/battle_replays.json`, battles the C simulation recorded (every hero
at least twice, all modes, gear, runes and game changers), and require the same outcome, crit count, damage, faction
hit, squad buff and SHA-256 of the whole action timeline, plus the same campaign and Nexus rosters, game changers and
PvP opponents. The client regenerates the corpus with `make golden`; `scripts/sync-client-data.sh` copies it with the
data, and `go test` fails when either drifts from a client checkout next to this one.

Battle inputs in `battle_start` are the cells the client is about to simulate:

| Field | |
| --- | --- |
| `squad` | `{"units": [{"slot", "instance_id", "id", "tier", "level", "consume_xp", "ascended", "gear": [5 ids], "fortify": [{"slot", "id", "tier", "ascended"}]}], "runes": [def ids]}`; `fortify` only for a fortified hero |
| `rune_ids` | the uuids of the squad's active runes, in slot order |
| `enemy` | `{"units": [{"slot", "id", "tier", "level"}], "runes": [def ids]}` |
| `changer` | the game changer the client applies, `{"effects": [{"groups", "units", "faction_mask", "health_pct", "power_pct"}]}`, or `null` |
| `match` | PvP ladder, event and chosen-bot battles: the `id` of the match `pvp_match` answered |
| `opponent` | PvP revenge only: `{"kind": "revenge", "seed"}`, the seed of the lost defense |
| `revenge_at` | PvP revenge only: the time of the lost defense in the server's history |
| `tutorial_outcome` | optional `"victory"` or `"defeat"`; only while the tutorial runs, on the first two maps |

The server rebuilds the squad from its stored roster, gear and rune activations (runes must be activated and not
expired) and the enemy from its own data: the campaign or Nexus roster and the event's game changer; the PvP opponent
from the server's current match; a revenge from the lost defense; or a duel member's current squad. A difference, or a
`match` that is not the current one (already fought, replaced, or an event match whose event has ended), is refused as
`squad_changed` or `opponent_changed` before anything is spent. The server then rolls a
random seed (or, for `tutorial_outcome`, searches forward from it for the scripted result, as the client does),
stores the pending battle in the server state and answers `{"battle_id", "seed"}`.

PvP opponents come only from `pvp_match`. The server keeps a random roll per player; the match seed is mixed from the
roll, `event` and the chosen bot, and the opponent is derived as in the client from that seed, the server's trophies,
the player's level and the server's clock (event matchmaking also from the banners the server counted this event). The
answer is `{"match": {"id", "event", "chosen", "bot", "squad", "level", "trophies", "seed", "at"}}`, and the match is
stored as the one `battle_start` may fight. The roll stands until a matched battle starts or `next` spends one of the
`daily_nexts` (counted per UTC day; `counter_limit` when none are left; refused for a chosen bot), so leaving the
battle screen, switching between ladder and event, or asking again keeps the seed, and so the opponent while the
trophies, banners and day stay put.

`battle` replays the pending battle. The server pays its own outcome and crit count; PvP event banners come from the
derived opponent's trophy gain. If the client's `won` or `crit_gold` differ, the battle is still settled with the
replayed result and the answer is `422 outcome_mismatch` with the new profile, so a false win pays nothing and a hidden
loss still counts. A battle that is never finished settles the same way at the next `battle_start`, under `settled` in
its answer. A `battle` without `battle_id` (clients before this change) is refused as `unavailable`.

The pack catalog, hero pool (with factions and per-tier level caps), runes, gear, tutorial rewards, campaign, Nexus,
PvP rules, missions, new-player roster and the battle data (characters, custom heroes, intrinsic passives, ability
recipes, consume and fortify tables, rosters and game changers) are embedded from `internal/catalog/data`, copied from the client
by `scripts/sync-client-data.sh`; `go test` fails when the copies drift from a client checkout next to this one.

## Guilds

The 5.4.4 guild system and guild chat, under `/v1/players/{id}/` with the session token (no `If-Match`, except for
creating, which charges gold like a store purchase). Everything lives in `runtime/guilds.json`. Refusals are `422
{"error", "code"}` with the original `GuildResponseCode` names (`GUILD_FULL`, `INSUFFICIENT_RANK`, ...).

| Method | Path | |
| --- | --- | --- |
| `GET` | `guild` | your state: `{"rank", "guild", "applications", "notices", "unread"}`; notices are handed out once |
| `POST` | `guilds` | `{"request_id", "name", "description", "message_of_the_day", "language", "status", "icon", "required_level"}`, needs `If-Match`; 10000 gold → `{"revision", "profile", "granted": <state>}` |
| `GET` | `guilds/recommended` | up to 20 open or application-only guilds you can join → `{"guilds"}` |
| `GET` | `guilds/search?name=` | the guild with that name, ignoring case → `{"guilds"}` |
| `GET` | `guilds/{guild}` | `{"guild", "applied"}`; applications only for its officers |
| `POST` | `guilds/{guild}/join` | `{"application_text"}`; joins an open guild or applies to an application-only one → state |
| `POST` | `guilds/{guild}/cancel` | withdraws your application → state |
| `POST` | `guild/settings` | officers and the leader; same fields as creating except the name → state |
| `POST` | `guild/applications/{player}/accept` or `/deny` | officers and the leader → state |
| `POST` | `guild/members/{player}/rank` | `{"target_rank"}`, leader only; promoting an officer to leader makes you an officer → state |
| `POST` | `guild/members/{player}/kick` | needs a higher rank than the target → state |
| `POST` | `guild/leave` | the leader leaves only as the last member, which deletes the guild → state |
| `POST` | `guild/messages` | `{"after", "text", "read_through"}`; posts `text` if set, returns messages with an id above `after` and `unread` |
| `GET` | `guild/members/{player}/duel` | another member of your guild: `{"id", "name", "level", "trophies", "units": [{"slot", "id", "tier", "level"}], "runes"}` from their current squad and active runes |
| `GET` | `guild/onslaught?cycle=` | a started Onslaught: `{"cycle", "finished", "total_guilds", "guilds", "mine", "members", "my_banners", "minimum_banners", "award_rank", "claimed"}`; `guilds` is the top 100 by summed member banners |
| `POST` | `guild/onslaught/claim` | `{"request_id", "cycle"}`, needs `If-Match`; pays your guild's rank award once → `{"revision", "profile", "granted"}` |
| `POST` | `reports` | `{"kind": "guild" or "application", "abuse_type": "HARRASSMENT", "SEXUAL", "EXPLICIT" or "OTHER", "guild_id" or "applicant_id"}` → `{"status"}` |

Rules follow the 5.4.4 client: unlock at level 5, 30 members, 10 pending applications per guild, one pending
application per player (a new one withdraws the old), names of 4 to 18 characters, descriptions and messages of the
day up to 150, minimum level 5 to 423, 23 emblems, and leaders idle for 5 days handing over to the highest-ranked
active member. Membership changes, rank changes and settings changes post the original `GUILD_CHAT_EVENT_*` lines
into chat; the last 100 messages are kept. Chat lines are up to 140 characters; `<` and `>` are refused in text.

**Onslaught.** Every fourth PvP event cycle (`guild_every_cycles` in `pvp_rules.json`) runs an Onslaught from
`guild_catalog` instead of the ordinary event: Dangerous Purpose, War of Attrition and Ascension, with their original
title keys. It is a normal PvP event for each player (banners, milestones, individual ranks); on top, every banner a
member earns in its event battles is added to their guild's tally, if they were in that guild when the event
started. Guilds rank by summed banners; banners stay with the guild they were earned in. After the event each
member who earned at least `guild_minimum_banners` (100) claims the reward of their guild's rank once. Guild rank
rewards are the prize tables the client announced for the first two Onslaughts
(`INTERSTITIAL_GUILD_EVENT_ANNOUNCEMENT_MESSAGE1_KEY` and `INTERSTITIAL_GUILD_EVENT2_...`), parsed by the client's
`tools/build_pvp_rules.py`; Ascension reuses the second table. The last six Onslaughts are kept in `guilds.json`.

**Duels.** A duel is an ordinary PvP battle (`"mode": "pvp"`) against a member of your guild: the battle action
takes `"duel": "<member id>"`, costs one stamina and pays gold and XP for the member's level. It cannot be an event
battle. The defender's trophies do not change.

**Abuse reports** append `{"time", "reporter", "issue_type", "description"}` to `runtime/reports.jsonl` and log it.
The description is built on the server like the original help request: `Guild Name: <name>, Id: <id>` for a guild,
`Player Name: <name>, Id: <id>, Application Text: <text>` for an application, which only officers of the guild that
received it can report.

## Next to move server-side

1. Battle outcomes: replay the deterministic `battle_sim.c` instead of trusting `won` and `crit_gold`.
2. Hero progression (levels, consume, tiers) and mission counters fed by battles, still uploaded by the client.
3. PvP trophies, matchmaking and defense history against other players' squads.

## License

The code in this repository is licensed under the GNU Affero General Public License v3.0 (AGPL-3.0-only); see `LICENSE`.

Heroes of Dragon Age is a trademark of Electronic Arts. The original game data, art, audio, fonts and APK that the
game reads or downloads are © Electronic Arts Inc. and are not covered by this license. This is a non-commercial fan
project and is not affiliated with or endorsed by Electronic Arts or BioWare.
