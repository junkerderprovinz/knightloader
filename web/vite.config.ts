import { existsSync, readFileSync } from 'node:fs';
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

// The release notes of that version, which release.yml wants written before
// the tag. The window that opens after an update reads them out of the build,
// so it shows them without asking GitHub. A build between two releases carries
// the notes of the release behind it.
const notesFile = new URL(`../.github/release-notes/v${mobileVersion}.md`, import.meta.url);
const releaseNotes = {
  version: mobileVersion,
  text: existsSync(notesFile) ? readFileSync(notesFile, 'utf8') : '',
};

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
