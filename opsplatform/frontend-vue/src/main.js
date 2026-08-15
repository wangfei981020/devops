import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import './assets/css/main.css'

// 前端版本。构建时由 Dockerfile 的 ARG VERSION 注入，本地开发是 dev。
// 排障时经常要确认「页面上跑的到底是哪一版」——之前无处可查，
// 只能靠 k8s 的镜像 tag 反推，而浏览器缓存又可能让两者对不上。
const APP_VERSION = import.meta.env.VITE_APP_VERSION || 'dev'
window.__APP_VERSION__ = APP_VERSION
console.log(`%c运维平台 前端 ${APP_VERSION}`, 'color:#3a84ff;font-weight:600')

const app = createApp(App)
const pinia = createPinia()

app.use(pinia)
app.use(router)

app.mount('#app')
