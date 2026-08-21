import { ApiError, queryKeys, shouldRetry } from '@ops/api'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { api } from '../../lib/api.js'

/**
 * 主机数据。接的是真实后端 `GET /api/hosts`。
 *
 * 后端字段是 snake_case 且带一堆云厂商专有项，前端只取需要的那些并转成 camelCase。
 * 映射集中在本文件的 `toHost` —— 页面和列定义看不到后端结构，
 * 后端改字段时只有这一处要动。
 */

/**
 * 主机状态。
 *
 * ⚠️ `notReady` / `diskPressure` 目前**后端没有数据源**：
 * hosts 表存的是云厂商视角的实例状态，K8s 节点健康要另外从 apiserver 采。
 * 类型保留是因为界面已经能正确渲染它们，等节点采集接上就有值 ——
 * 在那之前，映射函数不会产生这两个值。
 */
export type HostStatus = 'running' | 'stopped' | 'destroyed' | 'notReady' | 'diskPressure'

/**
 * 一块磁盘。
 *
 * 机器普遍不止一块盘（系统盘 + 若干数据盘），把它们加总成一个数字会丢掉
 * 排障最需要的信息：满的是哪一块。「磁盘 85%」没法行动，
 * 「数据盘 pd-ssd 500G 用了 96%」才能。
 */
export interface Disk {
  /** 设备名或云上的磁盘名 */
  name: string
  sizeGb: number
  /** 云盘类型（pd-ssd / pd-balanced / local-ssd…），自建机可能为 null */
  type: string | null
  role: 'boot' | 'data'
  /** 用量百分比。null = 没采到，与 0（空盘）含义不同 */
  usedPercent: number | null
  /** 挂载点。排障时比云盘名有用 —— 人关心的是 /var/lib/kafka 满了 */
  mountPoint: string
}

export interface Host {
  id: string
  name: string
  status: HostStatus
  privateIp: string
  /**
   * 外网 IP。空串 = 确认没有公网入口（不是没采到）。
   * 排暴露面时这是第一个要问的问题 —— 此前后端一直在返回，前端一个字段都没声明。
   */
  publicIp: string
  /**
   * 所属 K8s 集群名。
   * ⚠️ 空串对非 K8s 节点是**正确**的值，不是"没采到"。
   * 🔴 此前这个字段装的是 GCP **项目**（`cluster: raw.project`），
   *	于是「集群」列显示的是项目名 —— 不报错、不为空，只是答非所问（OPSCMDB-037）。
   */
  cluster: string
  /** 云项目 ID 与显示名。和集群是**两个维度**，不能互相冒充 */
  project: string
  projectName: string
  /** 云厂商（gcp / aliyun …）与云账号名：跨账号排障时"这台归谁管" */
  provider: string
  accountName: string
  region: string
  nodePool: string | null
  /** 机型代号只是规格的**别名**，不能替代规格本身 */
  machineType: string
  vcpu: number | null
  memoryGb: number | null
  /** null = 没采到；[] = 确认没有盘（已销毁，盘跟着删了） */
  disks: Disk[] | null
  monthlyCost: number | null
  /**
   * 日均成本与累计成本。
   * ⚠️ 与 monthlyCost 一样，0 视为「没算出来」——云主机不可能免费。
   * costTotal 是做成本复盘时唯一能回答"这台机器到今天一共花了多少"的字段。
   */
  dailyCost: number | null
  costTotal: number | null
  /** estimate / bigquery —— 估算值和账单实数不能混为一谈 */
  costSource: string
  /** 台账是**快照**不是实时，必须让用户看到数据有多旧 */
  lastSyncAt: string | null
  zone: string
  os: string
  preemptible: boolean
  labels: Record<string, string>
}

/**
 * 状态筛选值。
 *
 * 与后端 swag 注解里的 enum 一一对应 —— 生成的 TS 类型就是这个联合类型，
 * 传一个不在其中的字符串**编译期就报错**。
 * 这比运行时被后端忽略（筛选静默失效、界面看起来筛了）强得多。
 */
export type StatusFilter = 'all' | 'running' | 'stopped' | 'destroyed'

/** 可排序字段。同样与后端 enum 对齐 —— 排错字段会静默失效（后端退回默认排序）。 */
export type SortField =
  | 'name'
  | 'project'
  | 'status'
  | 'vcpu'
  | 'mem'
  | 'disk'
  | 'cost'
  | 'created'
export type SortParam = SortField | `-${SortField}`

export interface HostListParams {
  page: number
  size: number
  /** `-cost` 表示降序 */
  sort?: SortParam
  q?: string
  status?: StatusFilter
  project?: string
  /** 云厂商筛选。后端 facets 一直算着这一维，前端此前没接（OPSCMDB-028 GAP-9） */
  provider?: string
  /** queryKeys 的 ListParams 要求可索引 */
  [k: string]: string | number | undefined
}

