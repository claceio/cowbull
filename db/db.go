// Package db implements sqlite storage for games, challenges and events.
package db

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const CURRENT_DB_VERSION = 6

type Status string

const (
	Created   Status = "CREATED"
	Started   Status = "STARTED"
	Completed Status = "COMPLETED"
	Resigned  Status = "RESIGNED"
)

const sqliteTimeFormat = "2006-01-02 15:04:05"

type GameDB struct {
	db *sql.DB
}

type GameStatus struct {
	GameId           string
	Status           string
	Location         string
	Word             string
	ChallengeId      string
	CompletedSeconds int
	CreateTime       *time.Time
	StartTime        *time.Time
	Duration         sql.NullString
	PlayerId         string
	PlayerName       string
	Round            int
}

type Challenge struct {
	ChallengeId string
	Word        string
	Type        string
	NumRounds   int
	Title       string
}

type GameClue struct {
	Clue  string
	Bulls int
	Cows  int
	Hint  bool
}

type ClueCount struct {
	Guesses int
	Hints   int
}

type ChallengeEvent struct {
	Message   string
	EventTime *time.Time
}

func NewDB(dbFile string) *GameDB {
	// busy_timeout and synchronous are per-connection settings, passed as
	// _pragma DSN params so the driver applies them to every pooled
	// connection. journal_mode=WAL is persisted in the database file and
	// racing conversions on new connections fail with SQLITE_BUSY, so it
	// is set once after open.
	db, err := sql.Open("sqlite", "file:"+dbFile+"?_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		panic(err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		panic(err)
	}
	// Keep idle connections around: sqlite connections are file opens plus
	// pragma execs, and churn costs more than the idle handles
	db.SetMaxIdleConns(10)
	return &GameDB{db}
}

// parseSqliteTime accepts datetime('now') text plus the formats written by
// older versions of the app.
func parseSqliteTime(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	layouts := []string{
		sqliteTimeFormat,
		"2006-01-02 15:04:05.999999999-07:00",
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s.String); err == nil {
			return &t
		}
	}
	return nil
}

func formatSqliteTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(sqliteTimeFormat)
}

func (db *GameDB) VersionUpgrade(enableUpgrade bool) bool {
	version := 0
	_ = db.db.QueryRow("SELECT version FROM version").Scan(&version)

	if version < CURRENT_DB_VERSION && !enableUpgrade {
		return false
	}

	if version < 1 {
		log.Println("No version, initializing")
		db.execStmt(`create table version (version int, last_upgraded datetime)`)
		db.execStmt(`insert into version values (1, datetime('now'))`)
		db.execStmt(`create table games(game_id varchar(30) UNIQUE, started_by varchar(30), game_type varchar(10), word varchar(10), status varchar(20), create_time datetime, start_time datetime, end_time datetime, duration varchar(20))`)
		db.execStmt(`create table clues(game_id varchar(30), clue varchar(10), bulls int, cows int, entry_time datetime, UNIQUE(game_id, clue))`)
	}

	if version < 2 {
		log.Println("Upgrading to version 2")
		db.execStmt(`alter table clues add column hint int default 0`)
		db.execStmt(`update version set version = 2, last_upgraded = datetime('now')`)
	}

	if version < 3 {
		log.Println("Upgrading to version 3")
		db.execStmt(`alter table games add column challenge_id varchar(30) default null`)
		db.execStmt(`alter table games add column location varchar(100) default null`)
		db.execStmt(`alter table games add column completed_seconds int default 0`)
		db.execStmt(`alter table games drop column game_type`)
		db.execStmt(`create table challenges(challenge_id varchar(30) UNIQUE, started_by varchar(30), word varchar(10), create_time datetime)`)
		db.execStmt(`update version set version = 3, last_upgraded = datetime('now')`)
	}

	if version < 4 {
		db.execStmt(`create index game_clues on clues(game_id)`)
		db.execStmt(`create index challenge_games on games(challenge_id)`)
		db.execStmt(`update version set version = 4, last_upgraded = datetime('now')`)
	}

	if version < 5 {
		log.Println("Upgrading to version 5")
		db.execStmt(`alter table games add column player_id varchar(64) default ''`)
		db.execStmt(`alter table games add column player_name varchar(64) default ''`)
		db.execStmt(`alter table games add column round int default 1`)
		db.execStmt(`alter table challenges add column challenge_type varchar(20) default 'CHALLENGE'`)
		db.execStmt(`alter table challenges add column num_rounds int default 1`)
		db.execStmt(`create table challenge_words(challenge_id varchar(30), round int, word varchar(10), UNIQUE(challenge_id, round))`)
		db.execStmt(`create table challenge_events(id integer primary key autoincrement, challenge_id varchar(30), message varchar(300), event_time datetime)`)
		db.execStmt(`create index challenge_events_idx on challenge_events(challenge_id)`)
		db.execStmt(`update version set version = 5, last_upgraded = datetime('now')`)
	}

	if version < 6 {
		log.Println("Upgrading to version 6")
		db.execStmt(`alter table challenges add column title varchar(80) default ''`)
		db.execStmt(`update version set version = 6, last_upgraded = datetime('now')`)
	}

	// Add version upgrade code here
	return true
}

