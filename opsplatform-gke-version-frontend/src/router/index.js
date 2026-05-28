import { createRouter, createWebHistory } from 'vue-router'
import AppLayout from '../components/AppLayout.vue'
import ClusterList from '../views/ClusterList.vue'
import ClusterDetail from '../views/ClusterDetail.vue'
import Settings from '../views/Settings.vue'

export default createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      component: AppLayout,
      children: [
        { path: '', component: ClusterList, meta: { title: '集群列表' } },
        { path: 'clusters/:id', component: ClusterDetail, props: true, meta: { title: '集群详情' } },
        { path: 'settings', component: Settings, meta: { title: '设置' } },
      ],
    },
  ],
})
