package api

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"strings"
	"time"

	gamedb "cowbull.co/game/db"
	"github.com/oschwald/geoip2-golang"
)

const (
	CleanupUnstarted = 2 * 24 * time.Hour
	CleanupStarted   = 14 * 24 * time.Hour
	CleanupCompleted = 60 * 24 * time.Hour
	CleanupResigned  = 30 * 24 * time.Hour
)

type GameAPI struct {
	db          *gamedb.GameDB
	ipLookup    *geoip2.Reader
	dictFile    string
	words       []string
	wordsEasy   []string
	wordsMedium []string
	wordsHard   []string
	wordDict    map[string]bool
}

func NewGameAPI(db *gamedb.GameDB, dictFile string, ipLookupFile string) *GameAPI {
	ipLookup, err := geoip2.Open(ipLookupFile)
	if err != nil {
		log.Fatal(err)
	}

	g := &GameAPI{db: db, ipLookup: ipLookup, dictFile: dictFile}
	rand.Seed(time.Now().UnixNano())
	g.initDict()
	return g
}

func (g *GameAPI) initDict() {
	g.words = loadWords(g.dictFile)
	g.wordsEasy = loadWords("words_easy.txt")
	g.wordsMedium = loadWords("words_medium.txt")
	g.wordsHard = loadWords("words_hard.txt")

	rand.Shuffle(len(g.words), func(i, j int) {
		g.words[i], g.words[j] = g.words[j], g.words[i]
	})

	g.wordDict = make(map[string]bool)
	for _, word := range g.words {
		g.wordDict[word] = true
	}
}

func loadWords(file string) []string {
	f, err := os.Open(file)
	if err != nil {
		panic(err)
	}

	words := make([]string, 0, 6000)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		word := strings.ToLower(scanner.Text())
		words = append(words, word)
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}

	return words
}

func (g *GameAPI) getWord(level string) string {
	level = strings.ToLower(level)
	randWord := ""
	switch level {
	case "easy":
		randIndex := rand.Intn(len(g.wordsEasy))
		randWord = g.wordsEasy[randIndex]
	case "medium":
		randIndex := rand.Intn(len(g.wordsMedium))
		randWord = g.wordsMedium[randIndex]
	default:
		randIndex := rand.Intn(len(g.wordsHard))
		randWord = g.wordsHard[randIndex]

	}
	return randWord
}

func (g *GameAPI) CreateChallenge(ipAddr string, level string) (map[string]string, error) {
	var randWord = g.getWord(level)
	var challengeId string
	for {
		challengeId = randSeq(4)
		err := g.db.CreateChallenge(challengeId, ipAddr, randWord)
		if err != nil {
			log.Println(err)
			continue
		}
		break
	}
	return map[string]string{"ChallengeId": challengeId, "Status": ""}, nil
}

func (g *GameAPI) GetChallenge(challengeId string) (map[string]interface{}, error) {
	challengeId = strings.ToUpper(challengeId)
	_, err := g.db.GetChallenge(challengeId)
	if err != nil {
		return nil, err
	}

	// Don't return the word!!
	return map[string]interface{}{"Status": "", "Challenge_Id": challengeId}, nil
}

func (g *GameAPI) GetChallengeGames(challengeId string) ([]map[string]interface{}, error) {
	challengeId = strings.ToUpper(challengeId)
	games, err := g.db.GetChallengeGames(challengeId)
	if err != nil {
		return nil, err
	}

	chGames := make([]map[string]interface{}, 0, len(games))
	for _, game := range games {
		gameStatus, err := g.GetGame(game)
		if err != nil {
			return nil, err
		}
		delete(gameStatus, "Clues")
		delete(gameStatus, "GameId")
		delete(gameStatus, "Word")
		chGames = append(chGames, gameStatus)
	}

	return chGames, err
}

func (g *GameAPI) getLocation(ipAddr string) string {
	ipAddr = strings.Split(ipAddr, ":")[0]
	ipAddr = strings.Split(ipAddr, ",")[0]
	ip := net.ParseIP(ipAddr)
	location := ""
	if ip != nil {
		record, err := g.ipLookup.City(ip)
		fmt.Printf("%#v", record)
		if err == nil {
			if record.City.Names["en"] != "N/A" {
				location += record.City.Names["en"]
			}
			if len(location) == 0 {
				if len(record.Subdivisions) > 0 && record.Subdivisions[0].Names["en"] != "N/A" {
					location += record.Subdivisions[0].Names["en"]
				}
			}
			if record.Country.Names["en"] != "N/A" {
				if len(location) != 0 {
					location += " "
				}
				location += record.Country.Names["en"]
			}
		}
	}
	if location == "" {
		location = "Unknown"
	}
	return location
}

