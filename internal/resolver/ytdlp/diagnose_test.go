package ytdlp

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// The fixtures are yt-dlp's own lines, kept whole; the bot check's length is
// part of what is tested.
const (
	botCheckStderr = "ERROR: [youtube] dQw4w9WgXcQ: Sign in to confirm you're not a bot. Use --cookies-from-browser or " +
		"--cookies for the authentication. See  https://github.com/yt-dlp/yt-dlp/wiki/FAQ#how-do-i-pass-cookies-to-yt-dlp  " +
		"for how to manually pass cookies. Also see  https://github.com/yt-dlp/yt-dlp/wiki/Extractors#exporting-youtube-cookies  " +
		"for tips on effectively exporting YouTube cookies"

	membersOnlyStderr = "ERROR: [youtube] AbC123dEf45: Join this channel to get access to members-only content " +
		"like this video, and other exclusive perks."

	membersLevelStderr = "ERROR: [youtube] AbC123dEf45: This video is available to this channel's members on level: " +
		"Supporter (or any higher level). Join this channel to get access to members-only content like this video, " +
		"and other exclusive perks."

	geoStderr = "ERROR: [youtube] AbC123dEf45: The uploader has not made this video available in your country"

	copyrightBlockStderr = "ERROR: [youtube] AbC123dEf45: Video unavailable. This video contains content from " +
		"SME, who has blocked it in your country on copyright grounds"

	goneStderr = "ERROR: [youtube] AbC123dEf45: Video unavailable. This video has been removed by the uploader"

	drmStderr = "ERROR: [generic] 12345: This video is DRM protected"

	extractorStderr = "ERROR: [youtube] AbC123dEf45: Unable to extract yt initial data; please report this issue on  " +
		"https://github.com/yt-dlp/yt-dlp/issues?q=  , filling out the appropriate issue template. Confirm you are " +
		"on the latest version using  yt-dlp -U"
)

// A trimmed fixture would let the tests below pass without testing a cut.
func TestTheBotCheckFixtureOutrunsTheCut(t *testing.T) {
	if len(botCheckStderr) <= errMsgRunes {
		t.Fatalf("the bot-check fixture is %d bytes and no longer outruns the %d-character cut, so nothing below is testing a truncation at all", len(botCheckStderr), errMsgRunes)
	}
	if strings.Contains(botCheckStderr[len(botCheckStderr)-errMsgRunes:], "not a bot") {
		t.Fatal("the fixture's last 200 bytes still carry the phrase, so it no longer stands in for the failure this change is about")
	}
}

func TestDiagnoseNamesEachCauseFromTheToolsOwnWords(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		want   core.Reason
	}{
		{"a bot check", botCheckStderr, core.ReasonBotCheck},
		{"a membership", membersOnlyStderr, core.ReasonMembersOnly},
		{"a membership with levels", membersLevelStderr, core.ReasonMembersOnly},
		{"a country the uploader excluded", geoStderr, core.ReasonGeoBlocked},
		{"a video the uploader deleted", goneStderr, core.ReasonGone},
		{"an encrypted stream", drmStderr, core.ReasonDRM},
		{"a page yt-dlp can no longer read", extractorStderr, core.ReasonExtractorBroken},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Diagnose(c.stderr); got != c.want {
				t.Errorf("Diagnose(%q) = %q, want %q", c.stderr, got, c.want)
			}
		})
	}
}

// The copyright block starts with "Video unavailable." but is a region block.
func TestDiagnoseReadsACopyrightBlockAsARegionBlock(t *testing.T) {
	if got := Diagnose(copyrightBlockStderr); got != core.ReasonGeoBlocked {
		t.Errorf("Diagnose(a copyright block) = %q, want %q", got, core.ReasonGeoBlocked)
	}
}

