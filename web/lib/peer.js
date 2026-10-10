// PeerJS 1.5.5 handles signaling; game data uses encrypted WebRTC channels.
// The local host tab bridges a single room, never arbitrary HTTP or files.
export const DEFAULT_ICE = [{ urls: 'stun:stun.l.google.com:19302' }];
const routePattern = /^[a-f0-9]{32}$/;
const MAX_FRAME = 3_000_000;
const connectionError = () => new Error('Cannot reach the host. Keep its tab open; some mobile networks require TURN. Any pending action was not retried.');

export function inviteURL(base, code, route, watch = false) {
    if (!routePattern.test(route)) throw new Error('Invalid internet invite.');
    const url = new URL(base);
    url.search = ''; url.hash = `p=${route}`;
    url.searchParams.set(watch ? 'watch' : 'room', code);
    return url.toString();
}

export function validateRequest(message, code) {
    if (!message || !Number.isSafeInteger(message.id) || message.id < 1 || JSON.stringify(message).length > 16000) throw new Error('Invalid peer request.');
    const { path, body, session } = message;
    if (body !== undefined && new TextEncoder().encode(JSON.stringify(body)).length > 12 * 1024) throw new Error('The request is too large.');
    if (path === '/api/info' || path === 'unsubscribe') return;
    if (path === '/api/join' || path === '/api/watch') {
        if (!body || body.code !== code) throw new Error('This invite is for another room.');
        return;
    }
    if (!['/api/action', '/api/leave', 'subscribe'].includes(path)) throw new Error('This action is not available through an invite.');
    if (!session || session.code !== code || !/^[a-f0-9]{64}$/.test(session.token)) throw new Error('A session for this room is required.');
}

let frameID = 0;
function send(conn, message, chunk = true) {
    if (!conn.open) throw connectionError();
    const text = JSON.stringify(message);
    if (text.length > MAX_FRAME || (conn.dataChannel?.bufferedAmount || 0) > 2_000_000) { conn.close(); throw connectionError(); }
    if (!chunk && new TextEncoder().encode(text).length > 16000) throw new Error('The request is too large.');
    if (!chunk || text.length <= 8000) { conn.send(message); return; }
    const id = ++frameID, total = Math.ceil(text.length / 8000);
    for (let index = 0; index < total; index++) conn.send({ kind: 'chunk', id, index, total, text: text.slice(index * 8000, (index + 1) * 8000) });
}

export function serveGuest(conn, { code, info, request, connect }) {
    let closed = false, stop = null, inFlight = 0, entryBusy = false, joined = null;
    const safeSend = (message) => { if (!closed) { try { send(conn, message); } catch { cleanup(); conn.close(); } } };
    const cleanup = () => { closed = true; stop?.(); stop = null; };
    conn.on('close', cleanup); conn.on('error', () => { cleanup(); conn.close(); });
    conn.on('data', async (message) => {
        if (closed) return;
        if (inFlight >= 16) { cleanup(); conn.close(); return; }
        inFlight++;
        const reply = (status, body) => safeSend({ kind: 'reply', id: message?.id, status, body });
        try {
            validateRequest(message, code);
            const { path, body, session, id } = message;
            if (path === '/api/info') {
                const { iceServers, ...publicInfo } = info;
                reply(200, publicInfo);
            } else if (path === 'subscribe') {
                stop?.();
                stop = connect(session,
                    (event) => safeSend({ kind: 'event', id, event }),
                    (status, message) => safeSend({ kind: 'status', id, status, message }));
                reply(200, { ok: true });
            } else if (path === 'unsubscribe') { stop?.(); stop = null; reply(200, { ok: true }); }
            else {
                const entry = path === '/api/join' || path === '/api/watch';
                if (entry && entryBusy) throw new Error('A join is already in progress.');
                // Repeating the same successful join returns its result, not another seat.
                if (entry && joined) {
                    if (joined.path !== path || JSON.stringify(joined.body) !== JSON.stringify(body)) throw new Error('This connection already joined. Reload to use another player.');
                    reply(201, joined.credentials);
                    return;
                }
                if (entry) entryBusy = true;
                try {
                    const result = await request(path, body, session);
                    if (entry && closed) {
                        // Credentials never reached this guest; release its seat/viewer.
                        await request('/api/leave', {}, result);
                        return;
                    }
                    if (entry) joined = { path, body, credentials: result };
                    reply(entry ? 201 : 200, result);
                } finally { if (entry) entryBusy = false; }
            }
        } catch (error) { reply(error.status || 400, { error: error.message }); }
        finally { inFlight--; }
    });
    return () => { cleanup(); conn.close(); };
}

