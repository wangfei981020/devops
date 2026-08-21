import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { opsBootScript } from '@ops/design/vite'
import { defineConfig } from 'vite'

// 版本号从构建参数来。⚠️ Dockerfile 必须传 --build-arg VERSION，
// 漏传的话线上「关于」页显示 dev，排障时看不出前端是哪一版。
const VERSION = process.env.VERSION ?? 'dev'
const COMMIT = process.env.GIT_COMMIT ?? 'unknown'

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    // 首屏主题引导脚本，必须排在样式之前，否则深色模式会闪一下白底
    opsBootScript(),
    {
      name: 'ops-version-banner',
      closeBundle() {
        const warn = VERSION === 'dev' ? '  ← 未传 --build-arg VERSION，产物会标成 dev' : ''
        console.log(`\n[ops] 构建版本 ${VERSION} (${COMMIT})${warn}\n`)
      },
    },
  ],
  define: {
    __APP_VERSION__: JSON.stringify(VERSION),
    __APP_COMMIT__: JSON.stringify(COMMIT),
  },
  server: {
    port: 5274,
    proxy: {
      '/api': { target: process.env.API_TARGET ?? 'http://127.0.0.1:18090', changeOrigin: true },
    },
  },
})
