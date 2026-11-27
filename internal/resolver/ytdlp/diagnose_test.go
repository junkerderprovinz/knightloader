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

	patreonStderr = "ERROR: [patreon] 110207086: You do not have access to this post"

	geoStderr = "ERROR: [youtube] AbC123dEf45: The uploader has not made this video available in your country"

	copyrightBlockStderr = "ERROR: [youtube] AbC123dEf45: Video unavailable. This video contains content from " +
		"SME, who has blocked it in your country on copyright grounds"

	goneStderr = "ERROR: [youtube] AbC123dEf45: Video unavailable. This video has been removed by the uploader"

	drmStderr = "ERROR: [generic] 12345: This video is DRM protected"

	extractorStderr = "ERROR: [youtube] AbC123dEf45: Unable to extract yt initial data; please report this issue on  " +
		"https://github.com/yt-dlp/yt-dlp/issues?q=  , filling out the appropriate issue template. Confirm you are " +
		"on the latest version using  yt-dlp -U"

	noFFmpegMergeStderr = "ERROR: You have requested merging of multiple formats but ffmpeg is not installed. " +
		"Aborting due to --abort-on-error"

	noFFmpegPostStderr = "ERROR: Postprocessing: ffprobe and ffmpeg not found. Please install or provide the " +
		"path using --ffmpeg-location"

	ageGateStderr = "ERROR: [youtube] AbC123dEf45: Sign in to confirm your age. This video may be inappropriate for some users."

	privateStderr = "ERROR: [youtube] AbC123dEf45: Private video. Sign in if you've been granted access to this video"

	privateUnavailableStderr = "ERROR: [youtube] AbC123dEf45: Video unavailable. This video is private"

	registeredStderr = "ERROR: [vimeo] 123456: This video is only available for registered users. Use --cookies-from-browser " +
		"or --cookies for the authentication. See  https://github.com/yt-dlp/yt-dlp/wiki/FAQ#how-do-i-pass-cookies-to-yt-dlp  " +
		"for how to manually pass cookies"

	instagramStderr = "ERROR: [Instagram] C0ffee: Requested content is not available, rate-limit reached or login required. " +
		"Use --cookies, --cookies-from-browser, --username and --password, --netrc-cmd, or --netrc (instagram) to provide " +
		"account credentials"
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
		{"a post for paying supporters", patreonStderr, core.ReasonMembersOnly},
		{"a country the uploader excluded", geoStderr, core.ReasonGeoBlocked},
		{"a video the uploader deleted", goneStderr, core.ReasonGone},
		{"an encrypted stream", drmStderr, core.ReasonDRM},
		{"a page yt-dlp can no longer read", extractorStderr, core.ReasonExtractorBroken},
		{"a merge without ffmpeg", noFFmpegMergeStderr, core.ReasonNoFFmpeg},
		{"a post-processor without ffmpeg", noFFmpegPostStderr, core.ReasonNoFFmpeg},
		{"a conversion without ffmpeg", "ERROR: Postprocessing: ffmpeg not found. Please install or provide the path using --ffmpeg-location", core.ReasonNoFFmpeg},
		{"an age gate", ageGateStderr, core.ReasonSignInRequired},
		{"a private video", privateStderr, core.ReasonSignInRequired},
		{"a private video YouTube calls unavailable", privateUnavailableStderr, core.ReasonSignInRequired},
		{"a video for registered users", registeredStderr, core.ReasonSignInRequired},
		{"a post behind a login", instagramStderr, core.ReasonSignInRequired},
		{"a probe without ffprobe", "ERROR: Postprocessing: ffprobe not found. Please install or provide the path using --ffmpeg-location", core.ReasonNoFFmpeg},
		{"a section without ffmpeg", "ERROR: You have requested downloading the video partially, but ffmpeg is not installed. Aborting", core.ReasonNoFFmpeg},
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
	if got := Diagnose(ageGateStderr); got == core.ReasonBotCheck {
		t.Errorf("Diagnose(an age gate) = %q, want anything but the bot check", got)
	}
}

func TestOnlyTheCausesASignInCanClearAskForOne(t *testing.T) {
	for _, r := range []core.Reason{core.ReasonBotCheck, core.ReasonMembersOnly, core.ReasonSignInRequired} {
		if !NeedsSignIn(r) {
			t.Errorf("NeedsSignIn(%q) = false", r)
		}
	}
	for _, r := range []core.Reason{core.ReasonUnknown, core.ReasonGeoBlocked, core.ReasonGone, core.ReasonDRM, core.ReasonAuth, core.ReasonExtractorBroken} {
		if NeedsSignIn(r) {
			t.Errorf("NeedsSignIn(%q) = true", r)
		}
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

// yt-dlp warns about a missing ffmpeg on every run, also when it then fails
// for a reason of the site's.
func TestDiagnoseDoesNotBlameTheFFmpegWarning(t *testing.T) {
	buf := "WARNING: ffmpeg not found. The downloaded format may not be the best available. " +
		"Installing ffmpeg is strongly recommended: https://github.com/yt-dlp/yt-dlp#dependencies\n" +
		"ERROR: unable to download video data: HTTP Error 403: Forbidden\n"
	if got := Diagnose(buf); got != core.ReasonUnknown {
		t.Errorf("Diagnose(a 403 after the ffmpeg warning) = %q, want no opinion at all", got)
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

// yt-dlp prints a GeoRestrictedError with the extractor's own sentence, the
// countries it names and a closing line about a VPN, the German and Austrian
// Mediatheks and SRF among them. Other extractors raise a plain error with a
// sentence of their own.
func TestDiagnoseReadsEveryWordingOfARegionBlock(t *testing.T) {
	const vpn = "\nYou might want to use a VPN or a proxy server (with --proxy) to workaround."
	for name, stderr := range map[string]string{
		"ARD Mediathek":    "ERROR: This video is not available due to geoblocking\nThis video is available in Germany." + vpn,
		"arte":             "ERROR: Video restricted to 'DE_FR'\nThis video is available in Germany, France." + vpn,
		"SRF":              "ERROR: For legal reasons, this video is only available in Switzerland.\nThis video is available in Switzerland." + vpn,
		"the default":      "ERROR: This video is not available from your location due to geo restriction" + vpn,
		"a country":        "ERROR: [tv5unis] 12345: This content is not available in your country",
		"a region":         "ERROR: [viu] 12345: Video 12345 is not available in your region.",
		"unavailable":      "ERROR: [tidal] 12345: Track unavailable in your country due to licensing restrictions",
		"geo-restricted":   "ERROR: [abc] 12345: HLS/DASH fragments and mp4 URLs are geo-restricted; only available in AU",
		"bilibili's guess": "ERROR: [BiliBili] BV1xx: This video may be deleted or geo-restricted. You might want to try a VPN or a proxy server (with --proxy)",
		"a region and DRM": "ERROR: [wwe] 12345: This video is either unavailable in your region or is DRM protected",
	} {
		t.Run(name, func(t *testing.T) {
			if got := Diagnose(stderr); got != core.ReasonGeoBlocked {
				t.Errorf("Diagnose(%q) = %q, want %q", stderr, got, core.ReasonGeoBlocked)
			}
		})
	}
}
