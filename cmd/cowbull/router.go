package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cowbull.co/game/api"
)

// APIHandler serves the JSON game API. All UI rendering happens in the
// openrun starlark app; this process only exposes /api endpoints (plus the
// SSE stream and the cookie-setting player endpoint, which the starlark app
// proxies straight through to the container).
type APIHandler struct {
	api *api.GameAPI
}

func NewAPIHandler(gameAPI *api.GameAPI) *APIHandler {
	return &APIHandler{api: gameAPI}
}

func (h *APIHandler) Router() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/game", h.createGame)
	mux.HandleFunc("GET /api/game/{id}", h.getGame)
	mux.HandleFunc("POST /api/game/{id}/submit", h.submit)
	mux.HandleFunc("POST /api/game/{id}/hint", h.hint)
	mux.HandleFunc("POST /api/game/{id}/resign", h.resign)

	mux.HandleFunc("POST /api/challenge", h.createChallenge)
	mux.HandleFunc("GET /api/challenge/{id}", h.getChallenge)
	mux.HandleFunc("GET /api/challenge/{id}/board", h.getBoard)
	mux.HandleFunc("POST /api/challenge/{id}/play", h.play)
	mux.HandleFunc("GET /api/challenge/{id}/events", h.events)

	mux.HandleFunc("POST /api/player", h.setPlayer)
	mux.HandleFunc("POST /api/player/ensure", h.ensurePlayer)
	mux.HandleFunc("GET /api/score", h.score)
	mux.HandleFunc("GET /api/ustats", h.stats)

	return mux
}

func writeJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"Error": err.Error()})
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

// playerName reads the player name from the request. The starlark UI passes
// the query-escaped cookie value as player_name_enc; plain player_name is
// accepted too.
func playerName(r *http.Request) string {
	if enc := r.FormValue("player_name_enc"); enc != "" {
		if dec, err := url.QueryUnescape(enc); err == nil {
			return api.SanitizeName(dec)
		}
		return ""
	}
	return api.SanitizeName(r.FormValue("player_name"))
}

// sanitizeId keeps player ids to hex-ish tokens
func sanitizeId(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	id = b.String()
	if len(id) > 64 {
		id = id[:64]
	}
	return id
}

func (h *APIHandler) createGame(w http.ResponseWriter, r *http.Request) {
	gameId := h.api.StartGame(getUserIP(r), r.FormValue("level"),
		sanitizeId(r.FormValue("player_id")), playerName(r))
	writeJSON(w, http.StatusOK, map[string]string{"GameId": gameId})
}

func (h *APIHandler) getGame(w http.ResponseWriter, r *http.Request) {
	view, err := h.api.GetGame(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"Game":  view,
		"Nudge": api.RandomNudge(),
		"Pun":   api.RandomWinPun(),
	})
}

