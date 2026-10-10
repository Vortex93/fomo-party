package game

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Handle performs authorization and phase checks BEFORE changing state.
// Its event name tells the transport whether it can send a small drawing delta.
func (r *Room) Handle(playerID string, a Action, now time.Time) (string, error) {
	p := r.Player(playerID)
	if p == nil {
		return "", errors.New("Only players can do that.")
	}
	host := playerID == r.HostID
	admin := a.Type == "configure" || a.Type == "start" || a.Type == "next" || a.Type == "pause" || a.Type == "skip" || a.Type == "rematch" || a.Type == "remove"
	if admin && !host {
		return "", errors.New("Only the host can do that.")
	}
	if !p.Connected {
		return "", errors.New("Reconnect to the room before playing.")
	}
	switch a.Type {
	case "configure":
		if r.Status != "lobby" {
			return "", errors.New("Change the playlist in the lobby.")
		}
		if err := a.Config.Validate(); err != nil {
			return "", err
		}
		r.Config = a.Config
	case "ready":
		if r.Status != "lobby" {
			return "", errors.New("The game has already started.")
		}
		p.Ready = a.Value == "true"
	case "remove":
		if r.Status != "lobby" || a.PlayerID == r.HostID {
			return "", errors.New("Only other players can be removed, and only in the lobby.")
		}
		if r.Player(a.PlayerID) == nil {
			return "", errors.New("That player is no longer here.")
		}
		for i, other := range r.Players {
			if other.ID == a.PlayerID {
				r.Players = append(r.Players[:i], r.Players[i+1:]...)
				break
			}
		}
	case "start":
		if r.Status != "lobby" {
			return "", errors.New("A game is already in progress.")
		}
		if err := r.Config.Validate(); err != nil {
			return "", err
		}
		ids := []string{}
		for _, player := range r.Players {
			if !player.Connected {
				continue
			}
			if !player.Ready {
				return "", errors.New("Every connected player must be ready first.")
			}
			ids = append(ids, player.ID)
		}
		if len(ids) < r.Config.Minimum() {
			return "", fmt.Errorf("This playlist needs at least %d connected players.", r.Config.Minimum())
		}
		r.Roster, r.LiarOrder = ids, shuffle(ids)
		r.Playlist = nil
		for i := 0; i < r.Config.Rounds; i++ {
			r.Playlist = append(r.Playlist, r.Config.Games...)
		}
		r.Cursor, r.DrawTurns, r.LiarTurns, r.MemoryTurns = -1, 0, 0, 0
		for _, player := range r.Players {
			player.Score = 0
		}
		r.Status = "playing"
		r.nextRound(now)
	case "rematch":
		if r.Status != "finished" {
			return "", errors.New("Finish the game before returning to the lobby.")
		}
		r.Status, r.Round, r.Playlist, r.Roster = "lobby", nil, nil, nil
		for _, player := range r.Players {
			player.Score = 0
			player.Ready = player.ID == r.HostID
		}
	default:
		rd := r.Round
		if r.Status != "playing" || rd == nil || a.RoundID != rd.ID {
			return "", errors.New("That round has ended. Wait for the current screen.")
		}
		if a.Type == "next" {
			if rd.Phase != "reveal" {
				return "", errors.New("Wait for the round results.")
			}
			r.nextRound(now)
		} else if a.Type == "skip" {
			if rd.Phase == "reveal" {
				return "", errors.New("This round is already over.")
			}
			r.finish(now, true)
		} else if a.Type == "pause" {
			if rd.Phase == "reveal" {
				return "", errors.New("Results do not need a pause.")
			}
			if rd.Paused {
				rd.Deadline = now.Add(rd.Remaining)
				rd.Paused = false
			} else {
				rd.Remaining = rd.Deadline.Sub(now)
				if rd.Remaining < 0 {
					rd.Remaining = 0
				}
				rd.Paused = true
			}
		} else {
			if rd.Paused {
				return "", errors.New("The host paused the game.")
			}
			if !rd.Deadline.IsZero() && !now.Before(rd.Deadline) {
				return "", errors.New("Time is up for this phase.")
			}
			if !contains(rd.Participants, playerID) {
				return "", errors.New("You can play in the next game night.")
			}
			event, err := r.play(p, a, now)
			if err != nil {
				return "", err
			}
			r.LastActivity = now
			return event, nil
		}
	}
	r.LastActivity = now
	return "state", nil
}

