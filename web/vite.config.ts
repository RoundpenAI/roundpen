import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

const apiProxy = process.env.ROUNDPEN_API_PROXY || 'http://127.0.0.1:19001'

export default defineConfig({
  plugins: [react()],
  server: {
    host: '0.0.0.0',
    port: 19000,
    proxy: {
      // WebSocket endpoints — only these paths need WS upgrade.
      '^/v1/agent-sessions/[^/]+/ws': {
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
      '/p': apiProxy,
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
