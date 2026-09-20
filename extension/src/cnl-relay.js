/**
 * The isolated-world half of the Click'n'Load interception. cnl-main.js sees
 * the page's fetch and forms but has no chrome.* APIs; this side has the APIs,
 * and postMessage connects the two.
 *
 * Any page can post any message, so the shape is checked and only string
 * fields are forwarded. The background worker decodes them and never executes
 * any of it (see cnl.js on `jk`).
 */
(() => {
  const TAG = 'knightloader-cnl';

  window.addEventListener('message', (event) => {
    // Same window only; an iframe from another origin must not steer this.
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
        // For the package name when the site sends no `source` field.
        pageUrl: location.href,
        pageTitle: document.title,
      });
    } catch {
      // The extension was reloaded and this content script is orphaned.
    }
  });
})();
