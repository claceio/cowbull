load("http.in", "http")
load("container.in", "container")
load("proxy.in", "proxy")

# All game/API logic lives in the Go server running in the app container;
# this file only renders the UI. The http plugin resolves container.URL to
# the container's proxy address at call time.
API = container.URL

def get_cookie(req, name):
    values = req.Headers.get("Cookie")
    if not values:
        return ""
    for part in values[0].split(";"):
        kv = part.strip().split("=", 1)
        if len(kv) == 2 and kv[0] == name:
            return kv[1]
    return ""


def form_value(req, name):
    value = req.Form.get(name)
    if not value:
        return ""
    return value[0]


def common(req):
    # PlayerNameEnc is the query-escaped cookie value; templates decode it
    # with queryUnescape, API calls pass it as player_name_enc for the Go
    # side to decode
    return {
        "PlayerNameEnc": get_cookie(req, "cb_name"),
        "PlayerId": get_cookie(req, "cb_pid"),
        "Error": "",
    }


def api_error(ret):
    # Returns the error string for a failed API call, "" when it succeeded
    if not ret:
        return str(ret.error)
    err = ret.value.json().get("Error")
    if err:
        return err
    return ""


def not_found(req, message):
    data = common(req)
    data["Message"] = message
    return ace.response(data, "error.go.html", code=404)


def index_handler(req):
    return common(req)


def create_game(req):
    ret = http.post(API + "/api/game", headers={"X-Forwarded-For": req.RemoteIP},
                    form_body={
                        "level": form_value(req, "level"),
                        "player_id": get_cookie(req, "cb_pid"),
                        "player_name_enc": get_cookie(req, "cb_name"),
                    }, error_on_fail=False)
    err = api_error(ret)
    if err:
        return not_found(req, err)
    return ace.redirect(req.AppPath + "/game/" + ret.value.json()["GameId"])


def fetch_game(req, game_id):
    ret = http.get(API + "/api/game/" + game_id, error_on_fail=False)
    if api_error(ret):
        return None
    data = common(req)
    data.update(ret.value.json())  # Game, Nudge, Pun
    data["Prefill"] = ""
    data["GamePath"] = req.AppPath + "/game/" + data["Game"]["GameId"]
    challenge_id = data["Game"]["ChallengeId"]
    if challenge_id:
        board = http.get(API + "/api/challenge/" + challenge_id + "/board", error_on_fail=False)
        if not api_error(board):
            data["Board"] = board.value.json()
    return data


def game_handler(req):
    data = fetch_game(req, req.UrlParams["game_id"])
    if not data:
        return not_found(req, "That game does not exist. Check the id?")
    return data


def game_action(req, action):
    game_id = req.UrlParams["game_id"]
    api_url = API + "/api/game/" + game_id + "/" + action
    guess = form_value(req, "guess")
    body = {}
    if action == "submit":
        body["guess"] = guess
    ret = http.post(api_url, form_body=body, error_on_fail=False)
    err = api_error(ret)

    data = fetch_game(req, game_id)
    if not data:
        return not_found(req, "That game does not exist. Check the id?")
    data["Error"] = err
    if action == "submit":
        if err:
            # let the player fix the rejected word
            data["Prefill"] = guess
        else:
            result = ret.value.json()
            if result.get("Won"):
                data["Won"] = True
            elif result.get("Bulls", 0) >= 2 or result.get("Cows", 0) >= 3:
                # strong guess: start the next turn from it
                data["Prefill"] = guess
    return ace.response(data, "game_main")


def create_challenge(req):
    ret = http.post(API + "/api/challenge", headers={"X-Forwarded-For": req.RemoteIP},
                    form_body={
                        "level": form_value(req, "level"),
                        "rounds": form_value(req, "rounds"),
                        "title": form_value(req, "title"),
                    }, error_on_fail=False)
    err = api_error(ret)
    if err:
        return not_found(req, err)
    return ace.redirect(req.AppPath + "/challenge/" + ret.value.json()["ChallengeId"])


def fetch_challenge(req, challenge_id):
    board = http.get(API + "/api/challenge/" + challenge_id + "/board", error_on_fail=False)
    if api_error(board):
        return None
    data = common(req)
    data["Board"] = board.value.json()
    data["ShareURL"] = req.AppUrl + "/challenge/" + data["Board"]["ChallengeId"]
    return data


def challenge_handler(req):
    data = fetch_challenge(req, req.UrlParams["challenge_id"])
    if not data:
        return not_found(req, "No game with that code. Check the id?")
    data["ShowLoc"] = True
    if form_value(req, "done") == "1":
        data["Notice"] = "You have played all the rounds! Hang around for the final standings."
    return data


def board_handler(req):
    data = fetch_challenge(req, req.UrlParams["challenge_id"])
    if not data:
        return not_found(req, "No game with that code. Check the id?")
    data["ShowLoc"] = form_value(req, "loc") == "1"
    return ace.response(data, "board_card")


