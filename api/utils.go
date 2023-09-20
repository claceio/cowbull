package api

import (
	"math/rand"
)

var chars = []rune("ABCDEFGHJKMNPQRSTUVWXYZ23456789") // exclude I, L, O, 1, 0

func randSeq(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func getClueStats(word, clue string) (int, int) {
	charPosition := make(map[rune]int)
	for i, r := range word {
		charPosition[r] = i
	}

	return getClueStatsFromMap(charPosition, clue)
}

func getClueStatsFromMap(charPosition map[rune]int, clue string) (int, int) {
	bulls := 0
	cows := 0
	for i, r := range clue {
		position, ok := charPosition[r]
		if ok {
			if i == position {
				bulls++
			} else {
				cows++
			}
		}
	}

	return bulls, cows
}

func checkUniqueChars(clue string) bool {
	m := make(map[rune]bool)
	for _, i := range clue {
		_, ok := m[i]
		if ok {
			return false
		}

		m[i] = true
	}
	return true
}
