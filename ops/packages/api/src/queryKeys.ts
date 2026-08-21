/**
 * TanStack Query 的 key 工厂。
 *
 * 全部 key 从这里派生，不在页面里手写字符串数组。
 * 手写的后果是失效（invalidate）时漏掉某个页面的缓存 ——
 * 用户改完数据回到列表，看到的还是旧值，而且刷新一下就好了，
 * 于是这个 bug 会被归因成"偶发"，长期查不出来。
 */

export interface ListParams {
  page?: number
  size?: number
  sort?: string
  q?: string
  [filter: string]: string | number | undefined
}

/** 稳定序列化：key 里的对象属性顺序不同会被当成两个不同的 key。 */
function stable(params: ListParams | undefined): string {
  if (!params) return ''
  return Object.keys(params)
    .filter((k) => params[k] !== undefined && params[k] !== '')
    .sort()
    .map((k) => `${k}=${params[k]}`)
    .join('&')
}

export const queryKeys = {
  hosts: {
    all: ['hosts'] as const,
    list: (params?: ListParams) => ['hosts', 'list', stable(params)] as const,
    detail: (ciId: number | string) => ['hosts', 'detail', String(ciId)] as const,
  },
  clusters: {
    all: ['clusters'] as const,
    list: (params?: ListParams) => ['clusters', 'list', stable(params)] as const,
    detail: (id: number | string) => ['clusters', 'detail', String(id)] as const,
  },
  nodes: {
    all: ['nodes'] as const,
    list: (params?: ListParams) => ['nodes', 'list', stable(params)] as const,
  },
  pods: {
    all: ['pods'] as const,
    list: (params?: ListParams) => ['pods', 'list', stable(params)] as const,
  },
  workloads: {
    all: ['workloads'] as const,
    list: (params?: ListParams) => ['workloads', 'list', stable(params)] as const,
  },
  namespaces: {
    all: ['namespaces'] as const,
    list: (params?: ListParams) => ['namespaces', 'list', stable(params)] as const,
  },
  certs: {
    all: ['certs'] as const,
    list: (params?: ListParams) => ['certs', 'list', stable(params)] as const,
  },
  subnets: {
    all: ['subnets'] as const,
    list: (params?: ListParams) => ['subnets', 'list', stable(params)] as const,
  },
  lbs: {
    all: ['lbs'] as const,
    list: (params?: ListParams) => ['lbs', 'list', stable(params)] as const,
  },
  domains: {
    all: ['domains'] as const,
    list: (params?: ListParams) => ['domains', 'list', stable(params)] as const,
  },
  services: {
    all: ['services'] as const,
    list: (params?: ListParams) => ['services', 'list', stable(params)] as const,
  },
  pvcs: {
    all: ['pvcs'] as const,
    list: (params?: ListParams) => ['pvcs', 'list', stable(params)] as const,
  },
} as const

/**
 * 失效某个资源的全部缓存。
 *
 *   queryClient.invalidateQueries({ queryKey: queryKeys.hosts.all })
 *
 * 用 all 而不是逐个 list：写操作之后哪些筛选组合受影响，
 * 调用方是算不清的，全清掉最省事也最不会错。
 */