export interface HostListResult {
  items: Host[]
  total: number
  /** 各维度的**全量**计数，驱动筛选下拉里的数字 */
  facets: Record<string, Record<string, number>>
  /**
   * "多久没更新就算旧了"，由后端按 host_sync 任务自己的 cron 算出来。
   *
   * ⚠️ known=false 时**不能**下"数据是旧的"这个结论 ——
   * 取不到任务周期和"数据很新"是两件事（OPSCMDB-031 P1-5）。
   */
  freshness: { staleAfterSeconds: number; known: boolean } | null
}

/** 后端返回的原始形状，只声明用得到的字段。 */
interface RawDisk {
  name?: string
  size_gb?: number
  type?: string
  is_boot?: boolean
  used_percent?: number
  mount_point?: string
}
interface RawHost {
  ci_id?: number
  name?: string
  status?: string
  lifecycle?: string
  internal_ip?: string
  project?: string
  k8s_pool?: string
  machine_type?: string
  vcpu?: number
  mem_mb?: number
  disks?: RawDisk[] | null
  cost_month?: number
  cost_daily?: number
  cost_total?: number
  cost_source?: string
  external_ip?: string
  project_name?: string
  provider?: string
  account_name?: string
  region?: string
  cluster_name?: string
  synced_at?: string
  zone?: string
  os?: string
  preemptible?: boolean
  labels?: Record<string, string>
}

/**
 * 云厂商的实例状态 → 界面上的状态。
 *
 * `lifecycle` 优先于 `status`：已销毁的机器 status 停在最后一次观测到的 RUNNING 上
 * （后端刻意不覆写真值，留作证据），只看 status 会得到
 * 「已删除但显示运行中」的自相矛盾。
 */
function toStatus(raw: RawHost): HostStatus {
  if (raw.lifecycle === 'gone') return 'destroyed'
  return raw.status?.toUpperCase() === 'RUNNING' ? 'running' : 'stopped'
}

function toDisk(d: RawDisk): Disk {
  return {
    name: d.name ?? '',
    sizeGb: d.size_gb ?? 0,
    type: d.type ? d.type : null,
    role: d.is_boot ? 'boot' : 'data',
    // 后端对 nil 用了 omitempty，字段缺失就是「没采到」。
    // ⚠️ 绝不能写 `?? 0` —— 那会把「没采到」变成「空盘」。
    usedPercent: d.used_percent === undefined ? null : d.used_percent,
    mountPoint: d.mount_point ?? '',
  }
}

function toHost(raw: RawHost): Host {
  return {
    id: String(raw.ci_id ?? ''),
    name: raw.name ?? '',
    status: toStatus(raw),
    privateIp: raw.internal_ip ?? '',
    publicIp: raw.external_ip ?? '',
    // 🔴 集群名取 cluster_name，**不是** project。
    //	原来写的是 `raw.project`，于是「集群」列整列显示 GCP 项目名。
    //	项目和集群是两个维度：一个项目里可以有多个集群。
    cluster: raw.cluster_name ?? '',
    project: raw.project ?? '',
    projectName: raw.project_name ?? '',
    provider: raw.provider ?? '',
    accountName: raw.account_name ?? '',
    region: raw.region ?? '',
    // 空串是后端 COALESCE 的结果，语义上就是「没有节点池」，转成 null
    nodePool: raw.k8s_pool ? raw.k8s_pool : null,
    machineType: raw.machine_type ?? '',
    // 0 在这里只可能是「没采到」——不存在 0 核的机器
    vcpu: raw.vcpu ? raw.vcpu : null,
    memoryGb: raw.mem_mb ? Math.round(raw.mem_mb / 1024) : null,
    disks: raw.disks == null ? null : raw.disks.map(toDisk),
    // 同理，云主机不可能免费，成本 0 就是没算出来
    monthlyCost: raw.cost_month ? raw.cost_month : null,
    dailyCost: raw.cost_daily ? raw.cost_daily : null,
    costTotal: raw.cost_total ? raw.cost_total : null,
    costSource: raw.cost_source ?? '',
    lastSyncAt: raw.synced_at ?? null,
    zone: raw.zone ?? '',
    os: raw.os ?? '',
    preemptible: raw.preemptible ?? false,
    labels: raw.labels ?? {},
  }
}

/**
 * 调试用的强制状态。
 *
 * `?state=error` / `?state=empty` 直接看那两态的真实渲染。
 * 错误态和空态在正常环境里很难复现，没有这个开关就只能等它在生产上第一次出现。
 */
function forcedState(): 'error' | 'empty' | null {
  const v = new URLSearchParams(window.location.search).get('state')
  return v === 'error' || v === 'empty' ? v : null
}

