package server

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"familyparty/internal/game"
)

// Hub serializes room state. Channels are bounded, and slow readers reconnect
// for a fresh snapshot rather than silently losing drawing events.
type Hub struct {
	mu    sync.Mutex
	rooms map[string]*roomEntry
	now   func() time.Time
}

type roomEntry struct {
	game    *game.Room
	viewers map[string]string // token -> viewer ID
	streams map[string]*subscription
}

type subscription struct {
	id     string
	events chan []byte
	done   chan struct{}
	once   sync.Once
	reason string
}

func (s *subscription) close() { s.closeReason("") }
func (s *subscription) closeReason(reason string) {
	s.once.Do(func() { s.reason = reason; close(s.done) })
}

func NewHub() *Hub { return &Hub{rooms: map[string]*roomEntry{}, now: time.Now} }

func envelope(kind string, value any) []byte {
	data, err := json.Marshal(struct {
		Type string `json:"type"`
		Data any    `json:"data"`
	}{kind, value})
	if err != nil {
		panic("invalid internal event: " + err.Error())
	}
	return data
}

func (h *Hub) broadcast(e *roomEntry) {
	now := h.now()
	for id, s := range e.streams {
		select {
		case <-s.done:
			continue
		default:
		}
		data := envelope("state", e.game.View(id, now))
		select {
		case s.events <- data:
		default:
			s.close()
		}
	}
}

func (h *Hub) broadcastStroke(e *roomEntry, stroke game.Stroke) {
	data := envelope("stroke", struct {
		RoundID string      `json:"roundId"`
		Stroke  game.Stroke `json:"stroke"`
	}{e.game.Round.ID, stroke})
	for _, s := range e.streams {
		select {
		case <-s.done:
			continue
		default:
		}
		select {
		case s.events <- data:
		default:
			s.close()
		}
	}
}

func roomCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	token := game.Token(6)
	// Each hex byte pair contributes an unbiased 5-bit alphabet index.
	bytes := []byte(token)
	code := make([]byte, 6)
	for i := range code {
		n := func(c byte) byte {
			if c <= '9' {
				return c - '0'
			}
			return c - 'a' + 10
		}
		code[i] = alphabet[((n(bytes[i*2])<<4)|n(bytes[i*2+1]))&31]
	}
	return string(code)
}

type Credentials struct {
	Code  string `json:"code"`
	ID    string `json:"id"`
	Token string `json:"token"`
	Watch bool   `json:"watch"`
}

func (h *Hub) Create(name string) (Credentials, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.rooms) >= 200 {
		return Credentials{}, errors.New("The server is full. Try again after a room closes.")
	}
	code := roomCode()
	for h.rooms[code] != nil {
		code = roomCode()
	}
	r, p, err := game.NewRoom(code, name, h.now())
	if err != nil {
		return Credentials{}, err
	}
	h.rooms[code] = &roomEntry{game: r, viewers: map[string]string{}, streams: map[string]*subscription{}}
	return Credentials{code, p.ID, p.Token, false}, nil
}

func (h *Hub) Join(code, name string, watch bool) (Credentials, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.rooms[code]
	if e == nil {
		return Credentials{}, errors.New("Room not found. Check the code, or ask the host to create a new room.")
	}
	if watch {
		if len(e.viewers) >= 8 {
			return Credentials{}, errors.New("This room already has eight TV/viewer sessions.")
		}
		token, id := game.Token(32), "viewer-"+game.Token(8)
		e.viewers[token] = id
		e.game.LastActivity = h.now()
		return Credentials{code, id, token, true}, nil
	}
	p, err := e.game.Join(name, h.now())
	if err != nil {
		return Credentials{}, err
	}
	h.broadcast(e)
	return Credentials{code, p.ID, p.Token, false}, nil
}

func (h *Hub) auth(code, token string) (*roomEntry, string, error) {
	e := h.rooms[code]
	if e == nil || len(token) != 64 {
		return nil, "", errors.New("This session expired. Return to the home screen to join again.")
	}
	for _, p := range e.game.Players {
		if p.Token == token {
			return e, p.ID, nil
		}
	}
	if id := e.viewers[token]; id != "" {
		return e, id, nil
	}
	return nil, "", errors.New("This session is no longer valid. Return to the home screen.")
}

func (h *Hub) Subscribe(code, token string) (*subscription, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, id, err := h.auth(code, token)
	if err != nil {
		return nil, err
	}
	if old := e.streams[id]; old != nil {
		old.closeReason("replaced")
	}
	s := &subscription{id: id, events: make(chan []byte, 32), done: make(chan struct{})}
	e.streams[id] = s
	e.game.Connected(id, true, h.now())
	h.broadcast(e)
	return s, nil
}

func (h *Hub) Unsubscribe(code string, s *subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s.close()
	e := h.rooms[code]
	if e == nil || e.streams[s.id] != s {
		return
	} // an older connection must not disconnect its replacement
	delete(e.streams, s.id)
	e.game.Connected(s.id, false, h.now())
	h.broadcast(e)
}

func (h *Hub) Act(code, token string, action game.Action) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, id, err := h.auth(code, token)
	if err != nil {
		return err
	}
	if e.game.Tick(h.now()) {
		h.broadcast(e)
	}
	event, err := e.game.Handle(id, action, h.now())
	if err != nil {
		return err
	}
	if action.Type == "remove" {
		if s := e.streams[action.PlayerID]; s != nil {
			s.close()
			delete(e.streams, action.PlayerID)
		}
	}
	switch event {
	case "stroke":
		h.broadcastStroke(e, action.Stroke)
	case "state":
		h.broadcast(e)
	}
	return nil
}

func (h *Hub) Leave(code, token string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, id, err := h.auth(code, token)
	if err != nil {
		return err
	}
	if s := e.streams[id]; s != nil {
		s.close()
		delete(e.streams, id)
	}
	if _, viewer := e.viewers[token]; viewer {
		delete(e.viewers, token)
	} else {
		e.game.Connected(id, false, h.now())
		if e.game.Status == "lobby" {
			for i, p := range e.game.Players {
				if p.ID == id {
					e.game.Players = append(e.game.Players[:i], e.game.Players[i+1:]...)
					break
				}
			}
		}
	}
	e.game.Tick(h.now())
	h.broadcast(e)
	return nil
}

func (h *Hub) Tick() {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	for code, e := range h.rooms {
		if (len(e.streams) == 0 && now.Sub(e.game.LastActivity) > 30*time.Minute) || now.Sub(e.game.CreatedAt) > 24*time.Hour {
			for _, s := range e.streams {
				s.close()
			}
			delete(h.rooms, code)
			continue
		}
		if e.game.Tick(now) {
			h.broadcast(e)
		}
	}
}

func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range h.rooms {
		for _, s := range e.streams {
			s.close()
		}
	}
}
