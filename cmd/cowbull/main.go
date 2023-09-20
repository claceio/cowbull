package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"

	gameapi "cowbull.co/game/api"
	gamedb "cowbull.co/game/db"
)

const (
	RequestWait = 30 * time.Second
)

var dbFile = flag.String("f", "./db.sqlite", "database file")
var port = flag.Int("p", 9999, "port number")
var enableUpgrade = flag.Bool("u", true, "enable database upgrade")
var staticDir = flag.String("s", "./static", "the directory to serve files from")
var dictFile = flag.String("w", "./words.txt", "the word dictionary file")
var ipLookupFile = flag.String("l", "./GeoLite2-City.mmdb", "the GeoLite IP lookup file")

func main() {
	flag.Parse()
	db := gamedb.NewDB(*dbFile)
	if ok := db.VersionUpgrade(*enableUpgrade); !ok {
		panic("Invalid db version and upgrade disabled")
	}

	api := gameapi.NewGameAPI(db, *dictFile, *ipLookupFile)
	game := &GameHandler{api}
	srv := http.Server{
		Addr:         "127.0.0.1:" + strconv.FormatInt(int64(*port), 10),
		WriteTimeout: 60 * time.Second,
		ReadTimeout:  60 * time.Second,
		IdleTimeout:  30 * time.Second,
		Handler:      game.NewRouter(*staticDir),
	}

	go api.CleanupDB()
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Println(err)
		}
	}()

	c := make(chan os.Signal, 1)
	// We'll accept graceful shutdowns when quit via SIGINT (Ctrl+C)
	signal.Notify(c, os.Interrupt)

	// Block until we receive our signal.
	<-c

	// Create a deadline to wait for.
	ctx, cancel := context.WithTimeout(context.Background(), RequestWait)
	defer cancel()
	log.Println("Shutting down")
	srv.Shutdown(ctx)
	game.Shutdown()
	os.Exit(0)
}
