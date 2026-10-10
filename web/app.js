import { connect, request } from './lib/api.js';
import { DrawingBoard } from './lib/canvas.js';
import { renderQR } from './lib/qr.js';
import { GAMES, SYMBOLS, BRUSHES, escapeHTML as e, initials, gameInfo, secondsLeft, normalizeRoomCode } from './lib/ui.js';

const main = document.querySelector('#main');
const notice = document.querySelector('#notice');
const connection = document.querySelector('#connection');
const help = document.querySelector('#help-dialog');
const ui = { mode: 'create', name: '', code: '', answer: '', memory: [], phaseKey: '', busy: false, brush: 0, width: 7 };
let info = null, session = null, state = null, stopStream = null;
let status = 'idle', clockOffset = 0, noticeTimeout = null, board = null, contentKey = '', shareKey = '';

function notify(message, error = false) {
    clearTimeout(noticeTimeout);
    notice.hidden = false;
    notice.className = `notice ${error ? 'error' : ''}`;
    notice.replaceChildren(document.createTextNode(message));
    const close = document.createElement('button');
    close.type = 'button'; close.textContent = '✕'; close.className = 'notice-close'; close.setAttribute('aria-label', 'Dismiss message');
    close.addEventListener('click', () => { notice.hidden = true; }); notice.append(close);
    noticeTimeout = setTimeout(() => { notice.hidden = true; }, error ? 12000 : 5000);
}
function savedSession() { try { return JSON.parse(sessionStorage.getItem('familyparty.session') || 'null'); } catch { return null; } }
function saveSession(value) { try { if (value) sessionStorage.setItem('familyparty.session', JSON.stringify(value)); else sessionStorage.removeItem('familyparty.session'); } catch { /* Play still works when storage is blocked, but reload cannot resume. */ } }
function player(id) { return state?.players.find((p) => p.id === id); }
function host() { return state && !state.isViewer && state.hostId === state.youId; }
function blocked() { return status !== 'connected' || ui.busy || state?.round?.paused; }
function disabled(value) { return value ? 'disabled' : ''; }
function preserveFocus(render) {
    const active = document.activeElement;
    const focusId = active?.id;
    const start = active?.selectionStart, end = active?.selectionEnd;
    render();
    if (focusId) {
        const next = document.getElementById(focusId);
        if (next && !next.disabled && next !== document.activeElement) {
            next.focus({ preventScroll: true });
            if (typeof start === 'number' && next.setSelectionRange) { try { next.setSelectionRange(start, end); } catch {} }
        }
    }
}
function setStatus(next, message) {
    const changed = status !== next;
    status = next;
    connection.className = `connection ${next}`;
    const labels = { idle: 'Made for together', connecting: 'Connecting…', connected: 'Live, together', reconnecting: 'Reconnecting…', expired: 'Session ended', replaced: 'Open in another tab' };
    connection.innerHTML = `<span class="status-dot"></span>${labels[next] || next}`;
    if (next === 'replaced') notify(message || 'This player is active in another tab.', true);
    if (next === 'expired') { saveSession(null); notify(message || 'This room is no longer available.', true); }
    if (changed && state) renderRoom();
    if (next === 'expired' && !state) main.innerHTML = `<section class="empty-state"><span class="big-icon">↶</span><h1>This room has closed.</h1><p>Create a new room to keep playing.</p><button class="button button-primary" data-action="home">Back home</button></section>`;
}