func (h *APIHandler) submit(w http.ResponseWriter, r *http.Request) {
	res, err := h.api.Submit(r.PathValue("id"), r.FormValue("guess"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *APIHandler) hint(w http.ResponseWriter, r *http.Request) {
	if err := h.api.Hint(r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{})
}

func (h *APIHandler) resign(w http.ResponseWriter, r *http.Request) {
	if err := h.api.Resign(r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{})
}

func (h *APIHandler) createChallenge(w http.ResponseWriter, r *http.Request) {
	rounds, _ := strconv.Atoi(r.FormValue("rounds"))
	challengeId, err := h.api.CreateChallenge(getUserIP(r), r.FormValue("level"), rounds, r.FormValue("title"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ChallengeId": challengeId})
}

func (h *APIHandler) getChallenge(w http.ResponseWriter, r *http.Request) {
	challenge, err := h.api.GetChallenge(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	// The words stay server side
	writeJSON(w, http.StatusOK, map[string]any{
		"ChallengeId": challenge.ChallengeId,
		"Type":        challenge.Type,
		"Title":       challenge.Title,
		"NumRounds":   challenge.NumRounds,
	})
}

func (h *APIHandler) getBoard(w http.ResponseWriter, r *http.Request) {
	board, err := h.api.GetBoard(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, board)
}

func (h *APIHandler) play(w http.ResponseWriter, r *http.Request) {
	gameId, err := h.api.StartChallengeGame(r.PathValue("id"), getUserIP(r),
		sanitizeId(r.FormValue("player_id")), playerName(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"GameId": gameId})
}

const (
	cookiePlayerId   = "cb_pid"
	cookiePlayerName = "cb_name"
)

func playerCookie(name, value string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   2 * 365 * 24 * 60 * 60,
		SameSite: http.SameSiteLaxMode,
	}
}

// ensurePlayer assigns a stable player id cookie if the browser has none
// yet; pages call it once on load so anonymous players still have a stable
// identity across games.
func (h *APIHandler) ensurePlayer(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookiePlayerId); err == nil && c.Value != "" && len(c.Value) <= 64 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	http.SetCookie(w, playerCookie(cookiePlayerId, hex.EncodeToString(buf)))
	w.WriteHeader(http.StatusNoContent)
}

// setPlayer stores the player name (and a stable player id, assigned on
// first use) in cookies. This endpoint is proxied to the browser by the
// starlark app since only the container can set cookies.
func (h *APIHandler) setPlayer(w http.ResponseWriter, r *http.Request) {
	pid := ""
	if c, err := r.Cookie(cookiePlayerId); err == nil {
		pid = c.Value
	}
	if pid == "" || len(pid) > 64 {
		buf := make([]byte, 16)
		_, _ = rand.Read(buf)
		pid = hex.EncodeToString(buf)
	}
	http.SetCookie(w, playerCookie(cookiePlayerId, pid))

	name := api.SanitizeName(r.FormValue("name"))
	if name == "" {
		// Empty name resets to anonymous: remove the cookie
		cleared := playerCookie(cookiePlayerName, "")
		cleared.MaxAge = -1
		http.SetCookie(w, cleared)
	} else {
		http.SetCookie(w, playerCookie(cookiePlayerName, url.QueryEscape(name)))
	}

	if r.Header.Get("HX-Request") == "true" {
		// The starlark app renders the name everywhere; reload the page
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ref := r.Header.Get("Referer")
	if ref == "" {
		// Back to the app root, not the server root
		ref = r.Header.Get("X-Forwarded-Prefix") + "/"
	}
	http.Redirect(w, r, ref, http.StatusSeeOther)
}

// The toast markup classes must appear in a template file for the openrun
// tailwind build to keep them; layout.go.html has a hidden class anchor.
const toastBellSvg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" class="inline-block w-4 h-4 shrink-0"><path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9"/><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0"/></svg>`

var toastTemplate = template.Must(template.New("toast").Parse(
	`<div class="alert shadow-lg bg-base-100 text-sm" _="on load wait 6s then transition my opacity to 0 over 500ms then remove me">` +
		toastBellSvg + `<span>{{.}}</span></div>`))

func (h *APIHandler) events(w http.ResponseWriter, r *http.Request) {
	challengeId := strings.ToUpper(r.PathValue("id"))
	if _, err := h.api.GetChallenge(challengeId); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	pid := ""
	if c, err := r.Cookie(cookiePlayerId); err == nil {
		pid = c.Value
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ch, cancel := h.api.Events.Subscribe(challengeId)
	defer cancel()

	// Periodic comment keeps proxies from timing out the stream. Container
	// idle shutdown is disabled for this app (container.idle_shutdown_secs=0
	// in the app config), so no byte-padding is needed to stay alive.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case ev := <-ch:
			// Suppress a player's own activity toasts; origin-less events
			// (like moos) go to everyone
			if ev.Name == "activity" && (ev.Origin == "" || ev.Origin != pid) {
				var sb strings.Builder
				if err := toastTemplate.Execute(&sb, ev.Data); err == nil {
					fmt.Fprintf(w, "event: activity\ndata: %s\n\n", strings.ReplaceAll(sb.String(), "\n", " "))
				}
			}
			// Everyone refreshes the board, including the origin player
			fmt.Fprint(w, "event: board\ndata: refresh\n\n")
			flusher.Flush()
		}
	}
}

// score computes what a game with the given stats would score.
func (h *APIHandler) score(w http.ResponseWriter, r *http.Request) {
	atoi := func(name string, minVal int) int {
		v, _ := strconv.Atoi(r.FormValue(name))
		return max(v, minVal)
	}
	score := api.ComputeScore(atoi("guesses", 1), atoi("hints", 0), atoi("seconds", 0))
	writeJSON(w, http.StatusOK, map[string]string{"Score": fmt.Sprintf("%0.2f", score)})
}

func (h *APIHandler) stats(w http.ResponseWriter, r *http.Request) {
	ret, err := h.api.GetStats()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ret)
}