function newPeer(id, iceServers) {
    if (!globalThis.Peer) throw new Error('PeerJS could not load. Reload the page.');
    return new globalThis.Peer(id, { debug: 0, config: { iceServers } });
}

export async function startHost(route, options) {
    const peer = newPeer(`fomo-${route}`, options.info.iceServers || DEFAULT_ICE);
    const guests = new Set();
    let stopped = false;
    const stop = () => { stopped = true; for (const close of guests) close(); guests.clear(); peer.destroy(); };
    peer.on('connection', (conn) => {
        if (stopped || guests.size >= 32) { conn.close(); return; }
        const close = serveGuest(conn, options);
        guests.add(close);
        // Unopened connections cannot occupy slots indefinitely.
        const timer = setTimeout(close, 20000);
        conn.on('open', () => clearTimeout(timer));
        conn.on('close', () => { clearTimeout(timer); guests.delete(close); });
    });
    try {
        await new Promise((resolve, reject) => {
            const timer = setTimeout(() => reject(connectionError()), 20000);
            peer.on('open', () => { clearTimeout(timer); resolve(); });
            peer.on('error', (error) => { clearTimeout(timer); reject(new Error(`Internet invites unavailable (${error.type || 'signaling error'}).`)); });
        });
    } catch (error) { stop(); throw error; }
    // Existing data channels survive signaling loss; reconnect allows new guests.
    peer.on('disconnected', () => { if (!stopped) peer.reconnect(); });
    peer.on('error', (error) => options.onError?.(`Internet signaling error (${error.type}). Existing guests may still be connected.`));
    return stop;
}

