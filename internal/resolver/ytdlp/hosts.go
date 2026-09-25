package ytdlp

import (
	"slices"
	"strings"
)

// A host preset is set before any link of its host has been probed, so it
// cannot read its menus off a source the way a row does. It offers what is
// known of the host instead: the formats yt-dlp's extractor lists for the
// site's links (knownSites), and the formats that probes of the host's links
// found. Either is enough, and only a host that neither covers is offered
// every format there is.

// site is what yt-dlp's extractor for one site lists for its links, in the
// words a preset stores: containers with their codec as VideoContainers names
// them, and the audio formats AudioFormatsOf would find.
type site struct {
	domains []string
	video   []string
	audio   []string
}

// knownSites covers the sites most links come from. A format one of their
// links has and this table lacks is still offered once a probe has seen it.
var knownSites = []site{
	{
		domains: []string{"youtube.com", "youtu.be", "youtube-nocookie.com"},
		video:   []string{"mp4 avc1", "mp4 vp9", "mp4 av1", "webm vp9"},
		audio:   []string{"aac", "opus"},
	},
	{
		domains: []string{"vimeo.com"},
		video:   []string{"mp4 avc1", "mp4 hevc"},
		audio:   []string{"aac"},
	},
	{
		domains: []string{"twitch.tv"},
		video:   []string{"mp4 avc1"},
		audio:   []string{"aac"},
	},
	// Dailymotion and TikTok serve video with its audio in one file and no
	// audio-only track, which is all AudioFormatsOf counts.
	{
		domains: []string{"dailymotion.com", "dai.ly"},
		video:   []string{"mp4 avc1"},
	},
	{
		domains: []string{"tiktok.com"},
		video:   []string{"mp4 avc1", "mp4 hevc"},
	},
	{
		domains: []string{"x.com", "twitter.com"},
		video:   []string{"mp4 avc1"},
		audio:   []string{"aac"},
	},
	{
		domains: []string{"instagram.com"},
		video:   []string{"mp4 avc1"},
		audio:   []string{"aac"},
	},
	{
		domains: []string{"facebook.com", "fb.watch"},
		video:   []string{"mp4 avc1"},
		audio:   []string{"aac"},
	},
	{
		domains: []string{"reddit.com", "redd.it"},
		video:   []string{"mp4 avc1"},
		audio:   []string{"aac"},
	},
	{
		domains: []string{"bilibili.com", "b23.tv"},
		video:   []string{"mp4 avc1", "mp4 hevc", "mp4 av1"},
		audio:   []string{"aac"},
	},
	{
		domains: []string{"soundcloud.com"},
		audio:   []string{"aac", "opus", "mp3"},
	},
	{
		domains: []string{"bandcamp.com"},
		audio:   []string{"mp3"},
	},
}

var siteByDomain = func() map[string]site {
	m := map[string]site{}
	for _, s := range knownSites {
		for _, d := range s.domains {
			m[d] = s
		}
	}
	return m
}()

// bareHost is host as a task carries it: lower case and without "www.".
func bareHost(host string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(host)), "www.")
}

// siteOf finds the site host belongs to, itself or as a subdomain, so
// m.youtube.com and an artist's page on bandcamp.com are found too.
func siteOf(host string) (site, bool) {
	host = bareHost(host)
	for strings.Contains(host, ".") {
		if s, ok := siteByDomain[host]; ok {
			return s, true
		}
		_, host, _ = strings.Cut(host, ".")
	}
	return site{}, false
}

// PresetKeys lists the hosts a preset for host's links can be saved under, in
// the order they are looked up: host itself, the domain of the site table it
// lies under, then the site's other addresses. A youtu.be link and one from
// m.youtube.com both find a preset saved for youtube.com. A host the table does
// not know has itself alone.
func PresetKeys(host string) []string {
	host = bareHost(host)
	if host == "" {
		return nil
	}
	keys := []string{host}
	s, ok := siteOf(host)
	if !ok {
		return keys
	}
	under := func(d string) bool { return strings.HasSuffix(host, "."+d) }
	for _, d := range s.domains {
		if under(d) {
			keys = append(keys, d)
		}
	}
	for _, d := range s.domains {
		if d != host && !under(d) {
			keys = append(keys, d)
		}
	}
	return keys
}

// SitePresetKey is the host a new preset for host's links is saved under: the
// site table's first address for its site, which every other address of the
// site looks up, or host itself for a site the table does not know.
func SitePresetKey(host string) string {
	if s, ok := siteOf(host); ok {
		return s.domains[0]
	}
	return bareHost(host)
}

// HostMenus is what a host preset offers for one host, each menu "best" first
// as a row's menus are.
type HostMenus struct {
	VideoFormats []string `json:"videoFormats"`
	AudioFormats []string `json:"audioFormats"`
	// Known is false where nothing is known of the host, and the menus are
	// then the full lists.
	Known bool `json:"known"`
}

// HostFormats is what a preset offers for host: what knownSites lists for its
// site together with seenVideo and seenAudio, the formats probes of the host's
// links found as VideoContainers and AudioFormatsOf list them. probed says
// whether any such probe answered, since one that found nothing to name still
// tells what the host has. A host neither covers gets PresetVideoFormats and
// AudioFormats.
func HostFormats(host string, seenVideo, seenAudio []string, probed bool) HostMenus {
	s, listed := siteOf(host)
	if !listed && !probed {
		return HostMenus{VideoFormats: PresetVideoFormats(), AudioFormats: AudioFormats()}
	}
	var containers []videoContainer
	for _, v := range slices.Concat(s.video, seenVideo) {
		// A bare container is a format no preset can name, and a track's key
		// comes from a row stored before its menus were split.
		if IsVideoContainer(v) {
			c, _ := parseContainer(strings.Split(v, " "))
			containers = append(containers, c)
		}
	}
	present := map[string]bool{}
	for _, a := range slices.Concat(s.audio, seenAudio) {
		present[foldAudioFormat(a)] = true
	}
	return HostMenus{VideoFormats: containerMenu(containers), AudioFormats: audioMenu(present), Known: true}
}