func (g *GameAPI) StartChallengeGame(challengeId string, ipAddr string) (map[string]string, error) {
	challengeId = strings.ToUpper(challengeId)
	challenge, err := g.db.GetChallenge(challengeId)
	if err != nil {
		return nil, err
	}

	location := g.getLocation(ipAddr)
	var gameId string
	for {
		gameId = randSeq(5)
		err := g.db.CreateGame(gameId, challenge.Word, ipAddr, location, challengeId)
		if err != nil {
			log.Println(err)
			continue
		}
		break
	}
	return map[string]string{"GameId": gameId, "Status": ""}, nil
}

func (g *GameAPI) StartGame(ipAddr string, level string) map[string]string {
	var randWord = g.getWord(level)
	location := g.getLocation(ipAddr)
	var gameId string
	for {
		gameId = randSeq(5)
		err := g.db.CreateGame(gameId, randWord, ipAddr, location, "")
		if err != nil {
			log.Println(err)
			continue
		}
		break
	}
	return map[string]string{"GameId": gameId, "Status": ""}
}

func (g *GameAPI) GetGame(gameId string) (map[string]interface{}, error) {
	gameId = strings.ToUpper(gameId)
	gs, err := g.db.CheckGameId(gameId)
	if err != nil {
		return nil, err
	}

	gameClues, err := g.db.GetClues(gameId)
	if err != nil {
		return nil, err
	}

	var hints int
	guesses := len(gameClues)
	for _, c := range gameClues {
		if c.Hint {
			hints++
		}
	}
	guesses -= hints
	location := g.getLocation(gs.Location)

	ret := map[string]interface{}{"Status": gs.Status,
		"Clues": gameClues, "GameId": gameId, "Location": location,
		"ChallengeId": gs.ChallengeId, "Time": gs.Duration.String, "GuessCount": guesses, "HintCount": hints}

	ret["Word"] = ""
	if gs.ChallengeId == "" && (gs.Status == "COMPLETED" || gs.Status == "RESIGNED") {
		ret["Word"] = gs.Word
	}

	ret["Score"] = ""
	if gs.Status == "COMPLETED" {
		score := computeScore(guesses, hints, gs.CompletedSeconds)
		scoreS := fmt.Sprintf("%0.2f", score)
		ret["Score"] = scoreS
	}

	return ret, nil
}

func (g *GameAPI) Submit(gameId string, clue string) (map[string]interface{}, error) {
	gameStatus, err := g.db.CheckGameId(gameId)
	if err != nil {
		return nil, err
	}

	if gameStatus.Status != "STARTED" && gameStatus.Status != "CREATED" {
		return nil, errors.New("Game already " + strings.ToLower(gameStatus.Status) + "...")
	}

	clue = strings.ToLower(clue)

	if len(clue) != 4 {
		return nil, errors.New(clue + ": guess should be four characters...")
	}
	if ok := checkUniqueChars(clue); !ok {
		return nil, errors.New(clue + " has repeating characters...")
	}
	if !g.wordDict[clue] {
		return nil, errors.New(clue + " is not in the game dictionary...")
	}

	bulls, cows := getClueStats(gameStatus.Word, clue)
	err = g.db.InsertClue(gameId, clue, bulls, cows, false)
	if err != nil {
		return nil, err
	}

	if gameStatus.Status == "CREATED" {
		g.db.UpdateStatus(gameId, gameStatus.Status, "STARTED", gameStatus.StartTime)
		gameStatus, _ = g.db.CheckGameId(gameId)
	}

	if bulls == 4 {
		g.db.UpdateStatus(gameId, gameStatus.Status, "COMPLETED", gameStatus.StartTime)
		gameStatus.Status = "COMPLETED"
		if err != nil {
			return nil, err
		}
	}

	return map[string]interface{}{
		"Status": "", "GameId": gameId, "Clue": clue,
		"Bulls": bulls, "Cows": cows}, nil
}

