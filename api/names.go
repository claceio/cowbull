package api

import (
	"fmt"
	"math/rand"
	"strings"
)

var funNames = []string{
	"Moolan", "Sir Loin", "Moozart", "Cowabunga Kid", "Bullseye Betty",
	"Milkshake Mike", "Moo-dini", "Anggus", "Buttercup", "Cowpernicus",
	"Bullwinkle", "Moo-riarty", "Legendairy Larry", "Heifer Potter",
	"Disco Bull", "Turbo Heifer", "Ninja Calf", "Moo-nica", "Calf Vader",
	"Cud-dles", "Moosette", "El Toro", "Daisy Duke", "Ferdinand",
}

var winPuns = []string{
	"Nailed it!", "Great solve!", "Sharp guessing!", "That was quick!",
	"Impressive!", "Word wizardry!", "Holy cow!", "Smooth work!",
	"Right on target!", "Clean finish!",
}

var nudgePuns = []string{
	"Vowels first is never a bad idea.",
	"Every guess narrows it down.",
	"Four unique letters, no repeats.",
	"Bulls don't lie — trust the positions.",
	"A miss still rules letters out.",
	"Try covering new letters each guess.",
	"Watch for letters that keep scoring.",
}

func RandomFunName() string {
	return funNames[rand.Intn(len(funNames))]
}

func RandomWinPun() string {
	return winPuns[rand.Intn(len(winPuns))]
}

func RandomNudge() string {
	return nudgePuns[rand.Intn(len(nudgePuns))]
}

// sanitizeText trims, strips angle brackets and control chars, and caps the
// length.
func sanitizeText(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	var b strings.Builder
	for _, r := range text {
		if r == '<' || r == '>' || r == '&' || r < 32 {
			continue
		}
		b.WriteRune(r)
	}
	text = b.String()
	if len(text) > maxLen {
		text = text[:maxLen]
	}
	return strings.TrimSpace(text)
}

// SanitizeName cleans up a player-provided name.
func SanitizeName(name string) string {
	return sanitizeText(name, 24)
}

// SanitizeTitle cleans up a game/tournament display name.
func SanitizeTitle(title string) string {
	return sanitizeText(title, 40)
}

func firstName(name string) string {
	if name == "" {
		return "Someone"
	}
	return name
}

// displayName renders "Name (Location)" when the location is known.
func displayName(name, location string) string {
	n := firstName(name)
	if location != "" && location != "Unknown" {
		return n + " (" + location + ")"
	}
	return n
}

func joinMessage(name, location string) string {
	return fmt.Sprintf("%s joined the game!", displayName(name, location))
}

func roundStartMessage(name, location string, round, numRounds int) string {
	if numRounds > 1 {
		return fmt.Sprintf("%s started round %d", displayName(name, location), round)
	}
	return fmt.Sprintf("%s started playing", displayName(name, location))
}

func bigGuessMessage(name, location string, bulls, cows int) string {
	if bulls >= 3 {
		return fmt.Sprintf("%s is closing in — %d bulls!", displayName(name, location), bulls)
	}
	return fmt.Sprintf("%s found all the letters, wrong spots — %d cows!", displayName(name, location), cows)
}

func completedMessage(name, location string, round, numRounds int, score string) string {
	if numRounds > 1 {
		return fmt.Sprintf("%s finished round %d with score %s. %s", displayName(name, location), round, score, RandomWinPun())
	}
	return fmt.Sprintf("%s got the word — score %s. %s", displayName(name, location), score, RandomWinPun())
}

func resignMessage(name, location string, round, numRounds int) string {
	if numRounds > 1 {
		return fmt.Sprintf("%s gave up on round %d", displayName(name, location), round)
	}
	return fmt.Sprintf("%s gave up", displayName(name, location))
}
