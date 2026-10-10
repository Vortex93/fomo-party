package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"familyparty/internal/game"
)

func testAPI() *API {
	return New(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<h1>Family Party</h1>")}}, "")
}
func post(a *API, path, body, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "http://example.com"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w
}
func TestCreateRoomAndAPIValidation(t *testing.T) {
	a := testAPI()
	good := post(a, "/api/rooms", `{"name":"Alex"}`, "http://example.com")
	if good.Code != 201 {
		t.Fatal(good.Body.String())
	}
	var credentials Credentials
	if err := json.Unmarshal(good.Body.Bytes(), &credentials); err != nil {
		t.Fatal(err)
	}
	if len(credentials.Code) != 6 || len(credentials.Token) != 64 {
		t.Fatal("bad credentials")
	}
	for _, body := range []string{`{"name":"Alex","score":1000}`, `{"name":"Alex"}{}`, `{"name":"` + strings.Repeat("x", 13000) + `"}`} {
		if status := post(a, "/api/rooms", body, "").Code; status != 400 {
			t.Fatalf("bad request status %d", status)
		}
	}
	if got := post(a, "/api/action", `{}`, "").Code; got != 401 {
		t.Fatalf("anonymous action: %d", got)
	}
}
func TestCrossOriginAndContentType(t *testing.T) {
	a := testAPI()
	for _, origin := range []string{"https://evil.example", "null", "http://example.com.evil.test", "http://example.com/path", "http://attacker@example.com"} {
		if got := post(a, "/api/rooms", `{"name":"Alex"}`, origin).Code; got != 403 {
			t.Fatalf("origin %s accepted", origin)
		}
	}
	r := httptest.NewRequest("POST", "/api/rooms", strings.NewReader(`{"name":"Alex"}`))
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("text/plain accepted")
	}
}
func TestStaticFilesAndSecurityHeaders(t *testing.T) {
	a := testAPI()
	for _, path := range []string{"/embed.go", "/lib/", "/../go.mod", "/missing.js"} {
		w := httptest.NewRecorder()
		a.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Fatalf("%s returned %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || w.Header().Get("Content-Security-Policy") == "" || w.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("missing page/headers")
	}
}
func TestReconnectReplacementAndAuthority(t *testing.T) {
	h := NewHub()
	credentials, _ := h.Create("Alex")
	old, err := h.Subscribe(credentials.Code, credentials.Token)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := h.Subscribe(credentials.Code, credentials.Token)
	if err != nil {
		t.Fatal(err)
	}
	h.Unsubscribe(credentials.Code, old)
	if !h.rooms[credentials.Code].game.Player(credentials.ID).Connected {
		t.Fatal("old stream disconnected replacement")
	}
	if _, err := h.Subscribe(credentials.Code, strings.Repeat("0", 64)); err == nil {
		t.Fatal("invalid token accepted")
	}
	h.Unsubscribe(credentials.Code, replacement)
	if h.rooms[credentials.Code].game.Player(credentials.ID).Connected {
		t.Fatal("disconnect not recorded")
	}
}
func TestWatchOnlyAndSessionSeparation(t *testing.T) {
	h := NewHub()
	c, _ := h.Create("Alex")
	viewer, _ := h.Join(c.Code, "", true)
	s, _ := h.Subscribe(viewer.Code, viewer.Token)
	defer h.Unsubscribe(viewer.Code, s)
	if err := h.Act(viewer.Code, viewer.Token, game.Action{Type: "ready", Value: "true"}); err == nil {
		t.Fatal("viewer could play")
	}
	other, _ := h.Create("Morgan")
	if _, err := h.Subscribe(other.Code, c.Token); err == nil {
		t.Fatal("token crossed rooms")
	}
}
func TestRemoveRevokesSession(t *testing.T) {
	h := NewHub()
	c, _ := h.Create("Alex")
	other, _ := h.Join(c.Code, "Morgan", false)
	s, _ := h.Subscribe(c.Code, c.Token)
	defer h.Unsubscribe(c.Code, s)
	if err := h.Act(c.Code, c.Token, game.Action{Type: "remove", PlayerID: other.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Subscribe(c.Code, other.Token); err == nil {
		t.Fatal("removed token still valid")
	}
}
func TestSlowConsumerIsDisconnected(t *testing.T) {
	h := NewHub()
	c, _ := h.Create("Alex")
	s, _ := h.Subscribe(c.Code, c.Token)
	h.mu.Lock()
	for i := 0; i < 40; i++ {
		h.broadcast(h.rooms[c.Code])
	}
	h.mu.Unlock()
	select {
	case <-s.done:
	default:
		t.Fatal("slow consumer was not closed")
	}
	h.Unsubscribe(c.Code, s)
}
func TestRoomIdleExpiry(t *testing.T) {
	h := NewHub()
	now := time.Now()
	h.now = func() time.Time { return now }
	c, _ := h.Create("Alex")
	now = now.Add(31 * time.Minute)
	h.Tick()
	if h.rooms[c.Code] != nil {
		t.Fatal("idle room was retained")
	}
}
func TestSSEThroughHTTP(t *testing.T) {
	a := testAPI()
	c, _ := a.Hub.Create("Alex")
	ts := httptest.NewServer(a)
	defer ts.Close()
	req, _ := http.NewRequest("GET", ts.URL+"/api/events", nil)
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("X-Room-Code", c.Code)
	response, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal("not an event stream")
	}
	scanner := bufio.NewScanner(response.Body)
	received := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			if !strings.Contains(line, `"type":"state"`) || strings.Contains(line, c.Token) {
				t.Fatal("unsafe/invalid initial event")
			}
			received = true
			break
		}
	}
	if !received {
		t.Fatal("did not receive snapshot")
	}
	body := bytes.NewBufferString(`{"type":"ready","value":"true"}`)
	action, _ := http.NewRequest("POST", ts.URL+"/api/action", body)
	action.Header.Set("Authorization", "Bearer "+c.Token)
	action.Header.Set("X-Room-Code", c.Code)
	action.Header.Set("Content-Type", "application/json")
	out, err := ts.Client().Do(action)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Body.Close()
	if out.StatusCode != 200 {
		b, _ := io.ReadAll(out.Body)
		t.Fatal(string(b))
	}
}
func TestConcurrentHubAccess(t *testing.T) {
	h := NewHub()
	c, _ := h.Create("Alex")
	s, _ := h.Subscribe(c.Code, c.Token)
	defer h.Unsubscribe(c.Code, s)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.Tick()
			_ = h.Act(c.Code, c.Token, game.Action{Type: "ready", Value: "true"})
		}()
	}
	wg.Wait()
}
func TestPublicURLValidation(t *testing.T) {
	for _, value := range []string{"", "https://games.example.com", "http://192.168.1.5:8080/"} {
		if err := ValidatePublicURL(value); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"ftp://example.com", "//example.com", "https://user:pass@example.com", "https://example.com/path", "https://example.com/?q=1"} {
		if ValidatePublicURL(value) == nil {
			t.Fatalf("accepted %s", value)
		}
	}
}
func TestRateLimiter(t *testing.T) {
	l := &limiter{items: map[string]*bucket{}}
	if !l.allow("test", 2, 0) || !l.allow("test", 2, 0) || l.allow("test", 2, 0) {
		t.Fatal("bucket failed")
	}
}

func TestReplacementSendsStopReason(t *testing.T) {
	h := NewHub()
	c, _ := h.Create("Alex")
	first, _ := h.Subscribe(c.Code, c.Token)
	second, _ := h.Subscribe(c.Code, c.Token)
	defer h.Unsubscribe(c.Code, second)
	<-first.done
	if first.reason != "replaced" {
		t.Fatal("old tab must stop automatic reconnecting")
	}
}
