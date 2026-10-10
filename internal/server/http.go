package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"familyparty/internal/game"
)

type API struct {
	Hub       *Hub
	publicURL string
	limiter   *limiter
	static    http.Handler
}

func New(assets fs.FS, publicURL string) *API {
	return &API{Hub: NewHub(), publicURL: strings.TrimSuffix(publicURL, "/"), limiter: &limiter{items: map[string]*bucket{}}, static: http.FileServer(http.FS(assets))}
}

func (a *API) Run(ctx context.Context) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	defer a.Hub.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.Hub.Tick()
		}
	}
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			fail(w, 405, "Use GET for app files.")
			return
		}
		// Do not serve the internal Go source or directory listings.
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		allowed := path == "index.html" || path == "styles.css" || path == "app.js" || path == "lib/api.js" || path == "lib/ui.js" || path == "lib/canvas.js" || path == "lib/qr.js"
		if !allowed {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		a.static.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if !sameOrigin(r) {
		fail(w, 403, "Open the game from its own website.")
		return
	}
	if r.URL.Path == "/api/health" && r.Method == http.MethodGet {
		respond(w, 200, map[string]string{"status": "ok"})
		return
	}
	if r.URL.Path == "/api/info" && r.Method == http.MethodGet {
		respond(w, 200, map[string]any{"games": game.Catalog, "defaults": game.DefaultConfig(), "publicURL": a.publicURL, "maxPlayers": game.MaxPlayers})
		return
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if r.URL.Path == "/api/events" && r.Method == http.MethodGet {
		if !a.limiter.allow("stream:"+ip, 60, 2) {
			fail(w, 429, "Too many reconnects. Try again in a moment.")
			return
		}
		a.events(w, r)
		return
	}
	if r.Method != http.MethodPost {
		fail(w, 405, "Use POST for this action.")
		return
	}
	if r.URL.Path == "/api/rooms" || r.URL.Path == "/api/join" || r.URL.Path == "/api/watch" {
		if !a.limiter.allow("entry:"+ip, 40, 0.5) {
			fail(w, 429, "Too many room requests. Try again in a moment.")
			return
		}
		var request struct {
			Name string `json:"name"`
			Code string `json:"code"`
		}
		if err := readJSON(w, r, &request); err != nil {
			fail(w, 400, err.Error())
			return
		}
		var credentials Credentials
		var err error
		if r.URL.Path == "/api/rooms" {
			credentials, err = a.Hub.Create(request.Name)
		} else {
			credentials, err = a.Hub.Join(strings.ToUpper(strings.TrimSpace(request.Code)), request.Name, r.URL.Path == "/api/watch")
		}
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		respond(w, 201, credentials)
		return
	}
	code, token := credentials(r)
	if code == "" || token == "" {
		fail(w, 401, "A room session is required.")
		return
	}
	if !a.limiter.allow("action:"+token, 60, 30) {
		fail(w, 429, "Too many actions. Slow down a little.")
		return
	}
	switch r.URL.Path {
	case "/api/action":
		var action game.Action
		if err := readJSON(w, r, &action); err != nil {
			fail(w, 400, err.Error())
			return
		}
		if err := a.Hub.Act(code, token, action); err != nil {
			fail(w, 409, err.Error())
			return
		}
	case "/api/leave":
		if err := a.Hub.Leave(code, token); err != nil {
			fail(w, 401, err.Error())
			return
		}
	default:
		fail(w, 404, "Unknown API route.")
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func credentials(r *http.Request) (string, string) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == r.Header.Get("Authorization") || len(token) != 64 {
		return "", ""
	}
	code := strings.ToUpper(r.Header.Get("X-Room-Code"))
	if len(code) != 6 {
		return "", ""
	}
	return code, token
}

// Credentials stay in request headers, not URLs, access logs, or QR codes.
// fetch() reads this SSE stream so it can send an Authorization header.
func (a *API) events(w http.ResponseWriter, r *http.Request) {
	code, token := credentials(r)
	s, err := a.Hub.Subscribe(code, token)
	if err != nil {
		fail(w, 401, err.Error())
		return
	}
	defer a.Hub.Unsubscribe(code, s)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	write := func(data []byte) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(8 * time.Second))
		if _, err := w.Write(data); err != nil {
			return false
		}
		if err := rc.Flush(); err != nil {
			return false
		}
		_ = rc.SetWriteDeadline(time.Time{})
		return true
	}
	if !write([]byte(": connected\n\n")) {
		return
	}
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.done:
			if s.reason == "replaced" {
				data := envelope("replaced", map[string]string{"message": "This player is now active in another tab. Use that tab, or leave this view and join with a different name."})
				_ = write(append(append([]byte("data: "), data...), '\n', '\n'))
			}
			return
		case data := <-s.events:
			if !write(append(append([]byte("data: "), data...), '\n', '\n')) {
				return
			}
		case <-heartbeat.C:
			if !write([]byte(": heartbeat\n\n")) {
				return
			}
		}
	}
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	} // native clients have no ambient cookie credentials
	parsed, err := url.Parse(origin)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && strings.EqualFold(parsed.Host, r.Host) && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.User == nil
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return errors.New("Send application/json.")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 12*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return errors.New("The request is invalid or too large.")
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return errors.New("Send a single JSON object.")
	}
	return nil
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, message string) {
	if status == 429 {
		w.Header().Set("Retry-After", "2")
	}
	respond(w, status, map[string]string{"error": message})
}

type bucket struct {
	tokens float64
	last   time.Time
}
type limiter struct {
	mu      sync.Mutex
	items   map[string]*bucket
	cleaned time.Time
}

func (l *limiter) allow(key string, capacity, refill float64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Sub(l.cleaned) > time.Minute {
		for k, b := range l.items {
			if now.Sub(b.last) > 10*time.Minute {
				delete(l.items, k)
			}
		}
		l.cleaned = now
	}
	b := l.items[key]
	if b == nil {
		if len(l.items) >= 10000 {
			return false
		}
		b = &bucket{tokens: capacity, last: now}
		l.items[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * refill
	if b.tokens > capacity {
		b.tokens = capacity
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// ValidatePublicURL rejects credentials, query strings, and paths. The app is
// served at the origin root and must be deployed there, not at a subpath.
func ValidatePublicURL(value string) error {
	if value == "" {
		return nil
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("public-url must be an http(s) origin, such as https://games.example.com")
	}
	return nil
}
