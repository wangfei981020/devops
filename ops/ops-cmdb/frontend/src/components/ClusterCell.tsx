import { clusterLabel } from '../lib/clusterLabel.js'

/**
 * 表格里显示集群名的**唯一**实现。
 *
 * 🔴 技术名必须可见：它才是 kubectl / PromQL 标签 / 提工单时用的标识。
 *	此前后端把 `COALESCE(display_name, name)` 拍进一个 `cluster_name` 字段，
 *	前端拿到「开发环境集群」后再也拿不到 `dev-k8s-cluster-01`，
 *	整页上找不到二者的对应关系（OPSCMDB-078）。
 *
 * ⚠️ 列窄，不能直接铺 `别名（技术名）`。用「主名 + 副行」排版：
 *	主行别名、副行技术名；两者相同时不重复渲染（不要出现「x / x」）。
 *	title 给全站统一口径的完整串，便于复制。
 *
 * ⚠️ 不要在各页各写一份 —— 集群名散开这件事已经按页面修过三轮
 *	（OPSCMDB-031 NEW-3/5 → OPSCMDB-053 → 本条），每次都是"又漏了两处"。
 */
export function ClusterCell({ name, display }: { name: string; display?: string }) {
  const tech = (name ?? '').trim()
  const alias = (display ?? '').trim()
  return (
    <div className="flex min-w-0 flex-col" title={clusterLabel(alias, tech)}>
      <span className="truncate text-[13px]">{alias !== '' ? alias : tech}</span>
      {alias !== '' && alias !== tech ? (
        <span className="truncate text-[11px] text-muted-foreground">{tech}</span>
      ) : null}
    </div>
  )
}
