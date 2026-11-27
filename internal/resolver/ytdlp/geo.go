package ytdlp

import (
	"net/url"
	"strings"
)

// geoPlaceholder reports a stream address that is a site's stand-in for a
// video this region may not see. ORF's CDN redirects such an episode to its
// insert_geoprotection clip, which yt-dlp downloads and exits 0 on.
func geoPlaceholder(media string) bool {
	return strings.Contains(strings.ToLower(media), "geoprotection")
}

// geoFolders are, for each Mediathek CDN, the folders that hold the files only
// some countries may fetch. A file anyone may fetch sits in a sibling folder
// ("none", "int", "weltweit", "cms-worldwide"), so a refusal of one of these
// is the region block and a refusal of any other file is not.
var geoFolders = map[string][]string{
	"nrodlzdf-a.akamaihd.net":            {"/de/", "/dach/", "/ebu/"},
	"tvdlzdf-a.akamaihd.net":             {"/de/", "/dach/", "/ebu/"},
	"ctv-videos.daserste.de":             {"/de/", "/dach/"},
	"sportschau-progressive.ard-mcdn.de": {"/de/", "/dach/"},
	"cdn-storage.br.de":                  {"/geo/"},
	"hr-progressive.ard-mcdn.de":         {"/odinson/geoavailability_"},
	"kika-progressive.ard-mcdn.de":       {"/kika_de-prod/geo/"},
	"swr-pd.ard-mcdn.de":                 {"/swr/geo/", "/kindernetz/geo/"},
	"ndr-progressive.ard-mcdn.de":        {"/progressive_geo"},
	"mdr-progressive.ard-mcdn.de":        {"/mdr/video/deutschlandweit/"},
	"wdr-progressive.ard-mcdn.de":        {"/medp/ondemand/de/", "/media/p/public/de/"},
	"wdrmedien-a.akamaihd.net":           {"/medp/ondemand/de/"},
	"apasfiis.sf.apa.at":                 {"/ipad/cms-austria", "/dash/cms-austria"},
}

// GeoLimited reports whether rawurl is a Mediathek file only some countries
// may fetch, so that the CDN refusing it means this region is blocked.
func GeoLimited(rawurl string) bool {
	u, err := url.Parse(rawurl)
	if err != nil {
		return false
	}
	for _, folder := range geoFolders[strings.ToLower(u.Hostname())] {
		if strings.HasPrefix(u.Path, folder) {
			return true
		}
	}
	return false
}

// geoRefusal reports a run that failed on a 403 for a Mediathek file only some
// countries may fetch, by the stream's address or the link's.
func geoRefusal(stderr, media, link string) bool {
	return strings.Contains(stderr, "HTTP Error 403") && (GeoLimited(media) || GeoLimited(link))
}
