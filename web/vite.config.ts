import { readFileSync } from 'node:fs';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The mobile app's own version, read straight out of mobile/app.json at build
// time rather than typed into a constant here.
//
// Not a stylistic preference: the extension's version number sat at 1.0.0
// through three releases because it was maintained by hand in a second place,
// and the Browser & App card showed that stale number beside the download as
// the answer to "do I have the current one" (jdp, 2026-08-27: "wird die
// versionsnummer der erweiterung automatisch aktualisiert?"). The extension
// reads its number from the manifest it actually ships; this is the same
// promise for the app, made the only way the web build can make it - a build
// that ships a number it did not read cannot be trusted, and there is no
// server endpoint to ask, because the instance does not host the APK.
const mobileVersion = (
  JSON.parse(readFileSync(new URL('../mobile/app.json', import.meta.url), 'utf8')) as {
    expo: { version: string };
  }
).expo.version;

// Builds the SPA into web/dist, which the Go binary embeds. Stable asset names
// keep the committed dist clean. In dev, /api (incl. WebSocket) is proxied to
// the Go backend on :8749.
export default defineConfig({
  plugins: [react()],
  base: '/',
  define: {
    __MOBILE_VERSION__: JSON.stringify(mobileVersion),
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
