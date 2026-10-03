package app

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/watch"
)

// echoJD cannot read the container either, and does what JD's crawler does
// then: it takes the address it fetched from for a plain link.
type echoJD struct{ containerJD }

func (*echoJD) AddContainer(_ context.Context, url, _ string, _ time.Duration) ([]resolver.Result, error) {
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

// lowerHostJD hands the address back the way JD does, with the host
// lower-cased.
type lowerHostJD struct{ containerJD }

func (*lowerHostJD) AddContainer(_ context.Context, raw, _ string, _ time.Duration) ([]resolver.Result, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	u.Host = strings.ToLower(u.Host)
	return []resolver.Result{{DirectURL: u.String(), Name: "Tok.ccf"}}, nil
}

func TestTheHandoverAddressIsKnownWhateverTheCaseOfItsHost(t *testing.T) {
	a := newCrawlApp(t, false)
	a.bmu.Lock()
	a.jd = &lowerHostJD{}
	a.bmu.Unlock()

	relay := "http://LocalHost:8749/api/containers/relay/Tok.ccf"
	if err := a.HandContainerToJD(relay, "garbage.ccf", "garbage"); err != nil {
		t.Fatalf("HandContainerToJD: %v", err)
	}
	waitFor(t, "the container to be given up on", func() bool {
		return len(a.SkippedLinks()) == 1 || len(a.Tasks()) > 0
	})
	if tasks := a.Tasks(); len(tasks) != 0 {
		t.Fatalf("staged %s, the handover address, as a link", tasks[0].URL)
	}
}

// emptyJD finishes every crawl of a dropped container without a link.
type emptyJD struct{ containerJD }

func (*emptyJD) AddContainerFile(context.Context, string, []byte, string, time.Duration) ([]resolver.Result, error) {
	return nil, jd.ErrNoLinks
}

func TestADroppedCCFOrRSDFJDFindsNothingInIsCalledDamaged(t *testing.T) {
	for _, c := range []struct {
		name, data string
		want       error
	}{
		{"Links.CCF", "encrypted bytes", errJDCouldNotOpen},
		{"Links.rsdf", "0a1b2c3d4e5f", errJDCouldNotOpen},
		// JD holds the only DLC key, so JD may have dropped its links.
		{"Links.dlc", strings.Repeat("QUJD", 30), jd.ErrNoLinks},
	} {
		a := newCrawlApp(t, false)
		a.bmu.Lock()
		a.jd = &emptyJD{}
		a.bmu.Unlock()

		a.stageWatchJob(watch.Job{File: &watch.File{Name: c.name, Data: []byte(c.data)}, Package: "Links"})
		skipped := a.SkippedLinks()
		if len(skipped) != 1 || skipped[0].Reason != c.want.Error() {
			t.Errorf("%s: skipped = %+v, want the reason %q", c.name, skipped, c.want)
		}
	}
}
