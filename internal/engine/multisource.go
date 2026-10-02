package engine

// One file from several sources at once.
//
// A job can name further links to its file (Job.Sources): a second debrid
// account that unlocked the same link, or a mirror on another hoster. The
// library spreads the transfer's connections over all of them and moves a
// connection on when its source fails (the fork's ReqExtra.Mirrors). What is
// decided here is which links may take part. A link joins only when it
// answers ranges, reports the size the job's own URL reported, and sends the
// same bytes at sample points across the file. The same size alone proves
// little: the volumes of split archives mostly share one.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/GopeedLab/gopeed/pkg/base"
)

// sourcesWait bounds asking for further links and checking them, which
// holds back the start of the transfer.
const sourcesWait = 45 * time.Second

// multiSourceMin is the smallest file worth more than one source. Below it
// the unlocks and the checks take longer than they can save. A var so the
// tests need not serve that much.
var multiSourceMin int64 = 64 << 20

// sampleLen is how much of the file each check compares.
const sampleLen = 64 << 10

// probe is what one source said about the file at the sample points.
type probe struct {
	size    int64
	etag    string
	host    string
	samples [][]byte
}

// vetSources asks the job for further links to its file and returns the ones
// that serve the same bytes as its own URL. It returns nil when the job has
// none, the file is too small or not ranged, or the job carries headers,
// which may hold a login meant only for its own URL.
func (e *Engine) vetSources(j Job, res *base.Resource) []string {
	if j.Sources == nil || res == nil || !res.Range || res.Name != "" || res.Size < multiSourceMin || len(j.Headers) > 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(e.ctx, sourcesWait)
	defer cancel()
	links := slices.DeleteFunc(j.Sources(ctx), func(u string) bool { return u == "" || u == j.URL })
	slices.Sort(links)
	links = slices.Compact(links)
	if len(links) == 0 {
		return nil
	}
	client := e.mendClient(j)
	defer client.CloseIdleConnections()
	ua := e.userAgent()
	offsets := sampleOffsets(res.Size)

	own, err := probeSource(ctx, client, j.URL, ua, offsets)
	if err != nil {
		log.Printf("not spreading task %s over %d more sources: its own link failed the check: %v", j.TaskID, len(links), err)
		return nil
	}
	if own.size != res.Size {
		log.Printf("not spreading task %s over more sources: its own link reports %s, not %s", j.TaskID, mib(own.size), mib(res.Size))
		return nil
	}
	verdicts := make([]error, len(links))
	var wg sync.WaitGroup
	for i, link := range links {
		wg.Go(func() {
			p, err := probeSource(ctx, client, link, ua, offsets)
			if err == nil {
				err = own.differs(p)
			}
			verdicts[i] = err
		})
	}
	wg.Wait()
	var keep []string
	for i, link := range links {
		if verdicts[i] != nil {
			log.Printf("task %s: %s not used as a further source: %v", j.TaskID, hostOf(link), verdicts[i])
			continue
		}
		keep = append(keep, link)
	}
	if len(keep) > 0 {
		log.Printf("task %s: fetching from %d sources", j.TaskID, 1+len(keep))
	}
	return keep
}

// sampleOffsets are where the file is compared: the middle and the end, past
// the headers that files of one kind share.
func sampleOffsets(size int64) []int64 {
	return []int64{size / 2, size - sampleLen}
}

// differs reports why q is not the file p is, or nil when it is.
func (p probe) differs(q probe) error {
	if q.size != p.size {
		return fmt.Errorf("it reports %s, not %s", mib(q.size), mib(p.size))
	}
	// An ETag is the server's own name for its copy, so two servers may give
	// one file different ones. Only the same host is held to it.
	if q.host == p.host && strongETag(p.etag) && strongETag(q.etag) && q.etag != p.etag {
		return errors.New("its ETag differs")
	}
	for i := range p.samples {
		if !bytes.Equal(p.samples[i], q.samples[i]) {
			return errors.New("it sends different bytes")
		}
	}
	return nil
}

func strongETag(tag string) bool {
	return tag != "" && !strings.HasPrefix(tag, "W/")
}

// probeSource asks link for each sample.
func probeSource(ctx context.Context, client *http.Client, link, ua string, offsets []int64) (probe, error) {
	u, err := url.Parse(link)
	if err != nil {
		return probe{}, err
	}
	p := probe{size: -1, host: u.Hostname()}
	for _, off := range offsets {
		b, size, etag, err := sample(ctx, client, link, ua, off)
		if err != nil {
			return probe{}, err
		}
		if p.size >= 0 && size != p.size {
			return probe{}, errors.New("it changed its size between two requests")
		}
		p.size, p.etag = size, etag
		p.samples = append(p.samples, b)
	}
	return p, nil
}

// sample fetches sampleLen bytes of link from off, with the file size and
// ETag the answer gives.
func sample(ctx context.Context, client *http.Client, link, ua string, off int64) ([]byte, int64, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, 0, "", err
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, off+sampleLen-1))
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return nil, 0, "", fmt.Errorf("HTTP %d to a range request", resp.StatusCode)
	}
	start, total, ok := contentRange(resp.Header.Get("Content-Range"))
	if !ok || start != off || total < 0 {
		return nil, 0, "", fmt.Errorf("an unusable Content-Range (%s)", resp.Header.Get("Content-Range"))
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, sampleLen))
	if err != nil {
		return nil, 0, "", err
	}
	if len(b) != sampleLen {
		return nil, 0, "", errors.New("a short answer")
	}
	return b, total, resp.Header.Get("ETag"), nil
}

// hostOf is the host of link for a log line, which must not carry the link
// itself: a debrid link works for anybody who has it.
func hostOf(link string) string {
	if u, err := url.Parse(link); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return "a link"
}
