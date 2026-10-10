import test from 'node:test';
import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { PeerClient, serveGuest, inviteURL, validateRequest } from '../web/lib/peer.js';

const code = 'ABC234';
const token = 'a'.repeat(64);
const route = 'b'.repeat(32);
class Connection extends EventEmitter {
    open = true;
    sent = [];
    send(message) { this.sent.push(message); this.emit('sent', message); }
    close() { if (!this.open) return; this.open = false; this.emit('close'); }
}

test('internet invites retain the Pages subpath and fit the QR encoder', () => {
    const url = inviteURL('https://vortex93.github.io/fomo-party/', code, route);
    assert.equal(new URL(url).pathname, '/fomo-party/');
    assert.equal(new URL(url).hash, `#p=${route}`);
    assert.ok(url.length <= 106);
    assert.equal(new URL(inviteURL('https://example.com/', code, route, true)).searchParams.get('watch'), code);
});

test('guest bridge refuses arbitrary URLs, room creation, foreign rooms, and oversized input', () => {
    const message = { id: 1, path: '/api/action', body: { type: 'ready' }, session: { code, token } };
    assert.doesNotThrow(() => validateRequest(message, code));
    for (const path of ['/api/rooms', 'https://evil.example', '/api/events', '/api/action?x=1']) {
        assert.throws(() => validateRequest({ ...message, path }, code));
    }
    assert.throws(() => validateRequest({ ...message, session: { code: 'OTHER1', token } }, code));
    assert.throws(() => validateRequest({ id: 1, path: '/api/join', body: { code: 'OTHER1', name: 'Joe' } }, code));
    assert.throws(() => validateRequest({ ...message, body: { value: 'x'.repeat(13000) } }, code));
    assert.throws(() => validateRequest({ ...message, id: {} }, code));
});

test('bridge returns server errors, strips ICE credentials, and cleans up subscriptions', async () => {
    const conn = new Connection();
    let stopped = 0;
    const request = async (path) => {
        if (path === '/api/action') throw Object.assign(new Error('Only the host can do that.'), { status: 409 });
        return { code, token, id: 'guest' };
    };
    const connect = (session, event, status) => {
        assert.equal(session.token, token);
        event({ type: 'state', data: { youId: 'guest' } });
        status('connected');
        return () => stopped++;
    };
    serveGuest(conn, { code, request, connect, info: { maxPlayers: 15, iceServers: [{ credential: 'secret' }] } });
    conn.emit('data', { id: 1, path: '/api/info' });
    await new Promise(setImmediate);
    assert.equal(conn.sent[0].body.iceServers, undefined);
    conn.emit('data', { id: 2, path: '/api/action', body: {}, session: { code, token } });
    await new Promise(setImmediate);
    assert.equal(conn.sent.at(-1).status, 409);
    conn.emit('data', { id: 3, path: 'subscribe', session: { code, token } });
    await new Promise(setImmediate);
    assert.ok(conn.sent.some((m) => m.kind === 'event' && m.id === 3 && m.event.data.youId === 'guest'));
    conn.close();
    assert.equal(stopped, 1);
});

test('client correlates replies and never replays an uncertain mutation', async () => {
    const conn = new Connection();
    const client = new PeerClient(route, { dial: async () => conn, timeout: 100 });
    conn.on('sent', (message) => {
        if (message.path === '/api/info') queueMicrotask(() => conn.emit('data', { kind: 'reply', id: message.id, status: 200, body: { maxPlayers: 15 } }));
    });
    assert.equal((await client.request('/api/info')).maxPlayers, 15);
    const pending = client.request('/api/action', { type: 'answer', value: 'yes' }, { code, token });
    await new Promise(setImmediate);
    conn.close();
    await assert.rejects(pending, /not retried/);
    assert.equal(conn.sent.filter((m) => m.path === '/api/action').length, 1);
    client.destroy();
});

test('large drawing snapshots are reassembled while large actions stay single-frame', async () => {
    const conn = new Connection();
    const client = new PeerClient(route, { dial: async () => conn });
    conn.on('sent', (message) => {
        assert.notEqual(message.kind, 'chunk');
        const text = JSON.stringify({ kind: 'reply', id: message.id, status: 200, body: { drawing: 'x'.repeat(45000) } });
        const total = Math.ceil(text.length / 8000);
        queueMicrotask(() => {
            for (let index = 0; index < total; index++) conn.emit('data', { kind: 'chunk', id: 5, index, total, text: text.slice(index * 8000, (index + 1) * 8000) });
        });
    });
    const result = await client.request('/api/action', { value: 'x'.repeat(9000) }, { code, token });
    assert.equal(result.drawing.length, 45000);
    client.destroy();
});

test('repeated joins on one channel do not allocate extra seats', async () => {
    const conn = new Connection();
    let joins = 0;
    serveGuest(conn, { code, info: {}, connect: () => () => {}, request: async () => { joins++; return { code, token }; } });
    conn.emit('data', { id: 1, path: '/api/join', body: { code, name: 'Guest' } });
    await new Promise(setImmediate);
    conn.emit('data', { id: 2, path: '/api/join', body: { code, name: 'Guest' } });
    await new Promise(setImmediate);
    assert.equal(joins, 1);
    assert.equal(conn.sent.at(-1).status, 201);
    conn.close();
});

test('connection lost during join releases the undelivered seat', async () => {
    const conn = new Connection();
    let finishJoin;
    const calls = [];
    const request = (path, body, session) => {
        calls.push({ path, session });
        if (path === '/api/join') return new Promise((resolve) => { finishJoin = resolve; });
        return Promise.resolve({ ok: true });
    };
    serveGuest(conn, { code, info: {}, request, connect: () => () => {} });
    conn.emit('data', { id: 1, path: '/api/join', body: { code, name: 'Guest' } });
    conn.close();
    finishJoin({ code, token, id: 'guest' });
    await new Promise(setImmediate);
    assert.equal(calls.at(-1).path, '/api/leave');
    assert.equal(calls.at(-1).session.token, token);
});