function renderHome() {
    board?.destroy(); board = null; contentKey = ''; shareKey = '';
    const saved = savedSession();
    main.innerHTML = `
        <section class="home-hero">
            <div class="hero-copy"><span class="eyebrow"><span class="tiny-spark">✳</span> THE BEST KIND OF SCREEN TIME</span><h1>Small screens.<br><span class="highlight">Big laughs.</span></h1><p class="hero-description">Five little games. One very good family night.<br>Bring your people. We'll bring the fun.</p>
                <div class="together-note"><div class="avatar-stack" aria-hidden="true"><span>☺</span><span>☻</span><span>☺</span><span>☻</span></div><p><strong>2–15 players</strong><br>No downloads. No accounts. Just you.</p></div>
                <div class="hero-doodle" aria-hidden="true"><span class="doodle-star">✳</span><span class="doodle-spark">✦</span><span class="doodle-note">a little friendly competition ↗</span></div>
            </div>
            <section class="entry-card" aria-labelledby="entry-title"><div class="entry-top"><span class="eyebrow">MAKE TONIGHT A GAME NIGHT</span><span class="little-ticket">PLAY!</span></div>
                <h2 id="entry-title">Pull up a phone.</h2><div class="tabs" role="group" aria-label="Room action"><button type="button" data-action="mode" data-value="create" aria-pressed="${ui.mode === 'create'}" class="tab ${ui.mode === 'create' ? 'selected' : ''}">Create a room</button><button type="button" data-action="mode" data-value="join" aria-pressed="${ui.mode === 'join'}" class="tab ${ui.mode === 'join' ? 'selected' : ''}">Join a room</button></div>
                <form id="entry-form"><label for="nickname">What should we call you?</label><input id="nickname" name="nickname" maxlength="20" placeholder="Your nickname" autocomplete="nickname" value="${e(ui.name)}" required>
                    ${ui.mode === 'join' ? `<label for="room-code">Room code</label><input id="room-code" name="code" class="code-input" minlength="6" maxlength="6" placeholder="ABC234" autocomplete="off" autocapitalize="characters" spellcheck="false" value="${e(ui.code)}" required>` : ''}
                    <button type="submit" class="button button-primary full entry-submit" ${disabled(ui.busy)}>${ui.busy ? 'Getting ready…' : ui.mode === 'create' ? 'Make some memories' : 'Join the fun'} <span aria-hidden="true">→</span></button>
                </form><p class="entry-foot">${ui.mode === 'create' ? 'You host. Everyone else joins with a code.' : 'Ask the host for the six-character room code.'}</p>
                ${ui.mode === 'join' ? '<button type="button" class="button button-quiet full" data-action="watch-home">Open TV / watch-only view</button>' : ''}
                ${saved?.code ? `<button type="button" class="button button-outline full resume" data-action="resume">Return to room ${e(saved.code)} ↗</button>` : ''}
            </section>
        </section>
        <section class="games-section"><div class="section-heading"><div><span class="eyebrow">THE LINEUP</span><h2>A little something for everyone.</h2></div><span class="pill">5 ways to play</span></div>
            <div class="game-cards">${GAMES.map((g, i) => `<article class="game-card ${g.theme}"><div class="card-meta"><span>0${i+1}</span><span>${g.min}+ PLAYERS</span></div><div class="game-icon" aria-hidden="true">${g.icon}</div><h3>${g.name}</h3><p>${g.description}</p><span class="card-verb">${g.verb}</span></article>`).join('')}</div>
        </section><section class="how-strip"><p><span>01</span> Make a room.</p><b aria-hidden="true">→</b><p><span>02</span> Get everyone in.</p><b aria-hidden="true">→</b><p><span>03</span> Let the laughs happen.</p></section>`;
}

function joinURL(watch = false) {
    const url = new URL(info?.publicURL || location.origin);
    url.searchParams.set(watch ? 'watch' : 'room', state.code);
    return url.toString();
}

function avatar(p, index = 0) { return `<span class="avatar avatar-${index % 5}" aria-hidden="true">${e(initials(p.name))}</span>`; }
function renderRoster() {
    return `<div class="aside-heading"><h3>${state.status === 'lobby' ? 'The good company' : 'Scoreboard'}</h3><span class="pill">${state.players.filter((p) => p.connected).length} / 15</span></div><ol class="roster">${[...state.players].sort((a, b) => state.status === 'lobby' ? 0 : b.score-a.score).map((p, i) => `<li>${avatar(p, state.players.findIndex((x) => x.id === p.id))}<div class="player-label"><strong>${e(p.name)}${p.id === state.youId ? ' <span class="muted">(you)</span>' : ''}</strong><span>${p.id === state.hostId ? 'Host · ' : ''}${!p.connected ? 'Reconnecting' : state.status === 'lobby' ? p.ready ? 'Ready to play' : 'Getting ready' : 'Playing'}</span></div><span class="${state.status === 'lobby' ? 'ready-indicator' : 'player-score'}">${state.status === 'lobby' ? p.ready && p.connected ? '✓' : '·' : p.score}</span>${host() && state.status === 'lobby' && p.id !== state.youId ? `<button class="icon-button remove-player" data-action="remove" data-value="${e(p.id)}" aria-label="Remove ${e(p.name)}">×</button>` : ''}</li>`).join('')}</ol>`;
}

