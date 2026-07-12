package api

import (
	"fmt"
	"math/rand"
	"strings"
)

var winPuns = []string{
	"Nailed it!", "Great solve!", "Sharp guessing!",
	"Impressive!", "Word wizardry!", "Holy cow!", "Smooth work!",
	"Right on target!", "Clean finish!",
}

var nudgePuns = []string{
	"Vowels first is never a bad idea.",
	"Every guess narrows it down.",
	"Four unique letters, no repeats.",
	"Bulls don't lie - trust the positions.",
	"A miss still rules letters out.",
	"Try covering new letters each guess.",
	"Watch for letters that keep scoring.",
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

func joinMessage(name string) string {
	return fmt.Sprintf("%s joined the game!", firstName(name))
}

func roundStartMessage(name string, round, numRounds int) string {
	if numRounds > 1 {
		return fmt.Sprintf("%s started round %d", firstName(name), round)
	}
	return fmt.Sprintf("%s started playing", firstName(name))
}

func bigGuessMessage(name string, bulls, cows int) string {
	if bulls >= 2 {
		return fmt.Sprintf("%s is closing in - %d bulls!", firstName(name), bulls)
	}
	return fmt.Sprintf("%s has %d cows - right letters, wrong spots!", firstName(name), cows)
}

func completedMessage(name string, round, numRounds int, score string) string {
	if numRounds > 1 {
		return fmt.Sprintf("%s finished round %d with score %s. %s", firstName(name), round, score, RandomWinPun())
	}
	return fmt.Sprintf("%s got the word - score %s. %s", firstName(name), score, RandomWinPun())
}

func resignMessage(name string, round, numRounds int) string {
	if numRounds > 1 {
		return fmt.Sprintf("%s gave up on round %d", firstName(name), round)
	}
	return fmt.Sprintf("%s gave up", firstName(name))
}
