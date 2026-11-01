package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/jre"
	"github.com/junkerderprovinz/knightloader/internal/provision"
)

// ensureJava fetches the Java runtime JD runs on, and runJD starts JD and
// waits for it to answer; the tests stand in for both.
var (
	ensureJava = jre.Ensure
	runJD      = func(ctx context.Context, pv *provision.Provisioner) (string, error) {
		_, url, err := pv.Ensure(ctx)
		return url, err
	}
)

var (
	errJDSettingUp = errors.New("JDownloader is already being set up")
	errJDRunning   = errors.New("JDownloader is already running")
)

// jdSetup brings up the app's own headless JDownloader in the background, on
// the Java runtime it fetches into the data folder rather than any Java the
// computer may have, and again on request after a try that failed.
type jdSetup struct {
	a       *app.App
	pv      *provision.Provisioner
	dataDir string
	// ctx ends with the program, and with it any setup under way.
	ctx     context.Context
	running atomic.Bool
	wg      sync.WaitGroup
}

func (s *jdSetup) start() error {
	if os.Getenv("KL_JD") != "" {
		return errJDRunning
	}
	if !s.running.CompareAndSwap(false, true) {
		return errJDSettingUp
	}
	s.a.SettingUpJD()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.running.Store(false)
		s.run()
	}()
	return nil
}

func (s *jdSetup) run() {
	log.Printf("setting up the headless JDownloader; the first run fetches Java and JDownloader and takes a few minutes")
	java, err := ensureJava(s.ctx, s.dataDir, s.a.JDSetupProgress)
	if err != nil {
		log.Printf("JD provisioning failed (%v); continuing without JD", err)
		s.a.JDSetupFailed(err)
		return
	}
	s.pv.Java = java
	url, err := s.launch(s.ctx)
	if url == "" {
		log.Printf("JD provisioning failed (%v); continuing without JD", err)
		s.a.JDSetupFailed(err)
		return
	}
	if err != nil {
		log.Print(err)
	}
	s.a.UseJD(url)
	log.Printf("headless JDownloader provisioned at %s", url)
}

// launch starts JD, on the Java runtime the weekly update fetched when one
// waits. When JD does not start on that one, it goes back to the runtime
// before it and returns JD's address together with an error that wraps
// app.ErrJavaRefused.
func (s *jdSetup) launch(ctx context.Context) (string, error) {
	sw, err := jre.Promote(s.dataDir)
	if err != nil {
		log.Printf("JD stays on the Java runtime it has: %v", err)
	}
	url, err := s.runJD(ctx)
	if sw == nil {
		return url, err
	}
	if err == nil {
		log.Printf("JD runs on Java %s", sw.To.Version)
		if err := sw.Commit(); err != nil {
			log.Printf("the Java runtime JD ran on before could not be removed: %v", err)
		}
		return url, nil
	}
	tried := err
	_ = s.pv.Stop()
	if err := sw.Rollback(); err != nil {
		return "", fmt.Errorf("%w (Java %s: %v), and the runtime before it could not be put back: %v", app.ErrJavaRefused, sw.To.Version, tried, err)
	}
	if ctx.Err() != nil {
		return "", tried
	}
	log.Printf("JD did not start on Java %s (%v), so it goes back to the runtime it had", sw.To.Version, tried)
	if url, err = s.runJD(ctx); err != nil {
		return "", fmt.Errorf("%w (Java %s: %v), and not on the runtime before it either: %v", app.ErrJavaRefused, sw.To.Version, tried, err)
	}
	return url, fmt.Errorf("%w (Java %s: %v)", app.ErrJavaRefused, sw.To.Version, tried)
}

// runJD gives JD ten minutes to answer, since its first run updates itself.
func (s *jdSetup) runJD(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	return runJD(ctx, s.pv)
}

// switchJava restarts JD on the Java runtime the weekly update fetched, or
// starts it there when an earlier setup did not get JD going. It ends with
// ctx, and with the program.
func (s *jdSetup) switchJava(ctx context.Context) error {
	if !s.running.CompareAndSwap(false, true) {
		return errJDSettingUp
	}
	s.wg.Add(1)
	defer s.wg.Done()
	defer s.running.Store(false)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(s.ctx, cancel)()
	if err := s.pv.Stop(); err != nil {
		return err
	}
	s.pv.Java = jre.Java(s.dataDir)
	url, err := s.launch(ctx)
	if url != "" && os.Getenv("KL_JD") == "" {
		s.a.UseJD(url)
	}
	return err
}

// wait returns once no setup is under way.
func (s *jdSetup) wait() { s.wg.Wait() }