export class PeerClient {
    constructor(route, { dial = null, timeout = 20000 } = {}) {
        this.route = route; this.timeout = timeout; this.dial = dial || (() => this.openPeer());
        this.conn = null; this.opening = null; this.peer = null; this.sequence = 0;
        this.pending = new Map(); this.listeners = new Set(); this.assembly = null;
    }
    openPeer() {
        return new Promise((resolve, reject) => {
            const peer = this.peer = newPeer(undefined, DEFAULT_ICE);
            const timer = setTimeout(() => { peer.destroy(); reject(connectionError()); }, this.timeout);
            const fail = () => { clearTimeout(timer); peer.destroy(); reject(connectionError()); };
            peer.on('error', fail);
            peer.on('open', () => {
                const conn = peer.connect(`fomo-${this.route}`, { reliable: true, serialization: 'json' });
                conn.on('open', () => { clearTimeout(timer); resolve(conn); });
                conn.on('error', fail); conn.on('close', () => { if (!conn.open) fail(); });
            });
        });
    }
    async ensure() {
        if (this.conn?.open) return this.conn;
        if (!this.opening) this.opening = this.dial().then((conn) => {
            this.conn = conn;
            conn.on('data', (message) => this.receive(message));
            conn.on('close', () => this.lost(conn));
            conn.on('error', () => { this.lost(conn); conn.close(); });
            return conn;
        }).finally(() => { this.opening = null; });
        return this.opening;
    }
    lost(conn) {
        if (this.conn !== conn) return;
        this.conn = null; this.assembly = null;
        for (const { reject, timer } of this.pending.values()) { clearTimeout(timer); reject(connectionError()); }
        this.pending.clear();
        for (const listener of this.listeners) listener({ kind: 'disconnect' });
        this.peer?.destroy(); this.peer = null;
    }
    receive(message) {
        try {
            if (message?.kind === 'chunk') {
                const { id, index, total, text } = message;
                if (!Number.isSafeInteger(id) || !Number.isInteger(total) || total < 1 || total > 375 || typeof text !== 'string' || text.length > 8000) throw connectionError();
                if (index === 0) this.assembly = { id, total, next: 0, parts: [] };
                const a = this.assembly;
                if (!a || a.id !== id || a.total !== total || a.next !== index) throw connectionError();
                a.parts.push(text); a.next++;
                if (a.next !== total) return;
                message = JSON.parse(a.parts.join('')); this.assembly = null;
            }
            if (!message || JSON.stringify(message).length > MAX_FRAME) throw connectionError();
            if (message.kind === 'reply') {
                const pending = this.pending.get(message.id);
                if (!pending) return;
                clearTimeout(pending.timer); this.pending.delete(message.id);
                if (message.status >= 200 && message.status < 300) pending.resolve(message.body);
                else pending.reject(Object.assign(new Error(message.body?.error || 'Peer request failed.'), { status: message.status }));
            } else for (const listener of this.listeners) listener(message);
        } catch { this.conn?.close(); }
    }
    async request(path, body, session = null, onID = null) {
        const conn = await this.ensure();
        if (this.pending.size >= 32) throw new Error('Too many pending actions. Wait a moment.');
        const id = ++this.sequence;
        onID?.(id);
        return new Promise((resolve, reject) => {
            const timer = setTimeout(() => {
                this.pending.delete(id); reject(connectionError()); conn.close();
            }, this.timeout);
            this.pending.set(id, { resolve, reject, timer });
            try { send(conn, { id, path, body, session }, false); }
            catch (error) { clearTimeout(timer); this.pending.delete(id); reject(error); }
        });
    }
    subscribe(session, onEvent, onStatus) {
        let stopped = false, streamID = null, release = null, delayTimer = null;
        const run = async () => {
            let attempts = 0;
            while (!stopped) {
                onStatus(attempts ? 'reconnecting' : 'connecting');
                let resolveLost;
                const lost = new Promise((resolve) => { resolveLost = resolve; });
                const listener = (message) => {
                    if (stopped) return;
                    if (message.kind === 'disconnect') { resolveLost(); return; }
                    if (message.id !== streamID) return;
                    if (message.kind === 'event') {
                        if (message.event.type === 'replaced') { stopped = true; onStatus('replaced', message.event.data.message); resolveLost(); }
                        else onEvent(message.event);
                    } else if (message.kind === 'status') {
                        onStatus(message.status, message.message);
                        if (message.status === 'connected') attempts = 0;
                        if (['expired', 'replaced'].includes(message.status)) { stopped = true; resolveLost(); }
                    }
                };
                this.listeners.add(listener);
                try {
                    await this.request('subscribe', undefined, session, (id) => { streamID = id; });
                    if (stopped) break;
                    release = resolveLost;
                    await lost;
                } catch (error) { if (!stopped) onStatus('reconnecting', error.message); }
                finally { this.listeners.delete(listener); release = null; }
                if (stopped) break;
                attempts++;
                await new Promise((resolve) => { release = resolve; delayTimer = setTimeout(resolve, Math.min(10000, 500 * 2 ** Math.min(attempts, 5))); });
                release = null;
            }
        };
        run();
        return () => {
            stopped = true; clearTimeout(delayTimer); release?.();
            if (this.conn?.open) this.request('unsubscribe').catch(() => {});
        };
    }
    destroy() { const conn = this.conn; if (conn) { this.lost(conn); conn.close(); } this.peer?.destroy(); }
}

const route = typeof location === 'undefined' ? null : new URLSearchParams(location.hash.slice(1)).get('p');
export const remote = routePattern.test(route || '') ? new PeerClient(route) : null;
