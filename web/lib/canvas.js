// Drawing stays local until small, ordered batches are accepted by the server.
// Normalized coordinates keep phones and the TV view in the same coordinate space.
export class DrawingBoard {
    constructor(canvas, { roundId, send, onError }) {
        this.canvas = canvas;
        this.context = canvas.getContext('2d');
        this.roundId = roundId;
        this.send = send;
        this.onError = onError;
        this.enabled = false;
        this.stopped = false;
        this.color = '#20263a';
        this.width = 7;
        this.epoch = 0;
        this.counter = 0;
        this.prefix = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 9)}`;
        this.strokes = [];
        this.pending = new Map();
        this.points = [];
        this.pointer = null;
        this.position = { x: 0.5, y: 0.5 };
        this.chain = Promise.resolve();
        this.events = new AbortController();
        const options = { signal: this.events.signal };
        canvas.addEventListener('pointerdown', (event) => this.down(event), options);
        canvas.addEventListener('pointermove', (event) => this.move(event), options);
        canvas.addEventListener('pointerup', (event) => this.up(event), options);
        canvas.addEventListener('pointercancel', (event) => this.up(event), options);
        canvas.addEventListener('lostpointercapture', () => { this.flush(true); this.pointer = null; }, options);
        canvas.addEventListener('keydown', (event) => this.key(event), options);
        this.interval = setInterval(() => { if (this.points.length > 1) this.flush(false); }, 80);
        this.paint();
    }
    setEnabled(enabled) {
        this.enabled = enabled;
        this.canvas.classList.toggle('editable', enabled);
        this.canvas.setAttribute('aria-disabled', String(!enabled));
        if (!enabled) { this.points = []; this.pointer = null; }
    }
    brush(color, width) { this.flush(true); this.color = color; this.width = width; }
    point(event) {
        const rect = this.canvas.getBoundingClientRect();
        return { x: Math.max(0, Math.min(1, (event.clientX-rect.left)/rect.width)), y: Math.max(0, Math.min(1, (event.clientY-rect.top)/rect.height)) };
    }
    down(event) {
        if (!this.enabled || (event.pointerType === 'mouse' && event.button !== 0) || this.pointer !== null) return;
        event.preventDefault();
        this.canvas.focus({ preventScroll: true });
        this.pointer = event.pointerId;
        this.canvas.setPointerCapture(event.pointerId);
        this.points = [this.point(event)];
        this.draw({ color: this.color, width: this.width, points: this.points });
    }
    move(event) {
        if (!this.enabled || this.pointer !== event.pointerId) return;
        const point = this.point(event);
        const last = this.points.at(-1);
        if (last && Math.hypot(point.x-last.x, point.y-last.y) < 0.0015) return;
        this.points.push(point);
        this.draw({ color: this.color, width: this.width, points: last ? [last, point] : [point] });
        if (this.points.length >= 22) this.flush(false);
    }
    up(event) {
        if (this.pointer !== event.pointerId) return;
        this.flush(true);
        if (this.canvas.hasPointerCapture(event.pointerId)) this.canvas.releasePointerCapture(event.pointerId);
        this.pointer = null;
    }
    key(event) {
        if (!this.enabled) return;
        const direction = { ArrowUp: [0, -0.025], ArrowDown: [0, 0.025], ArrowLeft: [-0.025, 0], ArrowRight: [0.025, 0] }[event.key];
        if (direction) {
            event.preventDefault();
            const old = { ...this.position };
            this.position = { x: Math.max(0, Math.min(1, old.x+direction[0])), y: Math.max(0, Math.min(1, old.y+direction[1])) };
            if (event.shiftKey) { this.points = [old, this.position]; this.flush(true); }
            this.paint(true);
        } else if (event.key === ' ' || event.key === 'Enter') {
            event.preventDefault(); this.points = [{ ...this.position }]; this.flush(true); this.paint(true);
        }
    }
    flush(finish) {
        if (!this.points.length || this.stopped) return;
        const stroke = { id: `${this.prefix}-${++this.counter}`, epoch: this.epoch, color: this.color, width: this.width, points: this.points.slice() };
        this.points = finish ? [] : [this.points.at(-1)];
        this.pending.set(stroke.id, stroke);
        this.enqueue({ type: 'stroke', roundId: this.roundId, stroke }, stroke.id);
    }
    enqueue(action, pendingId = null) {
        this.chain = this.chain.then(async () => {
            if (this.stopped) return;
            try { await this.send(action); }
            catch (error) { if (pendingId) this.pending.delete(pendingId); this.paint(); this.onError(error); }
        });
    }
    clear() {
        this.flush(true);
        this.setEnabled(false);
        this.enqueue({ type: 'clear', roundId: this.roundId });
    }
    sync(strokes, epoch) {
        if (epoch !== this.epoch) { this.epoch = epoch; this.pending.clear(); this.points = []; this.pointer = null; }
        this.strokes = strokes ?? [];
        for (const stroke of this.strokes) this.pending.delete(stroke.id);
        this.paint();
    }
    accept(stroke) {
        if (stroke.epoch !== this.epoch || this.strokes.some((item) => item.id === stroke.id)) return;
        this.strokes.push(stroke);
        if (this.pending.has(stroke.id)) this.pending.delete(stroke.id);
        else this.draw(stroke);
    }
    draw(stroke) {
        if (!this.context || !stroke.points.length) return;
        const c = this.context, w = this.canvas.width, h = this.canvas.height;
        c.strokeStyle = stroke.color; c.fillStyle = stroke.color; c.lineWidth = stroke.width;
        c.lineJoin = 'round'; c.lineCap = 'round'; c.beginPath();
        const first = stroke.points[0];
        if (stroke.points.length === 1) { c.arc(first.x*w, first.y*h, stroke.width/2, 0, Math.PI*2); c.fill(); return; }
        c.moveTo(first.x*w, first.y*h);
        for (const point of stroke.points.slice(1)) c.lineTo(point.x*w, point.y*h);
        c.stroke();
    }
    paint(cursor = false) {
        if (!this.context) return;
        const c = this.context;
        c.fillStyle = '#ffffff'; c.fillRect(0, 0, this.canvas.width, this.canvas.height);
        for (const stroke of this.strokes) this.draw(stroke);
        for (const stroke of this.pending.values()) this.draw(stroke);
        if (this.points.length) this.draw({ color: this.color, width: this.width, points: this.points });
        if (cursor) {
            const x = this.position.x*this.canvas.width, y = this.position.y*this.canvas.height;
            c.strokeStyle = '#20263a'; c.lineWidth = 2; c.beginPath(); c.arc(x, y, 10, 0, 2*Math.PI); c.stroke();
        }
    }
    destroy() { this.stopped = true; clearInterval(this.interval); this.events.abort(); this.pending.clear(); }
}
