package signinbrowser

import "os"

// Browser is a Chromium-family browser installed on this computer, which the
// sign-in window runs headless with a profile of its own.
type Browser struct {
	Name string
	Path string
}

// Installed is the browser to sign in with: the first Chromium-family
// browser on this computer, in the order its platform suggests.
func Installed() (Browser, bool) {
	return firstInstalled(candidates(), isFile)
}

func firstInstalled(cands []Browser, installed func(string) bool) (Browser, bool) {
	for _, b := range cands {
		if installed(b.Path) {
			return b, true
		}
	}
	return Browser{}, false
}

func isFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