function renderRoom() {
    if (!state) return;
    preserveFocus(() => {
        if (!document.querySelector('#room-layout')) {
            main.innerHTML = `<section id="room-heading" class="room-heading"></section><div id="room-layout" class="room-layout"><div id="room-content" class="room-content"></div><aside class="room-aside"><section id="share-panel" class="share-panel"></section><section id="room-players" class="players-panel"></section><button class="button button-quiet full" data-action="leave">Leave room</button></aside></div>`;
        }
        document.querySelector('#room-heading').innerHTML = `<div><span class="eyebrow">${state.isViewer ? 'THE BIG SCREEN' : 'YOUR PEOPLE. YOUR PARTY.'}</span><h1>${state.status === 'lobby' ? 'Everybody in?' : state.status === 'finished' ? 'That was a good night.' : e(gameInfo(state.round.game).name)}</h1>${status !== 'connected' ? `<p class="room-network">${status === 'replaced' ? 'This player is active in another tab.' : status === 'expired' ? 'This session ended. Leave the room to start again.' : 'Reconnecting… Your seat and score are saved.'}</p>` : ''}</div><div class="room-heading-right"><span class="room-code-badge">ROOM <b>${e(state.code)}</b></span>${state.status === 'playing' ? `<span class="pill">Round ${state.round.number} / ${state.totalRounds}</span>` : ''}</div>`;
        document.querySelector('#room-players').innerHTML = renderRoster();
        const share = joinURL();
        if (shareKey !== share) {
            shareKey = share;
            const localOnly = ['localhost', '127.0.0.1', '[::1]'].includes(new URL(share).hostname);
            document.querySelector('#share-panel').innerHTML = `<div class="aside-heading"><h3>Invite your people</h3><span aria-hidden="true">↗</span></div><div class="share-body"><div id="qr-code" class="qr-code"></div><div><span class="eyebrow">ROOM CODE</span><strong class="share-code">${e(state.code)}</strong><p class="muted">Scan. Pick a name.<br>You're in.</p></div></div>${localOnly ? '<p class="local-warning">Phones cannot use localhost. Open this site using your computer’s Wi-Fi IP, or restart with --public-url set to that address.</p>' : ''}<button class="button button-outline full" data-action="copy">Copy invite link</button><a class="button button-quiet full" href="${e(joinURL(true))}" target="_blank" rel="noopener">Open TV view <span aria-hidden="true">↗</span></a>`;
            try { renderQR(document.querySelector('#qr-code'), share); } catch (error) { document.querySelector('#qr-code').textContent = 'Use the room code'; }
        }
        const key = `${state.status}:${state.round?.id || ''}:${state.round?.phase || ''}:${state.youId}`;
        const content = document.querySelector('#room-content');
        // Keep the live canvas mounted while answers and scores update elsewhere.
        const keepCanvas = key === contentKey && state.round?.phase === 'draw' && board;
        if (!keepCanvas) {
            board?.destroy(); board = null;
            content.innerHTML = state.status === 'lobby' ? lobbyHTML() : state.status === 'finished' ? finishedHTML() : roundHTML();
            contentKey = key;
            const canvas = document.querySelector('#drawing-canvas');
            if (canvas) {
                const roundId = state.round.id;
                board = new DrawingBoard(canvas, {
                    roundId,
                    send: async (action) => {
                        if (status !== 'connected' || state.round?.id !== roundId) throw new Error('Reconnect before drawing again.');
                        await request('/api/action', action, session);
                    },
                    onError: (error) => { if (state.round?.id === roundId && state.round.phase === 'draw') notify(error.message, true); }
                });
                board.brush(BRUSHES[ui.brush][0], ui.width);
            }
        }
        if (state.status === 'playing') {
            const rd = state.round;
            const paused = document.querySelector('#phase-status');
            if (paused) paused.innerHTML = rd.paused ? '<strong>Time out.</strong> The host paused the round.' : status === 'connected' ? '' : '<strong>Reconnecting.</strong> Your answers and score are saved.';
            paused?.classList.toggle('visible', rd.paused || status !== 'connected');
            const hostControls = document.querySelector('#host-controls');
            if (hostControls) hostControls.innerHTML = controlsHTML();
            if (rd.phase === 'draw') {
                document.querySelector('#draw-chat').innerHTML = chatHTML();
                const formArea = document.querySelector('#guess-area');
                if (formArea && formArea.dataset.guessed !== String(rd.you.guessed)) {
                    formArea.innerHTML = guessHTML(); formArea.dataset.guessed = String(rd.you.guessed);
                }
            }
            if (board) {
                board.sync(rd.strokes, rd.canvasEpoch);
                board.setEnabled(rd.phase === 'draw' && rd.you.role === 'drawer' && !blocked());
            }
            document.querySelectorAll('[data-live-input]').forEach((element) => {
                element.disabled = blocked() || element.dataset.locked === 'true';
            });
        }
        updateTimer();
    });
}

