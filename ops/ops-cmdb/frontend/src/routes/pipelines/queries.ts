import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * KubeSphere DevOps 流水线。
 *
 * # 为什么这一页值得做
 *
 * 「构建为什么挂了」原来必须登 Jenkins / KubeSphere 控制台。
 * 而整条链后端一直是通的：
 *
 *   projects → pipeline_runs（哪次挂了 + Jenkins 构建号）→ pipeline_log（根因）
 *
 * 实测一路查到源码行号：
 *   `bet_service.go:601:44: undefined: bet.IGameBettorBeforeBalance`
 *   —— 依赖库升版后接口没了，连续三次构建失败。
 *
 * ⚠️ 只是从来没有页面调过它（OPSCMDB-023 第三档 + P0-2）。
 */

export interface DevOpsProject {
  namespace?: string
  phase?: string
  configmaps?: number
}

export interface PipelineRun {
  name?: string
  namespace?: string
  pipeline?: string
  /** Jenkins 构建号，取日志要用它 */
  run?: string
  phase?: string
  creator?: string
  start?: string
  end?: string
}

export interface PipelineRunsResult {
  ok?: boolean
  error?: string
  /** ⚠️ 已拉取批次里的总数，**不是**该命名空间的全量 —— 见 not_all_fetched */
  total?: number
  failed?: number
  count?: number
  items?: PipelineRun[]
  /** 筛出来的太多，只给了前 N 条 */
  truncated?: string
  /**
   * 🔴 **压根没拉全**。比 truncated 严重：
   * 没拉到的那部分连筛都没筛过，所以 total/failed 不是全量。
   * 不显示的话，「失败 39 次」会被读成"这个项目一共失败 39 次"，
   * 而实际是最近 100 次里的 39 次，另有 541 次没看过。
   */
  not_all_fetched?: string
}

export interface PipelineLogResult {
  ok?: boolean
  error?: string
  /** 从全文里抽出的报错行（含行号）—— 这是"为什么失败"的直接答案 */
  errors?: { line?: number; text?: string; fatal?: boolean }[]
  tail?: string
  total_lines?: number
  total_bytes?: number
}

export function useDevOpsProjects(clusterId: number) {
  return useQuery({
    queryKey: ['devops-projects', clusterId],
    enabled: clusterId > 0,
    queryFn: () =>
      apiGet<{
        ok?: boolean
        error?: string
        projects?: DevOpsProject[]
        /**
         * 🔴 项目为 0 时后端会说清**为什么**（命名空间没采到 / 这个集群确实没装 DevOps）。
         * 前端必须显示它 —— 只说「共 0 个」等于让人去选一个不存在的东西。
         */
        /** ⚠️ 优先于 hint —— hint 是后端还没迁的中文原句（OPSCMDB-054） */
        hint_key?: string
        hint?: string
      }>(`/api/devops/projects?cluster_id=${clusterId}`),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export function usePipelineRuns(clusterId: number, namespace: string, onlyFailed: boolean) {
  return useQuery({
    queryKey: ['pipeline-runs', clusterId, namespace, onlyFailed],
    enabled: clusterId > 0 && namespace !== '',
    queryFn: () =>
      apiGet<PipelineRunsResult>(
        `/api/devops/pipeline-runs?cluster_id=${clusterId}&namespace=${encodeURIComponent(namespace)}${onlyFailed ? '' : '&only_failed=0'}`,
      ),
    retry: shouldRetry,
  })
}

/**
 * ⚠️ 日志按需取：它要实时打 KubeSphere，而且可能很大
 * （后端默认只给尾部 60 行 + 抽出的报错行）。所以只在点开某一次构建时查。
 */
export function usePipelineLog(
  clusterId: number,
  namespace: string,
  pipeline: string,
  run: string,
) {
  return useQuery({
    queryKey: ['pipeline-log', clusterId, namespace, pipeline, run],
    enabled: run !== '',
    queryFn: () =>
      apiGet<PipelineLogResult>(
        `/api/devops/pipeline-log?cluster_id=${clusterId}&namespace=${encodeURIComponent(namespace)}&pipeline=${encodeURIComponent(pipeline)}&run=${encodeURIComponent(run)}`,
      ),
    retry: shouldRetry,
  })
}
