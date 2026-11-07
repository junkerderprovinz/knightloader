package jd

import "testing"

func TestHosterWaitIsReadFromThePackageOrALink(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pkg   string
		links []string
		want  string
	}{
		{"a free hoster's cool-down on a link", "", []string{"Download limit reached: Wait 3h 55m"}, "Download limit reached: Wait 3h 55m"},
		{"an IP block on the package", "Waiting for new IP (01:12:44)", []string{""}, "Waiting for new IP (01:12:44)"},
		{"a hoster that is down for a while", "", []string{"Hoster temporarily unavailable. Retry in 9m"}, "Hoster temporarily unavailable. Retry in 9m"},
		{"one waiting link among finished ones", "Running", []string{"Finished", "Wait 00:04:10"}, "Wait 00:04:10"},
		{"a plain running download", "Running", []string{"Running"}, ""},
		{"a link not started yet", "", []string{""}, ""},
		{"a captcha in progress", "Captcha recognition (rapidgator.net)", []string{""}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			links := make([]DownloadLink, len(tc.links))
			for i, s := range tc.links {
				links[i] = DownloadLink{Status: s}
			}
			if got := hosterWait(tc.pkg, links); got != tc.want {
				t.Errorf("hosterWait(%q, %q) = %q, want %q", tc.pkg, tc.links, got, tc.want)
			}
		})
	}
}
