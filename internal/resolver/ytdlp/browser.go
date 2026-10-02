package ytdlp

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

// cookieFileHeader is the first line yt-dlp's cookie loader insists on.
const cookieFileHeader = "# Netscape HTTP Cookie File"

// withBrowserCookies appends a browser's Cookie header for rawurl to a
// cookies.txt text, which may be empty. The cookies go to rawurl's host alone,
// not its sub-domains, and only over https when the link is https. They are
// given a day to live, because a cookie file entry without an expiry counts as
// a session cookie that some loaders skip.
func withBrowserCookies(jar, rawurl, header string, now time.Time) string {
	u, err := url.Parse(rawurl)
	if err != nil || u.Hostname() == "" || strings.TrimSpace(header) == "" {
		return jar
	}
	secure := "FALSE"
	if strings.EqualFold(u.Scheme, "https") {
		secure = "TRUE"
	}
	expires := strconv.FormatInt(now.Add(24*time.Hour).Unix(), 10)
	var lines []string
	for _, pair := range strings.Split(header, ";") {
		name, value, _ := strings.Cut(strings.TrimSpace(pair), "=")
		if name == "" || strings.ContainsAny(name+value, "\t\r\n") {
			continue
		}
		lines = append(lines, strings.Join([]string{strings.ToLower(u.Hostname()), "FALSE", "/", secure, expires, name, value}, "\t"))
	}
	if len(lines) == 0 {
		return jar
	}
	if strings.TrimSpace(jar) == "" {
		jar = cookieFileHeader + "\n"
	} else if !strings.HasSuffix(jar, "\n") {
		jar += "\n"
	}
	return jar + strings.Join(lines, "\n") + "\n"
}

// browserArgs passes a browser's Referer and User-Agent on to yt-dlp. Its
// cookies go through the cookie file instead, where yt-dlp keeps them to
// their host; a Cookie given as a header would go to every host the stream
// touches.
func browserArgs(sent map[string]string) []string {
	var args []string
	for _, name := range []string{"Referer", "User-Agent"} {
		if v := sent[name]; v != "" {
			args = append(args, "--add-header", name+":"+v)
		}
	}
	return args
}