func (r *Room) nextRound(now time.Time) {
	r.Cursor++
	if r.Cursor >= len(r.Playlist) {
		r.Status = "finished"
		return
	}
	rd := &Round{
		ID: Token(8), Game: r.Playlist[r.Cursor], Number: r.Cursor + 1,
		Participants: append([]string(nil), r.Roster...),
		Answers:      map[string]string{}, Votes: map[string]string{}, Guessed: map[string]bool{},
		LastGuess: map[string]time.Time{}, StrokeIDs: map[string]bool{}, Strokes: []Stroke{}, Chat: []Guess{},
	}
	switch rd.Game {
	case SameBrain:
		rd.Prompt = brainPrompts[r.promptIndex(SameBrain, len(brainPrompts))]
	case WhosLying:
		prompt := liarPrompts[r.promptIndex(WhosLying, len(liarPrompts))]
		rd.Prompt, rd.Topic = prompt.Question, prompt.Topic
		rd.LiarID = r.LiarOrder[r.LiarTurns%len(r.LiarOrder)]
		r.LiarTurns++
	case DrawGuess:
		rd.Secret = drawingWords[r.promptIndex(DrawGuess, len(drawingWords))]
		rd.Prompt = "Draw the secret word. Everyone else, take a guess!"
		// Prefer a connected drawer, but do not silently mutate the scoring roster.
		for i := 0; i < len(r.Roster); i++ {
			rd.DrawerID = r.Roster[r.DrawTurns%len(r.Roster)]
			r.DrawTurns++
			if p := r.Player(rd.DrawerID); p != nil && p.Connected {
				break
			}
		}
	case MostLikely:
		rd.Prompt = likelyPrompts[r.promptIndex(MostLikely, len(likelyPrompts))]
	case Memory:
		rd.Prompt = "Remember these symbols in order."
		length := 4 + r.MemoryTurns
		if length > 8 {
			length = 8
		}
		for i := 0; i < length; i++ {
			n := randomN(9)
			for i > 0 && n == rd.Sequence[i-1] {
				n = randomN(9)
			}
			rd.Sequence = append(rd.Sequence, n)
		}
		r.MemoryTurns++
	}
	r.Round = rd
	r.setPhase("intro", 3*time.Second, now)
}

func (r *Room) setPhase(phase string, duration time.Duration, now time.Time) {
	rd := r.Round
	rd.Phase, rd.PhaseStarted, rd.Paused = phase, now, false
	rd.Deadline, rd.Remaining = now.Add(duration), duration
	if duration == 0 {
		rd.Deadline = time.Time{}
	}
}

// Tick is called on the server clock, never on a player's clock.
func (r *Room) Tick(now time.Time) bool {
	changed := false
	host := r.Player(r.HostID)
	if host == nil || (!host.Connected && now.Sub(host.LastSeen) >= 15*time.Second) {
		for _, p := range r.Players {
			if p.Connected {
				r.HostID = p.ID
				if r.Status == "lobby" {
					p.Ready = true
				}
				changed = true
				break
			}
		}
	}
	rd := r.Round
	if r.Status != "playing" || rd == nil || rd.Paused || rd.Deadline.IsZero() || now.Before(rd.Deadline) {
		return changed
	}
	seconds := time.Duration(r.Config.Seconds) * time.Second
	switch rd.Phase {
	case "intro":
		switch rd.Game {
		case Memory:
			r.setPhase("memorize", time.Duration(len(rd.Sequence)+2)*time.Second, now)
		case DrawGuess:
			r.setPhase("draw", seconds*2, now)
		case MostLikely:
			r.setPhase("vote", seconds, now)
		default:
			r.setPhase("answer", seconds, now)
		}
	case "memorize":
		r.setPhase("recall", seconds, now)
	case "answer":
		if rd.Game == WhosLying {
			r.setPhase("discuss", 12*time.Second, now)
		} else {
			r.finish(now, false)
		}
	case "discuss":
		r.setPhase("vote", seconds, now)
	case "draw", "vote", "recall":
		r.finish(now, false)
	}
	return true
}

