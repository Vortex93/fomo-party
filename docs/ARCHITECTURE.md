# Architecture and protocol

## Data flow

```text
Phone / TV browser
    │
    ├── POST /api/action ───────► HTTP validation + rate limits
    │                                  │
    │                                  ▼
    │                             synchronized Hub
    │                                  │
    │                                  ▼
    │                            game.Room.Handle()
    │                                  │
    │                                  ▼
    └── GET /api/events ◄──── personalized, sanitized state
          streamed SSE        (or a small drawing delta)
```

The host is not a privileged client implementation: its token identifies a normal
player whose ID happens to match `Room.HostID`. The server checks that relationship
for every administrative action. TV credentials identify a viewer, never a player.

## Directory boundaries

`internal/game` implements deterministic transitions and scoring, with random
content selection at round creation. It does not know about HTTP, serialization
of internal secrets, goroutines, databases, or browser widgets. Its explicit `View`
method returns only allowed public/caller-private fields.

`internal/server` owns room lifetime, authentication, serialization, rate limits,
stream subscriptions, and HTTP. The hub uses one mutex to make state changes
atomic. Each SSE response writes outside that mutex through its own bounded queue.
A slow consumer is closed rather than accumulating unlimited memory or silently
losing drawing messages. This architecture is deliberately for a small single
instance, not a claim of horizontally scalable infrastructure.

`web` is a native-module application embedded with `go:embed`. No bundler, runtime
package fetches, external fonts, or image services are required.

## HTTP API

| Method | Route | Request | Result |
|---|---|---|---|
| GET | `/api/health` | None | Basic process health |
| GET | `/api/info` | None | Catalog, defaults, public share origin |
| POST | `/api/rooms` | `{ "name": "Alex" }` | Host credentials |
| POST | `/api/join` | `{ "name": "Sam", "code": "ABC234" }` | Player credentials |
| POST | `/api/watch` | `{ "code": "ABC234" }` | Read-only viewer credentials |
| GET | `/api/events` | Session headers | SSE stream |
| POST | `/api/action` | Session headers + action | `{ "ok": true }` or explicit error |
| POST | `/api/leave` | Session headers | Marks disconnected/removes lobby seat |

All POST JSON bodies use `Content-Type: application/json`. Authenticated requests
use `Authorization: Bearer <token>` and `X-Room-Code: ABC234`. Tokens are 32 random
bytes, hex encoded. A room code is not a player credential.

### Commands

```json
{ "type": "answer", "roundId": "<current-round-id>", "value": "banana" }
```

Lobby/admin commands: `configure`, `ready`, `remove`, `start`, `pause`, `skip`,
`next`, `rematch`. Play commands: `answer`, `vote`, `guess`, `memory`, `stroke`,
`clear`. Round-bound commands must include the current `roundId`.

`configure` includes the entire `config` object. `ready` has `value: "true"` or
`"false"`. `vote` uses a participant ID in `value`. `remove` uses `playerId`.
`memory` has a numeric `sequence` array. The client never queues commands offline
or automatically retries a POST with an unknown outcome.

### Stream framing

Every event is one UTF-8 JSON object following `data: ` and terminated by two
newlines. Heartbeats are SSE comment frames every ten seconds.

```json
{ "type": "state", "data": { "code": "ABC234", "youId": "...", "players": [], "round": {} } }
```

The exact typed projection is defined in `internal/game/view.go`. A state event
is a full snapshot for that viewer. On reconnect, the browser starts from that
snapshot rather than replaying old commands.

Drawing chunks avoid rebroadcasting the entire canvas for each movement:

```json
{
    "type": "stroke",
    "data": {
        "roundId": "...",
        "stroke": {
            "id": "client-chunk-id",
            "epoch": 0,
            "color": "#20263a",
            "width": 7,
            "points": [{ "x": 0.1, "y": 0.2 }, { "x": 0.3, "y": 0.4 }]
        }
    }
}
```

Stroke IDs are deduplicated. Clearing increments the canvas epoch, so a delayed
pre-clear stroke is rejected. Drawing requests are ordered through a client-side
promise chain. Full state updates carry the canonical history for reconnect.

A `replaced` event tells an older connection to stop reconnecting when a newer
tab takes over that player token. This avoids a reconnect loop between two tabs.

## Secret handling

Never JSON-encode `Room`, `Round`, or `Player` directly. These contain tokens,
target sequences, the secret word, hidden ballots, and the bluffer assignment.

The whitelist projection exposes only the caller's own locked answer/vote and
role. It omits the bluffer's question until discussion, the drawing word until
reveal except for the drawer, and the memory target during recall. Correct drawing
guesses are replaced by a success message before they enter public history.

This protects against accidental wire leaks, not against screenshots, collusion,
or hostile browser inspection of secrets that legitimately reached that player.

## Timing and disconnects

A 250 ms server tick advances due phases. Commands are checked against server time
and the hub ticks before acting. Browser countdowns use the snapshot's server-time
offset only for display. Client clocks cannot extend the actual answer window.

Streams use heartbeats, per-write deadlines, and a client inactivity watchdog.
Reconnect has capped exponential backoff with jitter. Network reconnection does
not create a new player. Reload recovery depends on browser session storage;
when that storage is blocked, in-page play still works but reload cannot recover
the saved token.

The host transfers after 15 seconds disconnected. All in-progress participants
remain in the round roster, so changing connection state cannot change voting
thresholds or remove a participant's score.

## QR code

The local QR encoder uses version 5, error correction L, byte mode, mask 0, with
one Reed–Solomon block: 108 data codewords and 26 parity codewords. Maximum URL
length is 106 UTF-8 bytes. Longer URLs fail visibly to the room-code fallback.
The invite QR contains only the public join URL, never a player token.

## Extension points

Add games by extending the catalog, prompt bank, round initialization, action
validation, finalizer, sanitized view, and UI panel. Preserve the shared result
shape and server-only score authority. Prefer a dedicated game interface only
once those switches become large enough to justify an abstraction.

Translations can start with `web/lib/ui.js` and `internal/game/content.go` but
require a broader UI-string extraction before the interface can be called fully
localized. Durable room recovery and multi-server publishing are future work,
not hidden behind an unimplemented storage abstraction.


## PeerJS internet invites

The local host browser now bridges remote guests to the authoritative Go server.
Guests load the static HTTPS UI from GitHub Pages and use the full invite URL to
find the host via PeerJS Cloud. WebRTC data channels carry requests and each
player's authenticated private SSE events; the bridge cannot create rooms or
proxy arbitrary URLs. Host tab and Go server must remain running. Reconnects
resubscribe for fresh state; uncertain action writes are never automatically
replayed. Large drawing snapshots use bounded ordered application chunks.

Default STUN helps with NAT traversal but is not sufficient for all cellular
networks. `FOMO_ICE_SERVERS` configures optional host-side TURN credentials; see
README for the configuration and privacy/cost caveats. LAN HTTP/SSE remains
available. `--internet-url` selects a different HTTPS static guest page, including
its path and trailing slash. This browser bridge is deliberately simpler than
Anvil's native Go WebRTC/yamux tunnel and does not inherit its password pairing,
workspace permissions, or direct-only policy. Anyone with the invite can request
a player/viewer session, just as someone with the original room code could.
