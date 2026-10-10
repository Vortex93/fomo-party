import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { qrMatrix } from '../web/lib/qr.js';
import { escapeHTML, normalizeRoomCode, secondsLeft, GAMES, SYMBOLS } from '../web/lib/ui.js';

test('user text is escaped in every HTML context', () => {
    assert.equal(escapeHTML(`<img src="x" onerror='bad()'>&`), '&lt;img src=&quot;x&quot; onerror=&#39;bad()&#39;&gt;&amp;');
    assert.equal(escapeHTML(null), '');
});
test('room codes are capped and normalized', () => {
    assert.equal(normalizeRoomCode('ab c2-34more'), 'ABC234');
    assert.equal(normalizeRoomCode(''), '');
});
test('timer follows the server offset and never goes negative', () => {
    assert.equal(secondsLeft({ deadline: 15000, paused: false }, 1000, 10000), 4);
    assert.equal(secondsLeft({ deadline: 15000, paused: false }, 0, 20000), 0);
    assert.equal(secondsLeft({ deadline: 15000, paused: true, remainingMs: 4250 }, 9999, 999999), 5);
    assert.equal(secondsLeft(null), 0);
});
test('five requested games and nine labeled memory symbols exist', () => {
    assert.equal(GAMES.length, 5);
    assert.equal(new Set(GAMES.map((g) => g.id)).size, 5);
    assert.equal(SYMBOLS.length, 9);
    assert.ok(SYMBOLS.every((s) => s.label && s.icon));
});
test('QR has version-five dimensions and fixed finder patterns', () => {
    const matrix = qrMatrix('http://192.168.1.5:8080/?room=ABC234');
    assert.equal(matrix.length, 37);
    assert.ok(matrix.every((row) => row.length === 37 && row.every((cell) => typeof cell === 'boolean')));
    assert.equal(matrix[0][0], true);
    assert.equal(matrix[1][1], false);
    assert.equal(matrix[3][3], true);
    assert.equal(matrix[29][8], true);
});
test('QR matches an independently generated reference matrix', () => {
    const matrix = qrMatrix('http://192.168.1.5:8080/?room=ABC234');
    const hash = createHash('sha256').update(matrix.map((row) => row.map(Number).join('')).join('\n')).digest('hex');
    assert.equal(hash, '5697d82f7ec2f5ada2de5171b0a171740f3d79d47fb0f8100f9753b209a15c46');
});
test('QR rejects oversized links instead of silently corrupting them', () => {
    assert.doesNotThrow(() => qrMatrix('a'.repeat(106)));
    assert.throws(() => qrMatrix('a'.repeat(107)), /too long/);
    assert.throws(() => qrMatrix('é'.repeat(54)), /too long/);
});
