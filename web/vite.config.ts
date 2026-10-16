import { readdirSync, readFileSync } from 'node:fs';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The mobile app's version, read out of mobile/app.json at build time. Kept in
// one place so the phone card on the Apps page cannot offer a download beside
// a number somebody forgot to raise.
const mobileVersion = (
  JSON.parse(readFileSync(new URL('../mobile/app.json', import.meta.url), 'utf8')) as {
    expo: { version: string };
  }
).expo.version;

// The release notes of every release, which release.yml wants written before
// the tag. The window that opens after an update shows a card for each release
// it skipped and reads them out of the build, so it shows them without asking
// GitHub. A build between two releases carries the notes of the release behind it.
const notesDir = new URL('../.github/release-notes/', import.meta.url);
const releaseNotes = Object.fromEntries(
  readdirSync(notesDir).flatMap((name) => {
    const version = /^v(\d+\.\d+\.\d+)\.md$/.exec(name)?.[1];
    return version ? [[version, readFileSync(new URL(name, notesDir), 'utf8')]] : [];
  }),
);

// Builds the SPA into web/dist, which the Go binary embeds. Stable asset names
// keep the committed dist clean. Asset paths are relative, resolved against the
// <base> element the server puts into index.html, so one build serves at the
// root and under a reverse proxy's path prefix. In dev, /api (incl. WebSocket)
// is proxied to the Go backend on :8749.
export default defineConfig({
  plugins: [react()],
  base: './',
  define: {
    __MOBILE_VERSION__: JSON.stringify(mobileVersion),
    __RELEASE_NOTES__: JSON.stringify(releaseNotes),
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        entryFileNames: 'assets/app.js',
        chunkFileNames: 'assets/[name].js',
        assetFileNames: 'assets/[name][extname]',
      },
    },
  },
  server: {
    proxy: {
      '/api': { target: 'http://localhost:8749', ws: true, changeOrigin: true },
    },
  },
});
