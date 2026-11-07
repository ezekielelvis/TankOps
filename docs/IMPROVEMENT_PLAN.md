# tank-ops: Improvement Plan

Based on a read of the Go backend (~1.5k lines) and TypeScript client (~3k lines excluding the generated `sprites.ts`).
The game is a 2-player, simultaneous-turn, hex-grid tank game with a shrinking arena, fog of war, and room codes.
The core design is good. The server is authoritative, each player only gets the results they are allowed to see, and the room and player state machines are clean.
The gaps are in robustness, content, and polish.

## Findings

### Build and repo hygiene
- `.gitignore` ignores `*.js`, so `web/dist` has no JS in git. A fresh clone shows a blank page until `tsc` is run.
- The 8.7 MB compiled `tank-ops` binary is committed (about 40k lines in the diff stat). It should not be in git.
- There are no tests, no CI, no Makefile, and no lint or format step.
- The README is minimal. It has no rules, no controls, and no architecture notes.
- `main.go` registers `net/http/pprof` on the public mux, which exposes `/debug/pprof` to anyone.
- The listen address is hardcoded to `localhost:8000`. The `ListenAndServe` error is ignored and there is no graceful shutdown.
- `fmt.Println("hello")` and `fmt.Println("queued", ...)` are used as logging.

### Server robustness
- `panic(...)` is used for "impossible" states in `room.go` and `player.go`. A panic in any goroutine kills the whole process and every room.
- Room-to-player sends are unbuffered and blocking. A slow or stuck client can stall its room. Because `Hub.Run` does `room.requests <- msg` synchronously, a stuck room can then stall joining for everyone.
- There are no read limits, read deadlines, or ping/pong on the websocket. Dead connections linger and big messages are accepted.
- Input is not validated: room codes (length, charset), `TankAction` lists (duplicates, unknown ids), and path length. The unit-step and traversal checks are good, but `Path` is copied and walked unbounded in length before the `driveRange` check short-circuits.
- Nothing handles a dropped connection. A refresh forfeits the game.
- There is no turn timer. One idle player blocks the game forever.
- Go map iteration order is random. Where tanks are iterated (visibility, collisions), results are not deterministic, which makes bugs hard to reproduce and tests flaky.
- Only one hardcoded map (`NewBasicConfig`) with 2 tanks per side. `swapPlayers` is unused and `p1First` is always true.
- `WebSocket Upgrader` has no `CheckOrigin` policy.

### Gameplay and content
- There is a single 14-hex map and no map variety. Sites are the only obstacles.
- Every tank is identical, with no unit variety and no objectives beyond elimination.
- Both players simultaneously planning with no timer works, but there is no spectator mode, rematch, or "ready" feedback.
- The arena shrink gives the game its pacing, but its parameters are not tunable.

### Client / UX
- All UI is drawn on a canvas. This is great for the look but weak for accessibility (no keyboard play, no screen-reader text, a single hidden-input hack for the room code).
- Mobile and touch: pointer events exist and the canvas has `touch-action: none`, but there are no pinch gestures, only wheel zoom.
- There is no reconnect or error UI. `WS_URL = "ws"` is relative and breaks behind a path prefix or on `https` without `wss`.
- `sprites.ts` is 3.5k lines of generated data. It should be a binary asset or at least clearly marked as generated.
- Audio has a failure message, but there are no volume or mute controls (to be confirmed in `ui.ts`).

## Plan

Phases are ordered by value over effort. Each phase ships independently.

### Phase 0: Hygiene (about half a day)
1. Remove the `tank-ops` binary from git (`git rm --cached`). Add it to `.gitignore`.
2. Add a `Makefile` with `build` (`tsc` and `go build`), `run`, `test`, and `fmt`.
3. Decide how to serve JS. Either commit `web/dist/*.js`, or have CI build and publish it. Remove `*.js` from `.gitignore` only if committing.
4. Put pprof behind an env flag or a separate localhost-only port.
5. Add a `PORT` / `ADDR` env var. Check the `ListenAndServe` error. Replace `fmt.Println` with `log/slog`.
6. Expand the README with rules, controls, an architecture sketch, and run/deploy steps.

**Done when:** a fresh clone runs with one documented command.