function lobbyHTML() {
    const connected = state.players.filter((p) => p.connected);
    const allReady = connected.every((p) => p.ready);
    const enough = connected.length >= state.minPlayers;
    const me = player(state.youId);
    return `<section class="lobby-card"><span class="eyebrow">THE PLAN FOR TONIGHT</span><h2>Mix it up.</h2><p class="muted">${host() ? 'Pick your games. Scores add up across the whole playlist.' : 'The host picks the playlist. Get comfortable and ready up.'}</p>
        <div class="playlist">${GAMES.map((g) => { const selected = state.config.games.includes(g.id); return `<label class="playlist-item ${selected ? 'chosen' : ''}"><span class="playlist-icon ${g.theme}" aria-hidden="true">${g.icon}</span><span><strong>${g.name}</strong><small>${g.min}+ players · ${g.description}</small></span><input type="checkbox" data-config-game="${g.id}" aria-label="Include ${g.name}" ${selected ? 'checked' : ''} ${disabled(!host() || ui.busy || status !== 'connected')}></label>`; }).join('')}</div>
        <div class="settings-row"><div><label for="round-count">Rounds per game</label><select id="round-count" ${disabled(!host() || ui.busy || status !== 'connected')}>${[1, 2, 3, 4, 5].map((n) => `<option value="${n}" ${n === state.config.rounds ? 'selected' : ''}>${n} ${n === 1 ? 'round' : 'rounds'}</option>`).join('')}</select></div><div><label for="timer-setting">Answer timer</label><select id="timer-setting" ${disabled(!host() || ui.busy || status !== 'connected')}>${[20, 30, 40, 60, 90].map((n) => `<option value="${n}" ${n === state.config.seconds ? 'selected' : ''}>${n} seconds</option>`).join('')}</select></div></div>
        <div class="lobby-summary"><span>${state.config.games.length * state.config.rounds} rounds altogether</span><span>${state.minPlayers}–15 players for this playlist</span></div>
        <div class="lobby-action">${state.isViewer ? '<p class="viewer-note">You are watching. Join on a phone to play.</p>' : host() ? `<button class="button button-primary full" data-action="start" ${disabled(!enough || !allReady || blocked())}>Start game night <span aria-hidden="true">→</span></button><p class="muted">${!enough ? `Waiting for ${state.minPlayers-connected.length} more connected player(s).` : !allReady ? 'Waiting for everyone to ready up.' : 'Your whole crew is ready. Let’s do this.'}</p>` : `<button class="button ${me?.ready ? 'button-outline' : 'button-primary'} full" data-action="ready" ${disabled(blocked())}>${me?.ready ? '✓ Ready! Tap to unready' : "I'm ready — let's play"}</button><p class="muted">${me?.ready ? 'Waiting for the host to start.' : 'Tap when you are ready for the first round.'}</p>`}</div></section>`;
}

function controlsHTML() {
    if (!host()) return '<span class="muted">The host controls the pace.</span>';
    if (state.round.phase === 'reveal') return `<button class="button button-primary" data-action="next" ${disabled(blocked())}>${state.round.number === state.totalRounds ? 'Final results' : 'Next round'} →</button>`;
    return `<button class="button button-outline" data-action="pause" ${disabled(status !== 'connected' || ui.busy)}>${state.round.paused ? 'Resume round' : 'Pause'}</button><button class="button button-quiet" data-action="skip" ${disabled(status !== 'connected' || ui.busy)}>Skip this round</button>`;
}

