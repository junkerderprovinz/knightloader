package ytdlp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

func TestTheEmbedLookupGoesWhereYtdlpGoes(t *testing.T) {
	var hits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer proxy.Close()
	u, _ := url.Parse(proxy.URL)
	httpx.SetClassProxy(httpx.ClassYtdlp, u)
	defer httpx.SetClassProxy(httpx.ClassYtdlp, nil)

	resp, err := (&Backend{}).client().Get("http://playmate.example/embed/abc")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if hits.Load() != 1 {
		t.Error("the embed lookup went past yt-dlp's proxy")
	}
}
