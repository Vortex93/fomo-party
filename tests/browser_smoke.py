"""Four-player UI + real HTTP/SSE smoke test, with optional local-only bridging.

Run against a running app:
    python tests/browser_smoke.py --base http://127.0.0.1:8080 --bridge

The build container blocks all Chromium navigation by managed policy. --bridge
renders the unchanged HTML/CSS locally, combines native JS modules for injection,
provides an in-memory URL/sessionStorage shim, and bridges fetch to the *real*
Go HTTP server. SSE framing, decoding, reconnect and UI logic still run in the
browser. It does not change browser policy or pretend to test real-device LAN.

Optional test-only dependencies: playwright, httpx; system Chromium or a
Playwright Chromium installation. Neither package is a runtime dependency.
"""
from __future__ import annotations

import argparse
import asyncio
import base64
import json
import os
import re
from pathlib import Path
from urllib.parse import urlparse

import httpx
from playwright.async_api import async_playwright

ROOT = Path(__file__).resolve().parents[1]


class Harness:
    def __init__(self, browser, base: str, out: Path):
        self.browser = browser
        self.base = base.rstrip('/')
        self.out = out
        self.client = httpx.AsyncClient(timeout=None, trust_env=False)
        self.streams = {}
        self.serial = 0
        self.errors = []
        self.pages = []

    async def fetch(self, source, path, method, body, headers):
        if not path.startswith('/api/') or '..' in path:
            raise ValueError('Only this test server’s API is allowed.')
        headers = {**headers, 'Origin': self.base}
        req = self.client.build_request(method, self.base+path, content=body, headers=headers)
        response = await self.client.send(req, stream=path == '/api/events')
        result = {'status': response.status_code, 'headers': dict(response.headers)}
        if path == '/api/events' and response.status_code == 200:
            self.serial += 1
            key = str(self.serial)
            self.streams[key] = (response, response.aiter_raw().__aiter__())
            result['stream'] = key
        else:
            result['body'] = (await response.aread()).decode()
            await response.aclose()
        return result

    async def read(self, source, key):
        pair = self.streams.get(key)
        if not pair:
            return None
        try:
            return base64.b64encode(await anext(pair[1])).decode()
        except (StopAsyncIteration, httpx.HTTPError, RuntimeError):
            await self.close_stream(source, key)
            return None

    async def close_stream(self, source, key):
        pair = self.streams.pop(key, None)
        if pair:
            await pair[0].aclose()

    async def page(self, query='', width=390):
        context = await self.browser.new_context(viewport={'width': width, 'height': 844})
        page = await context.new_page()
        page.on('pageerror', lambda error: self.errors.append(str(error)))
        page.on('dialog', lambda dialog: dialog.accept())
        await page.expose_binding('__bridgeFetch', self.fetch)
        await page.expose_binding('__bridgeRead', self.read)
        await page.expose_binding('__bridgeClose', self.close_stream)
        self.pages.append(page)
        await self.load(page, query)
        return page

    async def load(self, page, query='', storage=None):
        html = (ROOT/'web/index.html').read_text()
        html = re.sub(r'<script[^>]*src="(?:\./|/)(?:app.js|lib/peerjs.min.js)"[^>]*></script>', '', html)
        html = re.sub(r'<link rel="stylesheet" href="(?:\./|/)styles.css">', lambda _: '<style>'+(ROOT/'web/styles.css').read_text()+'</style>', html)
        files = ['lib/ui.js', 'lib/qr.js', 'lib/canvas.js', 'lib/peer.js', 'lib/api.js', 'app.js']
        source = '\n'.join((ROOT/'web'/p).read_text() for p in files)
        source = re.sub(r'^import .*?;\n', '', source, flags=re.M)
        source = re.sub(r'^export ', '', source, flags=re.M)
        source = source.replace('location.origin', 'window.__testURL.origin').replace('location.href', 'window.__testURL.href').replace('location.search', 'window.__testURL.search').replace('location.hash', 'window.__testURL.hash').replace('location.hostname', 'window.__testURL.hostname')
        source = source.replace('history.replaceState', 'window.__testReplace').replace('sessionStorage', 'window.__testStorage')
        source = source.replace('setInterval(updateTimer, 250);', 'window.__timer = setInterval(updateTimer, 250);')
        source += '\nwindow.__state = () => state; window.__stop = () => { stopStream?.(); board?.destroy(); clearInterval(window.__timer); };'
        setup = """
        window.__testURL = new URL(TEST_URL);
        window.__testReplace = (a,b,url) => { window.__testURL = new URL(url, window.__testURL); };
        window.__storageValues = STORAGE;
        window.__testStorage = {
            getItem: (k) => window.__storageValues[k] ?? null,
            setItem: (k,v) => { window.__storageValues[k] = String(v); },
            removeItem: (k) => { delete window.__storageValues[k]; }
        };
        window.fetch = async (path, options = {}) => {
            if (options.signal?.aborted) throw new DOMException('Aborted', 'AbortError');
            const value = await window.__bridgeFetch(String(path), options.method || 'GET', options.body || null, options.headers || {});
            if (!value.stream) return new Response(value.body || '', { status: value.status, headers: value.headers });
            const key = value.stream;
            let ended = false, control;
            const stream = new ReadableStream({
                start(c) { control = c; },
                async pull(c) {
                    if (ended) return;
                    try {
                        const data = await window.__bridgeRead(key);
                        if (ended) return;
                        if (data === null) { ended = true; c.close(); }
                        else c.enqueue(Uint8Array.from(atob(data), (v) => v.charCodeAt(0)));
                    } catch (error) { if (!ended) { ended = true; c.error(error); } }
                },
                cancel() { ended = true; return window.__bridgeClose(key); }
            });
            options.signal?.addEventListener('abort', () => {
                if (!ended) { ended = true; control.error(new DOMException('Aborted', 'AbortError')); }
                window.__bridgeClose(key).catch(() => {});
            }, { once: true });
            return new Response(stream, { status: value.status, headers: value.headers });
        };
        """.replace('TEST_URL', json.dumps(self.base+'/'+query)).replace('STORAGE', json.dumps(storage or {}))
        await page.set_content(html)
        await page.add_script_tag(content='(()=>{'+setup+'\nconst e=escapeHTML;\n'+source+'})();')

    async def snapshot(self, page):
        return await page.evaluate('window.__state()')

    async def wait(self, page, condition, timeout=15000):
        await page.wait_for_function('() => { const s=window.__state?.(); return s && ('+condition+'); }', timeout=timeout)

    async def close(self):
        for page in self.pages:
            try:
                await page.evaluate('window.__stop?.()')
            except Exception:
                pass
        for key in list(self.streams):
            await self.close_stream(None, key)
        await self.client.aclose()


