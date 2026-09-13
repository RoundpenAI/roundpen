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
  },
})
