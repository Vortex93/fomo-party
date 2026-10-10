package game

import (
	"strings"
	"time"
	"unicode/utf8"
)

// View types are a whitelist. Never serialize Room, Round, or Player directly:
// they contain bearer credentials and hidden answers.
type PlayerView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Score     int    `json:"score"`
	Ready     bool   `json:"ready"`
	Connected bool   `json:"connected"`
}

type PublicAnswer struct {
	PlayerID string `json:"playerId"`
	Name     string `json:"name"`
	Text     string `json:"text"`
}
type PrivateView struct {
	Role    string `json:"role"`
	Answer  string `json:"answer"`
	Vote    string `json:"vote"`
	Guessed bool   `json:"guessed"`
	CanPlay bool   `json:"canPlay"`
}

type RoundView struct {
	ID             string         `json:"id"`
	Game           GameID         `json:"game"`
	Number         int            `json:"number"`
	Phase          string         `json:"phase"`
	Deadline       int64          `json:"deadline"`
	ServerTime     int64          `json:"serverTime"`
	RemainingMS    int64          `json:"remainingMs"`
	Paused         bool           `json:"paused"`
	Prompt         string         `json:"prompt"`
	Topic          string         `json:"topic,omitempty"`
	Word           string         `json:"word,omitempty"`
	WordHint       string         `json:"wordHint,omitempty"`
	DrawerID       string         `json:"drawerId,omitempty"`
	Sequence       []int          `json:"sequence,omitempty"`
	SequenceLength int            `json:"sequenceLength,omitempty"`
	Submitted      []string       `json:"submitted"`
	Participants   []string       `json:"participants"`
	PublicAnswers  []PublicAnswer `json:"publicAnswers"`
	Strokes        []Stroke       `json:"strokes"`
	CanvasEpoch    int            `json:"canvasEpoch"`
	Chat           []Guess        `json:"chat"`
	Result         *Result        `json:"result,omitempty"`
	You            PrivateView    `json:"you"`
}

type View struct {
	Code        string       `json:"code"`
	Status      string       `json:"status"`
	HostID      string       `json:"hostId"`
	YouID       string       `json:"youId"`
	IsViewer    bool         `json:"isViewer"`
	ServerTime  int64        `json:"serverTime"`
	Config      Config       `json:"config"`
	Players     []PlayerView `json:"players"`
	TotalRounds int          `json:"totalRounds"`
	MinPlayers  int          `json:"minPlayers"`
	Round       *RoundView   `json:"round,omitempty"`
}

func (r *Room) View(id string, now time.Time) View {
	view := View{
		Code: r.Code, Status: r.Status, HostID: r.HostID, YouID: id,
		IsViewer: r.Player(id) == nil, ServerTime: now.UnixMilli(), Config: r.Config,
		MinPlayers: r.Config.Minimum(), TotalRounds: len(r.Playlist), Players: []PlayerView{},
	}
	for _, p := range r.Players {
		view.Players = append(view.Players, PlayerView{p.ID, p.Name, p.Score, p.Ready, p.Connected})
	}
	if r.Round == nil {
		return view
	}
	rd := r.Round
	remaining := rd.Deadline.Sub(now).Milliseconds()
	if rd.Deadline.IsZero() {
		remaining = 0
	}
	if rd.Paused {
		remaining = rd.Remaining.Milliseconds()
	}
	if remaining < 0 {
		remaining = 0
	}
	rv := &RoundView{
		ID: rd.ID, Game: rd.Game, Number: rd.Number, Phase: rd.Phase,
		ServerTime: now.UnixMilli(), RemainingMS: remaining, Paused: rd.Paused,
		Prompt: rd.Prompt, Topic: rd.Topic, DrawerID: rd.DrawerID,
		Participants: append([]string{}, rd.Participants...), Submitted: []string{},
		PublicAnswers: []PublicAnswer{}, Strokes: rd.Strokes, CanvasEpoch: rd.CanvasEpoch,
		Chat: rd.Chat, Result: rd.Result,
		You: PrivateView{Role: "player", Answer: rd.Answers[id], Vote: rd.Votes[id], Guessed: rd.Guessed[id], CanPlay: contains(rd.Participants, id)},
	}
	if !rd.Deadline.IsZero() {
		rv.Deadline = rd.Deadline.UnixMilli()
	}
	for _, pid := range rd.Participants {
		submitted := rd.Answers[pid] != ""
		if rd.Phase == "vote" {
			submitted = rd.Votes[pid] != ""
		}
		if rd.Game == DrawGuess {
			submitted = rd.Guessed[pid]
		}
		if submitted {
			rv.Submitted = append(rv.Submitted, pid)
		}
	}
	switch rd.Game {
	case WhosLying:
		if id == rd.LiarID {
			rv.You.Role = "bluffer"
		} else if !view.IsViewer {
			rv.You.Role = "detective"
		}
		if (id == rd.LiarID || view.IsViewer) && (rd.Phase == "intro" || rd.Phase == "answer") {
			rv.Prompt = "The topic is " + rd.Topic + ". Make a believable short answer without seeing the question."
		}
		if rd.Phase == "discuss" || rd.Phase == "vote" || rd.Phase == "reveal" {
			for _, pid := range rd.Participants {
				answer := rd.Answers[pid]
				if answer == "" {
					answer = "No answer"
				}
				rv.PublicAnswers = append(rv.PublicAnswers, PublicAnswer{pid, r.Player(pid).Name, answer})
			}
		}
	case DrawGuess:
		if id == rd.DrawerID {
			rv.You.Role = "drawer"
		}
		if id == rd.DrawerID || rd.Phase == "reveal" {
			rv.Word = rd.Secret
		}
		hints := []string{}
		for _, word := range strings.Fields(rd.Secret) {
			hints = append(hints, strings.Repeat("_", utf8.RuneCountInString(word)))
		}
		rv.WordHint = strings.Join(hints, " ")
	case Memory:
		rv.SequenceLength = len(rd.Sequence)
		if rd.Phase == "memorize" || rd.Phase == "reveal" {
			rv.Sequence = append([]int(nil), rd.Sequence...)
		}
	}
	view.Round = rv
	return view
}
