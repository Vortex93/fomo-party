# Running and deploying

## Local family night

Use a computer on the same private Wi-Fi as the players. Run:

```sh
go run ./cmd/server
```

The server binds to `0.0.0.0:8080` and prints candidate local IPv4 addresses.
Use the address for the actual Wi-Fi/Ethernet interface, not a VPN/container adapter.
Open that URL on the host and phones. If opening localhost on the computer,
restart with `-public-url http://<your-wifi-ip>:8080` so the QR is usable by phones.

On Windows, allow incoming connections on **Private networks** in the firewall
prompt. Do not expose the local HTTP port directly to the public Internet.
A guest Wi-Fi or corporate network may prevent phones from reaching the host.

Binding to `-addr 127.0.0.1:8080` intentionally allows only the local computer
(or a reverse proxy running on it) to connect.

## Standalone executable

`go build` embeds all frontend files. The binary requires no Node process or web
asset folder next to it. Source changes, including CSS, require another Go build.
Native Go builds and Linux execution were verified; see EXECUTION for other targets.

## Container

A Dockerfile and Compose file are included. Their build was not executed in the
restricted build environment. The Go image tag is intentionally configurable;
for an audited release, resolve a maintained Go builder image and pin its digest.

```sh
# PUBLIC_URL is optional when users already visit the correct public/LAN origin.
PUBLIC_URL=https://games.example.com docker compose up --build
```

The runtime image is `scratch` with a statically linked server running as a
non-root UID. Published port 8080 must still be protected appropriately by the
host firewall or reverse proxy.

## Public HTTPS deployment

Serve the app at the **origin root**, not a URL subpath. Put HTTPS in front of the
Go server; invitations should use that HTTPS origin. Example Caddy configuration:

```caddyfile
games.example.com {
    reverse_proxy 127.0.0.1:8080 {
        flush_interval -1
    }
}
```

Run the app on localhost with `-public-url https://games.example.com`. This proxy
example is included as deployment guidance, not as an externally deployed or
certificate-tested service.

Important proxy behavior: preserve `Host`, stream `/api/events` without buffering,
keep streaming requests alive across heartbeat intervals, and do not cache `/api/`
responses. The app emits `X-Accel-Buffering: no` and `Cache-Control: no-store`.
There are no cookie credentials and no cross-origin API mode.

The application's IP limiter uses the direct TCP peer. It deliberately ignores
untrusted `X-Forwarded-For`. Behind a reverse proxy, requests share that proxy's
bucket; apply appropriate additional client-IP rate limiting at a trusted edge.
Do not claim distributed/production capacity based on the small smoke test.

## Operating model

A process supports up to 200 in-memory rooms. Each room allows 15 player seats and
eight viewer sessions. Empty/inactive rooms expire after 30 minutes; every room
has an absolute 24-hour lifetime. Closing a tab disconnects it; using Leave
explicitly also releases viewer credentials or removes a lobby seat.

A server restart clears rooms and scores. The client will explain that a session
has ended. Multiple replicas do not share rooms: do not round-robin requests
between independent processes. There is no persistence or distributed broker.

`GET /api/health` reports process health. It does not certify Internet reachability,
TLS, capacity, game correctness, or readiness of another machine's firewall.
SIGINT/SIGTERM closes room streams and performs a graceful HTTP shutdown.

## Security boundaries

Room codes are informal access gates for family play, not authenticated invitations.
Someone who knows a code can join its lobby or watch. Player credentials are
bearer tokens in request headers; serve HTTPS when outside a trusted local setup.
Text is escaped and request inputs are validated, but player content is not
moderated for meaning. Audit the deployment and abuse model before making a
public multi-tenant service available.


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