func (db *GameDB) CreateChallenge(challengeId, ipAddr, word, challengeType string, numRounds int, title string) error {
	_, err := db.db.Exec(`INSERT into challenges(challenge_id, started_by, word, create_time, challenge_type, num_rounds, title)
		values(?, ?, ?, datetime('now'), ?, ?, ?)`,
		challengeId, ipAddr, word, challengeType, numRounds, title)
	return err
}

func (db *GameDB) InsertChallengeWord(challengeId string, round int, word string) error {
	_, err := db.db.Exec(`INSERT into challenge_words(challenge_id, round, word) values(?, ?, ?)`,
		challengeId, round, word)
	return err
}

func (db *GameDB) GetChallengeWord(challengeId string, round int) (string, error) {
	var word string
	err := db.db.QueryRow(`select word from challenge_words where challenge_id = ? and round = ?`,
		strings.ToUpper(challengeId), round).Scan(&word)
	if err != nil {
		return "", errors.New("no word for round")
	}
	return word, nil
}

func (db *GameDB) GetChallenge(challengeId string) (*Challenge, error) {
	challengeId = strings.ToUpper(challengeId)
	var word, ctype, title string
	var numRounds int
	err := db.db.QueryRow(`select word, challenge_type, num_rounds, title
		from challenges where challenge_id = ?`, challengeId).
		Scan(&word, &ctype, &numRounds, &title)
	if err != nil {
		return nil, errors.New("invalid challenge id")
	}

	return &Challenge{challengeId, word, ctype, numRounds, title}, nil
}

func (db *GameDB) CreateGame(gameId, word, ipAddr, location, challengeId string, round int, playerId, playerName string) error {
	_, err := db.db.Exec(`INSERT into games(game_id, word, started_by, location, status, create_time, challenge_id, round, player_id, player_name)
		values(?, ?, ?, ?, 'CREATED', datetime('now'), ?, ?, ?, ?)`,
		gameId, word, ipAddr, location, challengeId, round, playerId, playerName)
	return err
}

const gameColumns = `game_id, word, status, location, challenge_id, completed_seconds,
	create_time, start_time, duration, player_id, player_name, round`

func scanGame(row interface{ Scan(...any) error }) (*GameStatus, error) {
	var gs GameStatus
	var duration sql.NullString
	var createTime, startTime sql.NullString
	err := row.Scan(&gs.GameId, &gs.Word, &gs.Status, &gs.Location, &gs.ChallengeId,
		&gs.CompletedSeconds, &createTime, &startTime, &duration,
		&gs.PlayerId, &gs.PlayerName, &gs.Round)
	if err != nil {
		return nil, err
	}
	gs.CreateTime = parseSqliteTime(createTime)
	gs.StartTime = parseSqliteTime(startTime)
	gs.Duration = duration
	return &gs, nil
}

