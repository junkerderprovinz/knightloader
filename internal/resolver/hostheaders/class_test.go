package hostheaders

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

func TestThePreflightGoesWhereHosterTrafficGoes(t *testing.T) {
	var hits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer proxy.Close()
	u, _ := url.Parse(proxy.URL)
	httpx.SetClassProxy(httpx.ClassHosters, u)
	defer httpx.SetClassProxy(httpx.ClassHosters, nil)

	for _, c := range []*http.Client{Resolver{}.client(), Set{}.Client(nil)} {
		resp, err := c.Get("http://files.example/a.bin")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("the hosters' proxy saw %d of 2 requests", n)
	}
}
