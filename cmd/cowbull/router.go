package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"cowbull.co/game/api"
	"github.com/gorilla/mux"
)

type GameHandler struct {
	api *api.GameAPI
}

func (g *GameHandler) NewRouter(static_dir string) *mux.Router {
	r := mux.NewRouter().StrictSlash(false)

	r.Methods("POST").Path("/api/create_game/{level}").HandlerFunc(g.start)
	r.Methods("POST").Path("/api/game/{game_id}/submit/{clue}").HandlerFunc(g.submit)
	r.Methods("POST").Path("/api/game/{game_id}/hint").HandlerFunc(g.hint)
	r.Methods("POST").Path("/api/game/{game_id}/resign").HandlerFunc(g.resign)
	r.Methods("GET").Path("/api/game/{game_id}").HandlerFunc(g.getGame)
	r.Methods("GET").Path("/api/game/{game_id}/clues").HandlerFunc(g.getCluesHtml)

	r.Methods("POST").Path("/api/create_challenge/{level}").HandlerFunc(g.createChallenge)
	r.Methods("GET").Path("/api/challenge/{challenge_id}").HandlerFunc(g.getChallenge)
	r.Methods("GET").Path("/api/challenge/{challenge_id}/games").HandlerFunc(g.getChallengeGames)
	r.Methods("POST").Path("/api/challenge/{challenge_id}/play").HandlerFunc(g.startChallengeGame)

	r.Methods("GET").Path("/api/ustats").HandlerFunc(g.getStats)
	return r
}

func (g *GameHandler) Shutdown() {
	g.api.Close()
}

func (g *GameHandler) start(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	ret := g.api.StartGame(getUserIP(r), vars["level"])
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ret)
}

func getUserIP(r *http.Request) string {
	IPAddress := r.Header.Get("X-Real-Ip")
	if IPAddress == "" {
		IPAddress = r.Header.Get("X-Forwarded-For")
	}
	if IPAddress == "" {
		IPAddress = r.RemoteAddr
	}
	return IPAddress
}

func (g *GameHandler) getGame(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	ret, err := g.api.GetGame(vars["game_id"])
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"Status": err.Error(), "GameInfo": ""})
		return
	}

	w.WriteHeader(http.StatusOK)
	delete(ret, "Clues")
	json.NewEncoder(w).Encode(ret)
}

func (g *GameHandler) createChallenge(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	ret, err := g.api.CreateChallenge(getUserIP(r), vars["level"])
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"Status": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ret)
}

func (g *GameHandler) getChallenge(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	ret, err := g.api.GetChallenge(vars["challenge_id"])
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"Status": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ret)
}

func (g *GameHandler) getChallengeGames(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	games, err := g.api.GetChallengeGames(vars["challenge_id"])
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "%s", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(games)
}

func (g *GameHandler) startChallengeGame(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	ret, err := g.api.StartChallengeGame(vars["challenge_id"], getUserIP(r))
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"Status": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ret)
}

func (g *GameHandler) getCluesHtml(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	ret, err := g.api.GetGame(vars["game_id"])
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "%s", err)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ret["Clues"])
}

func (g *GameHandler) submit(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	ret, err := g.api.Submit(vars["game_id"], vars["clue"])
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"Status": err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ret)
}

func (g *GameHandler) hint(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	ret, err := g.api.Hint(vars["game_id"])
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"Status": err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ret)
}

func (g *GameHandler) resign(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	ret, err := g.api.Resign(vars["game_id"])
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"Status": err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ret)
}

func (g *GameHandler) getStats(w http.ResponseWriter, r *http.Request) {
	ret, err := g.api.GetStats()
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"Status": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ret)
}