func (g *GameAPI) Hint(gameId string) (map[string]interface{}, error) {
	gameStatus, err := g.db.CheckGameId(gameId)
	if err != nil {
		return nil, err
	}

	if gameStatus.Status != "STARTED" && gameStatus.Status != "CREATED" {
		return nil, errors.New("Game already " + strings.ToLower(gameStatus.Status) + "...")
	}

	gameClues, err := g.db.GetClues(gameId)
	if err != nil {
		return nil, err
	}

	hint, err := g.getHint(*gameStatus, gameClues)
	if err != nil {
		return nil, err
	}
	bulls, cows := getClueStats(gameStatus.Word, hint)
	err = g.db.InsertClue(gameId, hint, bulls, cows, true)
	if err != nil {
		return nil, err
	}

	if gameStatus.Status == "CREATED" {
		g.db.UpdateStatus(gameId, gameStatus.Status, "STARTED", gameStatus.StartTime)
		gameStatus, _ = g.db.CheckGameId(gameId)
	}

	return map[string]interface{}{
		"Status": "", "GameId": gameId, "Clue": hint,
		"Bulls": bulls, "Cows": cows}, nil
}

func (g *GameAPI) getHint(status gamedb.GameStatus, clues []gamedb.GameClue) (string, error) {
	word := status.Word
	charPosition := make(map[rune]int)
	for i, r := range word {
		charPosition[r] = i
	}

	clueMap := make(map[string]bool)
	for _, c := range clues {
		clueMap[c.Clue] = true
	}

	indexes := []int{0, 1, 2, 3}
	rand.Shuffle(len(indexes), func(i, j int) {
		indexes[i], indexes[j] = indexes[j], indexes[i]
	})
	for _, i := range indexes {
		letter := word[i]

		for _, w := range g.words {
			if w[i] == letter && !clueMap[w] {
				bulls, cows := getClueStatsFromMap(charPosition, w)
				if bulls == 1 && cows == 0 {
					return w, nil
				}
			}
		}
	}

	return "", errors.New("No suitable hint found")
}

func (g *GameAPI) Resign(gameId string) (map[string]interface{}, error) {
	gameStatus, err := g.db.CheckGameId(gameId)
	if err != nil {
		return nil, err
	}

	if gameStatus.Status != "CREATED" && gameStatus.Status != "STARTED" {
		return nil, errors.New("Game already " + strings.ToLower(gameStatus.Status) + "...")
	}
	g.db.UpdateStatus(gameId, gameStatus.Status, "RESIGNED", gameStatus.StartTime)
	if err != nil {
		return nil, err
	}

	gameClues, err := g.db.GetClues(gameId)
	if err != nil {
		return nil, err
	}

	statusText, shareText, _ := getStatusText(*gameStatus, gameClues, false, false)
	infoText := getInfoText(*gameStatus)

	return map[string]interface{}{
		"Status": statusText, "ShareText": shareText, "GameInfo": infoText, "GameId": gameId}, nil
}

