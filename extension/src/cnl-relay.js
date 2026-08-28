/**
 * The other half of the interception: isolated world, so it can reach chrome.*.
 *
 * cnl-main.js runs in the page's own context, where it can see the page's fetch
 * and forms but has no extension APIs at all. This runs beside it in the
 * extension's context, where the reverse is true. postMessage is the only door
 * between the two, and this is the far side of it.
 *
 * Everything arriving here is untrusted: any page can post any message, so the
 * shape is checked before it is forwarded and the fields are passed through as
 * plain strings. The background worker does the decoding and never executes a
 * byte of it (see cnl.js on why `jk` is regex-extracted rather than run).
 */
(() => {
  const TAG = 'knightloader-cnl';

  window.addEventListener('message', (event) => {
    // Same-window only. A message from an iframe on another origin has no
    // business steering this, and event.source lets us insist on that.
    if (event.source !== window) return;
    const msg = event.data;
    if (!msg || msg[TAG] !== true || typeof msg.path !== 'string') return;

    const fields = {};
    if (msg.fields && typeof msg.fields === 'object') {
      for (const [k, v] of Object.entries(msg.fields)) {
        if (typeof v === 'string') fields[k] = v;
      }
    }

    try {
      chrome.runtime.sendMessage({
        type: 'knightloader-cnl',
        path: msg.path,
        fields,
        // Where it came from, for the package name. The site's own `source`
        // field wins when it sends one; this is the fallback.
        pageUrl: location.href,
        pageTitle: document.title,
      });
    } catch {
      // The usual one: the extension was reloaded and this content script is
      // orphaned. Nothing to recover, and nothing worth an error in the page's
      // console for.
    }
  });
})();