async function fetchHosts(p: HostListParams): Promise<HostListResult> {
  if (forcedState() === 'error') {
    throw new ApiError({
      kind: 'transient',
      code: 'upstream_timeout',
      messageKey: 'error.upstreamTimeout',
      detail: 'GET /api/hosts → 504 · req_id 8f2a91c3',
      retryable: true,
    })
  }

  const { data, error } = await api.GET('/hosts', {
    params: {
      query: {
        page: p.page,
        size: p.size,
        ...(p.sort ? { sort: p.sort } : {}),
        ...(p.q ? { q: p.q } : {}),
        // 'all' 是前端下拉的默认值，表示不筛选，不能当成一个真实取值发给后端
        ...(p.status && p.status !== 'all' ? { status: p.status } : {}),
        ...(p.project && p.project !== 'all' ? { project: p.project } : {}),
        // 后端 facets 一直在算 provider 这一维、Filters 也认它，
        // 只是前端从来没发过 —— 算了没人用（OPSCMDB-028 GAP-9）
        ...(p.provider && p.provider !== 'all' ? { provider: p.provider } : {}),
      },
    },
  })
  if (error) throw error

  const empty = forcedState() === 'empty'
  return {
    items: empty ? [] : ((data?.items ?? []) as RawHost[]).map(toHost),
    total: empty ? 0 : (data?.total ?? 0),
    facets: (data?.facets ?? {}) as Record<string, Record<string, number>>,
    freshness: toFreshness(
      (data as { freshness?: { stale_after_seconds?: number; known?: boolean } } | undefined)
        ?.freshness,
    ),
  }
}

function toFreshness(
  raw: { stale_after_seconds?: number; known?: boolean } | undefined,
): HostListResult['freshness'] {
  // 后端没给这一段（老版本后端）→ null。不能兜成一个假的阈值，
  // 那会让新旧后端搭配时安静地按错误判据报警或不报警
  if (!raw || typeof raw.stale_after_seconds !== 'number') return null
  return { staleAfterSeconds: raw.stale_after_seconds, known: raw.known === true }
}

export function useHosts(params: HostListParams) {
  return useQuery({
    queryKey: queryKeys.hosts.list(params),
    queryFn: () => fetchHosts(params),
    // 翻页时保留上一页数据，避免整张表闪成骨架屏再闪回来 ——
    // 没有它，每次翻页都像页面崩了一下重建
    placeholderData: keepPreviousData,
    // 排障场景下陈旧数据比"转圈"更危险：看到的是 5 分钟前的状态却以为是当下的
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

/**
 * 这台机器和 k8s 节点的关系。
 *
 * ⚠️ 三态，不能压成 `pods.length > 0`：
 *   linked        已关联，pods 是真实清单（空数组 = 节点上真的没 Pod）
 *   not_ingested  主机侧标了是节点，但集群没接进采集 —— 这是**缺口**
 *   none          不是集群节点，本来就该没有 Pod
 * 后两者都会渲染成"没有 Pod"，但一个要提示去接集群，一个什么都不用做。
 */
export type NodeLink = 'linked' | 'not_ingested' | 'none'

export interface NodeInfo {
  clusterId: number
  clusterName: string
  name: string
  pool: string
  readyStatus: string
  /** 节点自报的 Pod 数，和 pods.length 对不上说明两张表同步轮次不同 */
  podCount: number
}

export interface PodOnNode {
  namespace: string
  name: string
  workload: string
  phase: string
  restarts: number
}

export interface HostDetail {
  host: Host
  disks: Disk[]
  relatedDomains: { fqdn: string; ip: string }[]
  costHourly: number
  nodeLink: NodeLink
  node: NodeInfo | null
  pods: PodOnNode[]
}

interface RawNode {
  cluster_id?: number
  cluster_name?: string
  name?: string
  pool?: string
  ready_status?: string
  pod_count?: number
}

function toNode(n: RawNode): NodeInfo {
  return {
    clusterId: n.cluster_id ?? 0,
    clusterName: n.cluster_name ?? '',
    name: n.name ?? '',
    pool: n.pool ?? '',
    readyStatus: n.ready_status ?? '',
    podCount: n.pod_count ?? 0,
  }
}

/**
 * 主机详情。
 *
 * 独立请求而不是复用列表里那条数据：列表为了轻量只取了部分字段，
 * 详情要的标签、关联域名、成本明细都不在里面。
 * 拿列表数据凑合的话，抽屉里会有一半字段永远是空的。
 */
export function useHostDetail(ciId: string | null) {
  return useQuery({
    queryKey: queryKeys.hosts.detail(ciId ?? ''),
    enabled: ciId !== null,
    queryFn: async (): Promise<HostDetail> => {
      const { data, error } = await api.GET('/hosts/{ciid}', {
        params: { path: { ciid: Number(ciId) } },
      })
      if (error) throw error
      return {
        host: toHost((data?.host ?? {}) as RawHost),
        disks: ((data?.disks ?? []) as RawDisk[]).map(toDisk),
        relatedDomains: (data?.related_domains ?? []) as { fqdn: string; ip: string }[],
        costHourly: data?.cost_hourly ?? 0,
        // 后端没给 node_link 时按 'none' 兜底会说谎（把"没数据"说成"不是节点"）。
        // 但这里只能兜底一个值 —— 选 not_ingested：它的界面表述是
        // "没接进来"，恰好也覆盖"后端版本太老没这个字段"这种情况。
        nodeLink: (data?.node_link ?? 'not_ingested') as NodeLink,
        node: data?.node ? toNode(data.node as RawNode) : null,
        pods: (data?.pods ?? []) as PodOnNode[],
      }
    },
    staleTime: 30_000,
    retry: shouldRetry,
  })
}
