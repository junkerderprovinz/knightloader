package captcha

import (
	"context"
	"errors"
	"testing"
	"time"
)

func turnstile(host, taskID string) Challenge {
	return Challenge{Host: host, TaskID: taskID, Kind: KindWidget, Payload: &WidgetPayload{
		Vendor: VendorTurnstile, SiteKey: "0x4AAAA", SiteURL: "https://" + host + "/login",
	}}
}

// asked waits until s lists one challenge and returns it.
func asked(t *testing.T, s *AskSource) Challenge {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if list := s.List(); len(list) == 1 {
			return list[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the challenge was never listed")
	return Challenge{}
}

func TestAnAskedCaptchaIsListedUntilItsAnswerComesBack(t *testing.T) {
	s := NewAskSource()
	changes := make(chan struct{}, 4)
	s.Changed = func() { changes <- struct{}{} }

	type result struct {
		text string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		text, err := s.Ask(context.Background(), turnstile("ddownload.com", "t1"))
		done <- result{text, err}
	}()

	c := asked(t, s)
	if !IsAsked(c.ID) || c.Source != SourceAsk || c.TaskID != "t1" || c.ExpiresAt.IsZero() {
		t.Errorf("listed %+v, want an asked challenge for t1 with a deadline", c)
	}
	<-changes
	if !s.Answer(c.ID, "token") {
		t.Fatal("the answer was turned away")
	}
	if r := <-done; r.text != "token" || r.err != nil {
		t.Errorf("Ask = %q, %v; want the answer", r.text, r.err)
	}
	if len(s.List()) != 0 {
		t.Error("an answered challenge is still listed")
	}
	if s.Answer(c.ID, "again") {
		t.Error("a challenge took a second answer")
	}
}

func TestASkippedCaptchaTellsTheAskerSo(t *testing.T) {
	s := NewAskSource()
	done := make(chan error, 1)
	go func() {
		_, err := s.Ask(context.Background(), turnstile("ddownload.com", ""))
		done <- err
	}()

	s.Abort(asked(t, s).ID)
	if err := <-done; !errors.Is(err, ErrSkipped) {
		t.Errorf("Ask = %v, want ErrSkipped", err)
	}
}

func TestACaptchaNobodyAnswersRunsOut(t *testing.T) {
	s := NewAskSource()
	s.TTL = 20 * time.Millisecond

	_, err := s.Ask(context.Background(), turnstile("ddownload.com", ""))
	if !errors.Is(err, ErrUnanswered) {
		t.Errorf("Ask = %v, want ErrUnanswered", err)
	}
	if len(s.List()) != 0 {
		t.Error("a captcha that ran out is still listed")
	}
}

func TestACaptchaWhoseDownloadStopsLeavesTheList(t *testing.T) {
	s := NewAskSource()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := s.Ask(ctx, turnstile("ddownload.com", "t1"))
		done <- err
	}()

	asked(t, s)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("Ask = %v, want the context's error", err)
	}
	if len(s.List()) != 0 {
		t.Error("the challenge of a stopped download is still listed")
	}
}
