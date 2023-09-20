package db

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	CURRENT_DB_VERSION = 4
)

type Status string

const (
	Created   Status = "CREATED"
	Started   Status = "STARTED"
	Completed Status = "COMPLETED"
	Resigned  Status = "RESIGNED"
)

type GameDB struct {
	db *sql.DB
}

type GameStatus struct {
	GameId           string
	Status           string
	Location         string
	StartedBy        string
	Word             string
	ChallengeId      string
	CompletedSeconds int
	CreateTime       *time.Time
	StartTime        *time.Time
	EndTime          *time.Time
	Duration         sql.NullString
	Score            string
	Stats            string
}

type Challenge struct {
	ChallengeId string
	Word        string
	CreateTime  *time.Time
}

type GameClue struct {
	Clue  string
	Bulls int
	Cows  int
	Hint  bool
}

func NewDB(db_file string) *GameDB {
	db, err := sql.Open("sqlite3", db_file)
	checkErr(err)
	return &GameDB{db}
}

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}

func (db *GameDB) VersionUpgrade(enable_upgrade bool) bool {
	row := db.db.QueryRow("SELECT version, last_upgraded FROM version")
	version := 0
	var dt time.Time
	_ = row.Scan(&version, &dt)

	if version < CURRENT_DB_VERSION && !enable_upgrade {
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

	// Add version upgrade code here
	return true
}

func (db *GameDB) CreateChallenge(challengeId, ipAddr, word string) error {
	stmt, err := db.db.Prepare(`INSERT into challenges(challenge_id, started_by, word, create_time) values(?, ?, ?, datetime('now'))`)
	checkErr(err)
	_, err = stmt.Exec(challengeId, ipAddr, word)
	return err
}

func (db *GameDB) GetChallenge(challengeId string) (*Challenge, error) {
	challengeId = strings.ToUpper(challengeId)
	stmt, err := db.db.Prepare(`select started_by, word, create_time from challenges where challenge_id = ?`)
	checkErr(err)
	row := stmt.QueryRow(challengeId)
	var startedBy, word string
	var createTime *time.Time
	err = row.Scan(&startedBy, &word, &createTime)
	if err != nil {
		return nil, errors.New("Invalid challenge id")
	}

	return &Challenge{startedBy, word, createTime}, nil
}

func (db *GameDB) GetChallengeGames(challengeId string) ([]GameStatus, error) {
	challengeId = strings.ToUpper(challengeId)
	stmt, err := db.db.Prepare(`select game_id from games where challenge_id = ? order by create_time asc`)
	checkErr(err)
	rows, err := stmt.Query(challengeId)
	if err != nil {
		return nil, errors.New("Invalid challenge id")
	}
	defer rows.Close()
	games := make([]GameStatus, 0)
	var gameId string
	for rows.Next() {
		err := rows.Scan(&gameId)
		if err != nil {
			return nil, err
		}
		game, err := db.CheckGameId(gameId)
		if err != nil {
			return nil, err
		}
		games = append(games, *game)
	}

	return games, err
}

func (db *GameDB) CreateGame(gameId, word string, ipAddr string, location string, challengeId string) error {
	stmt, err := db.db.Prepare(`INSERT into games(game_id, word, started_by, location, status, create_time, challenge_id) values(?, ?, ?, ?, "CREATED", datetime('now'), ?)`)
	checkErr(err)
	_, err = stmt.Exec(gameId, word, ipAddr, location, challengeId)
	return err
}

func (db *GameDB) CheckGameId(gameId string) (*GameStatus, error) {
	gameId = strings.ToUpper(gameId)
	stmt, err := db.db.Prepare(`select word, status, location, started_by, challenge_id, completed_seconds, create_time, start_time, end_time, duration, challenge_id from games where game_id = ?`)
	checkErr(err)
	row := stmt.QueryRow(gameId)
	var status, word, challengeId string
	var location, startedBy string
	var duration sql.NullString
	var createTime, startTime, endTime *time.Time
	var completed_seconds int
	err = row.Scan(&word, &status, &location, &startedBy, &challengeId, &completed_seconds, &createTime, &startTime, &endTime, &duration, &challengeId)
	if err != nil {
		return nil, errors.New("Invalid game id")
	}

	return &GameStatus{gameId, status, location, startedBy, word, challengeId, completed_seconds, createTime, startTime, endTime, duration, "", ""}, nil
}

func (db *GameDB) InsertClue(gameId, clue string, bulls, cows int, hint bool) error {
	gameId = strings.ToUpper(gameId)
	stmt, err := db.db.Prepare(`INSERT into clues(game_id, clue, bulls, cows, hint, entry_time) values(?, ?, ?, ?, ?, datetime('now'))`)
	checkErr(err)
	_, err = stmt.Exec(gameId, clue, bulls, cows, hint)
	if err != nil {
		return errors.New(clue + " is already used ...")
	}
	return nil
}

func (db *GameDB) GetClues(gameId string) ([]GameClue, error) {
	gameId = strings.ToUpper(gameId)
	stmt, err := db.db.Prepare(`select clue, bulls, cows, hint from clues where game_id = ? order by entry_time desc`)
	checkErr(err)
	rows, err := stmt.Query(gameId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ret := make([]GameClue, 0)
	for rows.Next() {
		var clue string
		var bulls int
		var cows int
		var hint bool
		err := rows.Scan(&clue, &bulls, &cows, &hint)
		if err != nil {
			return nil, err
		}

		ret = append(ret, GameClue{clue, bulls, cows, hint})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ret, nil
}

func (db *GameDB) GetCount(query string) (int64, error) {
	stmt, err := db.db.Prepare(query)
	checkErr(err)
	row := stmt.QueryRow()
	var count int64
	err = row.Scan(&count)
	if err != nil {
		return 0, errors.New("Invalid count query")
	}
	return count, nil
}

func (db *GameDB) GetStats() (map[string]interface{}, error) {
	durations := []int{1, 6, 24, 24 * 2, 24 * 7, 24 * 14, 24 * 30}

	ret := map[string]interface{}{}
	ret["st"] = map[int]int64{}
	ret["ch_st"] = map[int]int64{}
	ret["co"] = map[int]int64{}
	ret["ch_co"] = map[int]int64{}
	for _, duration := range durations {
		durationS := fmt.Sprintf("%d", duration)
		started, err := db.GetCount(`select count(*) from games where create_time > datetime('now', '-` + durationS + ` hours')`)
		if err != nil {
			return nil, err
		}

		completed, err := db.GetCount(`select count(*) from games where create_time > datetime('now', '-` + durationS + ` hours') and status = 'COMPLETED'`)
		if err != nil {
			return nil, err
		}

		cstarted, err := db.GetCount(`select count(*) from games where create_time > datetime('now', '-` + durationS + ` hours') and challenge_id != ""`)
		if err != nil {
			return nil, err
		}

		ccompleted, err := db.GetCount(`select count(*) from games where create_time > datetime('now', '-` + durationS + ` hours') and challenge_id != "" and status = 'COMPLETED'`)
		if err != nil {
			return nil, err
		}

		ret[durationS] = map[string]int64{
			"st":    started,
			"ch_st": cstarted,
			"co":    completed,
			"ch_co": ccompleted,
		}
		ret["st"].(map[int]int64)[duration] = started
		ret["ch_st"].(map[int]int64)[duration] = cstarted
		ret["co"].(map[int]int64)[duration] = completed
		ret["ch_co"].(map[int]int64)[duration] = ccompleted
	}

	return ret, nil
}

func (db *GameDB) UpdateStatus(gameId, currentStatus string, newStatus string, startTime *time.Time) error {
	var endTime time.Time
	duration := ""
	completed_seconds := 0

	switch newStatus {
	case "STARTED":
		if startTime == nil {
			tmpTime := time.Now()
			startTime = &tmpTime
		}
	case "COMPLETED", "RESIGNED":
		endTime = time.Now()
		if startTime != nil {
			d := endTime.Sub(*startTime).Round(time.Second)
			duration = fmt.Sprint(d)
			if newStatus == "COMPLETED" {
				completed_seconds = int(d.Seconds())
			}
		} else {
			duration = "Not started"
		}
	}
	stmt, err := db.db.Prepare(`UPDATE games set status = ?, start_time = ?, end_time = ?, duration = ?, completed_seconds = ? where game_id = ?`)
	checkErr(err)
	_, err = stmt.Exec(newStatus, startTime, &endTime, duration, completed_seconds, gameId)
	return err
}

func (db *GameDB) Cleanup(status Status, duration time.Duration) error {
	currentTime := time.Now()
	cleanupTime := currentTime.Add(-duration)

	stmt, err := db.db.Prepare(`select game_id, challenge_id from games where status = ? and create_time < ?`)
	checkErr(err)
	rows, err := stmt.Query(status, cleanupTime)
	if err != nil {
		return err
	}
	defer rows.Close()

	games := make([]string, 0, 10)
	for rows.Next() {
		var game string
		err := rows.Scan(&game)
		if err != nil {
			return err
		}
		games = append(games, game)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	for _, game := range games {
		err := db.CleanupGame(game)
		if err != nil {
			return err
		}
	}

	//log.Printf("Deleted %d games status %s createTime < %s\n", len(games), status, cleanupTime)
	return nil
}

func (db *GameDB) CleanupGame(gameId string) error {
	if gameId == "ABCDE" {
		// special game id used for help info
		return nil
	}

	stmt, err := db.db.Prepare(`delete from clues where game_id = ?`)
	checkErr(err)
	_, err = stmt.Exec(gameId)
	if err != nil {
		return err
	}

	stmt, err = db.db.Prepare(`delete from games where game_id = ?`)
	checkErr(err)
	_, err = stmt.Exec(gameId)
	if err != nil {
		return err
	}

	return nil
}

func (db *GameDB) Close() {
	db.db.Close()
}

func (db *GameDB) execStmt(stmt string) {
	_, err := db.db.Exec(stmt)
	checkErr(err)
}