function roundHTML() {
    const rd = state.round, game = gameInfo(rd.game);
    const headings = { intro: 'GET READY', answer: 'LOCK IN YOUR ANSWER', discuss: 'TALK IT OVER', vote: 'MAKE YOUR PICK', memorize: 'TAKE A MENTAL PICTURE', recall: 'YOUR TURN TO REMEMBER', draw: 'MAKE YOUR MARK', reveal: 'THE BIG REVEAL' };
    let body = '';
    if (rd.phase === 'intro') {
        body = `<div class="intro-game"><span class="intro-icon ${game.theme}" aria-hidden="true">${game.icon}</span><h2>${game.name}</h2><p>${game.rule}</p>${rd.you.role === 'bluffer' ? '<div class="private-note"><strong>You are the bluffer.</strong> Only your phone shows this. You will get a topic, but not the question.</div>' : ''}${rd.you.role === 'drawer' ? '<div class="private-note"><strong>You are the artist this round.</strong> Get your drawing finger ready.</div>' : ''}<span class="pill">Starting in <b data-seconds>3</b></span></div>`;
    } else if (rd.phase === 'reveal') {
        body = revealHTML();
    } else if (rd.phase === 'draw') {
        body = drawHTML();
    } else {
        body = `<div class="prompt-area">${rd.game === 'whos-lying' ? `<span class="role-tag ${rd.you.role === 'bluffer' ? 'bluff' : ''}">${state.isViewer ? 'Watch-only screen' : rd.you.role === 'bluffer' ? 'PRIVATE · YOU ARE THE BLUFFER' : 'PRIVATE · YOU HAVE THE QUESTION'}</span>` : ''}<h2>${e(rd.prompt)}</h2></div>`;
        if (rd.phase === 'answer') {
            body += state.isViewer || !rd.you.canPlay ? watchingHTML() : rd.you.answer ? lockedHTML('Your answer is locked in.', rd.you.answer) : `<form id="answer-form" class="answer-form"><label for="answer-input">${rd.you.role === 'bluffer' ? 'Your most believable answer' : 'Your answer'}</label><input id="answer-input" maxlength="48" placeholder="Keep it short…" autocomplete="off" value="${e(ui.answer)}" data-live-input required><button class="button button-primary" type="submit" data-live-input>Lock it in →</button></form><p class="muted center">Answers stay hidden until everyone is done, or time runs out.</p>`;
        } else if (rd.phase === 'discuss') {
            body += `<p class="muted center">Read the answers aloud. Who sounds like they never saw the question?</p>${publicAnswersHTML()}<div class="locked-card"><span aria-hidden="true">☏</span><h3>Talk with each other.</h3><p>Voting opens in <b data-seconds>12</b> seconds.</p></div>`;
        } else if (rd.phase === 'vote') {
            if (rd.game === 'whos-lying') body += publicAnswersHTML();
            body += state.isViewer || !rd.you.canPlay ? watchingHTML() : rd.you.vote ? lockedHTML('Your vote is locked in.', player(rd.you.vote)?.name) : `<p class="muted center">${rd.game === 'whos-lying' ? 'Who is bluffing? Tap one person. You cannot vote for yourself.' : 'Tap your pick. Voting for yourself is allowed. Keep it kind.'}</p><div class="vote-grid">${state.players.filter((p) => rd.participants.includes(p.id) && (rd.game !== 'whos-lying' || p.id !== state.youId)).map((p, i) => `<button type="button" class="vote-card" data-action="vote" data-value="${e(p.id)}" data-live-input>${avatar(p, i)}<strong>${e(p.name)}</strong><span>Choose →</span></button>`).join('')}</div>`;
        } else if (rd.phase === 'memorize') {
            body += `<p class="muted center">Left to right. Top to bottom. Remember the order—not just the symbols.</p>${sequenceHTML(rd.sequence || [])}<p class="memory-warning">These disappear when the timer ends.</p>`;
        } else if (rd.phase === 'recall') {
            if (state.isViewer || !rd.you.canPlay) body += watchingHTML();
            else if (rd.you.answer) body += lockedHTML('Sequence locked in.', 'Now let’s see what everyone remembered.');
            else body += `<p class="muted center">Tap ${rd.sequenceLength} symbols in the order you remember. Repeats are allowed.</p><div class="memory-slots" aria-label="Your remembered sequence">${Array.from({ length: rd.sequenceLength }, (_, i) => `<span class="memory-slot ${ui.memory[i] !== undefined ? 'filled' : ''}">${ui.memory[i] !== undefined ? `<span aria-hidden="true">${SYMBOLS[ui.memory[i]].icon}</span><small>${SYMBOLS[ui.memory[i]].label}</small>` : `<span class="slot-number">${i+1}</span>`}</span>`).join('')}</div><div class="symbol-grid">${SYMBOLS.map((symbol, i) => `<button type="button" data-action="symbol" data-value="${i}" class="symbol-button symbol-${i}" data-live-input data-locked="${ui.memory.length >= rd.sequenceLength}" ${disabled(ui.memory.length >= rd.sequenceLength)}><span aria-hidden="true">${symbol.icon}</span><span>${symbol.label}</span></button>`).join('')}</div><div class="memory-actions"><button class="button button-outline" data-action="undo-memory" data-live-input data-locked="${!ui.memory.length}" ${disabled(!ui.memory.length)}>← Undo</button><button class="button button-primary" data-action="memory" data-live-input data-locked="${ui.memory.length !== rd.sequenceLength}" ${disabled(ui.memory.length !== rd.sequenceLength)}>Lock sequence →</button></div>`;
        }
        if (['answer', 'vote', 'recall'].includes(rd.phase)) body += `<div class="submission-meter"><span>${rd.submitted.length} of ${rd.participants.length} locked in</span><progress value="${rd.submitted.length}" max="${rd.participants.length}" aria-label="Players who submitted"></progress></div>`;
    }
    return `<section class="round-card ${rd.phase === 'draw' ? 'drawing-round' : ''}"><header class="round-top"><span class="eyebrow">${headings[rd.phase] || 'GAME ON'}</span>${rd.phase !== 'reveal' ? '<span class="timer" role="timer" aria-label="Seconds remaining"><span aria-hidden="true">◷</span> <b data-seconds>—</b>s</span>' : '<span class="pill">✓ Round complete</span>'}</header><div id="phase-status" class="phase-status" role="status"></div>${body}<footer id="host-controls" class="host-controls">${controlsHTML()}</footer></section>`;
}

