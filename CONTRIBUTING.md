# Contributing

Thanks for helping out. This is a hobby project, so keep it friendly and small.

## Setup

```
git clone git@github.com:ezekielelvis/tank_ops_game.git
cd tank_ops_game
make run
```

## Before you open a pull request

- `make check` passes (vet, race-enabled tests, TypeScript type check).
- Game rules live in `gamestate.go` and must stay free of networking. Add or update a test in `gamestate_test.go` for any rule change.
- New maps go in `config.go`, should be point-symmetric (use `symmetricConfig`), and `TestMapsAreWellFormed` must pass.
- Bot changes: run `make test-full`. Medium should still beat easy and hard should still beat medium.
- Don't commit build output (`*.js` under `web/dist`, the `tank-ops` binary).
- Art and sound come from `tools/gen_assets`. Change the script and regenerate; don't hand-edit `web/src/sprites.ts`.
- Don't add assets you didn't make unless their license allows redistribution under MIT-compatible terms. Say where they came from in the PR.

## Reporting bugs

Open an issue with what you did, what you expected and what happened. For
gameplay bugs, include the map if you can see it and which turn it was.
For security problems see [SECURITY.md](SECURITY.md).

## Conduct

Be kind. See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
