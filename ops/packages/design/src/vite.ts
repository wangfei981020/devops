/**
 * Vite 插件：把首屏引导脚本内联进 index.html 的 <head> 最前面。
 *
 * 为什么必须是插件而不是手写进 index.html：
 * 每个应用都有自己的 index.html，手抄一遍就有一份会漂移。
 * 主题防闪这种"改了也不报错、只是偶尔白闪一帧"的东西，漂移了没人会发现。
 */

import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

// 这里刻意不声明 vite 的类型依赖 —— design 是纯设计系统包，
// 让它依赖构建工具的类型会把 vite 拖进每个消费方的依赖图。
interface IndexHtmlTag {
  tag: string
  children?: string
  injectTo?: 'head' | 'head-prepend' | 'body' | 'body-prepend'
  attrs?: Record<string, string | boolean>
}

interface MinimalPlugin {
  name: string
  enforce?: 'pre' | 'post'
  transformIndexHtml: {
    order: 'pre'
    handler: (html: string) => { html: string; tags: IndexHtmlTag[] }
  }
}

export function opsBootScript(): MinimalPlugin {
  return {
    name: 'ops-boot-script',
    enforce: 'pre',
    transformIndexHtml: {
      order: 'pre',
      handler(html: string) {
        const path = fileURLToPath(new URL('../src/boot.js', import.meta.url))
        const code = readFileSync(path, 'utf8')
        return {
          html,
          tags: [
            {
              tag: 'script',
              // head-prepend：必须排在任何 <link rel=stylesheet> 之前，
              // 否则浏览器已经用默认配色画了第一帧。
              injectTo: 'head-prepend',
              children: code,
            },
          ],
        }
      },
    },
  }
}
