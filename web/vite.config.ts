import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    // make dev: UI :19000 → API :19001 (see scripts/dev-up.sh)
    host: '0.0.0.0',
    port: 19000,
    proxy: {
      '/v1': { target: 'http://127.0.0.1:19001', ws: true },
      '/v2': 'http://127.0.0.1:19001',
      '/v3': 'http://127.0.0.1:19001',
      '/sandboxes': 'http://127.0.0.1:19001',
      '/templates': 'http://127.0.0.1:19001',
      '/p': 'http://127.0.0.1:19001',
      '/health': 'http://127.0.0.1:19001',
    },
  },
  build: {
    // Embed target for roundpend (see internal/ui).
    outDir: '../internal/ui/dist',
    emptyOutDir: true,
  },
})
