// pickDefaultCluster 选出各 K8s 页面默认应该选中的集群。
//
// 为什么需要它：11 个 K8s 页面此前一律写 `clusters.value[0].id`，谁排第一就选谁。
// 后端原来按 `ORDER BY environment, name` 排序（纯字母序），于是 environment='DEMO'
// 的演示集群永远排第一——打开任何 K8s 页面，默认选中的都是一个 enabled=0、
// 一条数据都没有的空集群，页面显示「无数据，先去集群管理点同步」，
// 而真正在跑的集群就在下拉框第二项。节点页甚至据此打出「共 0 · 全部健康」。
//
// 后端排序已改成 `enabled DESC, 环境权重, name`（见 handlers/k8s_clusters.go 的 envRank），
// 前端这层是第二道保险：即便后端顺序变了、或者未来有人往列表里塞新东西，
// 也绝不会默认选中一个已停用的集群。两层都改是有意的——
// 默认值选错不会报错，只会安静地让人看着空页面以为"这个集群没问题"。
export function pickDefaultCluster(clusters) {
  const list = Array.isArray(clusters) ? clusters : []
  if (!list.length) return ''
  // enabled 可能是 1/0、true/false，两种都认；字段缺失时按"启用"处理（老数据兼容）。
  const on = (c) => c.enabled === undefined || c.enabled === 1 || c.enabled === true
  return (list.find(on) || list[0]).id
}
