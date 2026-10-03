package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GopeedLab/gopeed/pkg/download"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// throttled sends a response at about rate bytes a second.
type throttled struct {
	http.ResponseWriter
	rate int
}

func (w throttled) Write(p []byte) (int, error) {
	var sent int
	for len(p) > 0 {
		n := min(16<<10, len(p))
		m, err := w.ResponseWriter.Write(p[:n])
		sent += m
		if err != nil {
			return sent, err
		}
		p = p[n:]
		time.Sleep(time.Duration(n) * time.Second / time.Duration(w.rate))
	}
	return sent, nil
}

func TestStreamReadsFarAheadOfARunningDownload(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts; see settle")
	}
	const size = 24 << 20
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i*7 + i/509)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(throttled{w, 2 << 20}, r, "film.mkv", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()

	var done atomic.Bool
	e, err := New(t.TempDir(), func(_ string, u core.Update) {
		if u.Status == core.StatusDone {
			done.Store(true)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.Start(Job{TaskID: "t1", URL: srv.URL + "/film.mkv", Conns: 2})

	var r download.StreamReader
	deadline := time.Now().Add(10 * time.Second)
	for r == nil {
		if r, err = e.Stream("t1", 0); err != nil {
			if time.Now().After(deadline) {
				t.Fatalf("the running download never opened for reading: %v", err)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	defer r.Close()

	// Two connections at 2 MB/s take about six seconds for the file, and the
	// last part would come last without the read.
	off := int64(size - 2<<20)
	if _, err := r.Seek(off, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	got := make([]byte, 128<<10)
	for read := 0; read < len(got); {
		n, err := r.ReadContext(ctx, got[read:])
		if err != nil {
			t.Fatalf("read at %d: %v", off+int64(read), err)
		}
		read += n
	}
	if done.Load() {
		t.Fatal("the download had finished before the read, so the read proves nothing")
	}
	if !bytes.Equal(got, data[off:off+int64(len(got))]) {
		t.Fatal("the read bytes differ from the file")
	}
}

func TestStreamRefusesATaskTheEngineDoesNotRun(t *testing.T) {
	e, err := New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if _, err := e.Stream("nobody", 0); !errors.Is(err, ErrNotStreaming) {
		t.Fatalf("Stream of an unknown task = %v, want ErrNotStreaming", err)
	}
}

func TestStreamRefusesATransferBeingMended(t *testing.T) {
	e, _ := eventEngine(t)
	e.mends["t1"] = &mend{}
	if _, err := e.Stream("t1", 0); !errors.Is(err, ErrMending) {
		t.Fatalf("Stream while mending = %v, want ErrMending", err)
	}
}

// A reader still open when the library finishes a transfer short stops where
// the refused range begins. Those bytes are not on disk until the mend has
// fetched them.
func TestStreamReaderOpenWhenATransferEndsShortStopsAtTheMissingRange(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer")
	}
	o := newRefusingOrigin(t, 4<<20, -1)
	o.pace = 5 * time.Millisecond
	e, err := New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	// The fresh link never comes, so the mend leaves the range missing.
	e.Start(Job{TaskID: "t1", URL: o.srv.URL + "/old", Conns: 2, Relink: func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}})

	var r download.StreamReader
	waitUntil(t, "the download opening for reading", func() bool {
		r, err = e.Stream("t1", 0)
		return err == nil
	})
	defer r.Close()
	waitUntil(t, "the mend", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.mends["t1"] != nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var got []byte
	buf := make([]byte, 64<<10)
	for {
		n, err := r.ReadContext(ctx, buf)
		got = append(got, buf[:n]...)
		if err != nil {
			break
		}
	}
	if len(got) >= len(o.data) || !bytes.Equal(got, o.data[:len(got)]) {
		t.Fatalf("read %d of %d bytes, not all of them the file's; want the bytes before the refused range", len(got), len(o.data))
	}
}
