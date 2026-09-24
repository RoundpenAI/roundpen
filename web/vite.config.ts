import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

const apiProxy = process.env.ROUNDPEN_API_PROXY || 'http://127.0.0.1:19001'

export default defineConfig({
  plugins: [react()],
  server: {
    host: '0.0.0.0',
    port: 19000,
    proxy: {
      '/v1': { target: apiProxy, ws: true },
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
