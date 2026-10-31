package main

import (
	"context"
	"errors"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/jre"
	"github.com/junkerderprovinz/knightloader/internal/provision"
)

// ensureJava fetches the Java runtime JD runs on; the tests stand in for it.
var ensureJava = jre.Ensure

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
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Minute)
	defer cancel()
	_, url, err := s.pv.Ensure(ctx)
	if err != nil {
		log.Printf("JD provisioning failed (%v); continuing without JD", err)
		s.a.JDSetupFailed(err)
		return
	}
	s.a.UseJD(url)
	log.Printf("headless JDownloader provisioned at %s", url)
}

// wait returns once no setup is under way.
func (s *jdSetup) wait() { s.wg.Wait() }
