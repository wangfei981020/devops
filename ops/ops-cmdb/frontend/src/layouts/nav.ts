import {
  Activity,
  AlertTriangle,
  ArrowUpCircle,
  BellRing,
  Bot,
  Boxes,
  Cloud,
  CloudLightning,
  Coins,
  Database,
  FileSearch,
  Flame,
  Globe,
  HardDrive,
  History,
  KeyRound,
  Layers,
  LayoutDashboard,
  Locate,
  Network,
  Package,
  Radar,
  Radio,
  Route,
  Server,
  Settings,
  Share2,
  ShieldAlert,
  ShieldCheck,
  Terminal,
  Timer,
  UserCog,
  Users,
  Workflow,
  type LucideIcon,
} from 'lucide-react'

/**
 * 菜单的**唯一**数据源。
 *
 * 权限过滤、EE 门控、路由生成全部从这里派生，别处不许再写一份菜单。
 * 上一代系统里菜单在前端硬编码、权限在后端另有一份，两边判据不一致，
 * 同一个问题栽了三次 —— 收口到一个数组是最省事的解法。
 */

export interface NavItem {
  key: string
  /** i18n key，命名空间 nav。组件里用 t(item.labelKey) 取。 */
  labelKey: string
  icon: LucideIcon
  path: string
  /**
   * 需要的权限码，必须与后端 `permPrefixRules` 里那一条**同名**。
   *
   * ⚠️ 前端这份只决定"显不显示"，**拦截全在后端**。
   * 少写一个 perm 只会让菜单多露一个，点进去照样 403；
   * 但如果反过来把它当拦截手段，改一行前端就能绕过去。
   *
   * ⚠️ 写错码名不会报错，只会让菜单对所有非管理员静默消失 ——
   * `check-perm-codes.mjs` 会拿后端源码里的码表来核对。
   */
  perm: string
  /**
   * 需要的 EE feature。
   * 未授权时不是隐藏菜单，而是进去看到「此功能需要企业版」的说明 ——
   * 悄悄消失的菜单会让人以为系统坏了或自己记错了。
   */
  feature?: string
  /** 尚未实现，界面上置灰。占位菜单必须显式标注，不能假装能点。 */
  planned?: boolean
}

export interface NavGroup {
  key: string
  labelKey: string
  items: NavItem[]
  // 🔴 这里曾经有 footer?: boolean（把「管理」单独吊在侧栏最底下）。
  // 已删除，侧栏必须是一列——理由见 @ops/ui 的 NavGroup 说明。
}

