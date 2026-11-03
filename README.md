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
the backend when the game exits. On first start it imports the client's `player_save.dat` as the `local` player, so
existing progress carries over. Server logs go to `runtime/server.log`.

| Variable | Default |
| --- | --- |
| `HOD_CLIENT_DIR` | `../hoda-custom-client` |
| `HOD_BACKEND_ADDR` | `127.0.0.1:8787` |
| `HOD_BACKEND_DATA` | `runtime` |
| `HOD_PLAYER_ID` | `local` |

The client only talks to a backend when `HOD_BACKEND_URL` is set; without it the game keeps using
`player_save.dat`.

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
| `POST` | `/v1/sessions` | `{"player_id": "local"}` → `{"token", "player_id", "server_time"}` |
| `GET` | `/v1/players/{id}/profile` | the save JSON; revision in `X-Profile-Revision`, 404 before the first save |
| `PUT` | `/v1/players/{id}/profile` | requires `Authorization: Bearer <token>` and `If-Match: <revision>` (`0` creates) |

A write with a stale revision gets `409` and the current revision, so a client that started from an old copy
cannot overwrite newer progress. Profiles are validated (object, `account`/`roster`/`squads` present, account
counters non-negative) and stored as `runtime/players/<id>.json`.

## Next to move server-side

The client still computes everything and uploads the result. In order of cheat value:

1. Store pack rolls (`store.c` RNG and pity counters) and gem spending.
2. Battle rewards: campaign, Nexus escrow and PvP payouts (`progression.c`), verified by replaying the
   deterministic `battle_sim.c`.
3. Energy and stamina regeneration from server time instead of client timers.
4. PvP matchmaking and defense history against other players' squads.
