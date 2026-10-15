package usenet

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

func TestTheServicesAPIsGoWhereUsenetGoes(t *testing.T) {
	var hits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer proxy.Close()
	u, _ := url.Parse(proxy.URL)
	httpx.SetClassProxy(httpx.ClassUsenet, u)
	defer httpx.SetClassProxy(httpx.ClassUsenet, nil)

	for _, c := range []*http.Client{NewTorBox("", "", "").hc, NewPremiumize("", "", "").hc} {
		resp, err := c.Get("http://api.example/v1")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("Usenet's proxy saw %d of 2 API calls", n)
	}
}
