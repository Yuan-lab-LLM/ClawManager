import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    port: 9002,
    host: true,
    proxy: {
      // Preserve the browser Host for the Desktop BFF same-origin checks.
      '^/api/v1/instances/[0-9]+/(?:hermes-desktop|access|proxy)(?:/|$)': {
        target: 'http://127.0.0.1:9001',
        changeOrigin: false,
        ws: true,
      },
      // ksec-bridge dev proxy: when running ksec-bridge locally via
      // `npm run dev` (default 127.0.0.1:9101), front-end `/api/host/*`
      // calls land directly on it — mirrors the prod nginx rewrite rule.
      // Must come BEFORE the generic `/api` rule (vite picks first match).
      '/api/host': {
        target: 'http://127.0.0.1:9101',
        changeOrigin: true,
        rewrite: (p) => p.replace(/^\/api\/host/, '/agent/v1'),
      },
      '/api': {
        target: 'http://127.0.0.1:9001',
        changeOrigin: true,
        ws: true,
      },
    },
  },
})