def play_challenge(req):
    challenge_id = req.UrlParams["challenge_id"]
    # No name set is fine: the server generates a name for the player,
    # sticky via the player id
    ret = http.post(API + "/api/challenge/" + challenge_id + "/play",
                    headers={"X-Forwarded-For": req.RemoteIP},
                    form_body={
                        "player_id": get_cookie(req, "cb_pid"),
                        "player_name_enc": get_cookie(req, "cb_name"),
                    }, error_on_fail=False)
    err = api_error(ret)
    if err:
        if "all rounds" in err:
            return ace.redirect(req.AppPath + "/challenge/" + challenge_id + "?done=1")
        return not_found(req, err)
    return ace.redirect(req.AppPath + "/game/" + ret.value.json()["GameId"])


def score_handler(req):
    ret = http.get(API + "/api/score", params={
        "guesses": form_value(req, "guesses"),
        "hints": form_value(req, "hints"),
        "seconds": form_value(req, "seconds"),
    }, error_on_fail=False)
    data = common(req)
    data["ScoreError"] = api_error(ret)
    if not data["ScoreError"]:
        data["Score"] = ret.value.json()["Score"]
    return ace.response(data, "score_result")


def join(req):
    id = form_value(req, "id").strip().upper()
    if len(id) == 4:
        return ace.redirect(req.AppPath + "/challenge/" + id)
    return ace.redirect(req.AppPath + "/game/" + id)


def error_handler(req, ret):
    data = common(req)
    data["Message"] = ret["error"]
    if req.IsPartial:
        return ace.response(data, "error_block", retarget="#toasts", reswap="afterbegin")
    return ace.response(data, "error.go.html")


# Light mode uses the stock daisyui bumblebee theme; dark mode is custom
# with the same bright yellow primary.
COWBULL_THEMES = {
    "cowbull-dark": {
        "color-scheme": "dark",
        "--color-base-100": "#353b48",
        "--color-base-200": "#2c313c",
        "--color-base-300": "#464e5e",
        "--color-base-content": "#e9ecf2",
        "--color-primary": "#ffd633",
        "--color-primary-content": "#332b00",
        "--color-secondary": "#ffa94d",
        "--color-secondary-content": "#3d2200",
        "--color-accent": "#4dd4ac",
        "--color-accent-content": "#06352a",
        "--color-neutral": "#424a59",
        "--color-neutral-content": "#e6eaf1",
        "--color-info": "#7db8ff",
        "--color-info-content": "#0a2647",
        "--color-success": "#6bd984",
        "--color-success-content": "#093a15",
        "--color-warning": "#ffc861",
        "--color-warning-content": "#402d00",
        "--color-error": "#ff8f88",
        "--color-error-content": "#420605",
        "--radius-selector": "1rem",
        "--radius-field": "0.75rem",
        "--radius-box": "1rem",
        "--size-selector": "0.25rem",
        "--size-field": "0.25rem",
        "--border": "1px",
        "--depth": "1",
        "--noise": "0",
    },
}

app = ace.app("CowBull",
              custom_layout=True,
              routes=[
                  ace.html("/", "index.go.html", handler=index_handler),
                  ace.html("/help", "help.go.html", handler=index_handler,
                           fragments=[
                               ace.fragment("score", partial="score_result", method="POST",
                                            handler=score_handler),
                           ]),

                  ace.html("/game", "", method="POST", handler=create_game),
                  ace.html("/game/{game_id}", "game.go.html", partial="game_main", handler=game_handler,
                           fragments=[
                               ace.fragment("submit", partial="game_main", method="POST",
                                            handler=lambda req: game_action(req, "submit")),
                               ace.fragment("hint", partial="game_main", method="POST",
                                            handler=lambda req: game_action(req, "hint")),
                               ace.fragment("resign", partial="game_main", method="POST",
                                            handler=lambda req: game_action(req, "resign")),
                               ace.fragment("board", partial="board_card", handler=board_handler),
                           ]),

                  ace.html("/challenge", "", method="POST", handler=create_challenge),
                  ace.html("/challenge/{challenge_id}", "challenge.go.html", partial="board_card", handler=challenge_handler,
                           fragments=[
                               ace.fragment("board", partial="board_card", handler=board_handler),
                               ace.fragment("play", method="POST", handler=play_challenge),
                           ]),

                  ace.html("/join", "", method="POST", handler=join),

                  # The cookie-setting player endpoint and the SSE stream go
                  # straight through to the container
                  ace.proxy("/api", proxy.config(container.URL)),
              ],
              container=container.config(container.AUTO, port=param.port, health="/api/ustats"),
              settings={"routing": {"push_events": True}},
              permissions=[
                  # http calls are only allowed against the app's own container
                  ace.permission("http.in", "get", ["regex:^" + container.URL + "/.*"]),
                  ace.permission("http.in", "post", ["regex:^" + container.URL + "/.*"]),
                  ace.permission("proxy.in", "config", [container.URL]),
                  ace.permission("container.in", "config", [container.AUTO]),
              ],
              style=ace.style("daisyui",
                              light="bumblebee",
                              dark="cowbull-dark",
                              custom_themes=COWBULL_THEMES))
