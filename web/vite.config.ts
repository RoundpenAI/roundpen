import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'

const root = dirname(fileURLToPath(import.meta.url))
const apiProxy = process.env.ROUNDPEN_API_PROXY || 'http://127.0.0.1:19001'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    // make dev: UI :19000 → API :19001 (see scripts/dev-up.sh)
    // UI smoke: ROUNDPEN_API_PROXY=http://127.0.0.1:19021
    host: '0.0.0.0',
    port: 19000,
    proxy: {
      '/v1': { target: apiProxy, ws: true },
      '/p': apiProxy,
      '/health': apiProxy,
    },
  },
  build: {
    // Embed target for roundpend (see internal/ui).
    outDir: '../internal/ui/dist',
    emptyOutDir: true,
    rollupOptions: {
      input: {
        main: resolve(root, 'index.html'),
        vnc: resolve(root, 'vnc.html'),
      },
    },
  },
})
