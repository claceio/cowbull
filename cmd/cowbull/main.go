// Command cowbull runs the CowBull game API server.
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"

	game "cowbull.co/game"
	gameapi "cowbull.co/game/api"
	gamedb "cowbull.co/game/db"
)

const shutdownWait = 10 * time.Second

var (
	dbFile        = flag.String("f", "./db.sqlite", "database file")
	port          = flag.Int("p", 9999, "port number")
	host          = flag.String("b", "0.0.0.0", "bind address")
	enableUpgrade = flag.Bool("u", true, "enable database upgrade")
	ipLookupFile  = flag.String("l", "./GeoLite2-City.mmdb", "the GeoLite IP lookup file")
)

func main() {
	flag.Parse()
	db := gamedb.NewDB(*dbFile)
	if ok := db.VersionUpgrade(*enableUpgrade); !ok {
		log.Fatal("invalid db version and upgrade disabled")
	}

	api := gameapi.NewGameAPI(db, game.WordFS, *ipLookupFile)
	apiHandler := NewAPIHandler(api)

	// Request contexts derive from appCtx; cancelling it on shutdown ends
	// the long-lived SSE streams so Shutdown does not hang on them
	appCtx, cancelRequests := context.WithCancel(context.Background())
	srv := http.Server{
		Addr: *host + ":" + strconv.Itoa(*port),
		// No WriteTimeout: SSE connections are long-lived
		ReadTimeout: 60 * time.Second,
		IdleTimeout: 120 * time.Second,
		Handler:     apiHandler.Router(),
		BaseContext: func(net.Listener) context.Context { return appCtx },
	}

	go api.CleanupDB()
	go func() {
		log.Printf("CowBull listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig

	log.Println("Shutting down")
	cancelRequests()
	ctx, cancel := context.WithTimeout(context.Background(), shutdownWait)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %s", err)
	}
	api.Close()
}