async def run(args):
    args.out.mkdir(parents=True, exist_ok=True)
    async with async_playwright() as p:
        executable = args.chromium if Path(args.chromium).exists() else None
        browser = await p.chromium.launch(executable_path=executable, headless=True, args=['--no-sandbox'])
        h = Harness(browser, args.base, args.out)
        try:
            host = await h.page(width=1280)
            await host.locator('#nickname').fill('Alex')
            await host.locator('#entry-form button[type=submit]').click()
            await h.wait(host, 's.status === "lobby"')
            code = (await h.snapshot(host))['code']
            print('Created room', code, flush=True)
            await host.locator('#round-count').select_option('1')
            await h.wait(host, 's.config.rounds === 1')
            pages = [host]
            for name in ('Sam', 'Riley', 'Morgan'):
                page = await h.page('?room='+code)
                await page.locator('#nickname').fill(name)
                await page.locator('#entry-form button[type=submit]').click()
                await h.wait(page, 's.status === "lobby"')
                await page.locator('[data-action=ready]').click()
                await h.wait(page, 's.players.find(p=>p.id===s.youId).ready')
                pages.append(page)
            await h.wait(host, 's.players.length === 4 && s.players.every(p=>p.ready && p.connected)')
            tv = await h.page('?watch='+code, width=1280)
            await h.wait(tv, 's.isViewer')
            await host.screenshot(path=str(args.out/'lobby-desktop.png'), full_page=True)
            await pages[1].screenshot(path=str(args.out/'lobby-mobile.png'), full_page=True)
            print('Four players + read-only TV connected', flush=True)
            await host.locator('[data-action=start]').click()
            await h.wait(host, 's.round?.phase === "answer"')
            for page, answer in zip(pages, ('Banana!', 'banana', 'apple', 'banana')):
                await page.locator('#answer-input').fill(answer)
                await page.locator('#answer-form button').click()
            await h.wait(host, 's.round?.phase === "reveal"')
            assert [x['score'] for x in (await h.snapshot(host))['players']] == [100, 100, 0, 100]
            print('Same Brain scoring verified', flush=True)
            # Reload through the test URL/session-storage shim; the token and the
            # real server connection are reused, rather than creating a new seat.
            saved = await pages[1].evaluate('window.__storageValues')
            before = await h.snapshot(pages[1])
            await pages[1].evaluate('window.__stop()')
            await h.load(pages[1], '?room='+code, saved)
            await h.wait(pages[1], 's.round?.phase === "reveal"')
            after = await h.snapshot(pages[1])
            assert after['youId'] == before['youId'] and len(after['players']) == 4
            assert [x['score'] for x in after['players']] == [100, 100, 0, 100]
            print('Reload/reconnect preserves identity and score', flush=True)
            await host.locator('[data-action=next]').click()
            await h.wait(host, 's.round?.game === "whos-lying" && s.round.phase === "answer"')
            snapshots = [await h.snapshot(page) for page in pages]
            liars = [i for i, s in enumerate(snapshots) if s['round']['you']['role'] == 'bluffer']
            assert len(liars) == 1
            liar = liars[0]
            secret_question = snapshots[(liar+1)%4]['round']['prompt']
            assert snapshots[liar]['round']['prompt'] != secret_question
            assert (await h.snapshot(tv))['round']['prompt'] != secret_question
            await pages[liar].screenshot(path=str(args.out/'bluffer-mobile.png'), full_page=True)
            for page in pages:
                await page.locator('#answer-input').fill('Something delicious')
                await page.locator('#answer-form button').click()
            await h.wait(host, 's.round.phase === "discuss"')
            await h.wait(host, 's.round.phase === "vote"', timeout=18000)
            ids = [s['youId'] for s in snapshots]
            for i, page in enumerate(pages):
                target = ids[liar] if i != liar else ids[(liar+1)%4]
                await page.locator(f'[data-action=vote][data-value="{target}"]').click()
            await h.wait(host, 's.round.phase === "reveal"')
            assert (await h.snapshot(host))['round']['result']['liarId'] == ids[liar]
            print('Bluffer privacy, discussion, vote and reveal verified', flush=True)
            await host.locator('[data-action=next]').click()
            await h.wait(host, 's.round.game === "draw-guess" && s.round.phase === "draw"')
            snapshots = [await h.snapshot(page) for page in pages]
            drawer_index = next(i for i, s in enumerate(snapshots) if s['round']['you']['role'] == 'drawer')
            drawer = pages[drawer_index]
            word = snapshots[drawer_index]['round']['word']
            assert all('word' not in s['round'] for i, s in enumerate(snapshots) if i != drawer_index)
            canvas = drawer.locator('#drawing-canvas')
            await canvas.scroll_into_view_if_needed()
            box = await canvas.bounding_box()
            await drawer.mouse.move(box['x']+40, box['y']+40)
            await drawer.mouse.down()
            await drawer.mouse.move(box['x']+box['width']*0.7, box['y']+box['height']*0.6, steps=9)
            await drawer.mouse.up()
            await h.wait(tv, 's.round.strokes.length > 0')
            await tv.screenshot(path=str(args.out/'drawing-tv.png'), full_page=True)
            await host.locator('[data-action=pause]').click()
            await h.wait(tv, 's.round.paused')
            await host.locator('[data-action=pause]').click()
            await h.wait(tv, '!s.round.paused')
            for i, page in enumerate(pages):
                if i == drawer_index:
                    continue
                await page.locator('#guess-input').fill(word)
                await page.locator('#guess-form button').click()
            await h.wait(host, 's.round.phase === "reveal"')
            print('Canvas sync, secret word, pause and correct guesses verified', flush=True)
            await host.locator('[data-action=next]').click()
            await h.wait(host, 's.round.game === "most-likely" && s.round.phase === "vote"')
            for page in pages:
                await page.locator(f'[data-action=vote][data-value="{ids[0]}"]').click()
            await h.wait(host, 's.round.phase === "reveal"')
            assert all(row['points'] == 100 for row in (await h.snapshot(host))['round']['result']['rows'])
            print('Most Likely nomination/scoring verified', flush=True)
            await host.locator('[data-action=next]').click()
            await h.wait(host, 's.round.game === "memory-clash" && s.round.phase === "memorize"')
            sequence = (await h.snapshot(host))['round']['sequence']
            await pages[1].screenshot(path=str(args.out/'memory-mobile.png'), full_page=True)
            await h.wait(host, 's.round.phase === "recall"', timeout=10000)
            for page in pages:
                assert 'sequence' not in (await h.snapshot(page))['round']
                for symbol in sequence:
                    await page.locator(f'[data-action=symbol][data-value="{symbol}"]').click()
                await page.locator('[data-action=memory]').click()
            await h.wait(host, 's.round.phase === "reveal"')
            assert all(row['points'] == 100 for row in (await h.snapshot(host))['round']['result']['rows'])
            print('Memory hides target and scores ordered taps', flush=True)
            await host.locator('[data-action=next]').click()
            await h.wait(host, 's.status === "finished"')
            await host.screenshot(path=str(args.out/'final-desktop.png'), full_page=True)
            await host.locator('[data-action=rematch]').click()
            await h.wait(host, 's.status === "lobby" && s.players.every(p=>p.score === 0)')
            # Responsive layout and modal keyboard path.
            for width in (320, 390, 768, 1280):
                await host.set_viewport_size({'width': width, 'height': 900})
                assert not await host.evaluate('document.documentElement.scrollWidth > window.innerWidth'), f'Overflow at {width}'
            await host.locator('#help-open').click()
            assert await host.locator('#help-dialog').is_visible()
            await host.keyboard.press('Escape')
            assert not await host.locator('#help-dialog').is_visible()
            replacement = await h.page()
            stored = await pages[1].evaluate('window.__storageValues')
            await replacement.evaluate('window.__stop()')
            await h.load(replacement, '?room='+code, stored)
            await h.wait(replacement, 's.status === "lobby"')
            await pages[1].wait_for_function('document.querySelector("#connection").textContent.includes("another tab")')
            await asyncio.sleep(1.5)
            assert (await h.snapshot(replacement))['players'][1]['connected']
            print('Duplicate tab stops reconnecting; latest tab remains connected', flush=True)
            assert not h.errors, h.errors
            print('PASS: all five games, reconnect, duplicate tabs, TV, rematch, responsive widths; zero browser exceptions', flush=True)
        finally:
            await h.close()
            await browser.close()


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--base', default=os.environ.get('TEST_BASE_URL', 'http://127.0.0.1:8080'))
    parser.add_argument('--bridge', action='store_true', help='Explicitly use the local HTTP bridge (required in restricted containers).')
    parser.add_argument('--chromium', default='/usr/bin/chromium')
    parser.add_argument('--out', type=Path, default=ROOT/'test-results')
    args = parser.parse_args()
    if not args.bridge:
        parser.error('This harness requires --bridge; see the module docstring for its coverage limits.')
    if urlparse(args.base).hostname not in ('localhost', '127.0.0.1', '::1'):
        parser.error('The bridge only permits a loopback test server.')
    asyncio.run(run(args))