func (db *GameDB) queryGames(query string, args ...any) ([]*GameStatus, error) {
	rows, err := db.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	games := make([]*GameStatus, 0)
	for rows.Next() {
		gs, err := scanGame(rows)
		if err != nil {
			return nil, err
		}
		games = append(games, gs)
	}
	return games, rows.Err()
}

func (db *GameDB) CheckGameId(gameId string) (*GameStatus, error) {
	gs, err := scanGame(db.db.QueryRow(
		`select `+gameColumns+` from games where game_id = ?`, strings.ToUpper(gameId)))
	if err != nil {
		return nil, errors.New("invalid game id")
	}
	return gs, nil
}

func (db *GameDB) GetChallengeGameStatuses(challengeId string) ([]*GameStatus, error) {
	return db.queryGames(`select `+gameColumns+` from games where challenge_id = ? order by create_time asc`,
		strings.ToUpper(challengeId))
}

// GetPlayerChallengeGames returns a player's games in a challenge, by round.
func (db *GameDB) GetPlayerChallengeGames(challengeId, playerId string) ([]*GameStatus, error) {
	return db.queryGames(`select `+gameColumns+` from games where challenge_id = ? and player_id = ? order by round asc`,
		strings.ToUpper(challengeId), playerId)
}

// LastPlayerName returns the most recent name a player id has played under.
func (db *GameDB) LastPlayerName(playerId string) string {
	if playerId == "" {
		return ""
	}
	var name string
	err := db.db.QueryRow(`select player_name from games where player_id = ? and player_name != ''
		order by create_time desc limit 1`, playerId).Scan(&name)
	if err != nil {
		return ""
	}
	return name
}

func (db *GameDB) InsertClue(gameId, clue string, bulls, cows int, hint bool) error {
	_, err := db.db.Exec(`INSERT into clues(game_id, clue, bulls, cows, hint, entry_time)
		values(?, ?, ?, ?, ?, datetime('now'))`,
		strings.ToUpper(gameId), clue, bulls, cows, hint)
	if err != nil {
		return errors.New(clue + " is already used")
	}
	return nil
}

func (db *GameDB) GetClues(gameId string) ([]GameClue, error) {
	rows, err := db.db.Query(`select clue, bulls, cows, hint from clues where game_id = ?
		order by entry_time desc, rowid desc`, strings.ToUpper(gameId))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	clues := make([]GameClue, 0)
	for rows.Next() {
		var c GameClue
		if err := rows.Scan(&c.Clue, &c.Bulls, &c.Cows, &c.Hint); err != nil {
			return nil, err
		}
		clues = append(clues, c)
	}
	return clues, rows.Err()
}

// GetChallengeClueCounts returns guess/hint counts per game of a challenge
// in a single query.
func (db *GameDB) GetChallengeClueCounts(challengeId string) (map[string]ClueCount, error) {
	rows, err := db.db.Query(`select g.game_id, count(c.clue), coalesce(sum(c.hint), 0)
		from games g left join clues c on c.game_id = g.game_id
		where g.challenge_id = ? group by g.game_id`, strings.ToUpper(challengeId))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]ClueCount{}
	for rows.Next() {
		var gameId string
		var total, hints int
		if err := rows.Scan(&gameId, &total, &hints); err != nil {
			return nil, err
		}
		counts[gameId] = ClueCount{Guesses: total - hints, Hints: hints}
	}
	return counts, rows.Err()
}

func (db *GameDB) GetClueCounts(gameId string) (ClueCount, error) {
	var total, hints int
	err := db.db.QueryRow(`select count(*), coalesce(sum(hint), 0) from clues where game_id = ?`,
		strings.ToUpper(gameId)).Scan(&total, &hints)
	return ClueCount{Guesses: total - hints, Hints: hints}, err
}

