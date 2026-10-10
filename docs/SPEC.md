# SPEC — Family Party v0.1

## 1. Product goal

A family can start a mixed game night without installing an application or
creating accounts. Each participant uses their phone; an optional TV browser
shows a read-only view. The host also plays.

Selected scope: **1 Same Brain; 2 Who's Lying?; 4 Draw & Guess; 5 Most Likely To;
9 Memory Clash**. No other games are included in this release.

### Acceptance path

Create room → invite family → choose playlist → ready up → play all five game
types → accumulate scores → reveal final ranking → return to lobby for a rematch.
A player refreshing their browser must keep the same seat and score while their
server room and tab session remain available.

## 2. Room and lobby

- A cryptographically random six-character, unambiguous room code identifies a room.
- Each player chooses a unique normalized nickname of 1–20 characters, including
  at least one letter/number; control and format characters are rejected.
- Maximum 15 players. Minimum is the largest minimum among selected games:
  Same Brain 3, Who's Lying 4, Draw & Guess 2, Most Likely 3, Memory Clash 2.
- The creator is host and ready. Connected guests must mark themselves ready.
- The host selects one or more distinct games, 1–5 rounds per game, and an answer
  timer from 20, 30, 40, 60, or 90 seconds. Defaults: all games, two rounds, 40 seconds.
- The queue cycles through selected games in their displayed order, then repeats
  for the configured number of rounds. Drawing roles rotate across drawing rounds;
  selecting fewer drawing rounds than players does not give everyone a drawing turn.
- New players join only in the lobby. Existing player tokens may reconnect in
  progress; participants for a game night are frozen at the start.
- A disconnected lobby player does not block the connected roster from starting.
  That player, on reconnecting after the start, observes until the next game night.
- The host may remove another player in the lobby; removal revokes that token.
- Up to eight viewer sessions per room. A viewer never votes, submits, scores,
  sees private roles, or counts toward the minimum player count.

## 3. Shared round lifecycle

Every game has a three-second instruction/intro phase. The server owns time.
Submission phases close at the server deadline, or early after all participants
submit. Disconnected participants remain in the round roster; otherwise play
waits only until the existing timer expires. A host may pause to accommodate them.

The host can pause/resume any timed phase. While paused, submissions and drawing
are blocked. Skipping finalizes the round with zero points for everyone. The
reveal is untimed; the host explicitly advances to the next round or final results.

| Game | Phase sequence | Timing after intro |
|---|---|---|
| Same Brain | answer → reveal | Configured answer timer |
| Who's Lying? | answer → discuss → vote → reveal | Answer timer, 12-second discussion, answer timer |
| Draw & Guess | draw → reveal | Twice the configured answer timer |
| Most Likely To | vote → reveal | Configured answer timer |
| Memory Clash | memorize → recall → reveal | Sequence length + 2 seconds, then answer timer |

## 4. Game rules

### Same Brain

Every player receives the same original family-friendly prompt and submits one
answer of 1–48 characters. The answer must contain a letter/number. Normalize
case, punctuation, and repeated spaces. Do not use fuzzy matching or remove
accents. Answers cannot be edited after submission.

Count normalized groups. Each player in a largest group earns 100, provided the
group contains at least two answers. All tied largest groups score. All unique
answers means zero points. Example: `banana`, `BANANA!`, `apple` scores 100, 100, 0.
Other players' answers remain private until reveal.

### Who's Lying?

Choose one bluffer from a shuffled roster; move through that roster across this
game's appearances. Detectives see the exact question. The bluffer sees only a
topic and instructions to invent a believable answer. The TV cannot see the
question during the intro/answer phases either.

Everyone submits an answer; then the real question and named answers are shown
for discussion. Voting opens after 12 seconds. Each player votes once for another
participant; self-votes are prohibited.

The bluffer is caught only with **more than half of the entire round roster's
votes**, not merely a plurality or half of submitted ballots. Every detective
who correctly identifies the bluffer earns 100 independently of the group verdict.
The bluffer earns 100 if not caught and they submitted an answer. An absent
bluffer earns no survival points. There is no final word-guess mechanic.

