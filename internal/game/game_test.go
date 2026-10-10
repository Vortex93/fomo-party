package game

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, n int, ids ...GameID) (*Room, time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC)
	r, _, err := NewRoom("ABC234", "Alex", now)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"Sam", "Riley", "Morgan", "Casey", "Jordan"}
	for i := 1; i < n; i++ {
		if _, err := r.Join(names[i-1], now); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range r.Players {
		p.Connected = true
		p.Ready = true
	}
	r.Config = Config{Games: ids, Rounds: 1, Seconds: 20}
	if _, err := r.Handle(r.HostID, Action{Type: "start"}, now); err != nil {
		t.Fatal(err)
	}
	now = r.Round.Deadline
	r.Tick(now)
	return r, now
}
func act(t *testing.T, r *Room, id, kind, value string, now time.Time) {
	t.Helper()
	if _, err := r.Handle(id, Action{Type: kind, RoundID: r.Round.ID, Value: value}, now); err != nil {
		t.Fatalf("%s: %v", kind, err)
	}
}
func wire(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestConfigValidation(t *testing.T) {
	cases := []struct {
		name   string
		config Config
	}{
		{"empty", Config{nil, 1, 20}}, {"duplicate", Config{[]GameID{Memory, Memory}, 1, 20}},
		{"unknown", Config{[]GameID{"made-up"}, 1, 20}}, {"zero rounds", Config{[]GameID{Memory}, 0, 20}},
		{"too many rounds", Config{[]GameID{Memory}, 6, 20}}, {"too short", Config{[]GameID{Memory}, 1, 19}},
		{"too long", Config{[]GameID{Memory}, 1, 91}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.config.Validate() == nil {
				t.Fatal("expected rejection")
			}
		})
	}
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatal(err)
	}
	if DefaultConfig().Minimum() != 4 {
		t.Fatal("all-five playlist must require four")
	}
}
func TestJoinValidationAndCapacity(t *testing.T) {
	now := time.Now()
	r, _, _ := NewRoom("ABC234", "Alex", now)
	for _, name := range []string{"", "  ", strings.Repeat("x", 21), "Alex!", "a\nb", "a\u202Eb", "😀"} {
		if _, err := r.Join(name, now); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	for len(r.Players) < MaxPlayers {
		_, err := r.Join("Player "+Token(4), now)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Join("Overflow", now); err == nil {
		t.Fatal("accepted sixteenth player")
	}
}
func TestStartNeedsReadyAndMinimum(t *testing.T) {
	now := time.Now()
	r, p, _ := NewRoom("ABC234", "Alex", now)
	p.Connected = true
	if _, err := r.Handle(p.ID, Action{Type: "start"}, now); err == nil {
		t.Fatal("started alone")
	}
	for _, name := range []string{"Sam", "Morgan", "Riley"} {
		p, _ := r.Join(name, now)
		p.Connected = true
	}
	if _, err := r.Handle(r.HostID, Action{Type: "start"}, now); err == nil {
		t.Fatal("started without readiness")
	}
}
func TestNormalize(t *testing.T) {
	for input, want := range map[string]string{" Ice-CREAM! ": "ice cream", "BANANA": "banana", "  café  ": "café", "Hello   World": "hello world"} {
		if got := Normalize(input); got != want {
			t.Fatalf("%q: got %q, want %q", input, got, want)
		}
	}
}
func TestSameBrainMajorityAndDuplicateGuard(t *testing.T) {
	r, now := fixture(t, 3, SameBrain)
	act(t, r, r.Players[0].ID, "answer", "Banana!", now)
	act(t, r, r.Players[1].ID, "answer", " banana ", now)
	if _, err := r.Handle(r.Players[0].ID, Action{Type: "answer", RoundID: r.Round.ID, Value: "apple"}, now); err == nil {
		t.Fatal("duplicate changed an answer")
	}
	act(t, r, r.Players[2].ID, "answer", "apple", now)
	if r.Round.Phase != "reveal" || r.Players[0].Score != 100 || r.Players[1].Score != 100 || r.Players[2].Score != 0 {
		t.Fatal("bad majority score")
	}
	r.finish(now, false)
	r.finish(now, false)
	if r.Players[0].Score != 100 {
		t.Fatal("scored a round twice")
	}
}
func TestSameBrainTiesAndUniqueAnswers(t *testing.T) {
	for _, values := range [][]string{{"a", "a", "b", "b"}, {"a", "b", "c", "d"}} {
		r, now := fixture(t, 4, SameBrain)
		for i, answer := range values {
			act(t, r, r.Players[i].ID, "answer", answer, now)
		}
		want := 0
		if values[0] == values[1] {
			want = 100
		}
		for _, p := range r.Players {
			if p.Score != want {
				t.Fatalf("tie/unique score: %d", p.Score)
			}
		}
	}
}
func TestPrivateAnswerAndTokenViews(t *testing.T) {
	r, now := fixture(t, 3, SameBrain)
	act(t, r, r.Players[0].ID, "answer", "hidden pineapple", now)
	for _, id := range []string{r.Players[1].ID, "viewer-example"} {
		data := wire(t, r.View(id, now))
		if strings.Contains(data, "hidden pineapple") {
			t.Fatal("leaked another player's answer")
		}
		for _, p := range r.Players {
			if strings.Contains(data, p.Token) {
				t.Fatal("leaked a session token")
			}
		}
	}
	if r.View(r.Players[0].ID, now).Round.You.Answer != "hidden pineapple" {
		t.Fatal("owner cannot restore answer")
	}
}
func TestBlufferDoesNotReceiveQuestion(t *testing.T) {
	r, now := fixture(t, 4, WhosLying)
	for _, id := range []string{r.Round.LiarID, "viewer-example"} {
		data := wire(t, r.View(id, now))
		if strings.Contains(data, r.Round.Prompt) {
			t.Fatal("bluffer or TV received the secret question")
		}
		if strings.Contains(data, "liarId") {
			t.Fatal("public view contains the role assignment")
		}
	}
	for _, p := range r.Players {
		if p.ID != r.Round.LiarID && r.View(p.ID, now).Round.Prompt != r.Round.Prompt {
			t.Fatal("detective did not get question")
		}
	}
}
func TestBlufferStrictMajorityScoring(t *testing.T) {
	for _, caught := range []bool{false, true} {
		r, now := fixture(t, 4, WhosLying)
		liar := r.Round.LiarID
		detectives := []string{}
		for _, p := range r.Players {
			act(t, r, p.ID, "answer", "banana", now)
			if p.ID != liar {
				detectives = append(detectives, p.ID)
			}
		}
		if r.Round.Phase != "discuss" {
			t.Fatal("missing discussion phase")
		}
		now = r.Round.Deadline
		r.Tick(now)
		for i, id := range detectives {
			target := liar
			if !caught && i == 2 {
				target = detectives[0]
			}
			act(t, r, id, "vote", target, now)
		}
		act(t, r, liar, "vote", detectives[0], now)
		want := 100
		if caught {
			want = 0
		}
		if r.Player(liar).Score != want {
			t.Fatalf("caught=%t: score=%d", caught, r.Player(liar).Score)
		}
		if r.Player(detectives[0]).Score != 100 {
			t.Fatal("correct vote not rewarded")
		}
	}
}
func TestBlufferNoSelfVoteOrAbsentReward(t *testing.T) {
	r, now := fixture(t, 4, WhosLying)
	now = r.Round.Deadline
	r.Tick(now)
	now = r.Round.Deadline
	r.Tick(now)
	if _, err := r.Handle(r.Round.LiarID, Action{Type: "vote", RoundID: r.Round.ID, Value: r.Round.LiarID}, now); err == nil {
		t.Fatal("self vote accepted")
	}
	now = r.Round.Deadline
	r.Tick(now)
	if r.Player(r.Round.LiarID).Score != 0 {
		t.Fatal("absent bluffer rewarded")
	}
}
func TestMostLikelyVotesStayPrivateAndTiesCount(t *testing.T) {
	r, now := fixture(t, 4, MostLikely)
	targetA, targetB := r.Players[0].ID, r.Players[1].ID
	for i, p := range r.Players {
		target := targetA
		if i > 1 {
			target = targetB
		}
		act(t, r, p.ID, "vote", target, now)
		if i == 0 && r.View(r.Players[1].ID, now).Round.You.Vote != "" {
			t.Fatal("leaked vote")
		}
	}
	for _, p := range r.Players {
		if p.Score != 100 {
			t.Fatal("top tied pick was not rewarded")
		}
	}
}
func TestDrawSecretAndCorrectGuessPrivacy(t *testing.T) {
	r, now := fixture(t, 3, DrawGuess)
	rd := r.Round
	var guesser string
	for _, p := range r.Players {
		if p.ID != rd.DrawerID {
			guesser = p.ID
			break
		}
	}
	for _, id := range []string{guesser, "viewer-example"} {
		if r.View(id, now).Round.Word != "" {
			t.Fatal("word leaked")
		}
	}
	act(t, r, guesser, "guess", rd.Secret, now)
	data := wire(t, r.View("viewer-example", now))
	if strings.Contains(data, rd.Secret) {
		t.Fatal("correct guess leaked the word")
	}
	if r.Player(guesser).Score != 0 {
		t.Fatal("points awarded before round finished")
	}
	for _, p := range r.Players {
		if p.ID != rd.DrawerID && p.ID != guesser {
			act(t, r, p.ID, "guess", rd.Secret, now)
		}
	}
	for _, p := range r.Players {
		if p.Score != 100 {
			t.Fatal("drawing/guessing score incorrect")
		}
	}
}
func validStroke() Stroke {
	return Stroke{ID: "stroke-1", Epoch: 0, Color: "#20263a", Width: 7, Points: []Point{{0.1, 0.2}, {0.3, 0.4}}}
}
func TestCanvasValidationAndAuthority(t *testing.T) {
	r, now := fixture(t, 2, DrawGuess)
	drawer := r.Round.DrawerID
	other := r.Players[1].ID
	if other == drawer {
		other = r.Players[0].ID
	}
	if _, err := r.Handle(other, Action{Type: "stroke", RoundID: r.Round.ID, Stroke: validStroke()}, now); err == nil {
		t.Fatal("guesser could draw")
	}
	cases := []Stroke{validStroke(), validStroke(), validStroke(), validStroke(), validStroke()}
	cases[0].Points[0].X = -1
	cases[1].Color = "javascript:bad"
	cases[2].Width = 25
	cases[3].Points[0].X = math.NaN()
	cases[4].Points = make([]Point, 25)
	for i, stroke := range cases {
		if _, err := r.Handle(drawer, Action{Type: "stroke", RoundID: r.Round.ID, Stroke: stroke}, now); err == nil {
			t.Fatalf("invalid stroke %d accepted", i)
		}
	}
	good := Action{Type: "stroke", RoundID: r.Round.ID, Stroke: validStroke()}
	if _, err := r.Handle(drawer, good, now); err != nil {
		t.Fatal(err)
	}
	if event, err := r.Handle(drawer, good, now); err != nil || event != "noop" || len(r.Round.Strokes) != 1 {
		t.Fatal("duplicate stroke not deduplicated")
	}
	act(t, r, drawer, "clear", "", now)
	if r.Round.CanvasEpoch != 1 || len(r.Round.Strokes) != 0 {
		t.Fatal("canvas was not cleared")
	}
	if _, err := r.Handle(drawer, good, now); err == nil {
		t.Fatal("stale stroke resurrected after clear")
	}
}
func TestGuessRateLimit(t *testing.T) {
	r, now := fixture(t, 2, DrawGuess)
	id := r.Players[1].ID
	act(t, r, id, "guess", "not the word", now)
	if _, err := r.Handle(id, Action{Type: "guess", RoundID: r.Round.ID, Value: "try again"}, now.Add(time.Millisecond)); err == nil {
		t.Fatal("guess spam not limited")
	}
}
func TestMemoryProjectionAndOrderedScoring(t *testing.T) {
	r, now := fixture(t, 3, Memory)
	r.Round.Sequence = []int{0, 2, 4, 8}
	if r.Round.Phase != "memorize" || len(r.View(r.HostID, now).Round.Sequence) != 4 {
		t.Fatal("missing memorize phase")
	}
	now = r.Round.Deadline
	r.Tick(now)
	for _, id := range []string{r.HostID, "viewer-example"} {
		if len(r.View(id, now).Round.Sequence) != 0 {
			t.Fatal("memory target leaked during recall")
		}
		if strings.Contains(wire(t, r.View(id, now)), `"sequence":`) {
			t.Fatal("sequence key not removed")
		}
	}
	answers := [][]int{{0, 2, 4, 8}, {0, 2, 4, 7}, {1, 1, 1, 1}}
	for i, answer := range answers {
		if _, err := r.Handle(r.Players[i].ID, Action{Type: "memory", RoundID: r.Round.ID, Sequence: answer}, now); err != nil {
			t.Fatal(err)
		}
	}
	for i, want := range []int{100, 75, 0} {
		if r.Players[i].Score != want {
			t.Fatalf("player %d: got %d, want %d", i, r.Players[i].Score, want)
		}
	}
}
func TestInvalidMemorySubmission(t *testing.T) {
	r, now := fixture(t, 2, Memory)
	now = r.Round.Deadline
	r.Tick(now)
	for _, sequence := range [][]int{{}, {0, 1}, {0, 0, 0, 9}, {-1, 0, 0, 0}} {
		if _, err := r.Handle(r.HostID, Action{Type: "memory", RoundID: r.Round.ID, Sequence: sequence}, now); err == nil {
			t.Fatal("invalid sequence accepted")
		}
	}
}
func TestPauseAndLateActions(t *testing.T) {
	r, now := fixture(t, 3, SameBrain)
	original := r.Round.Deadline
	act(t, r, r.HostID, "pause", "", now.Add(time.Second))
	if _, err := r.Handle(r.Players[1].ID, Action{Type: "answer", RoundID: r.Round.ID, Value: "hello"}, now); err == nil {
		t.Fatal("paused answer accepted")
	}
	r.Tick(now.Add(time.Hour))
	if r.Round.Phase != "answer" {
		t.Fatal("paused clock advanced")
	}
	act(t, r, r.HostID, "pause", "", now.Add(time.Hour))
	if r.Round.Deadline.Sub(now.Add(time.Hour)) != original.Sub(now.Add(time.Second)) {
		t.Fatal("resume changed remaining duration")
	}
	if _, err := r.Handle(r.HostID, Action{Type: "answer", RoundID: r.Round.ID, Value: "late"}, r.Round.Deadline); err == nil {
		t.Fatal("accepted answer at deadline")
	}
}
func TestHostOnlyControlsAndStaleRound(t *testing.T) {
	r, now := fixture(t, 3, SameBrain)
	for _, command := range []string{"skip", "pause", "configure", "next", "start", "rematch", "remove"} {
		if _, err := r.Handle(r.Players[1].ID, Action{Type: command, RoundID: r.Round.ID}, now); err == nil {
			t.Fatalf("non-host could %s", command)
		}
	}
	if _, err := r.Handle(r.HostID, Action{Type: "answer", RoundID: "old-round", Value: "answer"}, now); err == nil {
		t.Fatal("stale round accepted")
	}
	if _, err := r.Handle("viewer-example", Action{Type: "skip", RoundID: r.Round.ID}, now); err == nil {
		t.Fatal("viewer could control")
	}
	if _, err := r.Join("Latecomer", now); err == nil {
		t.Fatal("midgame join accepted")
	}
}
func TestSkipDoesNotAwardDrawingPoints(t *testing.T) {
	r, now := fixture(t, 3, DrawGuess)
	act(t, r, r.Players[1].ID, "guess", r.Round.Secret, now)
	act(t, r, r.HostID, "skip", "", now)
	for _, p := range r.Players {
		if p.Score != 0 {
			t.Fatal("skip awarded points")
		}
	}
}
func TestPlaylistFinishRematchAndRotation(t *testing.T) {
	r, now := fixture(t, 4, SameBrain, WhosLying, DrawGuess, MostLikely, Memory)
	for i := 0; i < 5; i++ {
		if r.Round.Number != i+1 {
			t.Fatal("wrong round number")
		}
		act(t, r, r.HostID, "skip", "", now)
		act(t, r, r.HostID, "next", "", now)
	}
	if r.Status != "finished" {
		t.Fatal("playlist did not finish")
	}
	if _, err := r.Handle(r.HostID, Action{Type: "rematch"}, now); err != nil {
		t.Fatal(err)
	}
	if r.Status != "lobby" || r.Round != nil {
		t.Fatal("rematch did not reset")
	}
	for _, p := range r.Players {
		if p.Ready != (p.ID == r.HostID) {
			t.Fatal("readiness not reset")
		}
	}
}
func TestHostReassignment(t *testing.T) {
	r, now := fixture(t, 3, SameBrain)
	original := r.HostID
	r.Connected(original, false, now)
	r.Tick(now.Add(14 * time.Second))
	if r.HostID != original {
		t.Fatal("host changed too early")
	}
	r.Tick(now.Add(15 * time.Second))
	if r.HostID != r.Players[1].ID {
		t.Fatal("host did not transfer")
	}
	r.Connected(original, true, now.Add(16*time.Second))
	if r.HostID == original {
		t.Fatal("old host reclaimed authority on reconnect")
	}
}
func TestPromptDoesNotRepeat(t *testing.T) {
	r, _ := fixture(t, 3, SameBrain)
	seen := map[int]bool{}
	r.UsedPrompts[SameBrain] = nil
	for i := 0; i < len(brainPrompts); i++ {
		n := r.promptIndex(SameBrain, len(brainPrompts))
		if seen[n] {
			t.Fatal("prompt repeated before exhaustion")
		}
		seen[n] = true
	}
}

func TestLobbyHostHandoffIsNotStuckUnready(t *testing.T) {
	now := time.Now()
	r, host, _ := NewRoom("ABC234", "Alex", now)
	p, _ := r.Join("Morgan", now)
	p.Connected = true
	if p.Ready {
		t.Fatal("new guest should start unready")
	}
	host.Connected = false
	r.Tick(now.Add(16 * time.Second))
	if r.HostID != p.ID || !p.Ready {
		t.Fatal("new host must be ready so the lobby can start")
	}
}
