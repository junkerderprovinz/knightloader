import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Builds the SPA into web/dist, which the Go binary embeds. Stable asset names
// keep the committed dist clean. In dev, /api (incl. WebSocket) is proxied to
// the Go backend on :8749.
export default defineConfig({
  plugins: [react()],
  base: '/',
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
