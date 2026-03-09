import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5174,
    proxy: {
      '/api': {
        target: 'http://localhost:9000',
        changeOrigin: true
      },
      '/.well-known': {
        target: 'http://localhost:9000',
        changeOrigin: true
      },
      '/storage': {
        target: 'http://localhost:9000',
        changeOrigin: true
      }
    }
  }
})