func (db *GameDB) InsertEvent(challengeId, message string) error {
	_, err := db.db.Exec(`INSERT into challenge_events(challenge_id, message, event_time)
		values(?, ?, datetime('now'))`, strings.ToUpper(challengeId), message)
	return err
}

func (db *GameDB) GetEvents(challengeId string, limit int) ([]ChallengeEvent, error) {
	rows, err := db.db.Query(`select message, event_time from challenge_events
		where challenge_id = ? order by id desc limit ?`, strings.ToUpper(challengeId), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]ChallengeEvent, 0)
	for rows.Next() {
		var ev ChallengeEvent
		var eventTime sql.NullString
		if err := rows.Scan(&ev.Message, &eventTime); err != nil {
			return nil, err
		}
		ev.EventTime = parseSqliteTime(eventTime)
		events = append(events, ev)
	}
	return events, rows.Err()
}

// GetStats returns started/completed counts (all and challenge-only) for a
// set of trailing durations, keyed by hours.
func (db *GameDB) GetStats() (map[string]any, error) {
	durations := []int{1, 6, 24, 24 * 2, 24 * 7, 24 * 14, 24 * 30}

	stats := map[string]any{}
	for _, hours := range durations {
		var started, completed, chStarted, chCompleted int64
		err := db.db.QueryRow(`select count(*),
			coalesce(sum(status = 'COMPLETED'), 0),
			coalesce(sum(challenge_id != ''), 0),
			coalesce(sum(challenge_id != '' and status = 'COMPLETED'), 0)
			from games where create_time > datetime('now', ?)`,
			fmt.Sprintf("-%d hours", hours)).
			Scan(&started, &completed, &chStarted, &chCompleted)
		if err != nil {
			return nil, err
		}
		stats[fmt.Sprintf("%d", hours)] = map[string]int64{
			"st":    started,
			"co":    completed,
			"ch_st": chStarted,
			"ch_co": chCompleted,
		}
	}
	return stats, nil
}

func (db *GameDB) UpdateStatus(gameId, newStatus string, startTime *time.Time) error {
	var endTime *time.Time
	duration := ""
	completedSeconds := 0

	switch newStatus {
	case "STARTED":
		if startTime == nil {
			now := time.Now()
			startTime = &now
		}
	case "COMPLETED", "RESIGNED":
		now := time.Now()
		endTime = &now
		if startTime != nil {
			d := endTime.Sub(*startTime).Round(time.Second)
			duration = fmt.Sprint(d)
			if newStatus == "COMPLETED" {
				completedSeconds = int(d.Seconds())
			}
		} else {
			duration = "Not started"
		}
	}
	_, err := db.db.Exec(`UPDATE games set status = ?, start_time = ?, end_time = ?, duration = ?, completed_seconds = ?
		where game_id = ?`,
		newStatus, formatSqliteTime(startTime), formatSqliteTime(endTime), duration, completedSeconds, gameId)
	return err
}

// Cleanup deletes games (and their clues) in the given status older than the
// duration. The ABCDE game is kept as the permanent sample game.
func (db *GameDB) Cleanup(status Status, age time.Duration) error {
	cutoff := time.Now().Add(-age)
	_, err := db.db.Exec(`delete from clues where game_id in
		(select game_id from games where status = ? and create_time < ? and game_id != 'ABCDE')`,
		string(status), formatSqliteTime(&cutoff))
	if err != nil {
		return err
	}
	_, err = db.db.Exec(`delete from games where status = ? and create_time < ? and game_id != 'ABCDE'`,
		string(status), formatSqliteTime(&cutoff))
	return err
}

func (db *GameDB) Close() {
	db.db.Close()
}

func (db *GameDB) execStmt(stmt string) {
	if _, err := db.db.Exec(stmt); err != nil {
		panic(err)
	}
}
