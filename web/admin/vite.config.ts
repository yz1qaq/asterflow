import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath, URL } from 'node:url'

const apiTarget = process.env.ASTERFLOW_DEV_API_TARGET || 'http://127.0.0.1:18090'

export default defineConfig({
  base: '/',
  plugins: [react()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: {
    host: '127.0.0.1',
    port: 15174,
    strictPort: true,
    proxy: {
      '/healthz': { target: apiTarget, changeOrigin: true },
      '/readyz': { target: apiTarget, changeOrigin: true },
      '/api': { target: apiTarget, changeOrigin: true },
    },
  },
  build: { outDir: 'dist', sourcemap: false },
})
