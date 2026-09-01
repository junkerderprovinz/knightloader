package hosterauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The WIRE shape of queryAccounts, which is where the defect was and which no
// existing test could see.
//
// Everything else in this package is tested against a fake that implements the
// jdOps interface, so the request never becomes a URL and the parameters are
// never encoded. That is a fine way to test the reconciler and a blind spot for
// the client: this call carried maxResults and startAt from the day it shipped,
// JD answers HTTP 500 to the mere presence of either, and the failure was
// invisible until the package first met a real JDownloader.
//
// Measured against the bundled JDownloader 48637 before the fix: with -1, with
// 0, and with both, all HTTP 500; without them, `{"data":[]}`.
func TestQueryAccountsSendsNoPagingFields(t *testing.T) {
	var gesehen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gesehen = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	if _, err := newJDClient(srv.URL).queryAccounts(context.Background()); err != nil {
		t.Fatalf("queryAccounts: %v", err)
	}

	roh, err := url.QueryUnescape(gesehen)
	if err != nil {
		t.Fatalf("the query is not a single url-escaped value: %v", err)
	}
	var q map[string]any
	if err := json.Unmarshal([]byte(roh), &q); err != nil {
		t.Fatalf("the one parameter is not a JSON object: %v (%q)", err, roh)
	}

	for _, verboten := range []string{"maxResults", "startAt"} {
		if _, da := q[verboten]; da {
			t.Errorf("the query carries %q; JD answers 500 to its presence at ANY value, "+
				"because AccountQuery is not an APIQuery and has no paging", verboten)
		}
	}
	// The three that carry the answer: without them JD returns the accounts with
	// an empty info map, and `valid` is what tells a queued login from a
	// rejected one apart.
	for _, noetig := range []string{"username", "enabled", "valid"} {
		if v, da := q[noetig]; !da || v != true {
			t.Errorf("the query is missing %q; without it JD reports nothing useful about the account", noetig)
		}
	}
	if strings.Contains(gesehen, "&") {
		t.Errorf("the query has more than one parameter (%q); queryAccounts takes exactly one object", gesehen)
	}
}