function watchingHTML() { return '<div class="locked-card"><span aria-hidden="true">◉</span><h3>Watch the fun unfold.</h3><p>Players answer privately on their phones.</p></div>'; }
function lockedHTML(title, value) { return `<div class="locked-card"><span class="lock-check" aria-hidden="true">✓</span><h3>${e(title)}</h3><p>${e(value)}</p></div>`; }
function publicAnswersHTML() { return `<div class="public-answers">${state.round.publicAnswers.map((a) => `<article><span>${e(a.name)}</span><strong>“${e(a.text)}”</strong></article>`).join('')}</div>`; }
function sequenceHTML(sequence) { return `<ol class="memory-sequence">${sequence.map((id, index) => `<li class="symbol-${id}"><span class="sequence-index">${index+1}</span><span class="sequence-symbol" aria-hidden="true">${SYMBOLS[id].icon}</span><strong>${SYMBOLS[id].label}</strong></li>`).join('')}</ol>`; }
function chatHTML() { return state.round.chat.length ? `<ul class="guess-list">${state.round.chat.slice(-6).map((g) => `<li class="${g.correct ? 'correct-guess' : ''}"><strong>${e(g.name)}</strong><span>${g.correct ? '✓ ' : ''}${e(g.text)}</span></li>`).join('')}</ul>` : '<p class="muted center">A blank canvas. So many possibilities.</p>'; }
function guessHTML() {
    const rd = state.round;
    if (state.isViewer || !rd.you.canPlay) return '<p class="muted center">Guess along in your head. Keep the answer a secret!</p>';
    if (rd.you.role === 'drawer') return '<p class="muted center">No letters or spoken hints. Let your masterpiece do the talking.</p>';
    if (rd.you.guessed) return '<div class="guess-success" role="status">✓ You got it! Keep the word a secret.</div>';
    return `<form id="guess-form" class="guess-form"><label class="sr-only" for="guess-input">Your guess</label><input id="guess-input" maxlength="48" placeholder="What could it be?" autocomplete="off" value="${e(ui.answer)}" data-live-input required><button type="submit" class="button button-primary" data-live-input>Guess →</button></form>`;
}
function drawHTML() {
    const rd = state.round, mine = rd.you.role === 'drawer';
    return `<div class="draw-title"><span>${mine ? 'YOUR SECRET WORD' : `${e(player(rd.drawerId)?.name || 'The artist')} is drawing`}</span><h2 class="${mine ? '' : 'word-hint'}">${e(mine ? rd.word : rd.wordHint)}</h2></div>
        ${mine ? `<div class="drawing-tools" role="group" aria-label="Drawing tools"><div class="swatches">${BRUSHES.map(([color, label], i) => `<button type="button" data-action="brush" data-value="${i}" class="swatch swatch-${i} ${ui.brush === i ? 'selected' : ''}" aria-label="${label}" aria-pressed="${ui.brush === i}"></button>`).join('')}</div><label class="brush-size">Brush<select id="brush-size" aria-label="Brush width">${[3, 7, 14].map((n) => `<option value="${n}" ${ui.width === n ? 'selected' : ''}>${n === 3 ? 'Fine' : n === 7 ? 'Medium' : 'Bold'}</option>`).join('')}</select></label><button class="button button-quiet" data-action="clear" data-live-input>Clear</button></div>` : ''}
        <div class="canvas-wrap"><canvas id="drawing-canvas" width="960" height="600" tabindex="${mine ? '0' : '-1'}" role="img" aria-label="${mine ? 'Drawing canvas. Arrow keys move the cursor. Hold Shift with arrows to draw. Space makes a dot.' : 'Live drawing by the artist'}">A live drawing canvas. This browser needs HTML Canvas support.</canvas></div>
        ${mine ? '<p class="keyboard-hint">Touch or mouse to draw. Keyboard: arrows move, Shift + arrows draw, Space makes a dot.</p>' : ''}
        <div id="guess-area" data-guessed="${rd.you.guessed}">${guessHTML()}</div><div id="draw-chat" aria-live="polite">${chatHTML()}</div>`;
}
function revealHTML() {
    const rd = state.round, result = rd.result;
    return `<div class="result-heading"><span class="result-spark" aria-hidden="true">${result.skipped ? '↶' : '✦'}</span><h2>${e(result.title)}</h2><p>${e(result.detail)}</p></div>${rd.game === 'whos-lying' && !result.skipped ? `<p class="question-reveal"><strong>The question:</strong> ${e(rd.prompt)}</p>` : ''}${result.sequence ? sequenceHTML(result.sequence) : ''}${rd.game === 'draw-guess' && !result.skipped ? '<div class="canvas-wrap result-canvas"><canvas id="drawing-canvas" width="960" height="600" role="img" aria-label="The completed drawing"></canvas></div>' : ''}<div class="result-rows">${result.rows.map((row, i) => `<article>${avatar(player(row.playerId) || { name: row.name }, i)}<div><strong>${e(row.name)}</strong><p>${e(row.answer)}</p>${row.detail ? `<small>${e(row.detail)}</small>` : ''}</div><b class="points-gain ${row.points ? 'earned' : ''}">+${row.points}</b></article>`).join('')}</div>`;
}
function finishedHTML() {
    const ranked = [...state.players].filter((p) => state.round.participants.includes(p.id)).sort((a, b) => b.score-a.score);
    const best = ranked[0]?.score || 0;
    const winners = ranked.filter((p) => p.score === best).map((p) => p.name);
    return `<section class="final-card"><span class="eyebrow">THE MOMENTS ARE THE REAL PRIZE</span><div class="trophy" aria-hidden="true">♛</div><h2>${e(winners.join(' & '))}</h2><p class="winner-line">${winners.length > 1 ? 'Share the crown.' : 'Takes the crown.'} ${best} points. Plenty of memories.</p><ol class="final-scores">${ranked.map((p, i) => `<li><span class="rank">${ranked.findIndex((other) => other.score === p.score)+1}</span>${avatar(p, i)}<strong>${e(p.name)}</strong><b>${p.score}</b></li>`).join('')}</ol>${host() ? `<button class="button button-primary full" data-action="rematch" ${disabled(status !== 'connected' || ui.busy)}>Another round of together? →</button>` : '<p class="muted">The host can take everyone back to the lobby for another game night.</p>'}</section>`;
}

