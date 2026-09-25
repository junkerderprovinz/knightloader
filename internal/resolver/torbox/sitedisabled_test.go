package torbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// refusingTorBox answers every createwebdownload with body.
func refusingTorBox(t *testing.T, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := NewClient("test-key")
	c.base = srv.URL
	return c
}

// TorBox switching off one site says nothing about the account, so the backend
// reports it as that site being down rather than as a plain failure.
func TestASiteTorBoxSwitchedOffIsReportedAsThatSiteBeingDown(t *testing.T) {
	const sentence = "The site you are trying to download from is temporarily disabled. You may view the status of supported sites at https://torbox.app/hosters."
	cases := []struct {
		name string
		body string
		down bool
	}{
		{"code", `{"success":false,"error":"TEMPORARILY_DISABLED","detail":"` + sentence + `","data":null}`, true},
		{"sentence without a code", `{"success":false,"error":null,"detail":"` + sentence + `","data":null}`, true},
		{"another code", `{"success":false,"error":"BAD_TOKEN","detail":"Invalid API token.","data":null}`, false},
		{"no code and another sentence", `{"success":false,"error":null,"detail":"Something went wrong.","data":null}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := make(chan core.Update, 4)
			b := NewBackend(refusingTorBox(t, tc.body), &fakeEngine{got: make(chan string, 1)}, func(_ string, u core.Update) {
				if u.Status == core.StatusError {
					got <- u
				}
			})
			b.Download("t1", "https://rapidgator.net/file/abc", nil, 0)

			select {
			case u := <-got:
				if u.HostDown != tc.down {
					t.Errorf("HostDown = %v for %s, want %v", u.HostDown, tc.body, tc.down)
				}
				if !strings.HasPrefix(u.Err, "torbox: ") {
					t.Errorf("Err = %q, want TorBox's own words", u.Err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the refusal was never reported")
			}
		})
	}
}

// requestdl carries the API key in its query. A refusal is shown on the task
// row and written to the log, so its text must not carry the key along.
func TestARefusedRequestDLKeepsTheKeyOutOfTheError(t *testing.T) {
	c := refusingTorBox(t, `{"success":false,"error":"DATABASE_ERROR","detail":"Try again later.","data":null}`)

	_, err := c.RequestDL(context.Background(), 1, 2)
	if err == nil {
		t.Fatal("RequestDL succeeded against a refusing TorBox")
	}
	if strings.Contains(err.Error(), "test-key") {
		t.Errorf("error %q carries the API key", err)
	}
	if !strings.Contains(err.Error(), "/api/webdl/requestdl") {
		t.Errorf("error %q does not say which call failed", err)
	}
}

// The same holds for an answer that is not TorBox's JSON at all, and for a
// call that gets no answer.
func TestAFailedRequestDLKeepsTheKeyOutOfTheError(t *testing.T) {
	unreadable := refusingTorBox(t, `<html>502 Bad Gateway</html>`)
	unanswered := NewClient("test-key")
	gone := httptest.NewServer(http.NotFoundHandler())
	unanswered.base = gone.URL
	gone.Close()

	for name, c := range map[string]*Client{"unreadable": unreadable, "unanswered": unanswered} {
		_, err := c.RequestDL(context.Background(), 1, 2)
		if err == nil {
			t.Fatalf("%s: RequestDL succeeded", name)
		}
		if strings.Contains(err.Error(), "test-key") {
			t.Errorf("%s: error %q carries the API key", name, err)
		}
	}
}

// The error text is what the task row shows, so it keeps TorBox's code and
// sentence.
func TestAPIErrorReadsLikeTorBoxsAnswer(t *testing.T) {
	err := &APIError{Path: "/api/webdl/createwebdownload", Code: "TEMPORARILY_DISABLED", Detail: "The site is off."}
	if got, want := err.Error(), "torbox /api/webdl/createwebdownload: TEMPORARILY_DISABLED The site is off."; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	err.Code = ""
	if got, want := err.Error(), "torbox /api/webdl/createwebdownload: The site is off."; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
