<template>
  <router-view v-if="route.path === '/login'" />

  <div v-else class="app">
    <aside class="sidebar" :class="{ collapsed }">
      <div class="brand">
        <el-icon :size="20" color="#3b82f6"><Files /></el-icon>
        <span v-show="!collapsed" class="brand-n">CMDB</span>
        <span v-show="!collapsed" class="brand-t">{{ version }}</span>
      </div>
      <el-menu class="nav" :default-active="route.path" router :collapse="collapsed" :collapse-transition="false"
               :default-openeds="['资产管理','系统管理']"
               background-color="transparent" text-color="#c0c4d0" active-text-color="#fff">
        <template v-for="m in visibleMenus" :key="m.label || m.path">
          <el-menu-item v-if="m.type === 'item'" :index="m.path">
            <el-icon><component :is="m.icon" /></el-icon>
            <template #title>{{ m.label }}</template>
          </el-menu-item>
          <el-sub-menu v-else :index="m.label">
            <template #title><el-icon><component :is="m.icon" /></el-icon><span>{{ m.label }}</span></template>
            <el-menu-item v-for="ch in m.children" :key="ch.path" :index="ch.path">
              <el-icon><component :is="ch.icon" /></el-icon>
              <template #title>{{ ch.label }}</template>
            </el-menu-item>
          </el-sub-menu>
        </template>
      </el-menu>
      <div class="foot" @click="toggle">
        <el-icon><Fold v-if="!collapsed" /><Expand v-else /></el-icon>
        <span v-show="!collapsed">收起菜单</span>
      </div>
    </aside>

    <main class="main">
      <div class="topbar">
        <!-- 匹配不到 title 时显示「未知页面」而不是回退到「总览」——
             那个默认值会把 404 伪装成一个正常页面（CMDB-018） -->
        <div class="crumb">CMDB / <b>{{ route.meta.title || '未知页面' }}</b></div>
        <el-dropdown trigger="click" @command="onCmd">
          <span class="user"><el-icon><User /></el-icon> {{ auth.user?.username || 'admin' }} <el-icon><ArrowDown /></el-icon></span>
          <template #dropdown>
            <el-dropdown-menu>
              <!-- 改自己的密码不该要"管别人账号"的权限。原先只有用户管理页有改密入口，
                   而那个页面要 cmdb:manage_users——只读账号完全没有改密的地方，
                   只能找管理员代改，而代改意味着管理员知道了别人的新密码。 -->
              <el-dropdown-item v-if="auth.isLocal" command="passwd">
                <el-icon><Key /></el-icon> 修改密码
              </el-dropdown-item>
              <el-dropdown-item command="logout" divided><el-icon><SwitchButton /></el-icon> 退出登录</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
      <div class="content"><router-view /></div>
    </main>

    <el-dialog v-model="pwd.show" title="修改密码" width="440px" :close-on-click-modal="false">
      <el-form label-width="90px">
        <el-form-item label="当前密码">
          <el-input v-model="pwd.old" type="password" show-password placeholder="验证身份用" />
        </el-form-item>
        <el-form-item label="新密码">
          <el-input v-model="pwd.new1" type="password" show-password placeholder="至少 8 位" />
        </el-form-item>
        <el-form-item label="确认新密码">
          <el-input v-model="pwd.new2" type="password" show-password />
        </el-form-item>
      </el-form>
      <el-alert type="warning" :closable="false" show-icon
        title="改完密码所有登录会话会立即失效（包括当前这个），需要用新密码重新登录" />
      <template #footer>
        <el-button @click="pwd.show = false">取消</el-button>
        <el-button type="primary" :loading="pwd.saving" @click="savePwd">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { shallowRef, ref, reactive, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { permOf as permOfPath } from './router'
import { Key, Odometer, Connection, Lock, Share, Grid, Operation, Files, User, ArrowDown, SwitchButton, Fold, Expand, Coin, Tools, List, CircleCheck, Clock, Bell, Monitor, Tickets, Cloudy, Location, Sort, TrendCharts, Document, Warning } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { changeOwnPassword } from './api/cmdb'
import { useAuthStore } from './stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const version = import.meta.env.VITE_APP_VERSION || 'dev'
const collapsed = ref(localStorage.getItem('cmdb_menu_collapsed') === '1')
function toggle() { collapsed.value = !collapsed.value; localStorage.setItem('cmdb_menu_collapsed', collapsed.value ? '1' : '0') }

const menus = shallowRef([
  { type: 'item', path: '/overview', label: '总览', icon: Odometer },
  { type: 'group', label: '云资源', icon: Cloudy, children: [
    { path: '/hosts', label: '主机', icon: Monitor },
    { path: '/cloud-ips', label: 'IP 地址', icon: Location },
    { path: '/cloud-networks', label: 'VPC 网络', icon: Share },
    { path: '/cloud-firewalls', label: '防火墙', icon: Lock },
    { path: '/cloud-lbs', label: '负载均衡', icon: Sort },
    { path: '/cloud-audit', label: '云平台审计', icon: CircleCheck },
  ] },
  { type: 'group', label: '资产管理', icon: Coin, children: [
    { path: '/domains', label: '域名', icon: Connection },
    { path: '/dns-records', label: 'DNS 记录', icon: List },
    { path: '/cdn-sites', label: 'CDN 站点', icon: Cloudy },
    { path: '/certs', label: '证书', icon: Lock },
    { path: '/cert-inspect', label: '到期巡检', icon: CircleCheck },
    // 关系图谱冷藏：当前只有 域名→证书 一种关系、节点少、价值低；二期 M7 接入
    // 物理→虚拟→容器→应用全链路后升级为「影响分析」再放出。路由/组件保留。
    // { path: '/relations', label: '关系图谱', icon: Share },
  ] },
  { type: 'group', label: 'K8s', icon: Grid, children: [
    { path: '/k8s-clusters', label: '集群管理', icon: Cloudy },
    { path: '/version-upgrade', label: '版本与升级', icon: Clock },
    { path: '/k8s-nodes', label: '节点', icon: Monitor },
    { path: '/k8s-workloads', label: '工作负载', icon: Coin },
    { path: '/k8s-pods', label: 'Pod', icon: List },
    { path: '/k8s-networking', label: '网络', icon: Share },
    { path: '/k8s-storage', label: '存储/伸缩', icon: Coin },
    { path: '/k8s-events', label: '事件', icon: Bell },
    { path: '/k8s-health', label: '集群体检', icon: CircleCheck },
    { path: '/k8s-topology', label: '全链路', icon: Share },
    { path: '/k8s-ns-project', label: '命名空间归属', icon: Coin },
  ] },
  { type: 'item', path: '/event-center', label: '事件中心', icon: Bell },
  { type: 'item', path: '/alerts', label: '告警', icon: Warning },
  { type: 'item', path: '/k8s-usage', label: '资源使用率', icon: TrendCharts },
  { type: 'item', path: '/cost', label: '云成本', icon: Coin },
  // 「展示台」已删除：它和总览用的是同一个 /dashboard 接口，7 个 KPI 全部是总览的子集，
  // 两个"独有"图表实测都是废的——按环境分布饼图 100% 落在「未分类」（cis.env 无人填写），
  // 到期倒计时图整块空白；反而漏掉了总览里最该上墙的「线上证书检测失败」。
  // /dashboard 路由保留为重定向到总览，老书签不至于 404。
  // ⚠️ 这里原来用 Setting（齿轮）。它的 SVG path 长 1403 字符，是全站 37 个菜单图标里
  // 唯一的断层离群值（中位数 185，第二名 443），在部分 Windows Chrome 上**渲染不出来**——
  // DOM、尺寸(24x18)、fill、opacity 全部正常，强制 fill:red 也依然不可见。
  // 换成路径简单的 Operation。新增菜单图标时注意别再挑这种超复杂路径的。
  { type: 'group', label: '系统管理', icon: Operation, children: [
    { path: '/basic', label: '基础配置', icon: Tools },
    { path: '/integrations', label: '接入管理', icon: Cloudy },
    // 「模型管理」整页只有一个 2 行只读表格、无任何操作，已并入「基础配置 → CI 类型」卡片。
    // /models 路由保留并重定向，老书签不至于 404。
    { path: '/notify', label: '通知', icon: Bell },
    { path: '/cron', label: '定时任务', icon: Clock },
    { path: '/task-runs', label: '执行记录', icon: Tickets },
    { path: '/users', label: '用户管理', icon: User },
    { path: '/audit', label: '操作审计', icon: Document },
    // 「AI 接入」并进「接入管理」的 mcp tab：它就是一个接入点 + 一份凭据，
    // 和注册商/云账号/观测数据源同类，不值得单占一个菜单位。/mcp 路由保留做兼容跳转
    // 「设置」整页只剩一个指标导出白名单，已并进「基础配置」，菜单去掉这一项。
    // /settings 路由保留并重定向，老书签不至于 404
  ] },
])

// 按权限过滤菜单。
//
//	分组本身不发权限：只要组里还有一个子项可见就渲染这个组，
//	一个子项都不剩就整组隐藏——否则会出现点开是空的分组。
//	路径 → 权限页代号复用 router 里那张表，两处只有一份真相。
const visibleMenus = computed(() => {
  // isUnrestricted 而不是 isLocal：运维平台超管也不受权限码约束（和后端 IsAdmin 一致）。
  // 这里用错会出现"后端放行、菜单却是空的"。
  if (auth.isUnrestricted) return menus.value
  const out = []
  for (const m of menus.value) {
    if (m.type === 'item') {
      if (auth.hasMenu(permOfPath(m.path))) out.push(m)
      continue
    }
    const children = m.children.filter(ch => auth.hasMenu(permOfPath(ch.path)))
    if (children.length) out.push({ ...m, children })
  }
  return out
})

// 权限同步已经前移到路由守卫（auth.ensureFresh），那里会在**做拦截判断之前**
// 拉一次。这里保留调用只是兜底（比如直接挂载而没走守卫的场景），
// ensureFresh 内部有去重，不会重复发请求。
//
// ⚠️ 别把它改回 refreshPermissions：那样又会变成"守卫先跑、刷新后到"，
// 菜单和路由拦截依据不同的权限快照，出现"菜单里有、点进去被拦"。
onMounted(() => { auth.ensureFresh?.().catch(() => {}) })

const pwd = reactive({ show: false, old: '', new1: '', new2: '', saving: false })

function onCmd(c) {
  if (c === 'logout') { auth.logout(); router.replace('/login') }
  if (c === 'passwd') Object.assign(pwd, { show: true, old: '', new1: '', new2: '', saving: false })
}

async function savePwd() {
  if (pwd.new1.length < 8) { ElMessage.warning('新密码至少 8 位'); return }
  if (pwd.new1 !== pwd.new2) { ElMessage.warning('两次输入的新密码不一致'); return }
  pwd.saving = true
  try {
    const r = await changeOwnPassword({ old_password: pwd.old, new_password: pwd.new1 })
    ElMessage.success(r?.msg || '密码已修改，请重新登录')
    pwd.show = false
    // 后端已经把所有会话作废了，本地状态必须跟着清，否则会带着废 token 到处 401
    auth.logout()
    router.replace('/login')
  } catch (e) {
    ElMessage.error(e?.raw?.response?.data?.error || e?.message || '修改失败')
  } finally { pwd.saving = false }
}
</script>

<style scoped>
.app { display: flex; height: 100vh; }
.sidebar { width: 200px; background: #1f2430; flex-shrink: 0; display: flex; flex-direction: column; transition: width .2s; }
.sidebar.collapsed { width: 64px; }
.brand { display: flex; align-items: center; gap: 8px; padding: 16px 18px; border-bottom: 1px solid rgba(255,255,255,.06); height: 56px; box-sizing: border-box; }
.brand-n { color: #fff; font-weight: 700; font-size: 15px; letter-spacing: 1px; }
.brand-t { margin-left: auto; font-size: 10px; color: rgba(255,255,255,.45); background: rgba(255,255,255,.08); padding: 1px 6px; border-radius: 3px; }
.nav { flex: 1; border-right: none; padding: 8px 0; overflow-y: auto; overflow-x: hidden; }
.nav::-webkit-scrollbar { width: 6px; }
.nav::-webkit-scrollbar-thumb { background: rgba(255,255,255,.15); border-radius: 3px; }
.nav:not(.el-menu--collapse) { width: 200px; }
.nav :deep(.el-menu-item) { height: 42px; font-size: 13.5px; border-left: 2px solid transparent; }
.nav :deep(.el-menu-item:hover) { background: rgba(255,255,255,.05); color: #fff; }
.nav :deep(.el-menu-item.is-active) { background: rgba(59,130,246,.16); border-left-color: #3b82f6; color: #fff; }
.nav :deep(.el-sub-menu__title) { height: 42px; font-size: 13.5px; }
.nav :deep(.el-sub-menu__title:hover) { background: rgba(255,255,255,.05); color: #fff; }
.nav :deep(.el-sub-menu .el-menu-item) { min-width: auto; }
/* 箭头：收起朝右(›)、展开朝下(▾)，与运维平台一致。!important 覆盖 element 默认的 180° 旋转 */
.nav :deep(.el-sub-menu__icon-arrow) { transition: transform .2s !important; transform: rotate(-90deg) !important; }
.nav :deep(.el-sub-menu.is-opened .el-sub-menu__icon-arrow) { transform: rotate(0deg) !important; }
.foot { display: flex; align-items: center; gap: 8px; padding: 12px 20px; font-size: 12px; color: rgba(255,255,255,.5); border-top: 1px solid rgba(255,255,255,.06); cursor: pointer; }
.foot:hover { color: #fff; background: rgba(255,255,255,.05); }
.main { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.topbar { height: 52px; background: #fff; border-bottom: 1px solid #e7eaf0; display: flex; align-items: center; justify-content: space-between; padding: 0 20px; }
.crumb { font-size: 13px; color: #606266; }
.crumb b { color: #1f2430; }
.user { display: flex; align-items: center; gap: 6px; cursor: pointer; font-size: 13px; color: #606266; }
.content { flex: 1; overflow: auto; background: #f4f6fa; }
</style>
