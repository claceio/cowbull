// Package api implements the game rules, scoring and live events for
// CowBull, a bulls-and-cows word game.
package api

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"math/rand"
	"net"
	"sort"
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

	TypeTournament = "TOURNAMENT"

	MaxRounds = 10
)

type GameAPI struct {
	db       *gamedb.GameDB
	Events   *Broker
	ipLookup *geoip2.Reader
	wordFS   fs.FS
	stop     chan struct{}
	// Only the main dictionary stays in memory; level word lists are
	// streamed from the embedded files on demand.
	wordDict map[string]bool
}

func NewGameAPI(db *gamedb.GameDB, wordFS fs.FS, ipLookupFile string) *GameAPI {
	ipLookup, err := geoip2.Open(ipLookupFile)
	if err != nil {
		log.Printf("GeoIP lookup disabled: %s", err)
		ipLookup = nil
	}

	g := &GameAPI{db: db, ipLookup: ipLookup, wordFS: wordFS, Events: NewBroker(), stop: make(chan struct{})}
	g.wordDict = make(map[string]bool)
	g.scanWords("words.txt", func(word string) {
		g.wordDict[word] = true
	})
	return g
}

// scanWords streams a word file line by line without keeping it in memory.
func (g *GameAPI) scanWords(file string, fn func(word string)) {
	f, err := g.wordFS.Open(file)
	if err != nil {
		panic(err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		word := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if word != "" {
			fn(word)
		}
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
}

// getWord reservoir-samples one word from the level's word file.
func (g *GameAPI) getWord(level string) string {
	file := "words_hard.txt"
	switch strings.ToLower(level) {
	case "easy":
		file = "words_easy.txt"
	case "medium":
		file = "words_medium.txt"
	}

	picked := ""
	count := 0
	g.scanWords(file, func(word string) {
		count++
		if rand.Intn(count) == 0 {
			picked = word
		}
	})
	if picked == "" {
		panic("no words found in " + file)
	}
	return picked
}

func (g *GameAPI) getLocation(ipAddr string) string {
	if g.ipLookup == nil {
		return "Unknown"
	}
	ipAddr = strings.TrimSpace(strings.Split(ipAddr, ",")[0])
	if host, _, err := net.SplitHostPort(ipAddr); err == nil {
		ipAddr = host
	}

	location := ""
	if ip := net.ParseIP(ipAddr); ip != nil {
		if record, err := g.ipLookup.City(ip); err == nil {
			if name := record.City.Names["en"]; name != "N/A" {
				location = name
			}
			if location == "" && len(record.Subdivisions) > 0 && record.Subdivisions[0].Names["en"] != "N/A" {
				location = record.Subdivisions[0].Names["en"]
			}
			if name := record.Country.Names["en"]; name != "N/A" {
				if location != "" {
					location += " "
				}
				location += name
			}
		}
	}
	if location == "" {
		location = "Unknown"
	}
	return location
}

// challengeRounds returns the round count for a challenge, defaulting to 1.
func (g *GameAPI) challengeRounds(challengeId string) int {
	if ch, err := g.db.GetChallenge(challengeId); err == nil {
		return ch.NumRounds
	}
	return 1
}

// publish stores an activity event and pushes it to connected players.
func (g *GameAPI) publish(challengeId, origin, message string) {
	if challengeId == "" || message == "" {
		return
	}
	if err := g.db.InsertEvent(challengeId, message); err != nil {
		log.Printf("event insert failed: %s", err)
	}
	g.Events.Publish(challengeId, Event{Name: "activity", Data: message, Origin: origin})
}

// createGame inserts a game with a fresh unique id and returns the id.
func (g *GameAPI) createGame(word, ipAddr, location, challengeId string, round int, playerId, playerName string) string {
	for {
		gameId := randSeq(5)
		if err := g.db.CreateGame(gameId, word, ipAddr, location, challengeId, round, playerId, playerName); err != nil {
			log.Println(err)
			continue
		}
		return gameId
	}
}

// CreateChallenge creates a multi-player tournament of 1..MaxRounds
// rounds; a single round is a quick match. title is an optional display
// name.
func (g *GameAPI) CreateChallenge(ipAddr, level string, numRounds int, title string) (string, error) {
	numRounds = min(max(numRounds, 1), MaxRounds)

	words := make([]string, 0, numRounds)
	used := map[string]bool{}
	for len(words) < numRounds {
		w := g.getWord(level)
		if used[w] {
			continue
		}
		used[w] = true
		words = append(words, w)
	}

	var challengeId string
	for {
		challengeId = randSeq(4)
		// challenges.word keeps the round 1 word for backward compatibility
		err := g.db.CreateChallenge(challengeId, ipAddr, words[0], TypeTournament, numRounds, SanitizeTitle(title))
		if err != nil {
			log.Println(err)
			continue
		}
		break
	}
	for i, w := range words {
		if err := g.db.InsertChallengeWord(challengeId, i+1, w); err != nil {
			return "", err
		}
	}
	return challengeId, nil
}

func (g *GameAPI) GetChallenge(challengeId string) (*gamedb.Challenge, error) {
	return g.db.GetChallenge(challengeId)
}

func (g *GameAPI) getChallengeWord(challengeId string, round int) (string, error) {
	word, err := g.db.GetChallengeWord(challengeId, round)
	if err == nil {
		return word, nil
	}
	// Pre-tournament challenges only have the word on the challenge row
	if round == 1 {
		ch, cerr := g.db.GetChallenge(challengeId)
		if cerr != nil {
			return "", cerr
		}
		return ch.Word, nil
	}
	return "", err
}

// StartChallengeGame starts (or resumes) the player's next round and
// returns the game id to play.
func (g *GameAPI) StartChallengeGame(challengeId, ipAddr, playerId, playerName string) (string, error) {
	challengeId = strings.ToUpper(challengeId)
	challenge, err := g.db.GetChallenge(challengeId)
	if err != nil {
		return "", err
	}

	// Nameless players get a generated name, sticky via their player id
	if playerName == "" {
		playerName = g.db.LastPlayerName(playerId)
		if playerName == "" {
			playerName = generatePlayerName()
		}
	}

	prevGames, err := g.db.GetPlayerChallengeGames(challengeId, playerId)
	if err != nil {
		return "", err
	}
	for _, gs := range prevGames {
		if gs.Status == "CREATED" || gs.Status == "STARTED" {
			return gs.GameId, nil // resume the in-progress round
		}
	}

	round := len(prevGames) + 1
	if round > challenge.NumRounds {
		return "", errors.New("you have played all rounds of this game")
	}

	word, err := g.getChallengeWord(challengeId, round)
	if err != nil {
		return "", err
	}

	location := g.getLocation(ipAddr)
	gameId := g.createGame(word, ipAddr, location, challengeId, round, playerId, playerName)

	if round == 1 {
		g.publish(challengeId, playerId, joinMessage(playerName, location))
	} else {
		g.publish(challengeId, playerId, roundStartMessage(playerName, location, round, challenge.NumRounds))
	}
	return gameId, nil
}

func (g *GameAPI) StartGame(ipAddr, level, playerId, playerName string) string {
	return g.createGame(g.getWord(level), ipAddr, g.getLocation(ipAddr), "", 1, playerId, playerName)
}

// GameView is the game state exposed to the UI. The hidden word is only set
// once it is safe to reveal.
type GameView struct {
	GameId         string
	Status         string
	Location       string
	ChallengeId    string
	ChallengeType  string
	ChallengeTitle string
	Round          int
	NumRounds      int
	Word           string
	Score          string
	Time           string
	GuessCount     int
	HintCount      int
	Clues          []gamedb.GameClue
	PlayerName     string
	CreatedAgo     string
}

func (g *GameAPI) GetGame(gameId string) (*GameView, error) {
	gs, err := g.db.CheckGameId(gameId)
	if err != nil {
		return nil, err
	}
	clues, err := g.db.GetClues(gs.GameId)
	if err != nil {
		return nil, err
	}

	hints := 0
	for _, c := range clues {
		if c.Hint {
			hints++
		}
	}
	guesses := len(clues) - hints

	view := &GameView{
		GameId:      gs.GameId,
		Status:      gs.Status,
		Location:    gs.Location,
		ChallengeId: gs.ChallengeId,
		Round:       gs.Round,
		NumRounds:   1,
		Clues:       clues,
		GuessCount:  guesses,
		HintCount:   hints,
		PlayerName:  gs.PlayerName,
	}
	if gs.Duration.Valid {
		view.Time = gs.Duration.String
	}
	if gs.CreateTime != nil {
		age := time.Since(*gs.CreateTime).Round(time.Second)
		view.CreatedAgo = age.String()
		if age > 48*time.Hour {
			view.CreatedAgo = fmt.Sprintf("%d days", int64(age)/int64(24*time.Hour))
		}
	}

	if gs.ChallengeId != "" {
		if ch, err := g.db.GetChallenge(gs.ChallengeId); err == nil {
			view.NumRounds = ch.NumRounds
			view.ChallengeType = ch.Type
			view.ChallengeTitle = ch.Title
		}
	}

	// Solo words are revealed when the game ends; challenge words only to
	// the player who found them, since others may still be playing
	if gs.Status == "COMPLETED" || (gs.ChallengeId == "" && gs.Status == "RESIGNED") {
		view.Word = gs.Word
	}
	if gs.Status == "COMPLETED" {
		view.Score = fmt.Sprintf("%0.2f", ComputeScore(guesses, hints, gs.CompletedSeconds))
	}

	return view, nil
}

// setStatus updates a game status, logging failures.
func (g *GameAPI) setStatus(gameId, status string, startTime *time.Time) {
	if err := g.db.UpdateStatus(gameId, status, startTime); err != nil {
		log.Printf("status update %s -> %s failed: %s", gameId, status, err)
	}
}

// SubmitResult is returned from a guess submission.
type SubmitResult struct {
	Bulls, Cows int
	Won         bool
}

func (g *GameAPI) Submit(gameId, clue string) (*SubmitResult, error) {
	gameStatus, err := g.db.CheckGameId(gameId)
	if err != nil {
		return nil, err
	}
	if gameStatus.Status != "STARTED" && gameStatus.Status != "CREATED" {
		return nil, errors.New("Game already " + strings.ToLower(gameStatus.Status))
	}

	clue = strings.ToLower(strings.TrimSpace(clue))
	if len(clue) != 4 {
		return nil, errors.New(strings.ToUpper(clue) + ": guess should be four letters")
	}
	if !checkUniqueChars(clue) {
		return nil, errors.New(strings.ToUpper(clue) + " has repeating letters")
	}
	if !g.wordDict[clue] {
		return nil, errors.New(strings.ToUpper(clue) + " is not in the game dictionary")
	}

	bulls, cows := getClueStats(gameStatus.Word, clue)
	if err := g.db.InsertClue(gameStatus.GameId, clue, bulls, cows, false); err != nil {
		return nil, err
	}

	if gameStatus.Status == "CREATED" {
		g.setStatus(gameStatus.GameId, "STARTED", gameStatus.StartTime)
		gameStatus, _ = g.db.CheckGameId(gameStatus.GameId)
	}

	won := bulls == 4
	if won {
		g.setStatus(gameStatus.GameId, "COMPLETED", gameStatus.StartTime)
	}

	if gameStatus.ChallengeId != "" {
		g.notifyGuess(gameStatus, bulls, cows, won)
	}

	return &SubmitResult{Bulls: bulls, Cows: cows, Won: won}, nil
}

// notifyGuess publishes wins and near-misses to the challenge activity feed.
func (g *GameAPI) notifyGuess(gs *gamedb.GameStatus, bulls, cows int, won bool) {
	numRounds := g.challengeRounds(gs.ChallengeId)
	if won {
		score := ""
		if final, err := g.db.CheckGameId(gs.GameId); err == nil {
			if counts, err := g.db.GetClueCounts(gs.GameId); err == nil {
				score = fmt.Sprintf("%0.2f", ComputeScore(counts.Guesses, counts.Hints, final.CompletedSeconds))
			}
		}
		g.publish(gs.ChallengeId, gs.PlayerId, completedMessage(gs.PlayerName, gs.Location, gs.Round, numRounds, score))
	} else if bulls >= 3 || cows >= 4 {
		g.publish(gs.ChallengeId, gs.PlayerId, bigGuessMessage(gs.PlayerName, gs.Location, bulls, cows))
	}
}

func (g *GameAPI) Hint(gameId string) error {
	gameStatus, err := g.db.CheckGameId(gameId)
	if err != nil {
		return err
	}
	if gameStatus.Status != "STARTED" && gameStatus.Status != "CREATED" {
		return errors.New("Game already " + strings.ToLower(gameStatus.Status))
	}

	clues, err := g.db.GetClues(gameStatus.GameId)
	if err != nil {
		return err
	}
	hint, err := g.getHint(gameStatus, clues)
	if err != nil {
		return err
	}

	bulls, cows := getClueStats(gameStatus.Word, hint)
	if err := g.db.InsertClue(gameStatus.GameId, hint, bulls, cows, true); err != nil {
		return err
	}
	if gameStatus.Status == "CREATED" {
		g.setStatus(gameStatus.GameId, "STARTED", gameStatus.StartTime)
	}
	return nil
}

// getHint finds an unused dictionary word that reveals exactly one letter
// position of the hidden word.
func (g *GameAPI) getHint(status *gamedb.GameStatus, clues []gamedb.GameClue) (string, error) {
	word := status.Word
	charPosition := make(map[rune]int)
	for i, r := range word {
		charPosition[r] = i
	}

	used := make(map[string]bool, len(clues))
	for _, c := range clues {
		used[c.Clue] = true
	}

	indexes := []int{0, 1, 2, 3}
	rand.Shuffle(len(indexes), func(i, j int) {
		indexes[i], indexes[j] = indexes[j], indexes[i]
	})
	for _, i := range indexes {
		// Map iteration order is randomized, which shuffles the candidates
		for w := range g.wordDict {
			if w[i] == word[i] && !used[w] {
				if bulls, cows := getClueStatsFromMap(charPosition, w); bulls == 1 && cows == 0 {
					return w, nil
				}
			}
		}
	}
	return "", errors.New("no suitable hint found")
}

func (g *GameAPI) Resign(gameId string) error {
	gameStatus, err := g.db.CheckGameId(gameId)
	if err != nil {
		return err
	}
	if gameStatus.Status != "CREATED" && gameStatus.Status != "STARTED" {
		return errors.New("Game already " + strings.ToLower(gameStatus.Status))
	}
	if err := g.db.UpdateStatus(gameStatus.GameId, "RESIGNED", gameStatus.StartTime); err != nil {
		return err
	}

	if gameStatus.ChallengeId != "" {
		numRounds := g.challengeRounds(gameStatus.ChallengeId)
		g.publish(gameStatus.ChallengeId, gameStatus.PlayerId,
			resignMessage(gameStatus.PlayerName, gameStatus.Location, gameStatus.Round, numRounds))
	}
	return nil
}

// RoundResult is one player's result for one round of a challenge.
type RoundResult struct {
	Round   int
	Status  string
	Score   float64
	ScoreS  string
	Guesses int
	Hints   int
	Time    string
	Active  bool
}

// PlayerRow is one player's aggregate standing in a challenge.
type PlayerRow struct {
	PlayerId   string
	Name       string
	Location   string
	Rounds     []*RoundResult // indexed by round-1, nil if not played
	Total      float64
	TotalS     string
	CurGuesses int // guesses in the player's current (latest) game
	CurHints   int
	RoundsDone int
	Finished   bool // completed every round
	DNF        bool // gave up on a round
	IsLeader   bool
}

// Board is the live leaderboard for a challenge.
type Board struct {
	ChallengeId string
	Type        string
	Title       string
	NumRounds   int
	Players     []*PlayerRow
	Events      []gamedb.ChallengeEvent
	AnyFinished bool
}

func (g *GameAPI) GetBoard(challengeId string) (*Board, error) {
	challengeId = strings.ToUpper(challengeId)
	challenge, err := g.db.GetChallenge(challengeId)
	if err != nil {
		return nil, err
	}
	games, err := g.db.GetChallengeGameStatuses(challengeId)
	if err != nil {
		return nil, err
	}
	clueCounts, err := g.db.GetChallengeClueCounts(challengeId)
	if err != nil {
		return nil, err
	}

	players := map[string]*PlayerRow{}
	order := []string{}
	for _, gs := range games {
		pid, name := gs.PlayerId, gs.PlayerName
		if pid == "" {
			// Games from before player identity existed
			pid = gs.GameId
			if name == "" {
				name = "Guest"
			}
		}
		row, ok := players[pid]
		if !ok {
			row = &PlayerRow{
				PlayerId: pid,
				Name:     name,
				Location: gs.Location,
				Rounds:   make([]*RoundResult, challenge.NumRounds),
			}
			players[pid] = row
			order = append(order, pid)
		}
		if name != "" {
			row.Name = name
		}

		counts := clueCounts[gs.GameId]
		// games are ordered by create time, so the last one is current
		row.CurGuesses = counts.Guesses
		row.CurHints = counts.Hints

		if gs.Round < 1 || gs.Round > challenge.NumRounds {
			continue
		}
		rr := &RoundResult{
			Round:   gs.Round,
			Status:  gs.Status,
			Guesses: counts.Guesses,
			Hints:   counts.Hints,
			Active:  gs.Status == "CREATED" || gs.Status == "STARTED",
		}
		if gs.Duration.Valid {
			rr.Time = gs.Duration.String
		}
		if gs.Status == "COMPLETED" {
			rr.Score = ComputeScore(counts.Guesses, counts.Hints, gs.CompletedSeconds)
			rr.ScoreS = fmt.Sprintf("%0.2f", rr.Score)
		}
		row.Rounds[gs.Round-1] = rr
	}

	board := &Board{
		ChallengeId: challengeId,
		Type:        challenge.Type,
		Title:       challenge.Title,
		NumRounds:   challenge.NumRounds,
	}
	for _, pid := range order {
		row := players[pid]
		for _, rr := range row.Rounds {
			if rr == nil {
				continue
			}
			if rr.Status == "COMPLETED" {
				row.RoundsDone++
				row.Total += rr.Score
			}
			if rr.Status == "RESIGNED" {
				row.DNF = true
			}
		}
		row.Finished = row.RoundsDone == challenge.NumRounds
		board.AnyFinished = board.AnyFinished || row.Finished
		row.TotalS = fmt.Sprintf("%0.2f", row.Total)
		board.Players = append(board.Players, row)
	}

	// Rank by running total: stopping early keeps the score in play
	sort.SliceStable(board.Players, func(i, j int) bool {
		return board.Players[i].Total > board.Players[j].Total
	})
	if len(board.Players) > 0 && board.Players[0].Total > 0 {
		board.Players[0].IsLeader = true
	}

	if events, err := g.db.GetEvents(challengeId, 20); err == nil {
		board.Events = events
	}
	return board, nil
}

const (
	scoreComputeMax = 10000.0
	maxScore        = 10.0
	maxTimePenalty  = 6000
)

// ComputeScore maps guesses, hints and time taken to a 1..10 score.
func ComputeScore(guesses, hints, completedSeconds int) float64 {
	completedSeconds = max(completedSeconds-guesses*10, 0)

	penalty := min(completedSeconds*5, maxTimePenalty)
	penalty += (guesses - 1) * 300
	penalty += hints * 800

	score := (scoreComputeMax - float64(penalty)) / (scoreComputeMax / maxScore)
	return min(max(score, 1), maxScore)
}

func (g *GameAPI) GetStats() (map[string]any, error) {
	return g.db.GetStats()
}

// CleanupDB periodically deletes old games based on their status, until
// Close is called.
func (g *GameAPI) CleanupDB() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		for status, age := range map[gamedb.Status]time.Duration{
			gamedb.Created:   CleanupUnstarted,
			gamedb.Started:   CleanupStarted,
			gamedb.Completed: CleanupCompleted,
			gamedb.Resigned:  CleanupResigned,
		} {
			if err := g.db.Cleanup(status, age); err != nil {
				log.Printf("cleanup %s failed: %s", status, err)
			}
		}
		select {
		case <-g.stop:
			return
		case <-ticker.C:
		}
	}
}

func (g *GameAPI) Close() {
	close(g.stop)
	if g.ipLookup != nil {
		g.ipLookup.Close()
	}
	g.db.Close()
}
