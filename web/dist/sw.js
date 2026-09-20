// Registered from src/main.tsx. Browsers gate the install prompt on a
// fetch-handling service worker, so this one exists and caches nothing: the
// server answers assets with an ETag and Cache-Control: no-cache, and a cache
// here would hand out yesterday's build after a redeploy and answer /api from
// stale state.
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (event) => event.waitUntil(self.clients.claim()));
self.addEventListener('fetch', () => {
  // Without respondWith() the event falls through to the network. The
  // listener has to be registered for the installability check all the same.
});
