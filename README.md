# Family Party

**Five phone-friendly multiplayer games. One Go command. No application packages to install.**

Create a room, share the QR code or six-character room code, and play together.
A shared TV is optional: every player can use only their own phone.

## Start

Requires **Go 1.23 or newer**. Node is optional and used only for JavaScript tests.

```sh
go run ./cmd/server
```

Open `http://localhost:8080` on the computer running the server.
The terminal also prints local network addresses. **On phones, open the computer's
Wi-Fi address**, such as `http://192.168.1.5:8080`, not `localhost`.
Keep the server running and allow port 8080 on your private-network firewall.

For QR codes opened from the host's localhost browser, explicitly set the
address other devices should use:

```sh
go run ./cmd/server -public-url http://192.168.1.5:8080
```

Replace that example IP with your computer's actual local IP. Devices on guest
Wi-Fi may be isolated from one another. For play outside your home, deploy the
server behind HTTPS; see [DEPLOYMENT.md](docs/DEPLOYMENT.md).

**All five games require 4–15 connected players.** With two players, choose Draw &
Guess and/or Memory Clash. With three, Same Brain and Most Likely To also work.
The host is a player. A TV viewer is not.

## What's playable

| Game | What happens | Scoring |
|---|---|---|
| Same Brain | Everyone submits a short answer to the same prompt. | 100 for members of a largest matching group of at least two. |
| Who's Lying? | One player gets a topic but not the question. Answer, discuss, then vote. | Correct detectives get 100; an answering bluffer gets 100 if not caught by a strict majority. |
| Draw & Guess | One phone is the sketchpad. The other players guess. | 100 per correct guesser; 100 for the drawer if anyone gets it. |
| Most Likely To | Everyone makes a kind nomination, including optionally themselves. | 100 for voters matching a top pick with at least two votes. |
| Memory Clash | Memorize a symbol sequence, then tap it back in order. | A share of 100 per correct position, rounded down. |

Scores carry through a mixed playlist. The host chooses 1–5 appearances per game,
controls the answer timer, and can pause, skip, advance results, or start a rematch.
Tied top groups count in Same Brain and Most Likely To; final score ties share the crown.

## Included

Room creation and joining; locally generated QR codes; private per-player views;
read-only TV view; touch/mouse/keyboard drawing; live canvas updates; server-owned
timers and scores; reload/reconnect support; automatic host handoff after a
15-second disconnect; duplicate-tab protection; lobby player removal; bounded
rooms, requests, input, drawing history, and connection queues.

No accounts, external fonts, CDN calls, analytics, advertising, database, or
runtime JavaScript dependencies are used.

## Stack and engine

- **Server:** Go standard library, with a deterministic game-rules package.
- **UI:** plain JavaScript ES modules, HTML, and CSS.
- **Drawing:** HTML Canvas, normalized points, small ordered stroke batches.
- **Realtime:** a fetch-based server-sent event stream for updates; authenticated
  HTTP POST commands for player actions. This implementation does **not** use WebSockets.
- **Distribution:** the frontend is embedded inside one Go executable.

SvelteKit was considered initially. Package downloads were unavailable during
this build, so the delivered version uses native browser modules instead of
shipping an uncompiled framework scaffold. No Godot or other game engine is needed.

## Build a standalone app

```sh
go build -trimpath -ldflags="-s -w" -o bin/family-party ./cmd/server
./bin/family-party
```

On Windows:

```powershell
go build -trimpath -ldflags="-s -w" -o bin/family-party.exe ./cmd/server
.\bin\family-party.exe
```

The built executable contains all HTML, CSS, and JavaScript. Rebuild it after
changing frontend files; there is no hot-reload watcher or separate npm build.

## Verify

```sh
go test -race -cover ./...
go vet ./...
node --test tests/client.test.mjs
```

`-race` needs a supported Go race toolchain; omit that flag where unavailable.
The regular app does not need Node, Python, or a browser automation package.

An optional browser test exercises all five games with four independent players
and a TV session against a running local server:

```sh
# Test-only tools, not app dependencies:
python -m pip install playwright httpx
python -m playwright install chromium
python tests/browser_smoke.py --base http://127.0.0.1:8080 --bridge
```

The browser harness uses a documented local HTTP bridge because managed browser
policy blocked direct navigation in the build environment. It tests UI behavior
and the real Go server, but is **not** a claim of testing an actual phone on Wi-Fi.
See [EXECUTION.md](docs/EXECUTION.md) for verified results and limits.

## File tree

```text
family-party/
├── cmd/server/main.go             # CLI, embedded app, graceful shutdown
├── internal/
│   ├── game/
│   │   ├── types.go               # Room, player, round, configuration, commands
│   │   ├── content.go             # Original prompts and drawing words
│   │   ├── engine.go              # Authorization, phases, clock, submissions
│   │   ├── scoring.go             # Single idempotent score-finalization path
│   │   ├── view.go                # Per-player privacy whitelist
│   │   └── game_test.go
│   └── server/
│       ├── hub.go                 # Room synchronization and subscriptions
│       ├── http.go                # JSON API, SSE, limits, security headers
│       └── server_test.go
├── web/
│   ├── embed.go                  # Compiles frontend files into the executable
│   ├── index.html
│   ├── styles.css
│   ├── app.js                    # Mobile UI, lobby, five games, results
│   └── lib/
│       ├── api.js                # Header authentication and reconnecting SSE
│       ├── canvas.js             # Live drawing controller
│       ├── qr.js                 # Local QR encoding; no third-party request
│       └── ui.js                 # Game metadata, escaping, timer helpers
├── tests/
│   ├── client.test.mjs
│   └── browser_smoke.py
├── docs/
│   ├── SPEC.md
│   ├── PLAN.md
│   ├── ARCHITECTURE.md
│   ├── DEPLOYMENT.md
│   └── EXECUTION.md
├── scripts/                      # Convenience launch scripts
├── .github/workflows/test.yml
├── Dockerfile
├── compose.yaml
├── Makefile
├── go.mod
└── package.json                  # Optional convenience/test commands only
```

## Current limits

Rooms and scores are in memory and disappear when the server restarts. A room
expires after 30 minutes without connections/activity, or after 24 hours maximum.
One server process is supported; horizontal scaling and persistence are not
implemented. Public, multi-tenant production hosting needs additional deployment
review and stronger abuse controls.

The initial content and interface are in English. Translations, custom prompt
packs, accounts, persistent leaderboards, teams, and PWA installation are not
implemented. Generated prompts are not used.

The prompt bank is family-friendly, but player-written names and answers are not
automatically moderated. The host can remove a player in the lobby or skip a round.
This is a friendly party game, not a cheat-proof contest: someone could inspect
or record information already shown on their own phone.
