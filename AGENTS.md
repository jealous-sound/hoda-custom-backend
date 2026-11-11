# AGENTS.md

Go backend for the Heroes of Dragon Age native client (`../hoda-custom-client`). Standard library only.
Run and API details are in `README.md`.

## Agent rules

- Source code has no comments. Express intent through names, structure and tests; do not add explanatory
  comments or doc comments. If a comment would carry something the code does not show (a client quirk, a
  protocol choice, a magic number), make the code show it: a named constant, a well-named helper or a test.
- Keep machine-read directives (`//go:build`, `//go:generate`, `//nolint`).
- `gofmt` is the format; `make format-check` must pass.

## Layout

- `cmd/hoda-server/`: entry point and flags.
- `internal/api/`: HTTP routes and their tests.
- `internal/profile/`: profile storage with revisions, and save validation.
- `internal/session/`: session tokens.
- `internal/activity/`, `internal/traffic/`: who is online, its history, and request counters for the `/admin` routes.
- `scripts/play-local.sh`: builds and runs backend and client together.

## Working here

- `make test` once after a change.
- The wire contract is shared with `source/backend.c` in the client; change both together.
- Never commit `runtime/` or `bin/`.
