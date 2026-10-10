// Real browser/WebRTC smoke test against PeerJS Cloud; no guest HTTP API exists.
// npm run test:browser (optional Playwright dev dependency; Brave or Chromium).
import test from 'node:test';
import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { spawn } from 'node:child_process';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { resolve, extname } from 'node:path';

const root = fileURLToPath(new URL('../', import.meta.url));

test('mobile guest joins, acts, reloads and reconnects through real WebRTC only', { timeout: 90000 }, async () => {
    const process = spawn(resolve(root, 'bin/peer-test-server'), ['-addr', '127.0.0.1:0'], { cwd: root });
    const staticServer = createServer(async (req, res) => {
        const pathname = new URL(req.url, 'http://localhost').pathname;
        const file = pathname === '/fomo-party/' ? 'index.html' : pathname.replace(/^\/fomo-party\//, '');
        if (!['index.html', 'styles.css', 'app.js'].includes(file) && !/^lib\/[a-z.-]+\.js$/.test(file)) { res.writeHead(404); res.end(); return; }
        try {
            res.setHeader('Content-Type', { '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html' }[extname(file)]);
            res.end(await readFile(resolve(root, 'web', file)));
        } catch { res.writeHead(404); res.end(); }
    });
    let browser;
    try {
        const hostURL = await new Promise((accept, reject) => {
            const timer = setTimeout(() => reject(new Error('Go test server did not start.')), 10000);
            process.once('error', reject);
            process.once('exit', () => reject(new Error('Go test server exited.')));
            process.stdout.on('data', (chunk) => {
                const match = chunk.toString().match(/Open http:\/\/localhost:(\d+)/);
                if (match) { clearTimeout(timer); accept(`http://127.0.0.1:${match[1]}`); }
            });
        });
        await new Promise((accept) => staticServer.listen(0, '127.0.0.1', accept));
        const staticURL = `http://127.0.0.1:${staticServer.address().port}/fomo-party/`;
        browser = await chromium.launch({ ...(globalThis.process.env.FOMO_TEST_BROWSER ? { executablePath: globalThis.process.env.FOMO_TEST_BROWSER } : {}), headless: true });
        const errors = [];
        const host = await browser.newPage();
        host.on('pageerror', (error) => errors.push(error.message));
        await host.goto(hostURL);
        await host.locator('#nickname').fill('Host');
        await host.locator('#entry-form button[type=submit]').click();
        await host.locator('#share-panel a[href*="#p="]').waitFor({ timeout: 30000 });
        const invite = new URL(await host.locator('#share-panel a').getAttribute('href'));
        const guestURL = new URL(staticURL);
        guestURL.searchParams.set('room', invite.searchParams.get('watch'));
        guestURL.hash = invite.hash;
        const guest = await browser.newPage({ viewport: { width: 390, height: 844 } });
        guest.on('pageerror', (error) => errors.push(error.message));
        const guestAPIRequests = [];
        guest.on('request', (req) => { if (new URL(req.url()).pathname.startsWith('/api/')) guestAPIRequests.push(req.url()); });
        await guest.goto(guestURL.href);
        await guest.locator('#nickname').fill('Guest');
        await guest.locator('#entry-form button[type=submit]').click();
        await guest.locator('#connection.connected').waitFor({ state: 'attached', timeout: 30000 });
        const youID = await guest.evaluate(async () => {
            const { remote } = await import('./lib/peer.js');
            return JSON.parse(sessionStorage.getItem(`familyparty.session.${remote.route}`)).id;
        });
        await guest.locator('[data-action=ready]').click();
        await guest.getByText('✓ Ready! Tap to unready').waitFor();
        const rejection = await guest.evaluate(async () => {
            const { request } = await import('./lib/api.js');
            const { remote } = await import('./lib/peer.js');
            const session = JSON.parse(sessionStorage.getItem(`familyparty.session.${remote.route}`));
            try { await request('/api/action', { type: 'start' }, session); return null; }
            catch (error) { return error.message; }
        });
        assert.match(rejection, /host/i);
        await guest.reload();
        await guest.locator('#connection.connected').waitFor({ state: 'attached' });
        assert.equal(await guest.evaluate(async () => {
            const { remote } = await import('./lib/peer.js');
            return JSON.parse(sessionStorage.getItem(`familyparty.session.${remote.route}`)).id;
        }), youID);
        await guest.evaluate(async () => { const { remote } = await import('./lib/peer.js'); remote.conn.close(); });
        await guest.locator('#connection.connected').waitFor({ state: 'attached', timeout: 30000 });
        assert.equal(await guest.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
        assert.deepEqual(guestAPIRequests, []);
        assert.deepEqual(errors, []);
        await guest.locator('[data-action=leave]').click();
        await guest.locator('#entry-form').waitFor();
    } finally {
        await browser?.close();
        await new Promise((accept) => staticServer.close(accept));
        process.kill('SIGTERM');
    }
});