### Phase 1: Tests (about 1 day)
1. Unit tests for `GameState` in Go: `validPath`, firing and collision, visibility transitions, shrink damage, win/draw resolution, and simultaneous-turn ordering (`turnP1` alternation).
2. Make iteration deterministic by sorting tank ids, or switching to slices, so tests are stable.
3. A room/player integration test using a `httptest` server and two websocket clients playing a scripted game.
4. Basic client tests for `vector.ts` and `grid.ts` pathing with a lightweight runner (vitest).
5. GitHub Actions: `go vet`, `go test -race`, `tsc --noEmit`.

**Done when:** CI is green and `-race` is clean.

### Phase 2: Server robustness (about 2 days)
1. Replace `panic` in state handlers with logged errors and a clean disconnect of the offending player.
2. Add `recover` at the top of each room and player goroutine, so one bad room cannot kill the process.
3. Make room-to-player sends non-blocking (buffered channels, or a per-player outbox with a write goroutine) and drop the player if the buffer fills.
4. Make `Hub.Run` never block on a room: buffer `room.requests` or hand off in a goroutine with a timeout.
5. Websocket hardening: `SetReadLimit`, read deadlines with ping/pong, write deadlines, and `CheckOrigin` for configured origins.
6. Validate input: room code `[A-Z0-9]{1,8}`, max actions per turn equal to the number of tanks, dedupe tank ids, and cap path length before processing.
7. Add a turn timer (for example 60 s, configurable). On timeout, submit empty actions for the idle player.

**Done when:** fuzzing malformed messages cannot panic or hang the server, and an idle player cannot stall a game.

### Phase 3: Reconnect and session (about 2 days)
1. Issue a session token on join. Keep a room alive for a grace period (for example 30 s) after a disconnect.
2. On reconnect, send the player a full state snapshot (their view only: config, current tank states, and the shrink radius).
3. Client: automatic reconnect with backoff, a "reconnecting..." overlay, and a `wss`-aware URL derived from `location`.
4. Add a rematch flow so a finished game offers "play again" in the same room.

**Done when:** a page refresh mid-game rejoins the same game.

### Phase 4: Gameplay depth (about 1 to 2 weeks, optional and iterative)
1. Move maps into data (JSON or Go embed). Add 3 to 5 maps, and pick one at random or by room option.
2. Randomly swap sides and first mover (`swapPlayers` and `p1First` already exist in the config).
3. Make tank count, `driveRange`, `fireRange`, `visibilityRange`, and shrink parameters part of a map or ruleset.
4. Add one or two new mechanics, each behind a ruleset flag so balance can be tested. Examples: cover that blocks line of sight, a repair or ammo pickup, a heavy tank with more range but less movement.
5. Play-test with a small group and tune.

### Phase 5: Client UX and accessibility (about 1 week)
1. Keyboard controls (arrow or WASD to select and move, space to fire, enter to submit).
2. Mute and volume controls with persistence in `localStorage`.
3. Pinch-to-zoom and a clear tap-to-select flow on mobile. Test on iOS Safari and Android Chrome.
4. A short interactive tutorial or first-turn hints.
5. Add an ARIA live region mirroring key events ("Tank 2 destroyed", "arena shrinking next turn") for screen readers.
6. Move `sprites.ts` to a real asset pipeline, or mark it generated and exclude it from review.

### Phase 6: Deployment and ops (about 1 day)
1. A multi-stage Dockerfile (node, then go build, then a small runtime image).
2. A `/healthz` endpoint, structured logs, and basic metrics (active rooms and games per hour).
3. A rate limit per IP on websocket connects and room creation.

### Phase 7: AI opponent (about 1 week)
Decisions made: this is a hobby project, so keep the AI simple, server-side and dependency-free.

**Design**
- The bot lives in the Go server as a third kind of "player". It implements the same contract as a human: it receives a `ClientConfig` and its own turn results, and it sends `[]TankAction` each turn. The room treats it like any other player, so the rules cannot diverge.
- Add `type Bot interface { StartGame(ClientConfig); Observe([]TurnResult); PlanTurn() []TankAction }`. The bot only sees what a human would see (fog of war applies). It must not read `GameState` directly, or it would cheat.
- Add `ClientJoinRoom` option `vsBot: true` (and a difficulty field). `Room` skips `WaitingForP2` and starts immediately with a bot in the P2 slot. The main menu gets a "Play vs computer" button.
- The bot's output goes through the same validation as human input (Phase 2), so a buggy bot cannot break the game.

