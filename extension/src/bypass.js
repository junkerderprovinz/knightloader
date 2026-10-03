/**
 * Tells the extension that a click came with a modifier key held, so the
 * download it starts can stay in the browser (takeover.js decides which key
 * counts). Registered only while taking over downloads is on. A page can
 * dispatch a click of its own, but not a trusted one, and the worst it could
 * do is keep a download in the browser.
 */
(() => {
  addEventListener(
    'mousedown',
    (event) => {
      if (!event.isTrusted || !(event.altKey || event.shiftKey || event.ctrlKey || event.metaKey)) return;
      try {
        chrome.runtime
          .sendMessage({
            type: 'knightloader-bypass',
            alt: event.altKey,
            shift: event.shiftKey,
            // Command on a Mac, where Ctrl with a click opens the context menu.
            ctrl: event.ctrlKey || event.metaKey,
          })
          ?.catch(() => {});
      } catch {
        // The extension was reloaded and this script is orphaned.
      }
    },
    true,
  );
})();
