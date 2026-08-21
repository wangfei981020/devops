import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * 关系拓扑图。
 *
 * # ⚠️ 布局不在前端做
 *
 * 后端已经把两件最难的事做完了（见 handlers/relations_graph.go 顶部注释）：
 *
 * 1. **固定分层**：`layer` 字段直接给出画第几列（证书→域名→入口→主机→其他）。
 *    依赖关系天然有方向，力导向会把它揉成团、抹掉方向感。
 * 2. **池折叠**：生产上 657 条边里 617 条是同一事实的重复表达
 *    （15 个 LB 各挂同样的 35 台机器 = 525 条线）。后端把后端集合相同的
 *    多个 LB 折成池节点，525 → 15 条。
 *
 * 所以前端**不需要图布局库**，按 layer 分列画 SVG 就行。
 * 引一个力导向库反而会把后端刻意保留的方向感重新打乱。
 */

export interface GraphNode {
  /** 单体 = "ci:123"；池 = "pool:<hash>" */
  id: string
  ci_id?: number
  name: string
  /** host / domain / certificate / loadbalancer …，原样透传 */
  type: string
  /** 画第几列，0 最上游 */
  layer: number
  /** 折叠出来的池节点 */
  pool?: boolean
  count?: number
  members?: { ci_id: number; name: string; type: string }[]
}

export interface GraphEdge {
  src: string
  dst: string
  rel_type: string
  /** 这条线代表多少条原始边（折叠后 > 1） */
  count?: number
}

export interface GraphResp {
  nodes: GraphNode[]
  edges: GraphEdge[]
  /** ⚠️ 折叠了多少必须显示 —— 隐去规模会让人以为图就这么大 */
  stats?: { raw_edges: number; shown_edges: number; pooled_nodes: number }
  /** 空图的原因。可能只是没跑过建边任务，不是功能坏了 */
  note?: string
}

/**
 * 跳数。后端支持 1–4，默认 2。
 *
 * ⚠️ 2 跳经常不够：`host → pod → service → ingress → domain` 是 4 跳，
 *	问"这台机器挂了影响哪些域名"时 2 跳只能走到 service 就停 ——
 *	而图上不会说"还有更远的没画"，看起来就像影响面到此为止（OPSCMDB-049）。
 */
export type GraphHops = 1 | 2 | 3 | 4

export function useGraph(ciId: number | null, dir: 'forward' | 'reverse', hops: GraphHops = 2) {
  return useQuery({
    queryKey: ['relations-graph', ciId, dir, hops],
    queryFn: () => apiGet<GraphResp>(`/api/relations/graph?node=${ciId}&dir=${dir}&hops=${hops}`),
    enabled: ciId !== null && ciId > 0,
    staleTime: 60_000,
    retry: false,
  })
}

export interface GraphEntry {
  ci_id: number
  name: string
  type: string
  /**
   * 关系边数。
   *
   * 🔴 **0 = 这个资源存在，但还没有任何关系边**，不能当图的起点（点开是空图）。
   *	后端在"一条有边的都没搜到"时会回查主表并返回这类结果，
   *	好让界面说清「资源在、只是分析不了」而不是「查无此物」（OPSCMDB-077）。
   */
  degree: number
}

/**
 * 图的起点候选。
 *
 * 支持 `host:` / `lb:` / `domain:` / `cert:` 前缀限定类型 —— 这个语法
 * 要在界面上写出来，否则没人会知道。
 */
export function useEntries(q: string) {
  return useQuery({
    queryKey: ['relations-entries', q],
    queryFn: () => apiGet<GraphEntry[]>(`/api/relations/entries?q=${encodeURIComponent(q)}`),
    staleTime: 60_000,
    retry: false,
  })
}
