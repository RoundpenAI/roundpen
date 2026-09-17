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
            if (['EPIPE', 'ECONNRESET', 'ECONNABORTED'].includes(err.code)) return
            console.warn('ws proxy error:', err.message)
          })
        },
      },
      '^/v1/sandboxes/[^/]+/terminal': {
        target: apiProxy,
        ws: true,
        configure: (proxy) => {
          proxy.on('error', (err) => {
            if (['EPIPE', 'ECONNRESET', 'ECONNABORTED'].includes(err.code)) return
            console.warn('ws proxy error:', err.message)
          })
        },
      },
      '^/v1/me/environments/browser/live': {
        target: apiProxy,
        ws: true,
        configure: (proxy) => {
          proxy.on('error', (err) => {
            if (['EPIPE', 'ECONNRESET', 'ECONNABORTED'].includes(err.code)) return
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
  },
})
