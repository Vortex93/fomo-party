package game

// Original, family-friendly prompts. Free-form names and player answers are not
// automatically moderated; the host can skip any uncomfortable round.
var brainPrompts = []string{
	"Name a food you eat with your hands.",
	"Name something you pack for the beach.",
	"Name an animal that makes a loud noise.",
	"Name something you put on a pizza.",
	"Name a thing that is usually yellow.",
	"Name something you forget when leaving home.",
	"Name a snack for movie night.",
	"Name a thing you find in a school bag.",
	"Name something that flies.",
	"Name a food you eat for breakfast.",
	"Name a place where you have to whisper.",
	"Name something people collect.",
	"Name something round.",
	"Name a game you can play outside.",
	"Name something you do when it rains.",
	"Name something you keep in the fridge.",
	"Name something you would bring to a picnic.",
	"Name a job that needs a uniform.",
	"Name something with wheels.",
	"Name something that smells nice.",
}

type liarPrompt struct{ Topic, Question string }

var liarPrompts = []liarPrompt{
	{"Food", "Name a food you would eat for breakfast."},
	{"Animals", "Name an animal you might see at a farm."},
	{"Travel", "Name something you would pack for a beach trip."},
	{"Home", "Name something you would find in a kitchen."},
	{"School", "Name something you would carry in a school bag."},
	{"Outdoors", "Name something you would do at a park."},
	{"Food", "Name a topping you would put on a pizza."},
	{"Weather", "Name something you would wear on a rainy day."},
	{"Celebrations", "Name something you would bring to a birthday party."},
	{"Sport", "Name a sport you can play with a ball."},
	{"Travel", "Name something you might see at an airport."},
	{"Home", "Name something you do before going to bed."},
	{"Entertainment", "Name something you would do on a day without screens."},
	{"Animals", "Name an animal you might see near the sea."},
	{"Food", "Name a food that tastes sweet."},
}

var drawingWords = []string{
	"sunflower", "bicycle", "umbrella", "ice cream", "rocket", "snowman",
	"pizza", "butterfly", "castle", "rainbow", "guitar", "cactus", "sailboat",
	"elephant", "sandcastle", "backpack", "watermelon", "helicopter", "turtle",
	"palm tree", "robot", "starfish", "lighthouse", "pineapple", "headphones",
	"octopus", "hot air balloon", "dinosaur", "birthday cake", "penguin",
}

var likelyPrompts = []string{
	"Who is most likely to make everyone laugh?",
	"Who is most likely to become friends with a stranger?",
	"Who is most likely to win a dance contest?",
	"Who is most likely to pack snacks for everyone?",
	"Who is most likely to remember every birthday?",
	"Who is most likely to build an amazing blanket fort?",
	"Who is most likely to sing in the car?",
	"Who is most likely to adopt a stray animal?",
	"Who is most likely to discover a new hobby this week?",
	"Who is most likely to plan a surprise party?",
	"Who is most likely to invent a strange sandwich?",
	"Who is most likely to tell the best bedtime story?",
	"Who is most likely to spot a shooting star first?",
	"Who is most likely to lead a treasure hunt?",
	"Who is most likely to grow a giant sunflower?",
	"Who is most likely to take the best group photo?",
}

func (r *Room) promptIndex(id GameID, count int) int {
	if r.UsedPrompts[id] == nil || len(r.UsedPrompts[id]) >= count {
		r.UsedPrompts[id] = map[int]bool{}
	}
	for {
		i := randomN(count)
		if !r.UsedPrompts[id][i] {
			r.UsedPrompts[id][i] = true
			return i
		}
	}
}

func shuffle(ids []string) []string {
	out := append([]string(nil), ids...)
	for i := len(out) - 1; i > 0; i-- {
		j := randomN(i + 1)
		out[i], out[j] = out[j], out[i]
	}
	return out
}
