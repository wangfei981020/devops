import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 3002,
    // Bind 0.0.0.0 so the local k8s ingress can reach the dev server via host.docker.internal
    host: true,
    allowedHosts: ['localhost', 'ops-alert.kerbos.test'],
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true
      }
    }
  }
})
