package playback

// sessions.go: one question to one server, "what is playing right now", and
// the two documents the answer comes back in.

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/mediahook"
)

// AskTimeout bounds one question. Below the poll interval, so a server that
// hangs cannot make two questions to it overlap.
const AskTimeout = 8 * time.Second

// maxAnswer bounds how much of a session list is read. A busy Plex server with
// twenty streams answers in well under a megabyte.
const maxAnswer = 4 << 20

// Stream is one session that has something loaded in a player.
type Stream struct {
	// Server is the id of the row it came from.
	Server string `json:"server"`
	// Session is the server's own key for it, which the start of a pause is
	// kept under. Not shown.
	Session string `json:"-"`
	Title   string `json:"title"`
	// Series is the show an episode belongs to, empty for a film or a track.
	Series string `json:"series,omitempty"`
	User   string `json:"user,omitempty"`
	Player string `json:"player,omitempty"`
	Paused bool   `json:"paused"`
	// Local says the player is on the server's own network.
	Local bool `json:"local"`
	// Counts says this stream holds quiet mode, and Reason why not when it
	// does not: "local" or "paused". Filled by the watcher.
	Counts bool   `json:"counts"`
	Reason string `json:"reason,omitempty"`
}

// Failure is a question that got no usable answer, in the codes
// mediahook.Classify uses so the interface can word it.
type Failure struct {
	Code   string         `json:"code"`
	Params map[string]any `json:"params,omitempty"`
	Raw    string         `json:"error,omitempty"`
}

func (f *Failure) Error() string {
	if f.Raw != "" {
		return f.Code + ": " + f.Raw
	}
	return f.Code
}

// codeFormat is an answer that arrived but was not the document this kind of
// server sends, which is what pointing the row at the wrong product looks like.
const codeFormat = "format"

// NewClient builds the client the questions go out on. Redirects are handed
// back rather than followed, for the reason mediahook.NewClient gives: the
// token travels in a header httpx does not know is a credential.
func NewClient() *http.Client {
	return httpx.New(httpx.Options{Timeout: AskTimeout, MaxRedirects: -1})
}

// Ask fetches what is playing on one server. A nil error with no streams is
// a server that answered and is idle.
func Ask(ctx context.Context, c *http.Client, s Server, token string) ([]Stream, error) {
	req, err := request(ctx, s, token)
	if err != nil {
		return nil, &Failure{Code: mediahook.CodeUnknown, Raw: err.Error()}
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, transportFailure(err, req)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		code, params, raw := mediahook.ClassifyStatus(resp.StatusCode, req.Method)
		return nil, &Failure{Code: code, Params: params, Raw: raw}
	}
	if err != nil {
		return nil, transportFailure(err, req)
	}
	var streams []Stream
	switch s.Kind {
	case KindPlex:
		streams, err = parsePlex(body)
	default:
		streams, err = parseEmby(body)
	}
	if err != nil {
		return nil, &Failure{Code: codeFormat, Params: map[string]any{"kind": string(s.Kind)}, Raw: err.Error()}
	}
	for i := range streams {
		streams[i].Server = s.ID
	}
	return streams, nil
}

// request builds the question for one kind of server. Jellyfin takes the token
// in the Authorization header it documents, since 10.11 can switch the older
// X-Emby-Token off; Emby still documents X-Emby-Token and wants its API under
// /emby.
func request(ctx context.Context, s Server, token string) (*http.Request, error) {
	base, err := url.Parse(strings.TrimSpace(s.URL))
	if err != nil {
		return nil, errors.New("the address could not be turned into a request")
	}
	var path string
	switch s.Kind {
	case KindPlex:
		path = "/status/sessions"
	case KindJellyfin:
		path = "/Sessions"
	case KindEmby:
		path = "/emby/Sessions"
	default:
		return nil, fmt.Errorf("%q is not a media server this build can read", s.Kind)
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	base.RawQuery = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, errors.New("the address could not be turned into a request")
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		switch s.Kind {
		case KindPlex:
			req.Header.Set("X-Plex-Token", token)
		case KindJellyfin:
			req.Header.Set("Authorization", `MediaBrowser Token="`+token+`"`)
		case KindEmby:
			req.Header.Set("X-Emby-Token", token)
		}
	}
	return req, nil
}

// plexSessions is the part of GET /status/sessions read here. Plex answers
// JSON when asked for it in Accept.
type plexSessions struct {
	MediaContainer *struct {
		Metadata []struct {
			Title            string `json:"title"`
			GrandparentTitle string `json:"grandparentTitle"`
			User             struct {
				Title string `json:"title"`
			} `json:"User"`
			Player struct {
				Title   string `json:"title"`
				Product string `json:"product"`
				State   string `json:"state"`
				Address string `json:"address"`
				Local   bool   `json:"local"`
			} `json:"Player"`
			Session struct {
				ID       string `json:"id"`
				Location string `json:"location"`
			} `json:"Session"`
		} `json:"Metadata"`
	} `json:"MediaContainer"`
}

func parsePlex(body []byte) ([]Stream, error) {
	var doc plexSessions
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	if doc.MediaContainer == nil {
		return nil, errors.New("the answer has no MediaContainer")
	}
	out := make([]Stream, 0, len(doc.MediaContainer.Metadata))
	for _, m := range doc.MediaContainer.Metadata {
		out = append(out, Stream{
			Session: m.Session.ID,
			Title:   m.Title,
			Series:  m.GrandparentTitle,
			User:    m.User.Title,
			Player:  cmp.Or(m.Player.Title, m.Player.Product),
			// "buffering" is a stream that wants the line more than ever.
			Paused: m.Player.State == "paused",
			Local:  m.Player.Local || m.Session.Location == "lan" || isLocalAddr(m.Player.Address),
		})
	}
	return out, nil
}

// embySession is the part of GET /Sessions read here; Jellyfin kept Emby's
// shape. A session with no NowPlayingItem is a client that is merely open.
type embySession struct {
	ID             string `json:"Id"`
	UserName       string `json:"UserName"`
	Client         string `json:"Client"`
	DeviceName     string `json:"DeviceName"`
	RemoteEndPoint string `json:"RemoteEndPoint"`
	NowPlayingItem *struct {
		Name       string `json:"Name"`
		SeriesName string `json:"SeriesName"`
	} `json:"NowPlayingItem"`
	PlayState struct {
		IsPaused bool `json:"IsPaused"`
	} `json:"PlayState"`
}

func parseEmby(body []byte) ([]Stream, error) {
	var doc []embySession
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	out := make([]Stream, 0, len(doc))
	for _, s := range doc {
		if s.NowPlayingItem == nil {
			continue
		}
		out = append(out, Stream{
			Session: s.ID,
			Title:   s.NowPlayingItem.Name,
			Series:  s.NowPlayingItem.SeriesName,
			User:    s.UserName,
			Player:  cmp.Or(s.DeviceName, s.Client),
			Paused:  s.PlayState.IsPaused,
			Local:   isLocalAddr(s.RemoteEndPoint),
		})
	}
	return out, nil
}

// transportFailure is mediahook.ClassifyError with this package's own timeout
// in the sentence, which is shorter than a library call's.
func transportFailure(err error, req *http.Request) *Failure {
	code, params, raw := mediahook.ClassifyError(err, req)
	if code == mediahook.CodeTimeout {
		params = map[string]any{"seconds": int(AskTimeout / time.Second)}
	}
	return &Failure{Code: code, Params: params, Raw: raw}
}