function updateTimer() {
    if (!state?.round) return;
    const left = secondsLeft(state.round, clockOffset);
    document.querySelectorAll('[data-seconds]').forEach((node) => { node.textContent = String(left); });
    document.querySelectorAll('.timer').forEach((node) => node.classList.toggle('urgent', left <= 5 && state.round.phase !== 'intro'));
}

function begin(credentials) {
    stopStream?.(); board?.destroy(); board = null;
    session = credentials; state = null; shareKey = ''; contentKey = ''; ui.phaseKey = ''; ui.answer = ''; ui.memory = [];
    saveSession(credentials);
    const url = new URL(location.href); url.search = ''; url.searchParams.set(credentials.watch ? 'watch' : 'room', credentials.code); history.replaceState(null, '', url);
    main.innerHTML = '<section class="empty-state"><span class="big-icon">✳</span><h1>Finding your people…</h1><p>Your game room is getting ready.</p><button class="button button-quiet" data-action="home">Back home</button></section>';
    stopStream = connect(credentials, (event) => {
        if (session !== credentials) return;
        if (event.type === 'state') {
            state = event.data; clockOffset = state.serverTime-Date.now();
            const key = `${state.round?.id || ''}:${state.round?.phase || ''}`;
            if (key !== ui.phaseKey) { ui.phaseKey = key; ui.answer = ''; ui.memory = []; }
            renderRoom();
        } else if (event.type === 'stroke' && state?.round?.id === event.data.roundId && state.round.canvasEpoch === event.data.stroke.epoch) {
            const stroke = event.data.stroke;
            // Board.accept also mutates its own canonical list. Keep the state list
            // separate so an echoed local stroke is not accidentally skipped.
            board?.accept(stroke);
            if (!state.round.strokes.some((s) => s.id === stroke.id)) state.round.strokes.push(stroke);
        }
    }, (next, message) => { if (session === credentials) setStatus(next, message); });
}

async function perform(type, extra = {}) {
    if (!session || status !== 'connected') { notify('Reconnect to the room before playing.', true); return; }
    if (ui.busy) return;
    ui.busy = true;
    renderRoom();
    try { await request('/api/action', { type, roundId: state?.round?.id || '', ...extra }, session); }
    catch (error) { notify(error.message, true); }
    finally { ui.busy = false; if (state) renderRoom(); }
}

async function leave() {
    if (state?.status === 'playing' && !confirm('Leave this game? Your seat and score stay in this game night, but you will stop playing.')) return;
    const previous = status === 'replaced' ? null : session;
    stopStream?.(); stopStream = null; session = null; state = null; board?.destroy(); board = null;
    saveSession(null); history.replaceState(null, '', '/'); setStatus('idle'); renderHome();
    if (previous) request('/api/leave', {}, previous).catch(() => {});
}

