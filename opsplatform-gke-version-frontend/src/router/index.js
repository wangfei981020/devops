import { createRouter, createWebHistory } from 'vue-router'
import AppLayout from '../components/AppLayout.vue'
import Overview from '../views/Overview.vue'
import ClusterList from '../views/ClusterList.vue'
import ClusterDetail from '../views/ClusterDetail.vue'
import Settings from '../views/Settings.vue'
import NotifyUsers from '../views/NotifyUsers.vue'
import LarkWebhooks from '../views/LarkWebhooks.vue'
import AlertRules from '../views/AlertRules.vue'

export default createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      component: AppLayout,
      children: [
        { path: '', component: Overview, meta: { title: '总览' } },
        { path: 'clusters', component: ClusterList, meta: { title: '集群列表' } },
        { path: 'clusters/:id', component: ClusterDetail, props: true, meta: { title: '集群详情' } },
        { path: 'alert-rules', component: AlertRules, meta: { title: '告警规则' } },
        { path: 'lark-webhooks', component: LarkWebhooks, meta: { title: 'Lark Webhook' } },
        { path: 'notify-users', component: NotifyUsers, meta: { title: '通知人' } },
        { path: 'settings', component: Settings, meta: { title: '设置' } },
      ],
    },
  ],
})
