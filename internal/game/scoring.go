package game

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// finish is idempotent. Only this function changes scores, including drawing
// scores, so a skipped round can always award zero points.
func (r *Room) finish(now time.Time, skipped bool) {
	rd := r.Round
	if rd.Result != nil {
		return
	}
	result := &Result{Title: "Round complete!", Skipped: skipped, Rows: []ResultRow{}}
	points := map[string]int{}
	details := map[string]string{}
	answers := map[string]string{}
	for _, id := range rd.Participants {
		answers[id] = rd.Answers[id]
		if answers[id] == "" {
			answers[id] = "No answer"
		}
	}
	if skipped {
		result.Title, result.Detail = "Fresh start!", "The host skipped this round. No points were awarded."
	} else {
		switch rd.Game {
		case SameBrain:
			groups := map[string]int{}
			top := 0
			for _, answer := range rd.Answers {
				key := Normalize(answer)
				groups[key]++
				if groups[key] > top {
					top = groups[key]
				}
			}
			result.Title = "Great minds think alike."
			result.Detail = "100 points for matching a largest answer group. Tied largest groups count."
			if top < 2 {
				result.Detail = "Every answer was different. No matching group this time!"
			}
			for id, answer := range rd.Answers {
				n := groups[Normalize(answer)]
				details[id] = fmt.Sprintf("%d matching answer(s)", n)
				if n == top && top >= 2 {
					points[id] = 100
				}
			}
		case WhosLying:
			votes := 0
			for _, target := range rd.Votes {
				if target == rd.LiarID {
					votes++
				}
			}
			caught := votes > len(rd.Participants)/2
			liar := r.Player(rd.LiarID)
			result.LiarID = rd.LiarID
			if caught {
				result.Title = liar.Name + " was the bluffer. Caught!"
			} else {
				result.Title = liar.Name + " was the bluffer. Not caught!"
			}
			result.Detail = fmt.Sprintf("%d of %d voted for the bluffer. Catching them needs more than half of all players.", votes, len(rd.Participants))
			for _, id := range rd.Participants {
				if target := r.Player(rd.Votes[id]); target != nil {
					details[id] = "Voted for " + target.Name
				} else {
					details[id] = "Did not vote"
				}
				if id != rd.LiarID && rd.Votes[id] == rd.LiarID {
					points[id] = 100
				}
			}
			if !caught && rd.Answers[rd.LiarID] != "" {
				points[rd.LiarID] = 100
			}
			details[rd.LiarID] = "The bluffer · " + details[rd.LiarID]
		case DrawGuess:
			result.Title = "It was: " + rd.Secret
			result.Detail = "100 points per correct guesser. The drawer gets 100 when at least one player guesses it."
			for _, id := range rd.Participants {
				if id == rd.DrawerID {
					answers[id] = "The artist"
					if len(rd.Guessed) > 0 {
						points[id] = 100
					}
				} else if rd.Guessed[id] {
					answers[id] = "Guessed it!"
					points[id] = 100
				} else {
					answers[id] = "Better luck next sketch!"
				}
			}
		case MostLikely:
			counts := map[string]int{}
			top := 0
			for _, id := range rd.Votes {
				counts[id]++
				if counts[id] > top {
					top = counts[id]
				}
			}
			names := []string{}
			for _, id := range rd.Participants {
				if counts[id] == top && top >= 2 {
					names = append(names, r.Player(id).Name)
				}
			}
			if len(names) == 0 {
				result.Title = "Everyone thinks differently!"
				result.Detail = "No shared top pick. No points this time."
			} else {
				result.Title = "The room picked: " + strings.Join(names, " & ")
				result.Detail = "100 points for voters who matched a top pick. Tied top picks count."
			}
			for _, id := range rd.Participants {
				answers[id] = "No vote"
				if target := r.Player(rd.Votes[id]); target != nil {
					answers[id] = "Picked " + target.Name
					if top >= 2 && counts[target.ID] == top {
						points[id] = 100
					}
				}
				details[id] = fmt.Sprintf("Received %d vote(s)", counts[id])
			}
		case Memory:
			result.Title, result.Detail = "How much stuck?", "Earn a share of 100 points for each symbol in the correct position."
			result.Sequence = append([]int(nil), rd.Sequence...)
			for id, answer := range rd.Answers {
				parts := strings.Split(answer, ",")
				matches := 0
				for i, part := range parts {
					n, _ := strconv.Atoi(part)
					if i < len(rd.Sequence) && n == rd.Sequence[i] {
						matches++
					}
				}
				points[id] = matches * 100 / len(rd.Sequence)
				answers[id] = fmt.Sprintf("%d / %d positions correct", matches, len(rd.Sequence))
			}
		}
	}
	for _, id := range rd.Participants {
		p := r.Player(id)
		if p == nil {
			continue
		}
		p.Score += points[id]
		result.Rows = append(result.Rows, ResultRow{id, p.Name, answers[id], details[id], points[id]})
	}
	rd.Result = result
	r.setPhase("reveal", 0, now)
}
