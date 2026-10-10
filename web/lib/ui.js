export const GAMES = [
    { id: 'same-brain', name: 'Same Brain', icon: '✳', theme: 'lavender', min: 3, verb: 'THINK TOGETHER', description: 'Different minds. The same answer.', rule: 'Answer with a short word or phrase. Match a largest group of at least two to earn 100 points. Tied largest groups count.' },
    { id: 'whos-lying', name: "Who's Lying?", icon: '◕', theme: 'peach', min: 4, verb: 'TRUST NO ONE. NICELY.', description: 'One secret bluffer. A room of detectives.', rule: 'One player gets only the topic, not the question. Everyone answers, discusses, then votes. Detectives earn 100 for a correct vote. An answering bluffer earns 100 by avoiding a strict majority.' },
    { id: 'draw-guess', name: 'Draw & Guess', icon: '✎', theme: 'mint', min: 2, verb: 'MASTERPIECES OPTIONAL', description: 'Sketch something. Guess anything.', rule: 'Only the drawer sees the word. Correct guessers earn 100, and the drawer earns 100 if anyone gets it. No letters or saying the word out loud!' },
    { id: 'most-likely', name: 'Most Likely To', icon: '☝', theme: 'yellow', min: 3, verb: 'YOU KNOW YOUR PEOPLE', description: 'Who in this room? You decide.', rule: 'Nominate anyone, including yourself. Voters who match a top pick shared by at least two votes earn 100. Ties count. Keep the nominations kind.' },
    { id: 'memory-clash', name: 'Memory Clash', icon: '▦', theme: 'blue', min: 2, verb: 'TAKE A MENTAL PICTURE', description: 'A few symbols. A very short memory.', rule: 'Memorize the symbols in order, then tap them back. Each correct position earns a share of 100 points. Sequences get longer each time this game appears.' }
];
export const SYMBOLS = [
    { icon: '☀', label: 'Sun' }, { icon: '☾', label: 'Moon' }, { icon: '★', label: 'Star' },
    { icon: '☁', label: 'Cloud' }, { icon: '♣', label: 'Clover' }, { icon: '♦', label: 'Diamond' },
    { icon: '●', label: 'Circle' }, { icon: '⚡', label: 'Bolt' }, { icon: '♥', label: 'Heart' }
];
export const BRUSHES = [
    ['#20263a', 'Ink'], ['#ee6b6e', 'Coral'], ['#4c78dd', 'Blue'], ['#34a886', 'Green'],
    ['#f2b53f', 'Gold'], ['#a875d6', 'Purple'], ['#ffffff', 'Eraser']
];
export function escapeHTML(value) {
    return String(value ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
export function gameInfo(id) { return GAMES.find((g) => g.id === id) ?? GAMES[0]; }
export function initials(name) { return Array.from(name || '?').slice(0, 2).join('').toUpperCase(); }
export function secondsLeft(round, offset = 0, now = Date.now()) {
    if (!round || !round.deadline) return 0;
    const left = round.paused ? round.remainingMs : round.deadline - now - offset;
    return Math.max(0, Math.ceil(left / 1000));
}
export function normalizeRoomCode(value) { return String(value).toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 6); }
