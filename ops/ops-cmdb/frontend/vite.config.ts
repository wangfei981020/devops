import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { opsBootScript } from '@ops/design/vite'
import { defineConfig } from 'vite'

/**
 * 版本号从构建参数来，缺省是 'dev'。
 *
 * ⚠️ Dockerfile 必须传 --build-arg VERSION=xxx。
 * 上一代四个前端全都漏传过，线上跑着的版本在「关于」页显示 dev，
 * 排障时完全看不出前端是哪一版。所以这里额外在构建结束时打印实际值，
 * CI 日志里一眼能看到有没有传。
 */
const VERSION = process.env.VERSION ?? 'dev'
const COMMIT = process.env.GIT_COMMIT ?? 'unknown'

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    // 首屏主题引导脚本，必须排在样式之前
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
    port: 5273,
    proxy: {
      // 本地开发走 port-forward 到 enterprise/ops-data-plane/backend
      '/api': {
        target: process.env.API_TARGET ?? 'http://127.0.0.1:18080',
        changeOrigin: true,
      },
    },
  },
  build: {
    sourcemap: true,
    rollupOptions: {
      output: {
        // 只把 react 运行时单独切出来，其余交给 Rollup 默认分块。
        //
        // 两个坑都踩过了：
        // 1) 包名数组形式（{react: ['react-dom']}）要求这些包是本应用的**直接**依赖，
        //    而表格库是 @ops/ui 的内部依赖，pnpm 严格布局下解析不到，构建直接失败。
        // 2) 手动切多个 chunk 很容易切出循环依赖（table → react → table），
        //    Rollup 只是警告不报错，但产物加载顺序会变得不可预测。
        // react 是唯一"绝不会反向引用业务代码"的包，切它最安全，
        // 而且它最少变动，单独缓存的收益也最高。
        manualChunks(id) {
          return /[\\/]node_modules[\\/](\.pnpm[\\/])?(react|react-dom|scheduler)[@\\/]/.test(id)
            ? 'react'
            : undefined
        },
      },
    },
  },
})
