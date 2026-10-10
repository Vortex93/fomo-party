# PLAN — Dependency-ordered implementation

This document records the implementation plan for the delivered first release.
The execution record, including test evidence and scope changes, is separate.

## Phase 1 — Shared contracts and rules

Define the game catalog, config, roster, room status, round state, action envelope,
result rows, and sanitized player views. Keep rules independent of HTTP and
make `now` an explicit input. Implement normalization and original prompt banks.

Exit: room/rule fixtures can start a chosen game without a browser or web server.

## Phase 2 — Shared lifecycle and scoring

Implement readiness, selected-game minimums, a frozen roster, playlist rotation,
server-clock phases, pause, skip, results, rematch, and host-only commands.
Use one idempotent finalization path for every game; never let the client send a
score or authoritative deadline.

Exit: duplicates, stale rounds, timeouts, and pause cannot double-score or bypass
round boundaries. All connected-ready/minimum-player checks are enforceable.

## Phase 3 — Add games on the shared lifecycle

1. Same Brain establishes private answers and grouped scoring.
2. Most Likely To reuses private submission and adds target validation.
3. Memory Clash reuses timed phases and ordered partial-credit scoring.
4. Who's Lying reuses answers/voting and adds per-player secret projections.
5. Draw & Guess adds the bounded canvas stream, private word, guess throttling,
   canonical reconnect history, clear epochs, and deferred drawing awards.

Exit: all five can be completed entirely through engine commands and clock ticks.

## Phase 4 — Multiplayer transport

Add a synchronized room registry and authenticated HTTP actions. Broadcast only
personalized projections. Use bounded subscriber queues, per-write deadlines,
heartbeats, full reconnect snapshots, and tiny incremental drawing events.
Handle connection replacement without disconnecting the newer player session.

Add input size limits, origin checks, request limits, room expiry, static-file
whitelisting, and CSP/security headers. Credentials travel in headers, never QR
codes or query strings.

Exit: a live HTTP client can create, join, subscribe, submit, and reconnect.

## Phase 5 — Phone and TV interface

Build the landing screen, create/join forms, local QR encoder, lobby playlist,
readiness, scoreboard, timers, and common phase shell. Implement five game panels
plus shared result/final screens. Keep the active drawing canvas mounted while
other room state changes. Preserve typed drafts/focus during snapshot updates.

Provide keyboard equivalents for canvas input, labels for memory symbols, large
controls, responsive layout, disabled disconnected controls, and clear errors.

Exit: four independent browser clients and a viewer can finish a game night.

## Phase 6 — Verification and distribution

Run rule/privacy tests, hub/HTTP concurrency tests with the Go race detector,
JavaScript helper tests, independent QR matrix comparison, a full UI game-night
smoke test, responsive layout checks, and native build checks. Package the source,
CLI instructions, specification, architecture, deployment notes, and test record.

Exit: documented commands work without downloading application dependencies;
limitations and untested deployment targets are clearly recorded.

## Architecture decision made during execution

The initial direction was SvelteKit + Go + WebSockets. Dependency downloads were
not available. The implemented substitute is native JS modules + Go standard
library + fetch/SSE and JSON POST, which preserves the multiplayer product scope
and enables a tested dependency-free executable. There is no unused Svelte or
WebSocket scaffold in the source tree.

## Next implementation priorities (not implemented here)

For a public deployment, first add load/soak tests, stronger abuse controls and
trusted-proxy handling. For product growth, add curated localization/content
packs and host preview/customization. Add persistence only after defining room
recovery/expiry semantics. Move to per-room actors and a shared broker only when
multi-instance deployment is actually required.
