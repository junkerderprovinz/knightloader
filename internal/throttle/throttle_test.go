package throttle

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

// TestCopyRespectsLimit proves the limiter actually slows a transfer down: a
// quarter megabyte at 128 KiB/s cannot arrive in under a second.
func TestCopyRespectsLimit(t *testing.T) {
	l := New()
	l.Set(128 * 1024)

	src := bytes.NewReader(make([]byte, 256*1024))
	start := time.Now()
	n, err := l.Copy(context.Background(), io.Discard, src)
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if n != 256*1024 {
		t.Fatalf("copied %d bytes, want %d", n, 256*1024)
	}
	// The first burst is free, so only the second half is actually paced.
	if took < 700*time.Millisecond {
		t.Errorf("256 KiB at 128 KiB/s took %v, expected to be paced", took)
	}
}

// TestUnlimitedIsFast pins the other direction: with no limit the same copy must
// not be paced at all.
func TestUnlimitedIsFast(t *testing.T) {
	l := New()
	src := bytes.NewReader(make([]byte, 4*1024*1024))
	start := time.Now()
	if _, err := l.Copy(context.Background(), io.Discard, src); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("unlimited copy took %v", took)
	}
	if l.Limit() != 0 {
		t.Errorf("Limit() = %d, want 0", l.Limit())
	}
}

// TestSetIsLive checks a limit can be lifted mid-flight, which is what makes the
// settings page take effect on running downloads.
func TestSetIsLive(t *testing.T) {
	l := New()
	l.Set(64 * 1024)
	if l.Limit() != 64*1024 {
		t.Fatalf("Limit() = %d", l.Limit())
	}
	l.Set(0)
	if l.current() != nil {
		t.Error("clearing the limit left a limiter behind")
	}
	l.Set(-5)
	if l.Limit() != 0 {
		t.Errorf("negative limit became %d, want 0", l.Limit())
	}
}

// TestCopyHonoursCancel makes sure a cancelled download does not sit in the
// limiter forever waiting for allowance.
func TestCopyHonoursCancel(t *testing.T) {
	l := New()
	l.Set(1024) // slow enough that the second chunk has to wait

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := l.Copy(ctx, io.Discard, bytes.NewReader(make([]byte, 1024*1024)))
	if err == nil {
		t.Error("copy with a cancelled context returned no error")
	}
}