func (r *Room) play(p *Player, a Action, now time.Time) (string, error) {
	rd := r.Round
	switch a.Type {
	case "answer":
		if rd.Phase != "answer" || (rd.Game != SameBrain && rd.Game != WhosLying) {
			return "", errors.New("Answers are not open right now.")
		}
		if _, exists := rd.Answers[p.ID]; exists {
			return "", errors.New("Your answer is already locked in.")
		}
		value, err := cleanText(a.Value, 48)
		if err != nil {
			return "", err
		}
		if Normalize(value) == "" {
			return "", errors.New("Include at least one letter or number.")
		}
		rd.Answers[p.ID] = value
		if len(rd.Answers) == len(rd.Participants) {
			if rd.Game == WhosLying {
				r.setPhase("discuss", 12*time.Second, now)
			} else {
				r.finish(now, false)
			}
		}
	case "vote":
		if rd.Phase != "vote" || (rd.Game != WhosLying && rd.Game != MostLikely) {
			return "", errors.New("Voting is not open right now.")
		}
		if !contains(rd.Participants, a.Value) {
			return "", errors.New("Choose a player in this round.")
		}
		if rd.Game == WhosLying && a.Value == p.ID {
			return "", errors.New("Vote for someone other than yourself.")
		}
		if _, exists := rd.Votes[p.ID]; exists {
			return "", errors.New("Your vote is already locked in.")
		}
		rd.Votes[p.ID] = a.Value
		if len(rd.Votes) == len(rd.Participants) {
			r.finish(now, false)
		}
	case "memory":
		if rd.Game != Memory || rd.Phase != "recall" {
			return "", errors.New("Wait for the recall phase.")
		}
		if _, exists := rd.Answers[p.ID]; exists {
			return "", errors.New("Your sequence is already locked in.")
		}
		if len(a.Sequence) != len(rd.Sequence) {
			return "", errors.New("Fill every position before submitting.")
		}
		parts := []string{}
		for _, n := range a.Sequence {
			if n < 0 || n > 8 {
				return "", errors.New("Choose symbols from the board.")
			}
			parts = append(parts, strconv.Itoa(n))
		}
		rd.Answers[p.ID] = strings.Join(parts, ",")
		if len(rd.Answers) == len(rd.Participants) {
			r.finish(now, false)
		}
	case "guess":
		if rd.Game != DrawGuess || rd.Phase != "draw" || rd.DrawerID == p.ID {
			return "", errors.New("Only the other players can guess during drawing.")
		}
		if rd.Guessed[p.ID] {
			return "", errors.New("You already found it. Keep the word a secret!")
		}
		if !rd.LastGuess[p.ID].IsZero() && now.Sub(rd.LastGuess[p.ID]) < 800*time.Millisecond {
			return "", errors.New("Give it a moment before guessing again.")
		}
		value, err := cleanText(a.Value, 48)
		if err != nil {
			return "", err
		}
		rd.LastGuess[p.ID] = now
		correct := Normalize(value) == Normalize(rd.Secret)
		if correct {
			rd.Guessed[p.ID] = true
			value = "guessed it!"
		}
		rd.Chat = append(rd.Chat, Guess{p.ID, p.Name, value, correct})
		if len(rd.Chat) > 40 {
			rd.Chat = rd.Chat[len(rd.Chat)-40:]
		}
		if len(rd.Guessed) >= len(rd.Participants)-1 {
			r.finish(now, false)
		}
	case "stroke":
		if rd.Game != DrawGuess || rd.Phase != "draw" || rd.DrawerID != p.ID {
			return "", errors.New("Only the drawer can use the canvas.")
		}
		s := a.Stroke
		if s.Epoch != rd.CanvasEpoch {
			return "", errors.New("The canvas was cleared. Start a new stroke.")
		}
		if s.ID == "" || len(s.ID) > 64 {
			return "", errors.New("Invalid stroke identifier.")
		}
		if rd.StrokeIDs[s.ID] {
			return "noop", nil
		}
		if len(rd.Strokes) >= 600 || len(rd.StrokeIDs) >= 6000 {
			return "", errors.New("Canvas limit reached. Clear it, or finish this round.")
		}
		if s.Width < 1 || s.Width > 24 || len(s.Points) < 1 || len(s.Points) > 24 {
			return "", errors.New("Invalid brush size or stroke length.")
		}
		validColor := false
		for _, color := range []string{"#20263a", "#ee6b6e", "#4c78dd", "#34a886", "#f2b53f", "#a875d6", "#ffffff"} {
			if s.Color == color {
				validColor = true
			}
		}
		if !validColor {
			return "", errors.New("Choose a color from the palette.")
		}
		for _, point := range s.Points {
			if math.IsNaN(point.X) || math.IsNaN(point.Y) || math.IsInf(point.X, 0) || math.IsInf(point.Y, 0) || point.X < 0 || point.X > 1 || point.Y < 0 || point.Y > 1 {
				return "", errors.New("A drawing point is outside the canvas.")
			}
		}
		rd.StrokeIDs[s.ID] = true
		rd.Strokes = append(rd.Strokes, s)
		return "stroke", nil
	case "clear":
		if rd.Game != DrawGuess || rd.Phase != "draw" || rd.DrawerID != p.ID {
			return "", errors.New("Only the drawer can clear the canvas.")
		}
		if !rd.LastClear.IsZero() && now.Sub(rd.LastClear) < time.Second {
			return "", errors.New("Give the canvas a moment before clearing again.")
		}
		rd.LastClear = now
		rd.CanvasEpoch++
		rd.Strokes = []Stroke{}
	default:
		return "", errors.New("Unknown action.")
	}
	return "state", nil
}
