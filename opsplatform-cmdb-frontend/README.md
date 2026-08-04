# CMDB 前端

运维配置管理库的 Web 界面。功能说明、权限模型、审计与回滚的完整文档在
[后端 README](../opsplatform-cmdb-backend/README.md)，这里只讲前端本身。

## 技术栈

Vue 3（`<script setup>`）+ Element Plus + Pinia + Vue Router + Vite。
没有 TypeScript，没有 UI 二次封装层——页面直接用 Element Plus 组件。

## 目录

```
src/
  views/          一个页面一个文件
  components/     跨页复用
    LoadError.vue     加载失败横幅（区分「无权限」与「故障」）
    ChangeDiff.vue    字段级 diff 表格
    ChangeHistory.vue 对象变更历史时间线（详情页内嵌）
    Pager.vue         分页
    integrations/     接入管理的各凭据面板
  composables/
    useLoadState.js   加载三态（loading / error / success）
  stores/
    auth.js       登录态 + 权限码 + SSO
    app.js        全局配置与确认弹窗
  api/
    http.js       axios 实例、错误归一化、401 处理
    cmdb.js       接口定义
  router/index.js 路由 + 路由→权限映射 + SSO 入口 + 无权限拦截
```

## 本地开发

```bash
npm install
npm run dev      # 需要后端在 :8080，或改 vite 代理指向 30829
npm run build
```

构建镜像**必须传版本号**：

```bash
docker build --build-arg VERSION=v184 -t localhost:8070/opsplatform/opsplatform-cmdb-frontend:v184 .
```

不传的话 `ARG VERSION=dev` 会让界面左上角显示 `dev`，
线上排障时根本看不出前端是哪个版本。版本规则：每次构建 +1，
以 `k8s-deploy.yaml` 里的 tag 为准。

发版前跑一遍 `npm audit --omit=dev`，应为 0 漏洞。

## 权限在前端怎么用

```js
const auth = useAuthStore()
auth.hasMenu('k8s_nodes')      // → menu:cmdb_k8s_nodes
auth.hasButton('manage_dns')   // → cmdb:manage_dns
```

- 菜单显隐：`App.vue` 里 `visibleMenus` 统一过滤，
  **组下一个可见子项都没有就整组隐藏**，不留空分组
- 路由拦截：`router/index.js` 的 `ROUTE_PERM` 表，无权限导到 `/forbidden`
- 按钮显隐：页面里用 `computed` 收口，别在模板里散写权限码

  ```js
  const canManage = computed(() => auth.hasButton('manage_domains'))
  ```

三条注意：

1. **`v-if` 要加在 `el-tooltip` 上，不是它内层的按钮**。按钮被隐藏后
   tooltip 就没有子节点了，Element Plus 会报 `[ElOnlyChild] no valid child node found`
2. 前端只做显隐，**安全边界在后端**。少一个 `v-if` 是"看得见点了报错"的体验问题，
   不是漏洞
3. 本地 `admin` 账号 `isLocal === true`，前端一律放行（后端同样）

## UI 约定

- **禁止**  `window.confirm / alert / prompt`，统一用 `appStore.showConfirm`。
  它取消时是 **reject** 而不是返回 false：

  ```js
  try { await app.showConfirm('确定删除？', '确认') } catch (_) { return }
  ```
- 弹窗不能点遮罩关闭：`:close-on-click-modal="false"`
- 固定枚举值（PROD / UAT 等）原样展示，不加"（生产）"这类解释
- **三态纪律**：新页面一律 `useLoadState` + `LoadError`。
  加载失败必须显式报错，不能退化成"空数据"——那等于在故障时告诉运维"一切正常"。
  `LoadError` 会区分「无权限」（黄色、无重试按钮）和「加载失败」（红色、可重试）

## 在详情页嵌入变更历史

```vue
<ChangeHistory table="certificates" :pk="route.params.id" />
```

`table` 是审计里记录的表名（`domains` / `certificates` / `k8s_clusters` …），
`pk` 是行主键。组件自己拉数据、自己处理空态和失败态。