// Both messages start with "Sign in to confirm".
func TestDiagnoseDoesNotCallAnAgeGateABotCheck(t *testing.T) {
	const ageGate = "ERROR: [youtube] AbC123dEf45: Sign in to confirm your age. This video may be inappropriate for some users."
	if got := Diagnose(ageGate); got == core.ReasonBotCheck {
		t.Errorf("Diagnose(an age gate) = %q, want anything but the bot check", got)
	}
}

func TestDiagnoseIgnoresTheLettersDRMInAName(t *testing.T) {
	const inAPath = "ERROR: unable to rename file: /downloads/DRM Free Mixtape/track01.m4a"
	if got := Diagnose(inAPath); got == core.ReasonDRM {
		t.Errorf("Diagnose(a path with DRM in it) = %q, want anything but the DRM verdict", got)
	}
}

// The 403 must stay unknown so the shared classifier can call it an
// authentication failure.
func TestDiagnoseIsSilentAboutWhatItDoesNotKnow(t *testing.T) {
	for _, s := range []string{
		"",
		"ERROR: unable to download video data: HTTP Error 403: Forbidden",
		"ERROR: [youtube] AbC123dEf45: Failed to extract any player response",
		"ERROR: Postprocessing: ffmpeg exited with code 1",
	} {
		if got := Diagnose(s); got != core.ReasonUnknown {
			t.Errorf("Diagnose(%q) = %q, want no opinion at all", s, got)
		}
	}
}

func TestDiagnoseReadsTheWholeBuffer(t *testing.T) {
	buf := "[download] Destination: /data/A Video.f137.mp4\n" +
		botCheckStderr + "\n" +
		"WARNING: Falling back on generic information extractor\n"
	if got := Diagnose(buf); got != core.ReasonBotCheck {
		t.Errorf("Diagnose(a buffer with the error in the middle) = %q, want %q", got, core.ReasonBotCheck)
	}
}

func TestErrorLineKeepsTheFrontOfTheErrorLine(t *testing.T) {
	buf := "[download] Destination: /data/A Video.mp4\n" +
		botCheckStderr + "\n" +
		"WARNING: unable to obtain file audio codec with ffprobe\n"

	got := errorLine(buf)
	if !strings.HasPrefix(got, "ERROR: [youtube] dQw4w9WgXcQ:") {
		t.Fatalf("errorLine = %q, want it to start at the ERROR line", got)
	}
	if !strings.Contains(got, "Sign in to confirm you're not a bot") {
		t.Errorf("errorLine = %q, want the phrase that names the failure to survive", got)
	}
	if n := utf8.RuneCountInString(got); n != errMsgRunes {
		t.Errorf("errorLine kept %d characters, want the %d-character cut", n, errMsgRunes)
	}
}

func TestErrorLineNeverCutsThroughACharacter(t *testing.T) {
	// A three-byte character, since a byte cut at 200 would land on a
	// two-byte boundary by chance.
	got := errorLine("ERROR: [ard] 12345: " + strings.Repeat("€", 400))

	if !utf8.ValidString(got) {
		t.Fatalf("errorLine produced invalid UTF-8: %q", got)
	}
	if strings.ContainsRune(got, utf8.RuneError) {
		t.Errorf("errorLine = %q, want no replacement character", got)
	}
	if n := utf8.RuneCountInString(got); n != errMsgRunes {
		t.Errorf("errorLine kept %d characters, want %d - the cut is counted in characters, not bytes", n, errMsgRunes)
	}
}

// Output without an ERROR line, such as from a killed process, yields its last
// line.
func TestErrorLineFallsBackToTheLastLine(t *testing.T) {
	cases := map[string]string{
		"":                               "",
		"\n\n  \n":                       "",
		"only one line":                  "only one line",
		"first\nsecond\n":                "second",
		"first\nERROR: the middle\nlast": "ERROR: the middle",
	}
	for in, want := range cases {
		if got := errorLine(in); got != want {
			t.Errorf("errorLine(%q) = %q, want %q", in, got, want)
		}
	}
}
