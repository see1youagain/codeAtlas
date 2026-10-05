import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { resolve } from 'node:path'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: resolve(import.meta.dirname, '../internal/server/web/dist'),
    emptyOutDir: true,
  },
  server: {
    proxy: { '/api': 'http://localhost:8080', '/mcp': 'http://localhost:8080' },
  },
})