### Draw & Guess

One connected player is selected as drawer, rotating across drawing rounds. Only
that phone receives the word. Other participants and the TV see its word-length
hint, canvas, and recent guesses. Drawing words are chosen without repetition
until the word bank is exhausted.

Canvas input supports touch, mouse, and keyboard. Coordinates are normalized to
0–1, so drawing is consistent across display sizes. Provide a fixed safe palette,
three UI brush sizes, an eraser color, and a clear command. No text drawing tools.

Non-drawers submit guesses of at most 48 characters. Compare the same deterministic
normalization as Same Brain. Do not broadcast the text of a correct guess; show
`guessed it!` instead. A successful guess locks that participant's guessing input.
The round ends when everyone except the drawer has guessed, or time expires.

A correct guesser earns 100. The drawer earns 100 if at least one person guessed
correctly. Do not weight speed or number of guessers. Apply every award only at
finalization; skipping must still give zero points.

### Most Likely To

Display a positive, family-friendly prompt. Each participant nominates one
participant, including optionally themselves. Hide ballots until reveal.

Count the votes received by each nominee. A voter receives 100 when their chosen
nominee is tied for the most votes and received at least two votes. All tied top
nominees qualify. All unique votes means zero points. Being nominated itself
awards no extra points.

### Memory Clash

Generate an ordered sequence from nine labeled symbols. Start at four symbols;
add one each appearance, capped at eight. Repetition is allowed, but adjacent
identical symbols are avoided. During memorization show symbol order numbers.

During recall, remove the target sequence from all newly transmitted views,
including the TV. Show a fixed 3×3 labeled symbol grid and empty sequence slots.
Players fill every slot, may undo before submission, and submit once.

Compare symbol identity **at each position**, not just set membership. Award
`floor(correct_positions × 100 / sequence_length)`. An absent answer scores zero.
Reveal the correct sequence with everyone’s points afterward.

## 5. Results, reconnect and handoff

- All games share the same cumulative score field and round-result format.
- Score finalization is idempotent. An already-finalized round cannot score again.
- Final ranking includes the started roster; equal top scores share the crown.
- Rematch returns to the lobby, clears scores, keeps the playlist and names,
  and resets readiness except for the host.
- Session credentials are stored per browser tab, not attached to invitation URLs.
- An event-stream reconnect sends a full personalized snapshot, including the
  caller's own locked answer and the canonical canvas.
- After 15 seconds of host disconnection, authority transfers to the first
  connected player. An old host does not reclaim authority on reconnect.
- A new lobby host is marked ready so the Start button cannot become stuck.
- A newer connection for one token replaces the older one. The old tab receives
  a stop-reconnecting event, preventing two tabs from fighting over a seat.

## 6. Safety and bounds

Use server-side role, phase, roster, round-ID, timer, and payload validation.
Never serialize internal room/player objects. Explicit view whitelists omit
credentials, secret roles, private ballots, hidden questions, and target sequences.

Maximum 200 rooms, 15 players, eight viewer sessions, 12 KiB JSON request bodies,
24 points per stroke batch, 600 retained drawing chunks, 6,000 drawing IDs per
round, 40 retained guess messages, and bounded 32-event output queues. Brush
widths accepted by the server are 1–24. Guess attempts have an 800 ms minimum gap.
IP-level entry/connect limits and per-token action limits apply.

The code is not a strong invitation/access-control system. Anyone with a code
can join the lobby or use a viewer session. Player-written text is escaped but
not semantically moderated. No guarantee against player screenshots, collusion,
or inspection of information already delivered to their own browser.

## 7. Explicit non-goals

Persistence, authentication/accounts, permanent leaderboards, multi-server rooms,
translations, custom content packs, game bots, remote voice chat, PWA installation,
analytics, payments, teams, and a native Godot client are outside this release.