func getStatusText(status gamedb.GameStatus, gameClues []gamedb.GameClue, getIntermediateState, multiLine bool) (string, string, string) {
	titleCaseStatus := strings.Title(strings.ToLower(status.Status))
	durationString := ""
	if status.Duration.Valid {
		durationString = status.Duration.String
	}

	guesses := len(gameClues)
	hints := 0
	for _, c := range gameClues {
		if c.Hint {
			hints++
		}
	}
	guesses -= hints
	labelText, shareText, scoreS := "", "", ""
	if status.ChallengeId != "" {
		if getIntermediateState || (status.Status == "COMPLETED" || status.Status == "RESIGNED") {
			if len(gameClues) == 0 {
				labelText = fmt.Sprintf(`<b>%s</b>`, titleCaseStatus)
			} else {
				if multiLine {
					labelText = fmt.Sprintf(`<b>%s</b><br>Guesses: %d<br>Hints: %d`,
						titleCaseStatus, guesses, hints)
					if durationString != "" {
						labelText += fmt.Sprintf(`<br>Time: %s`,
							durationString)
					}
				} else {
					labelText = fmt.Sprintf(`%s, Guesses: %d Hints: %d`,
						titleCaseStatus, guesses, hints)
					if durationString != "" {
						labelText += fmt.Sprintf(`, Time: %s`,
							durationString)
					}

				}
			}
		}

		if status.Status == "COMPLETED" {
			shareText = fmt.Sprintf(`%s CowBull game https://cowbull.co/game?id=%s, word was "%s". Guesses: %d, Hints: %d, Time Taken: %s`,
				titleCaseStatus, status.GameId, strings.ToUpper(status.Word), guesses, hints, durationString)
		}
	} else {
		switch status.Status {
		case "RESIGNED", "COMPLETED":
			if len(gameClues) == 0 {
				labelText = fmt.Sprintf(`%s, word was <a class="link link-primary"href="https://www.merriam-webster.com/dictionary/%s">%s</a>`,
					titleCaseStatus, status.Word, status.Word)
				shareText = fmt.Sprintf(`%s CowBull game https://cowbull.co/game?id=%s, word was "%s"`,
					titleCaseStatus, status.GameId, strings.ToUpper(status.Word))
			} else {
				labelText = fmt.Sprintf(`%s, word was <a class="link link-primary"href="https://www.merriam-webster.com/dictionary/%s">%s</a><br> Guesses: %d, Hints: %d, Time Taken: %s`,
					titleCaseStatus, status.Word, status.Word, guesses, hints, durationString)
				shareText = fmt.Sprintf(`%s CowBull game https://cowbull.co/game?id=%s, word was "%s". Guesses: %d, Hints: %d, Time Taken: %s`,
					titleCaseStatus, status.GameId, strings.ToUpper(status.Word), guesses, hints, durationString)
			}
		}
	}

	if status.Status == "COMPLETED" {
		score := computeScore(guesses, hints, status.CompletedSeconds)
		if !multiLine {
			labelText += fmt.Sprintf("<br>Score %0.2f", score)
		}
		shareText += fmt.Sprintf(". Score %0.2f", score)
		scoreS = fmt.Sprintf("%0.2f", score)
	}

	return labelText, shareText, scoreS
}

const SCORE_COMPUTE_MAX = 10000.0
const MAX_SCORE = 10.0
const MAX_TIME_PENALTY = 6000

func computeScore(clues, hints, completed_seconds int) float64 {
	completed_seconds -= clues * 10
	if completed_seconds < 0 {
		completed_seconds = 0
	}

	penalty := completed_seconds * 5
	if penalty > MAX_TIME_PENALTY {
		penalty = MAX_TIME_PENALTY
	}
	penalty += (clues - 1) * 300
	penalty += hints * 800
	score := SCORE_COMPUTE_MAX - float64(penalty)

	scoreF := float64(score) / float64(SCORE_COMPUTE_MAX/MAX_SCORE)
	if scoreF < 1 {
		scoreF = 1
	} else if scoreF > MAX_SCORE {
		scoreF = MAX_SCORE
	}
	return scoreF
}

func getInfoText(status gamedb.GameStatus) string {
	text := ""
	age := time.Since(*status.CreateTime).Round(time.Second)
	ageStr := age.String()
	if age > 48*time.Hour {
		days := int64(age) / int64(24*time.Hour)
		ageStr = fmt.Sprintf("%d days", days)
	}

	if status.ChallengeId != "" {
		text += fmt.Sprintf(`Challenge <a class="link link-primary" href=challenge?id=%s>%s</a>,`, status.ChallengeId, status.ChallengeId)
	}
	text += fmt.Sprintf(` Game <a class="link link-primary" href=game?id=%s>%s</a>,`, status.GameId, status.GameId)
	text += fmt.Sprintf(" created %s ago", ageStr)
	return text
}

func (g *GameAPI) GetStats() (map[string]interface{}, error) {
	stats, err := g.db.GetStats()
	if err != nil {
		return nil, err
	}
	return stats, nil
}

func (g *GameAPI) CleanupDB() {
	for {
		g.db.Cleanup(gamedb.Created, CleanupUnstarted)
		g.db.Cleanup(gamedb.Started, CleanupStarted)
		g.db.Cleanup(gamedb.Completed, CleanupCompleted)
		g.db.Cleanup(gamedb.Resigned, CleanupResigned)

		time.Sleep(time.Hour)
	}
}

func (g *GameAPI) Close() {
	g.db.Close()
}
