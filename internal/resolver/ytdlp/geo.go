package ytdlp

import "strings"

// geoPlaceholder reports a stream address that is a site's stand-in for a
// video this region may not see. ORF's CDN redirects such an episode to its
// insert_geoprotection clip, which yt-dlp downloads and exits 0 on.
func geoPlaceholder(media string) bool {
	return strings.Contains(strings.ToLower(media), "geoprotection")
}
