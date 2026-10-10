// Small, dependency-free QR encoder: version 5, error correction L, byte mode,
// mask 0. One RS block (108 data + 26 parity codewords), at most 106 UTF-8 bytes.
// A fixed mask is valid; QR decodability is covered by a reference-matrix test.
export function qrMatrix(text) {
    const bytes = new TextEncoder().encode(text);
    if (bytes.length > 106) throw new Error('This link is too long for the built-in QR code. Share the room code instead.');
    const bits = [];
    const append = (value, count) => { for (let i = count - 1; i >= 0; i--) bits.push((value >>> i) & 1); };
    append(4, 4); append(bytes.length, 8);
    for (const byte of bytes) append(byte, 8);
    append(0, Math.min(4, 864 - bits.length));
    while (bits.length % 8) bits.push(0);
    const data = [];
    for (let i = 0; i < bits.length; i += 8) { let n = 0; for (let j = 0; j < 8; j++) n = (n << 1) | bits[i+j]; data.push(n); }
    for (let pad = 0; data.length < 108; pad++) data.push(pad % 2 ? 0x11 : 0xec);
    function multiply(a, b) {
        let product = 0;
        while (b) { if (b & 1) product ^= a; a <<= 1; if (a & 0x100) a ^= 0x11d; b >>>= 1; }
        return product;
    }
    let generator = [1], root = 1;
    for (let i = 0; i < 26; i++) {
        const next = Array(generator.length + 1).fill(0);
        generator.forEach((v, j) => { next[j] ^= v; next[j+1] ^= multiply(v, root); });
        generator = next; root = multiply(root, 2);
    }
    const parity = Array(26).fill(0);
    for (const byte of data) {
        const factor = byte ^ parity.shift(); parity.push(0);
        for (let i = 0; i < 26; i++) parity[i] ^= multiply(generator[i+1], factor);
    }
    const codewords = [...data, ...parity];
    const size = 37;
    const matrix = Array.from({ length: size }, () => Array(size).fill(false));
    const reserved = Array.from({ length: size }, () => Array(size).fill(false));
    function set(x, y, dark) {
        if (x >= 0 && y >= 0 && x < size && y < size) { matrix[y][x] = Boolean(dark); reserved[y][x] = true; }
    }
    for (let i = 0; i < size; i++) { set(6, i, i % 2 === 0); set(i, 6, i % 2 === 0); }
    function finder(x, y) {
        for (let dy = -1; dy <= 7; dy++) for (let dx = -1; dx <= 7; dx++) {
            const distance = Math.max(Math.abs(dx-3), Math.abs(dy-3));
            set(x+dx, y+dy, distance !== 2 && distance !== 4);
        }
    }
    finder(0, 0); finder(size-7, 0); finder(0, size-7);
    for (let dy = -2; dy <= 2; dy++) for (let dx = -2; dx <= 2; dx++) set(30+dx, 30+dy, Math.max(Math.abs(dx), Math.abs(dy)) !== 1);
    const formatData = 8; // L = 01, mask = 000
    let remainder = formatData;
    for (let i = 0; i < 10; i++) remainder = (remainder << 1) ^ ((remainder >>> 9) * 0x537);
    const format = ((formatData << 10) | remainder) ^ 0x5412;
    const bit = (i) => (format >>> i) & 1;
    for (let i = 0; i <= 5; i++) set(8, i, bit(i));
    set(8, 7, bit(6)); set(8, 8, bit(7)); set(7, 8, bit(8));
    for (let i = 9; i < 15; i++) set(14-i, 8, bit(i));
    for (let i = 0; i < 8; i++) set(size-1-i, 8, bit(i));
    for (let i = 8; i < 15; i++) set(8, size-15+i, bit(i));
    set(8, size-8, true);
    let index = 0;
    for (let right = size-1; right >= 1; right -= 2) {
        if (right === 6) right = 5;
        for (let vertical = 0; vertical < size; vertical++) {
            const upward = ((right+1) & 2) === 0;
            const y = upward ? size-1-vertical : vertical;
            for (let j = 0; j < 2; j++) {
                const x = right-j;
                if (!reserved[y][x]) {
                    let dark = false;
                    if (index < codewords.length*8) dark = ((codewords[index >>> 3] >>> (7-(index & 7))) & 1) !== 0;
                    matrix[y][x] = dark !== ((x+y) % 2 === 0);
                    index++;
                }
            }
        }
    }
    return matrix;
}

export function renderQR(container, text) {
    const matrix = qrMatrix(text);
    const ns = 'http://www.w3.org/2000/svg';
    const svg = document.createElementNS(ns, 'svg');
    svg.setAttribute('viewBox', '0 0 45 45');
    svg.setAttribute('role', 'img');
    svg.setAttribute('aria-label', 'Scan to join this room');
    svg.setAttribute('shape-rendering', 'crispEdges');
    const background = document.createElementNS(ns, 'rect');
    background.setAttribute('width', '45'); background.setAttribute('height', '45'); background.setAttribute('fill', '#ffffff');
    svg.append(background);
    const path = document.createElementNS(ns, 'path');
    let d = '';
    matrix.forEach((row, y) => row.forEach((dark, x) => { if (dark) d += `M${x+4},${y+4}h1v1h-1z`; }));
    path.setAttribute('d', d); path.setAttribute('fill', '#20263a');
    svg.append(path); container.replaceChildren(svg);
}
