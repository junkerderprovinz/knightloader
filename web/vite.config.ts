import { readFileSync } from 'node:fs';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The mobile app's version, read out of mobile/app.json at build time. Kept in
// one place so the phone card on the App page cannot offer a download beside
// a number somebody forgot to raise.
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
