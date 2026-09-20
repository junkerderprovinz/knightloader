// The answer to a site's "is JDownloader running here" probe.
//
// cnl-main.js already sets these globals, but some sites, filecrypt among them,
// decide from the script element's onload or onerror, and with nothing on port
// 9666 onerror fires. A declarativeNetRequest rule (cnl-rules.json) redirects
// the request here. The content matches what internal/cnl/cnl.go serves, so a
// site cannot tell the two apart.
jdownloader = true;
var version = '90000';