main.addEventListener('input', (event) => {
    if (event.target.id === 'nickname') ui.name = event.target.value;
    if (event.target.id === 'room-code') { ui.code = normalizeRoomCode(event.target.value); event.target.value = ui.code; }
    if (event.target.id === 'answer-input' || event.target.id === 'guess-input') ui.answer = event.target.value;
});
main.addEventListener('submit', async (event) => {
    event.preventDefault();
    if (event.target.id === 'entry-form') {
        if (ui.busy) return;
        ui.busy = true; renderHome();
        try { const credentials = await request(ui.mode === 'create' ? '/api/rooms' : '/api/join', { name: ui.name, ...(ui.mode === 'join' ? { code: ui.code } : {}) }); begin(credentials); }
        catch (error) { notify(error.message, true); }
        finally { ui.busy = false; if (state) renderRoom(); else if (!session) renderHome(); }
    } else if (event.target.id === 'answer-form') await perform('answer', { value: ui.answer });
    else if (event.target.id === 'guess-form') {
        const answer = ui.answer;
        ui.answer = '';
        const field = document.querySelector('#guess-input'); if (field) field.value = '';
        await perform('guess', { value: answer });
        document.querySelector('#guess-input')?.focus({ preventScroll: true });
    }
});
main.addEventListener('change', (event) => {
    const element = event.target;
    if (element.id === 'brush-size') { ui.width = Number(element.value); board?.brush(BRUSHES[ui.brush][0], ui.width); return; }
    if (!host() || state.status !== 'lobby') return;
    if (element.dataset.configGame || element.id === 'round-count' || element.id === 'timer-setting') {
        const config = { games: [...state.config.games], rounds: state.config.rounds, seconds: state.config.seconds };
        if (element.dataset.configGame) config.games = GAMES.filter((g) => g.id === element.dataset.configGame ? element.checked : config.games.includes(g.id)).map((g) => g.id);
        if (element.id === 'round-count') config.rounds = Number(element.value);
        if (element.id === 'timer-setting') config.seconds = Number(element.value);
        perform('configure', { config });
    }
});
main.addEventListener('click', async (event) => {
    const button = event.target.closest('[data-action]');
    if (!button || button.disabled) return;
    const action = button.dataset.action, value = button.dataset.value;
    if (action === 'mode') { ui.mode = value; renderHome(); }
    else if (action === 'resume') { const saved = savedSession(); if (saved) begin(saved); }
    else if (action === 'home' || action === 'leave') await leave();
    else if (action === 'watch-home') {
        if (ui.code.length !== 6) { notify('Enter a six-character room code first.', true); return; }
        try { begin(await request('/api/watch', { code: ui.code })); } catch (error) { notify(error.message, true); }
    }
    else if (action === 'copy') {
        try { if (!navigator.clipboard) throw new Error('Clipboard unavailable'); await navigator.clipboard.writeText(joinURL()); notify('Invite link copied. Send it to your people.'); }
        catch { prompt('Copy this invite link:', joinURL()); }
    }
    else if (action === 'ready') await perform('ready', { value: String(!player(state.youId)?.ready) });
    else if (['start', 'next', 'pause', 'rematch'].includes(action)) await perform(action);
    else if (action === 'skip') { if (confirm('Skip this round for everyone? No points will be awarded.')) await perform('skip'); }
    else if (action === 'remove') { if (confirm(`Remove ${player(value)?.name || 'this player'} from the lobby?`)) await perform('remove', { playerId: value }); }
    else if (action === 'vote') await perform('vote', { value });
    else if (action === 'symbol') { if (blocked() || ui.memory.length >= state.round.sequenceLength) return; ui.memory.push(Number(value)); renderRoom(); }
    else if (action === 'undo-memory') { ui.memory.pop(); renderRoom(); }
    else if (action === 'memory') await perform('memory', { sequence: ui.memory.slice() });
    else if (action === 'brush') {
        ui.brush = Number(value); board?.brush(BRUSHES[ui.brush][0], ui.width);
        document.querySelectorAll('.swatch').forEach((node) => { const selected = Number(node.dataset.value) === ui.brush; node.classList.toggle('selected', selected); node.setAttribute('aria-pressed', String(selected)); });
    } else if (action === 'clear') { if (confirm('Clear the drawing?')) board?.clear(); }
});
document.querySelector('#help-open').addEventListener('click', () => help.showModal());
setInterval(updateTimer, 250);

async function init() {
    try { info = await request('/api/info'); } catch (error) { notify(error.message, true); }
    const query = new URLSearchParams(location.search);
    const watch = query.get('watch');
    const code = normalizeRoomCode(watch || query.get('room') || '');
    const saved = savedSession();
    if (code && saved?.code === code && Boolean(saved.watch) === Boolean(watch)) { begin(saved); return; }
    if (watch && code.length === 6) {
        try { begin(await request('/api/watch', { code })); return; } catch (error) { notify(error.message, true); }
    }
    if (code) { ui.code = code; ui.mode = 'join'; }
    renderHome();
}
init();
