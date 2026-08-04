import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
// Element Plus 默认英文：不挂中文包，全站表格空态是 "No Data"、分页是 "Total 0 / 10/page"、
// 日期选择器和确认框也全是英文。一个中文运维平台里夹着这些英文控件文案，
// 既不一致，也让"没有数据"这种关键状态用外语说——这是一行配置就该解决的事。
import zhCn from 'element-plus/es/locale/lang/zh-cn.mjs'
import * as Icons from '@element-plus/icons-vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import './styles.css'

const app = createApp(App)
for (const [k, v] of Object.entries(Icons)) app.component(k, v)
app.use(createPinia()).use(router).use(ElementPlus, { locale: zhCn })
app.mount('#app')
