import { opsBootScript } from '@ops/design/vite'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { resolve } from 'node:path'
import { defineConfig } from 'vite'

/**
 * SSO 前端：**一个工程，两个入口**。
 *
 * # 为什么是双入口而不是两个工程
 *
 * 门户（终端用户）与控制台（管理员）不共用路由、不共用构建产物 ——
 * 门户不该加载控制台那一大堆策略、审计、身份源的代码。双入口就满足了这一点：
 * 产出两份 HTML 与各自的 chunk。
 *
 * 拆成两个工程额外买到的只有"可以分别发版"，而那恰恰是**不想要**的：
 * 两边共用同一个后端 API，门户新、控制台旧的时候契约会错开，
 * 而契约错开不会报错，只会有个别按钮静默失效。
 * 一次构建出两份产物，版本天然一致。
 *
 * # 为什么同域不同路径
 *
 * 部署时一个 nginx 把 `/` 指向门户、`/console` 指向控制台。
 * 换成两个域名的话，共享会话 Cookie 立刻变成跨域问题 ——
 * 而"登录一次两边都通"正是这个产品存在的理由。
 */
const VERSION = process.env.VERSION ?? 'dev'
const COMMIT = process.env.GIT_COMMIT ?? 'unknown'

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    // 首屏主题引导脚本，必须排在样式之前，否则深色下会白闪一帧
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
    port: 5274, // CMDB 占 5273
    proxy: {
      '/api': {
        target: process.env.API_TARGET ?? 'http://127.0.0.1:18095',
        changeOrigin: true,
      },
    },
  },
  build: {
    sourcemap: true,
    rollupOptions: {
      input: {
        portal: resolve(__dirname, 'portal.html'),
        console: resolve(__dirname, 'console.html'),
      },
      output: {
        // 只切 react 运行时，其余交给 Rollup 默认分块。
        // 手动切多个 chunk 很容易切出循环依赖，Rollup 只警告不报错，
        // 但产物加载顺序会变得不可预测。
        manualChunks(id) {
          return /[\\/]node_modules[\\/](\.pnpm[\\/])?(react|react-dom|scheduler)[@\\/]/.test(id)
            ? 'react'
            : undefined
        },
      },
    },
  },
})
