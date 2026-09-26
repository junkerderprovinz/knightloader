package app

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/extract"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// As in TestClassify, each input is a sentence or value this build really
// receives, and each code is a different sentence and a different next step in
// the interface.
func TestCodeFor(t *testing.T) {
	cases := []struct {
		name string
		in   failure
		want core.ErrorCode
	}{
		{"a dead link", failure{text: "http request fail, code:404"}, core.CodeGone},
		{"a host refusing the request", failure{text: "jd /downloads/add: HTTP 403: no"}, core.CodeAccessDenied},
		{"a host that only serves paid accounts", failure{text: "HTTP 403: premium only"}, core.CodePremiumNeeded},
		{"a debrid service naming a paid plan", failure{text: "neodebrid: this file hoster needs a premium account"}, core.CodePremiumNeeded},
		{"a service called premiumize", failure{text: "premiumize /transfer/directdl: the call failed"}, ""},
		{"a name that does not resolve", failure{err: &net.DNSError{Name: "host.example"}}, core.CodeUnreachable},
		{"nothing listening", failure{text: "dial tcp 10.0.0.1:443: connect: connection refused"}, core.CodeUnreachable},
		{"a connection that timed out", failure{text: "connection 2 failed: i/o timeout"}, core.CodeTimeout},
		{"a request that ran out of time", failure{err: context.DeadlineExceeded}, core.CodeTimeout},
		{"a server saying it waited too long", failure{status: 408}, core.CodeTimeout},
		{"a full disk", failure{text: "write /data/x.part: no space left on device"}, core.CodeDiskFull},
		{"a folder that may not be written", failure{text: "open /downloads/x.part: permission denied"}, core.CodeNoPermission},
		{"the same in Windows' words", failure{text: "open D:\\downloads\\x.part: Access is denied."}, core.CodeNoPermission},
		{"yt-dlp without an extractor", failure{text: "yt-dlp: ERROR: Unsupported URL: https://x.example/p"}, core.CodeUnsupported},
		{"an allowance spent", failure{status: 429}, core.CodeLimit},
		{"a host down for now", failure{status: 503}, core.CodeUnavailable},
		{"a sentence nothing recognises", failure{text: "rapidgator: error code 7731"}, ""},
		{"a file the system will not open", failure{err: &fs.PathError{Op: "open", Path: "/config/cookies.txt", Err: syscall.EACCES}},
			core.CodeNoPermission},
		{"a program the system will not run", failure{err: &fs.PathError{Op: "fork/exec", Path: "/usr/bin/yt-dlp", Err: syscall.EACCES}},
			core.CodeNoPermission},
		{"a read-only mount", failure{err: &fs.PathError{Op: "open", Path: "/downloads/x.part", Err: syscall.EROFS}},
			core.CodeNoPermission},
		{"a folder that is not there", failure{err: &fs.PathError{Op: "open", Path: "/mnt/share/x", Err: syscall.ENOENT}},
			core.CodeLocalFile},
		{"a file named like a network failure", failure{err: &fs.PathError{Op: "open", Path: "/downloads/Timeout.mkv", Err: syscall.EACCES}},
			core.CodeNoPermission},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := codeFor(tc.in, classify(tc.in)); got != tc.want {
				t.Errorf("codeFor(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// An errno satisfies net.Error, and a file the system refused is not a server
// that could not be reached. The row names the file instead.
func TestAFileSystemErrorIsNotANetworkOne(t *testing.T) {
	task := &core.Task{}
	recordFailure(task, failure{err: &fs.PathError{Op: "open", Path: "/downloads/film.mkv", Err: syscall.EACCES}})

	if task.Reason == core.ReasonNetwork {
		t.Fatalf("a refused file reads as the network: %q", task.Reason)
	}
	want := map[string]string{"path": "/downloads/film.mkv"}
	if task.ErrorCode != core.CodeNoPermission || !maps.Equal(task.ErrorParams, want) {
		t.Errorf("reads as %q %v, want %q naming the file", task.ErrorCode, task.ErrorParams, core.CodeNoPermission)
	}
}

// codeReasons is which reasons a code may stand beside.
var codeReasons = map[core.ErrorCode][]core.Reason{
	core.CodeGone:            {core.ReasonGone},
	core.CodeAccessDenied:    {core.ReasonAuth},
	core.CodePremiumNeeded:   {core.ReasonAuth, core.ReasonUnknown},
	core.CodeLimit:           {core.ReasonLimit},
	core.CodeUnavailable:     {core.ReasonUnavailable},
	core.CodeUnreachable:     {core.ReasonNetwork},
	core.CodeTimeout:         {core.ReasonNetwork},
	core.CodeDiskFull:        {core.ReasonDiskFull},
	core.CodeNoPermission:    {core.ReasonUnknown},
	core.CodeLocalFile:       {core.ReasonUnknown},
	core.CodeUnsupported:     {core.ReasonUnsupported},
	core.CodeHostExcluded:    {core.ReasonUnsupported},
	core.CodeCaptcha:         {core.ReasonCaptcha},
	core.CodeCancelled:       {core.ReasonCancelled},
	core.CodeBotCheck:        {core.ReasonBotCheck},
	core.CodeMembersOnly:     {core.ReasonMembersOnly},
	core.CodeGeoBlocked:      {core.ReasonGeoBlocked},
	core.CodeDRM:             {core.ReasonDRM},
	core.CodeExtractorBroken: {core.ReasonExtractorBroken},
}

// Whatever the classifier recognises has a code of its own group: a row whose
// badge says "Network" must not explain itself as a missing file.
func TestEveryRecognisedFailureIsWordedByItsOwnGroup(t *testing.T) {
	var inputs []failure
	for _, c := range textReasons {
		inputs = append(inputs, failure{text: "backend: " + c.phrase})
	}
	for status := 100; status < 600; status++ {
		inputs = append(inputs, failure{status: status})
	}
	inputs = append(inputs,
		failure{err: context.Canceled},
		failure{err: context.DeadlineExceeded},
		failure{err: fmt.Errorf("write: %w", syscall.ENOSPC)},
		failure{err: &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}},
	)
	for _, in := range inputs {
		r := classify(in)
		if r == core.ReasonUnknown {
			continue
		}
		code, _ := codeFor(in, r)
		if code == "" {
			t.Errorf("%+v is %q but has no code, so the row shows the general sentence", in, r)
			continue
		}
		if !belongs(code, r) {
			t.Errorf("%+v is %q but reads as %q", in, r, code)
		}
	}
	for _, r := range []core.Reason{
		core.ReasonGone, core.ReasonAuth, core.ReasonLimit, core.ReasonUnavailable, core.ReasonNetwork,
		core.ReasonDiskFull, core.ReasonUnsupported, core.ReasonCaptcha, core.ReasonCancelled,
		core.ReasonBotCheck, core.ReasonMembersOnly, core.ReasonGeoBlocked, core.ReasonDRM, core.ReasonExtractorBroken,
	} {
		if !belongs(r.Code(), r) {
			t.Errorf("%q reads as %q, a code of another group", r, r.Code())
		}
	}
}

func belongs(code core.ErrorCode, r core.Reason) bool {
	for _, allowed := range codeReasons[code] {
		if allowed == r {
			return true
		}
	}
	return false
}

func TestAFailedTaskCarriesItsCodeUntilItIsRestarted(t *testing.T) {
	a := retryApp(t, func(*settings.Settings) {})
	runningOn(a, "t1", plainHost, simpleResolverID)

	a.onUpdate("t1", core.Update{Status: core.StatusError, Err: "connection 2 failed: i/o timeout"})

	if got := liveTask(a, "t1"); got.ErrorCode != core.CodeTimeout || got.Reason != core.ReasonNetwork {
		t.Fatalf("settled as %q with code %q, want network and timeout", got.Reason, got.ErrorCode)
	}
	a.RestartTasks([]string{"t1"})
	if got := liveTask(a, "t1"); got.ErrorCode != "" || got.ErrorParams != nil {
		t.Errorf("after a restart the code is %q %v, want it gone with the sentence", got.ErrorCode, got.ErrorParams)
	}
}

// A debrid service's refusal is worded as one only when the classifier has
// nothing better: a limit it names is still a limit.
func TestABackendsCodeIsOnlyAFallback(t *testing.T) {
	service := map[string]string{"service": "AllDebrid"}
	cases := []struct {
		err  string
		want core.ErrorCode
	}{
		{"alldebrid: /link/unlock: This host is not supported (LINK_HOST_NOT_SUPPORTED)", core.CodeDebridRefused},
		{"alldebrid: /link/unlock: too many requests (MUST_BE_PREMIUM)", core.CodeLimit},
	}
	for _, tc := range cases {
		a := retryApp(t, func(*settings.Settings) {})
		runningOn(a, "d1", plainHost, simpleResolverID)

		a.onUpdate("d1", core.Update{Status: core.StatusError, Err: tc.err, Code: core.CodeDebridRefused, Params: service})

		got := liveTask(a, "d1")
		if got.ErrorCode != tc.want {
			t.Errorf("%q reads as %q, want %q", tc.err, got.ErrorCode, tc.want)
		}
		if tc.want == core.CodeDebridRefused && !maps.Equal(got.ErrorParams, service) {
			t.Errorf("the refusal came without the service's name: %v", got.ErrorParams)
		}
	}
}

func TestAnArchiveThatIsNotOneReadsAsADamagedPart(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
	})
	if err := os.WriteFile(filepath.Join(base, "release.zip"), []byte("this is not an archive at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := stageDone(t, a, "1", "release.zip")

	if err := a.StartExtraction([]string{task.ID}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the broken archive failing", func() bool {
		j, ok := jobFor(a, task.ID)
		return ok && j.Status == ExtractFailed
	})

	part := map[string]string{"part": "release.zip"}
	j, _ := jobFor(a, task.ID)
	if j.ErrorCode != core.CodeArchiveDamaged || !maps.Equal(j.ErrorParams, part) {
		t.Errorf("the job reads %q %v, want a damaged release.zip", j.ErrorCode, j.ErrorParams)
	}
	live := liveTask(a, task.ID)
	if live.ErrorCode != core.CodeArchiveDamaged || !maps.Equal(live.ErrorParams, part) {
		t.Errorf("the row reads %q %v, want a damaged release.zip", live.ErrorCode, live.ErrorParams)
	}
	if live.Error != j.Error && live.Error != "extract: "+j.Error {
		t.Errorf("the row's sentence %q is not the job's %q", live.Error, j.Error)
	}
}

func TestUnpackCode(t *testing.T) {
	missing := &fs.PathError{Op: "open", Path: "/out/Vier.part2.rar", Err: syscall.ENOENT}
	cases := []struct {
		name string
		err  error
		want core.ErrorCode
		// params is what the wording is given, part included.
		params map[string]string
	}{
		{"no password fits", extract.ErrPasswordRequired, core.CodeArchivePassword, nil},
		{"a damaged volume", &extract.PartError{Part: "Vier.part1.rar", Problem: extract.ErrDamaged, Err: fmt.Errorf("rardecode: bad block header")},
			core.CodeArchiveDamaged, map[string]string{"part": "Vier.part1.rar"}},
		{"a missing volume", &extract.PartError{Part: "Vier.part2.rar", Problem: extract.ErrPartMissing, Err: missing},
			core.CodeArchivePartMissing, map[string]string{"part": "Vier.part2.rar"}},
		{"an unknown format", &extract.PartError{Part: "x.bin", Problem: extract.ErrUnsupported, Err: extract.ErrUnsupported},
			core.CodeArchiveUnsupported, map[string]string{"part": "x.bin"}},
		{"a full disk", &fs.PathError{Op: "write", Path: "/out/x", Err: syscall.ENOSPC}, core.CodeDiskFull, nil},
		{"a folder that may not be written", &fs.PathError{Op: "open", Path: "/out/x", Err: syscall.EACCES},
			core.CodeNoPermission, map[string]string{"path": "/out/x"}},
		{"the collision policy said skip", &extract.DestinationTakenError{Dir: "/out/Film"},
			core.CodeArchiveFolderExists, map[string]string{"folder": "Film"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, params := unpackCode(tc.err)
			if code != tc.want || !maps.Equal(params, tc.params) {
				t.Errorf("unpackCode(%v) = %q %v, want %q %v", tc.err, code, params, tc.want, tc.params)
			}
		})
	}
}
