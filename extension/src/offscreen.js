// The document that reads the clipboard for the watch in Chromium, where a
// service worker cannot (clipwatch.js). An offscreen document has no extension
// API but messaging, so it hands the links to background.js, which sends them.
const tell = (msg) => chrome.runtime.sendMessage(msg).catch(() => {});

startClipPoller({
  remembered: null,
  onLinks: (links) => tell({ type: 'knightloader-clip', links }),
  onSeen: () => {},
  onRefused: () => tell({ type: 'knightloader-clip-refused' }),
});
