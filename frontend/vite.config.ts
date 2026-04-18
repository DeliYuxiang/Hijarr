import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { resolve } from 'path'

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue(), tailwindcss()],
  resolve: {
    alias: {
      '@': resolve(__dirname, './src'),
    },
  },
  build: {
    // Output to the Go embed folder
    outDir: '../internal/web/frontend_dist',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      // Proxy all API calls to Go backend in dev
      '/api': {
        target: 'http://localhost:8001',
        changeOrigin: true,
      },
      '/srn': {
        target: 'http://localhost:8001',
        changeOrigin: true,
      },
    },
  },
})