**Levels (build in this order, each is shippable)**
1. **Easy (random-ish):** each tank moves toward the nearest known enemy or the arena center, and fires if an enemy is in a line within `fireRange`. Otherwise it moves randomly.
2. **Medium (heuristic):** score each candidate action per tank (BFS over reachable hexes within `driveRange`). Score terms: in line of fire of an enemy next turn, inside the shrinking zone, distance to the last seen enemy position, cover from sites. Pick the best joint plan, avoiding two tanks colliding.
3. **Hard (search):** keep a belief set of possible enemy positions from the last seen position and `driveRange`. Run a short Monte Carlo or expectimax rollout using the real `GameState` resolver on a cloned state with sampled enemy actions. This needs `GameState.Clone()` and a seedable RNG, which Phase 1 already pushes toward.

**Needed from earlier phases**
- Phase 1 tests and deterministic iteration. The bot needs reproducible games, and bot-vs-bot games make a good regression test.
- A `GameState.Clone()` and a way to resolve a turn without side effects on the room.
- A seedable RNG injected into the bot.

**Testing**
- Bot-vs-bot simulation harness (`go test -run TestBotSelfPlay`) that plays N games per level pair and reports win rates. Medium should beat Easy clearly, and Hard should beat Medium. Also assert that no bot ever submits an invalid action.
- The game always terminates, which the shrinking arena guarantees. Assert a turn cap anyway.

**Done when:** a player can start a game against the computer from the menu at three difficulty levels, and each level beats the one below in self-play.

### Phase 8: Open-source readiness (about 1 day, do early)
1. **License:** choose one before the first public push. MIT or Apache-2.0 are the usual choices for a hobby game. Add a `LICENSE` file and the SPDX identifier to the README. This is your decision, because it is legally binding on contributors.
2. **Asset licensing:** `sprites.png`, `sprites.ts` and `sounds.mp3` need a known origin and a license that allows redistribution. If you made them, say so and license them (for example CC-BY or CC0). If not, record the source or replace them. Do this before publishing.
3. **Community files:** `CONTRIBUTING.md` (how to build, test and submit a PR), `CODE_OF_CONDUCT.md`, issue and PR templates, and a `SECURITY.md` for reporting problems.
4. **Secrets check:** scan the history for tokens or personal data before making the repo public (for example `gitleaks`). Also check that the commit author email is one you want public.
5. **Repo hygiene:** `.gitignore` covers binaries and OS files (done), CI is on (Phase 1), a good README with a screenshot and a "play locally" section, and a handful of `good first issue` labels drawn from this plan.
6. **Versioning:** tag `v0.1.0` once Phases 0 to 3 land. Keep a short `CHANGELOG.md`.
7. **Protocol docs:** the websocket message format is currently implicit in `messages.go` and `ws-driver.ts`. Write it down in `docs/protocol.md`, so others can build clients or bots. This is the main contribution surface for an open game.
8. **Optional:** run a public demo later (Phase 6). It is not required to open-source.

## Suggested order
1. Phase 0 (hygiene, partly done) and Phase 8 items 1 to 4 (license, asset check, secrets scan) before anything is made public.
2. Phase 1 (tests), then Phase 2 (robustness).
3. Phase 7 (AI opponent), since it benefits from Phase 1 and gives a single-player game that needs no second person to try.
4. Phase 3 (reconnect), then Phases 4 and 5 for gameplay and UX.
5. Phase 6 only if you decide to host a demo.

Phases 3 and 6 matter less for a hobby project. Reconnect is useful but not essential once there is an AI opponent, and Phase 6 is optional.

## Status
- Done: `tank-ops` binary removed from git and added to `.gitignore`. `.DS_Store` untracked as well. Not yet committed.
- The binary still exists in earlier git history (about 8 MB). History was not rewritten. See the open questions.

## Open questions
- Which license for the code, and who made the sprites and sounds (Phase 8)?
- History: leave the old binary in history (simplest, costs 8 MB on clone), or rewrite history before the first public push? A rewrite is cheap now, since the repo has two commits and no other contributors, and it gets harder after people fork.
- AI difficulty: are three levels enough, or do you want only one good bot first?
