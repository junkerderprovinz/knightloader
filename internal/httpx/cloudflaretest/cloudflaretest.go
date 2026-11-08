// Package cloudflaretest serves Cloudflare's managed challenge the way a
// hoster behind it answers a client that runs no scripts.
package cloudflaretest

import "net/http"

// Page is the challenge page, cut down to what marks it.
const Page = `<!DOCTYPE html><html lang="en-US"><head><title>Just a moment...</title></head>
<body><div class="main-wrapper" role="main"><div class="main-content">
<noscript><div class="h2"><span id="challenge-error-text">Enable JavaScript and cookies to continue</span></div></noscript>
</div></div>
<script>(function(){window._cf_chl_opt={cvId: '3',cZone: "filespace.com",cType: 'managed',cRay: '8f3a1c2b9d4e5f60'};
var a = document.createElement('script');a.src = '/cdn-cgi/challenge-platform/h/g/orchestrate/chl_page/v1?ray=8f3a1c2b9d4e5f60';
document.getElementsByTagName('head')[0].appendChild(a);}());</script></body></html>`

// Challenge answers with the challenge page under status, 403 or 503, and
// the headers Cloudflare sends with it.
func Challenge(w http.ResponseWriter, status int) {
	w.Header().Set("Server", "cloudflare")
	w.Header().Set("Cf-Mitigated", "challenge")
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(Page))
}
