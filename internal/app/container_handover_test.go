package app

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// echoJD cannot read the container either, and does what JD's crawler does
// then: it takes the address it fetched from for a plain link.
type echoJD struct{ containerJD }

func (*echoJD) AddContainer(url, _ string, _ time.Duration) ([]resolver.Result, error) {
	return []resolver.Result{{DirectURL: url, Name: "tok.ccf"}}, nil
}

func TestAContainerJDCannotOpenIsRecordedAsSkipped(t *testing.T) {
	a := newCrawlApp(t, false)
	a.bmu.Lock()
	a.jd = &echoJD{}
	a.bmu.Unlock()

	relay := "http://kl.example:8749/api/containers/relay/tok.ccf"
	if err := a.HandContainerToJD(relay, "garbage.ccf", "garbage"); err != nil {
		t.Fatalf("HandContainerToJD: %v", err)
	}
	waitFor(t, "the container to be given up on", func() bool {
		return len(a.SkippedLinks()) == 1 || len(a.Tasks()) > 0
	})
	if tasks := a.Tasks(); len(tasks) != 0 {
		t.Fatalf("staged %s, the handover address, as a link", tasks[0].URL)
	}
	if s := a.SkippedLinks()[0]; s.URL != "garbage.ccf" || s.Kind != "container" {
		t.Errorf("skipped = %+v, want the container named", s)
	}
}
