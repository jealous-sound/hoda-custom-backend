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
| `POST` | `/v1/players` | creates a guest → `201 {"player_id", "secret", "server_time"}` |
| `POST` | `/v1/sessions` | `{"player_id", "secret"}` → `{"token", "player_id", "server_time"}` |
| `GET` | `/v1/players/{id}/profile` | the save JSON; revision in `X-Profile-Revision`, 404 before the first save |
| `PUT` | `/v1/players/{id}/profile` | requires `Authorization: Bearer <token>` and `If-Match: <revision>` (`0` creates) |

A guest's session needs its secret (`401` otherwise); players created before accounts existed, like a seeded
`local`, open without one. A write with a stale revision gets `409` and the current revision, so a client that started from an old copy
cannot overwrite newer progress. Profiles are validated (object, `account`/`roster`/`squads` present, account
counters non-negative, `account.name` if present is 1-25 letters, digits, spaces or `-`, as in 5.4.4) and stored as
`runtime/players/<id>.json`. Accounts live in `runtime/accounts/<id>.json` with only a hash of the secret.

## Next to move server-side

The client still computes everything and uploads the result. In order of cheat value:

1. Store pack rolls (`store.c` RNG and pity counters) and gem spending.
2. Battle rewards: campaign, Nexus escrow and PvP payouts (`progression.c`), verified by replaying the
   deterministic `battle_sim.c`.
3. Energy and stamina regeneration from server time instead of client timers.
4. PvP matchmaking and defense history against other players' squads.
