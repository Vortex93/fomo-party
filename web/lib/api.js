import { remote } from './peer.js';

export class APIError extends Error {
    constructor(message, status) { super(message); this.status = status; }
}

export async function request(path, body, session = null) {
    if (remote) return remote.request(path, body, session);
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 12000);
    const headers = { 'Content-Type': 'application/json' };
    if (session) { headers.Authorization = `Bearer ${session.token}`; headers['X-Room-Code'] = session.code; }
    try {
        const response = await fetch(path, { method: body === undefined ? 'GET' : 'POST', headers, body: body === undefined ? undefined : JSON.stringify(body), signal: controller.signal });
        const payload = await response.json().catch(() => ({}));
        if (!response.ok) throw new APIError(payload.error || `Request failed (${response.status}).`, response.status);
        return payload;
    } catch (error) {
        if (error.name === 'AbortError') throw new Error('The server took too long. Reconnect before trying again.');
        if (error instanceof TypeError) throw new Error('Cannot reach the game server. Check your connection.');
        throw error;
    } finally { clearTimeout(timeout); }
}

// This stream uses fetch instead of EventSource: bearer credentials stay out
// of URLs. Server state is authoritative; disconnected actions are never queued.
export function connect(session, onEvent, onStatus) {
    if (remote) return remote.subscribe(session, onEvent, onStatus);
    let stopped = false;
    let active = null;
    let retryTimer = null;
    let releaseDelay = null;
    let attempts = 0;
    const delay = (ms) => new Promise((resolve) => { releaseDelay = resolve; retryTimer = setTimeout(resolve, ms); });
    async function run() {
        while (!stopped) {
            active = new AbortController();
            let watchdog = null;
            const touch = () => { clearTimeout(watchdog); watchdog = setTimeout(() => active?.abort(), 30000); };
            try {
                onStatus(attempts ? 'reconnecting' : 'connecting');
                touch();
                const response = await fetch('/api/events', {
                    headers: { Authorization: `Bearer ${session.token}`, 'X-Room-Code': session.code },
                    signal: active.signal, cache: 'no-store'
                });
                if (response.status === 401) {
                    const body = await response.json();
                    onStatus('expired', body.error);
                    stopped = true;
                    break;
                }
                if (!response.ok || !response.body) throw new Error('Stream unavailable.');
                const reader = response.body.getReader();
                const decoder = new TextDecoder();
                let buffer = '';
                while (!stopped) {
                    const { value, done } = await reader.read();
                    if (done) break;
                    touch();
                    buffer += decoder.decode(value, { stream: true }).replace(/\r/g, '');
                    if (buffer.length > 3_000_000) throw new Error('Stream frame is too large.');
                    let boundary;
                    while ((boundary = buffer.indexOf('\n\n')) !== -1) {
                        const frame = buffer.slice(0, boundary);
                        buffer = buffer.slice(boundary + 2);
                        const data = frame.split('\n').filter((line) => line.startsWith('data: ')).map((line) => line.slice(6)).join('\n');
                        if (data && !stopped) {
                            const event = JSON.parse(data);
                            if (event.type === 'replaced') { stopped = true; onStatus('replaced', event.data.message); break; }
                            if (event.type === 'state') { attempts = 0; onStatus('connected'); }
                            onEvent(event);
                        }
                    }
                }
            } catch (error) {
                if (!stopped) onStatus('reconnecting');
            } finally { clearTimeout(watchdog); active?.abort(); }
            if (stopped) break;
            onStatus('reconnecting');
            attempts++;
            await delay(Math.min(10000, 400 * 2 ** Math.min(attempts, 5)) + Math.random()*300);
        }
    }
    run();
    return () => { stopped = true; active?.abort(); clearTimeout(retryTimer); releaseDelay?.(); };
}
