// Registered from src/main.tsx. Exists only to satisfy the installability
// check most browsers gate "Add to Home Screen" / the install prompt behind
// (a fetch-handling service worker) — it deliberately caches nothing.
//
// internal/api/api.go's spaHandler already answers every asset with an ETag
// and Cache-Control: no-cache, which is a working revalidate-before-use
// scheme for a binary that redeploys by replacing the whole embedded build.
// A second, offline-first cache layered on top of that here would compete
// with it: the first real bug it would cause is a client that reconnects
// after a redeploy and keeps running yesterday's app.js because this worker
// served it from a cache instead of asking the network, and /api/* is live
// state (the task list, the WebSocket) that must never be answered from a
// cache at all. So every request just passes straight through.
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (event) => event.waitUntil(self.clients.claim()));
self.addEventListener('fetch', () => {
  // No respondWith(): an unhandled fetch event falls through to the network
  // exactly as if this listener were not here at all. It has to exist as a
  // real addEventListener('fetch', ...) call for the installability check
  // above, not merely be present in the file.
});
