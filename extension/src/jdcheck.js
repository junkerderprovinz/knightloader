// The answer to a site's "is JDownloader running here" probe.
//
// A container page loads <script src="http://127.0.0.1:9666/jdcheck.js"> and
// decides from the RESULT whether to offer Click'n'Load at all. cnl-main.js
// already declares the same two globals in the page at document_start, which
// covers every site that reads them - but not one that hangs its decision on
// the script element's own onload/onerror, because with nothing listening on
// that port the request fails and `onerror` fires whatever the globals say.
//
// Watching a real filecrypt container: it opened helper.html, asked for this
// file three times, got a network error each time and stopped. The globals were
// already set. It never looked at them.
//
// So the request itself is answered, by a declarativeNetRequest rule that
// redirects it here (see cnl-rules.json). Byte-for-byte what
// internal/cnl/cnl.go serves on the same route, so a site cannot tell a
// KnightLoader instance from the extension standing in for one.
jdownloader = true;
var version = '90000';
