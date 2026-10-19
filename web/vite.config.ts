import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

const apiProxy = process.env.ROUNDPEN_API_PROXY || 'http://127.0.0.1:19001'

export default defineConfig({
  plugins: [react()],
  // fnOS serves the console under a path prefix (/app/roundpen/); every other
  // channel leaves VITE_BASE unset so base stays '/' and behavior is unchanged.
  base: process.env.VITE_BASE || '/',
  server: {
    host: '0.0.0.0',
    port: 19000,
    proxy: {
      // Terminal / browser-live still need WebSocket upgrade.
      '^/v1/sandboxes/[^/]+/terminal': {
        target: apiProxy,
        ws: true,
        configure: (proxy) => {
          proxy.on('error', (err) => {
            const code = (err as Error & { code?: string }).code
            if (code && ['EPIPE', 'ECONNRESET', 'ECONNABORTED'].includes(code)) return
            console.warn('ws proxy error:', err.message)
          })
        },
      },
      '^/v1/me/environments/browser/live': {
        target: apiProxy,
        ws: true,
        configure: (proxy) => {
          proxy.on('error', (err) => {
            const code = (err as Error & { code?: string }).code
            if (code && ['EPIPE', 'ECONNRESET', 'ECONNABORTED'].includes(code)) return
            console.warn('ws proxy error:', err.message)
          })
        },
      },
      // All other /v1/* API calls (no WebSocket).
      '/v1': apiProxy,
      // Prefix must be /p/ so the console route /projects is not proxied.
      '/p/': apiProxy,
      '/health': apiProxy,
    },
  },
  build: {
    outDir: '../internal/ui/dist',
    emptyOutDir: true,
    rollupOptions: {
      onLog(level, log, defaultHandler) {
        // lottie-web evals expression animations; it reaches the graph only through
        // semi-foundation's Lottie component, which this app never renders.
        if (log.code === 'EVAL' && log.id?.includes('node_modules/lottie-web')) return
        defaultHandler(level, log)
      },
    },
  },
})
