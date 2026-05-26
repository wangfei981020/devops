import { createRouter, createWebHistory } from 'vue-router'
import ClusterList from '../views/ClusterList.vue'
import ClusterDetail from '../views/ClusterDetail.vue'
import Settings from '../views/Settings.vue'

export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: ClusterList },
    { path: '/clusters/:id', component: ClusterDetail, props: true },
    { path: '/settings', component: Settings },
  ],
})
