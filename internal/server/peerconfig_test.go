package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestPeerConfiguration(t *testing.T) {
	a := New(fstest.MapFS{}, "")
	if err := a.ConfigurePeers("https://example.com/party/", `[{"urls":["turn:relay.example.com:3478?transport=udp"],"username":"user","credential":"secret"}]`); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("GET", "/api/info", nil))
	if !strings.Contains(w.Body.String(), `"internetURL":"https://example.com/party/"`) || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "iceServers") {
		t.Fatal(w.Body.String())
	}
	creator, err := a.Hub.Create("Host")
	if err != nil {
		t.Fatal(err)
	}
	guest, err := a.Hub.Join(creator.Code, "Guest", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range []Credentials{{}, guest, creator} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/peer-config", strings.NewReader("{}"))
		r.Header.Set("Authorization", "Bearer "+session.Token)
		r.Header.Set("X-Room-Code", session.Code)
		r.Header.Set("Content-Type", "application/json")
		a.ServeHTTP(w, r)
		if session.Token == creator.Token {
			var body map[string]any
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || !strings.Contains(w.Body.String(), "secret") {
				t.Fatal(w.Code, "missing authenticated config")
			}
		} else if w.Code == 200 || strings.Contains(w.Body.String(), "secret") {
			t.Fatal("exposed TURN credentials")
		}
	}
	for _, raw := range []string{`[]`, `[{"urls":["https://evil.example"]}]`, `[{"urls":["turn:relay.example.com"]}]`, `[{"urls":["stun:"]}]`, `[{"urls":["stun:user@host"]}]`} {
		if err := a.ConfigurePeers("https://example.com/", raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if err := a.ConfigurePeers("http://example.com/", ""); err == nil {
		t.Fatal("accepted insecure guest URL")
	}
}

func TestPeerAssetsAndSignalingPolicy(t *testing.T) {
	a := New(fstest.MapFS{"lib/peer.js": &fstest.MapFile{Data: []byte("export const test = 1;")}}, "")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("GET", "/lib/peer.js", nil))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "wss://0.peerjs.com") || strings.Contains(csp, "connect-src *") {
		t.Fatal(csp)
	}
}
