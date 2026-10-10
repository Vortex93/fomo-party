# EXECUTION — Delivered build and verification

## Delivery status

Implemented a playable first release with all five selected games: Same Brain,
Who's Lying?, Draw & Guess, Most Likely To, and Memory Clash. Shared features
include QR/code joining, lobby configuration/readiness, a mixed playlist, live
scores, read-only TV sessions, private player views, reconnect, host handoff,
pause, skip, results, rematch, and duplicate-tab protection.

Source, run instructions, specification, implementation plan, architecture,
deployment notes, tests, and a new-repository unified patch are provided. The app
has **not** been published to a public website or hosted service.

## Verified in this environment

Build date: 2026-10-09.

| Check | Result |
|---|---|
| Go rule/server test functions | 36 top-level tests passed, plus configuration-validation subtests |
| Go race detector | Passed with `go test -race -cover ./...` |
| Go static checks | `go vet ./...` passed |
| Game package coverage | 86.7% of statements |
| Server package coverage | 69.9% of statements |
| JavaScript helper/QR tests | 7 passed with Node's built-in test runner |
| QR reference comparison | Version-five matrix exactly matches Python `qrcode`; zero different modules |
| Full browser game night | Four independent players plus one read-only TV completed all five games |
| Reconnect | Refresh/reconnect kept the same player ID, seat count, and score |
| Duplicate tabs | Older tab stopped reconnecting; newer tab retained the player connection |
| Secret-role handling | Exactly one bluffer; bluffer and TV did not receive the hidden question |
| Drawing | Real pointer input synchronized to TV; private word, pause/resume, and correct guesses verified |
| Memory | Target removed during recall; ordered tap submissions awarded the expected points |
| End-of-night flow | Final ranking shown; rematch returned to lobby and cleared scores |
| Responsive layout | No horizontal overflow at 320, 390, 768, and 1280 CSS px in the checked room screen |
| Home screen | No horizontal overflow at 320 and 390 px; desktop/mobile screenshots inspected |
| Browser exceptions | Zero uncaught page exceptions during the full UI flow |
| Native Linux executable | Built, started, served health and embedded HTML/JS/CSS, and shut down cleanly |
| Windows x64 executable | Cross-compiled successfully; not executed on Windows here |

Raw test output is saved in `TEST-RESULTS.txt`. The full browser flow also checks
modal open/Escape-close behavior. Server tests independently cover origin checks,
body limits, authentication, revoked tokens, cross-room token separation, slow
consumers, room expiry, SSE responses, host authorization, and concurrent hub use.
Rule tests cover strict majority vs tie, normalization, partial memory credit,
canvas validation/deduplication/epochs, deadlines, pause, stale-round rejection,
scoring exactly once, and zero points for skipped drawing rounds.

Tools actually available: Go 1.23.2 on Linux/amd64; Node 22.16.0; Chromium
144.0.7559.96. These are verification tool versions, not a claim that they are the
newest supported or audited production versions.

## Important verification limitation

The container's managed Chromium policy blocks direct URL navigation. No policy
was changed. Screens were rendered locally by `tests/browser_smoke.py --bridge`.
That harness combines the native modules for local injection, shims the test URL
and per-tab storage, and bridges browser fetch calls to the actual loopback Go
server. The actual client SSE parser/reconnect code, the actual server HTTP/SSE
handlers, and UI gameplay logic run during the test.

This provides UI/protocol integration evidence, **not** proof that a particular
physical phone, Wi-Fi network, browser origin, clipboard permission, or deployment
proxy is configured correctly. Real-device Safari/Android testing, external HTTPS
hosting, TLS certificates, long-duration load/soak testing, Docker builds, and a
live GitHub Actions run were not performed. The Windows binary is not code-signed.

## Architectural change from the initial direction

SvelteKit and WebSockets were initially considered. Package registry resolution
was unavailable. To ship runnable code rather than an unverified dependency
scaffold, this build uses native browser JavaScript/HTML Canvas and Go's standard
library, with server-sent events plus JSON POST. There are no runtime application
packages to fetch. This change does not remove any of the five selected games.

## Current scope limits

The app is an English-only, single-process, in-memory first release. Rooms and
scores disappear on restart. It has no persistent accounts, permanent leaderboard,
custom prompt editor, translations, teams, PWA install flow, or multi-server room
storage. Player text is escaped but not automatically moderated. Anyone knowing
a room code can watch or join an open lobby. Already-disclosed information cannot
be made screenshot-proof or hidden from its recipient's browser tools.

The server serializes rooms through one hub mutex; its small smoke test is not a
capacity benchmark. Proxy/client-IP treatment and public abuse controls require
review before wider multi-tenant deployment.
