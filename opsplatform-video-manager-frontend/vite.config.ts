import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig(() => {
  // Determine backend URL based on environment
  // In Docker, use container name; locally, use localhost
  const backendUrl = process.env.VITE_API_BASE_URL_INTERNAL ||
                     (process.env.DOCKER_ENV === 'true' ? 'http://backend:8080' : 'http://localhost:8080');

  return {
    plugins: [react()],
    server: {
      host: '0.0.0.0',
      port: 3000,
      proxy: {
        '/api': {
          target: backendUrl,
          changeOrigin: true,
          secure: false,
        },
        '/swagger': {
          target: backendUrl,
          changeOrigin: true,
          secure: false,
        },
      },
    },
  };
})