export const NAV: NavGroup[] = [
  {
    key: 'overview',
    labelKey: 'nav:group.overview',
    items: [
      {
        key: 'situation',
        perm: 'menu:cmdb_overview',
        labelKey: 'nav:overview.situation',
        icon: LayoutDashboard,
        path: '/overview',
      },
      {
        key: 'cluster-health',
        perm: 'menu:cmdb_k8s_health',
        labelKey: 'nav:overview.clusterHealth',
        icon: Activity,
        path: '/overview/clusters',
      },
    ],
  },
  {
    // 计算与网络：**我们买了什么**。变更走云控制台 / IaC，按量计费，是安全边界。
    key: 'cloud',
    labelKey: 'nav:group.cloud',
    items: [
      {
        key: 'hosts',
        labelKey: 'nav:cloud.hosts',
        icon: Server,
        path: '/resources/hosts',
        perm: 'menu:cmdb_hosts',
      },
      {
        key: 'cloud-network',
        perm: 'menu:cmdb_cloud_networks',
        labelKey: 'nav:cloud.network',
        icon: Network,
        path: '/resources/subnets',
      },
      {
        key: 'cloud-ips',
        perm: 'menu:cmdb_cloud_ips',
        labelKey: 'nav:cloud.ips',
        icon: Locate,
        path: '/resources/ips',
      },
      {
        key: 'cloud-firewalls',
        perm: 'menu:cmdb_cloud_firewalls',
        labelKey: 'nav:cloud.firewalls',
        icon: Flame,
        path: '/resources/firewalls',
      },
      {
        key: 'cloud-lb',
        perm: 'menu:cmdb_cloud_lbs',
        labelKey: 'nav:cloud.loadBalancers',
        icon: Share2,
        path: '/resources/lbs',
      },
    ],
  },
  {
    // 域名与入口：**外面怎么访问到我们的服务**。
    //
    // ⚠️ 这一组是按**排障路径**分的，不是按对象类型或供应商分的。
    // 域名 → DNS 解析 → CDN → 证书 是一条链路，人来看这几页几乎都是同一个场景：
    // 「某个域名打不开了」。
    //
    // CDN 曾被归到「平台服务」（理由是 Cloudflare 和 Harbor 一样是外部平台）——
    // 那是按供应商归类。没人早上起来想"我们用了哪些外部平台"，
    // 但天天有人在查"这个域名为什么 502"。**按人怎么用来分组，不按东西是什么。**
    key: 'ingress',
    labelKey: 'nav:group.ingress',
    items: [
      {
        key: 'domains',
        perm: 'menu:cmdb_domains',
        labelKey: 'nav:ingress.domains',
        icon: Globe,
        path: '/resources/domains',
      },
      {
        key: 'dns',
        perm: 'menu:cmdb_dns_records',
        labelKey: 'nav:ingress.dns',
        icon: Route,
        path: '/resources/dns',
      },
      {
        // 主机头台账紧挨 DNS 解析：两套数据，但都是"这个域名下有什么"
        key: 'host-records',
        perm: 'menu:cmdb_domains',
        labelKey: 'nav:ingress.hostRecords',
        icon: Route,
        path: '/resources/host-records',
      },
      {
        key: 'cdn',
        perm: 'menu:cmdb_cdn_sites',
        labelKey: 'nav:ingress.cdn',
        icon: CloudLightning,
        path: '/resources/cdn',
      },
      {
        key: 'certs',
        perm: 'menu:cmdb_certs',
        labelKey: 'nav:ingress.certs',
        icon: ShieldCheck,
        path: '/resources/certs',
      },
    ],
  },
  {
    // 集群：**我们跑了什么**。变更走 GitOps，对象数量比云资源高一两个量级。
    //
    // ⚠️ 与云资源分成两组是刻意的，不是为了菜单好看：
    // 「网络」在云侧指 VPC / 子网 / 防火墙，在 K8s 侧指 Service / Ingress /
    // NetworkPolicy；「存储」在云侧是云盘，在 K8s 侧是 PVC / StorageClass。
    // 合成一个入口，就等于把两个不同的东西压成一个名字 ——
    // 而点进去看到的是哪一种，取决于我们当时想给谁看，这正是全站在防的那类问题。
    //
    // ⚠️ 「主机」与「节点」会有重叠对象（GKE 节点就是一台虚机），这是**故意**的：
    // 主机页回答"这台机器多少钱、谁的、还在不在"，节点页回答"它还能不能调度、
    // 上面跑了什么"。两页互相链接（主机抽屉里已有节点关联三态）。
    key: 'k8s',
    labelKey: 'nav:group.k8s',
    items: [
      {
        key: 'clusters',
        perm: 'menu:cmdb_k8s_clusters',
        labelKey: 'nav:k8s.clusters',
        icon: Boxes,
        path: '/k8s/clusters',
      },
      {
        // 版本与升级紧跟集群：它回答的是"这个集群什么时候会被自动升级、
        // 升了会断什么"，属于集群的生命周期，不是一个独立主题
        key: 'version-upgrade',
        perm: 'menu:cmdb_version_upgrade',
        labelKey: 'nav:k8s.versionUpgrade',
        icon: ArrowUpCircle,
        path: '/k8s/upgrades',
      },
      {
        key: 'nodes',
        perm: 'menu:cmdb_k8s_nodes',
        labelKey: 'nav:k8s.nodes',
        icon: Server,
        path: '/k8s/nodes',
      },
      {
        // 变更前检查：PDB / HPA / 节点池 —— 三张表回答同一个问题
        // 「这次升级、缩容、驱逐能不能安全做」（OPSCMDB-023 第二档）。
        //
        // ⚠️ 菜单用 menu:cmdb_k8s_nodes，但后端 perm.go 里
        // /api/k8s/hpas 和 /api/k8s/pdbs 挂的是 **menu:cmdb_k8s_storage** ——
        // HPA/PDB 和存储没有任何关系，这是分类错误（同 OPSCMDB-045 的形态）。
        // 没在这里擅自改后端权限：改权限码会影响已经配好的角色。
        // 只有 nodes 权限的人会看到菜单，但那两个 tab 会如实报 403 而不是空表。
        key: 'disruption',
        perm: 'menu:cmdb_k8s_nodes',
        labelKey: 'nav:k8s.disruption',
        icon: ShieldAlert,
        path: '/k8s/disruption',
      },
      {
        key: 'namespaces',
        perm: 'menu:cmdb_k8s_ns_project',
        labelKey: 'nav:k8s.namespaces',
        icon: Layers,
        path: '/k8s/namespaces',
      },
      {
        key: 'workloads',
        perm: 'menu:cmdb_k8s_workloads',
        labelKey: 'nav:k8s.workloads',
        icon: Package,
        path: '/k8s/workloads',
      },
      {
        key: 'pods',
        perm: 'menu:cmdb_k8s_pods',
        labelKey: 'nav:k8s.pods',
        icon: Boxes,
        path: '/k8s/pods',
      },
      {
        key: 'k8s-network',
        perm: 'menu:cmdb_k8s_networking',
        labelKey: 'nav:k8s.network',
        icon: Network,
        path: '/k8s/services',
      },
      {
        key: 'k8s-storage',
        perm: 'menu:cmdb_k8s_storage',
        labelKey: 'nav:k8s.storage',
        icon: HardDrive,
        path: '/k8s/pvcs',
      },
      {
        key: 'k8s-events',
        perm: 'menu:cmdb_k8s_events',
        labelKey: 'nav:k8s.clusterEvents',
        icon: Radar,
        path: '/k8s/events',
      },
    ],
  },
  {
    // 平台服务：**我们自己搭的**那些东西（镜像仓库、制品库、中间件……）。
    //
    // 它既不是云资源（不是从云厂商买的、不按量计费），也不是 k8s 对象
    // （Harbor 里的仓库不是集群里的资源）。硬塞进任何一组都会让那组的定义变糊，
    // 而组的定义一糊，下一个人就不知道新东西该往哪放。
    key: 'platform',
    labelKey: 'nav:group.platform',
    items: [
      {
        key: 'registry',
        perm: 'menu:cmdb_integrations',
        labelKey: 'nav:platform.registry',
        icon: Database,
        path: '/resources/registry',
      },
      {
        // 构建流水线（KubeSphere DevOps）。
        // ⚠️ perm 必须与后端 perm.go 里 `/api/devops/` 那条**同名**，
        // 否则菜单显示了但接口 403 —— 后端给的是 menu:cmdb_k8s_usage
        key: 'pipelines',
        perm: 'menu:cmdb_k8s_usage',
        labelKey: 'nav:platform.pipelines',
        icon: Workflow,
        path: '/platform/pipelines',
      },
    ],
  },
  {
    key: 'topology',
    labelKey: 'nav:group.topology',
    items: [
      {
        key: 'graph',
        labelKey: 'nav:topology.graph',
        icon: Share2,
        path: '/topology',
        perm: 'menu:cmdb_relations',
      },
      {
        key: 'impact',
        perm: 'menu:cmdb_k8s_topology',
        labelKey: 'nav:topology.impact',
        icon: Workflow,
        path: '/topology/impact',
      },
    ],
  },
  {
    // 运行：**现在正在发生什么 / 刚发生了什么**。
    //
    // ⚠️ 这里的「事件中心」与集群组的「集群事件」是两个东西，名字必须分开：
    //   事件中心 = 平台层统一时间线（到期、变更、同步失败、K8s Warning），CMDB 自己产生
    //   集群事件 = apiserver 的 Event 对象，K8s 产生
    // 都叫"事件"的话，人点进去看到的不是想要的东西，还会以为是数据没采上来 ——
    // 这正是 §2.7.1 里"同一个词两边指不同东西"的同一类问题。
    key: 'runtime',
    labelKey: 'nav:group.runtime',
    items: [
      {
        key: 'alerts',
        perm: 'menu:cmdb_alerts',
        labelKey: 'nav:runtime.alerts',
        icon: AlertTriangle,
        path: '/runtime/alerts',
      },
      {
        key: 'event-center',
        perm: 'menu:cmdb_event_center',
        labelKey: 'nav:runtime.eventCenter',
        icon: History,
        path: '/runtime/events',
      },
      {
        // 资源使用率：实时查 Prometheus 的 CPU/内存曲线（OPSCMDB-021 补回）。
        // 放「运行」下而不是「成本」：它回答的是"现在忙不忙"，
        // 成本页回答的是"花了多少钱"——虽然常一起看，但不是同一个问题
        key: 'usage',
        perm: 'menu:cmdb_k8s_usage',
        labelKey: 'nav:runtime.usage',
        icon: Radar,
        path: '/runtime/usage',
      },
      {
        // 观测数据自助查询：自己写 PromQL / LogQL（OPSCMDB-023 第一档）。
        // 和「资源使用率」的区别：那一页是预设好的曲线，这一页是自由查询 ——
        // 排障到一定深度必然要自己拼查询，没有它这一步只能回 Grafana。
        // 权限跟 usage 同一个码：后端 /api/obs/prom-query 要的就是它
        key: 'obs-query',
        perm: 'menu:cmdb_k8s_usage',
        labelKey: 'nav:runtime.obsQuery',
        icon: Terminal,
        path: '/runtime/obs-query',
      },
      {
        key: 'inspection',
        perm: 'menu:cmdb_cert_inspect',
        labelKey: 'nav:runtime.inspection',
        icon: Radar,
        path: '/runtime/inspection',
      },
      {
        // 定时任务与执行记录合成一页。
        //
        // 旧版拆成两个菜单，但人要回答的问题只有一个：**这个任务跑成功了吗**。
        // 拆开意味着每次都要在两页之间跳，还得自己对上是哪条任务 ——
        // 而"这条任务上次跑挂了"恰恰是任务列表上最该直接看到的一列。
        key: 'cron',
        perm: 'menu:cmdb_cron',
        labelKey: 'nav:runtime.cron',
        icon: Timer,
        path: '/runtime/cron',
      },
    ],
  },
  {
    key: 'cost',
    labelKey: 'nav:group.cost',
    items: [
      {
        key: 'cost-overview',
        labelKey: 'nav:cost.overview',
        icon: Coins,
        path: '/cost',
        perm: 'menu:cmdb_cost',
      },
      {
        // 单价归在成本组：它是这一组所有数字的来源
        key: 'cost-rates',
        perm: 'menu:cmdb_cost',
        labelKey: 'nav:cost.rates',
        icon: Coins,
        path: '/cost/rates',
      },
      {
        key: 'cost-waste',
        perm: 'menu:cmdb_k8s_usage',
        labelKey: 'nav:cost.waste',
        icon: Coins,
        path: '/cost/waste',
        // 成本分析引擎代码量大、可独立成模块，放 EE 比"一行 if 就能解锁"的功能合理
        feature: 'cost_advanced',
      },
    ],
  },
  {
    key: 'security',
    labelKey: 'nav:group.security',
    items: [
      {
        key: 'exposure',
        perm: 'menu:cmdb_k8s_networking',
        labelKey: 'nav:security.exposure',
        icon: ShieldAlert,
        path: '/security/exposure',
        feature: 'compliance',
      },
      {
        key: 'cloud-audit',
        perm: 'menu:cmdb_cloud_audit',
        labelKey: 'nav:security.cloudAudit',
        icon: UserCog,
        path: '/security/cloud-iam',
      },
      {
        key: 'credentials',
        perm: 'menu:cmdb_integrations',
        labelKey: 'nav:security.credentials',
        icon: KeyRound,
        path: '/security/credentials',
      },
    ],
  },
  {
    key: 'admin',
    labelKey: 'nav:group.admin',
    items: [
      {
        // 基础配置：环境、项目、CI 类型这些字典。低频，但被所有页面引用
        key: 'basic',
        perm: 'menu:cmdb_basic',
        labelKey: 'nav:admin.basic',
        icon: Settings,
        path: '/admin/basic',
      },
      {
        key: 'cloud-accounts',
        perm: 'menu:cmdb_integrations',
        labelKey: 'nav:admin.cloudAccounts',
        icon: Cloud,
        path: '/admin/cloud-accounts',
      },
      {
        key: 'datasources',
        perm: 'menu:cmdb_integrations',
        labelKey: 'nav:admin.datasources',
        icon: Settings,
        path: '/admin/datasources',
      },
      {
        key: 'obs-endpoints',
        perm: 'menu:cmdb_integrations',
        labelKey: 'nav:admin.obsEndpoints',
        icon: Radio,
        path: '/admin/obs-endpoints',
      },
      {
        // 注册商归在系统管理：它是"数据从哪来"的配置，和云账号、观测端点同类
        key: 'registrars',
        perm: 'menu:cmdb_basic',
        labelKey: 'nav:admin.registrars',
        icon: Globe,
        path: '/admin/registrars',
      },
      {
        key: 'notify',
        perm: 'menu:cmdb_notify',
        labelKey: 'nav:admin.notify',
        icon: BellRing,
        path: '/admin/notify',
      },
      {
        key: 'users',
        perm: 'menu:cmdb_users',
        labelKey: 'nav:admin.users',
        icon: Users,
        path: '/admin/users',
      },
      {
        key: 'sso',
        // 跟用户管理同一个权限码：能管账号的人才谈得上改"账号从哪来"
        perm: 'menu:cmdb_users',
        labelKey: 'nav:admin.sso',
        icon: KeyRound,
        path: '/admin/sso',
      },
      {
        key: 'mcp',
        // 与后端 perm.go 的 /api/mcp/ 一致
        perm: 'menu:cmdb_integrations',
        labelKey: 'nav:admin.mcp',
        icon: Bot,
        path: '/admin/mcp',
      },
      {
        key: 'audit',
        perm: 'menu:cmdb_audit',
        labelKey: 'nav:admin.audit',
        icon: FileSearch,
        path: '/admin/audit',
      },
      {
        key: 'license',
        // 授权页任何人都该看得到：过期只读时，他需要知道"为什么保存没反应"。
        // 挂 menu:cmdb_basic 的话，普通用户看到的只是一堆莫名失败的操作。
        // 页面内的**激活框**才按权限显隐。
        perm: 'menu:cmdb_overview',
        labelKey: 'nav:admin.license',
        icon: FileSearch,
        path: '/admin/license',
      },
    ],
  },
]
