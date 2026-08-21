import { shouldRetry, toErrorInfo } from '@ops/api'
import { useTranslation } from '@ops/i18n'
import { Badge, Banner, Dialog, Select, Skeleton } from '@ops/ui'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { apiAction, apiGet } from '../../lib/fetchJson.js'
import { useSaveCluster } from './queries.js'

const PERM = 'cmdb:manage_clusters'

interface DiscoveredCluster {
  name: string
  location: string
  version: string
  node_count: number
  status: string
  /** apiserver 地址。纳管时必须一起存下来，否则连不上 */
  endpoint: string
  /** 集群 CA（base64 PEM）。同上 */
  ca: string
  /** 决定这个集群按官网表的哪一列自动升级。空 = 未入通道 */
  release_channel: string
}

interface CloudProject {
  account_id: number
  project_id: string
  account_name: string
  /** 没配 SA key 的项目扫不出任何东西，下拉里要能看出来 */
  has_cred: boolean
}

/**
 * 云账号下的项目。
 *
 * ⚠️ 没有 `/api/cloud-accounts/projects` 这个端点 —— 项目是**嵌在账号里**返回的，
 * 要自己拍平。猜一个端点名的话会得到 404，而 404 在这个弹窗里
 * 会表现成"一个项目都没有"。
 */
function useCloudProjects() {
  return useQuery({
    // 🔴 键必须和云账号页区分开。
    //
    //	两处都用过 ['cloud-accounts']，但**返回的形状完全不同**：
    //	  云账号页        → { items: CloudAccount[] }   （对象）
    //	  这里            → CloudProject[]              （拍平后的数组）
    //
    //	React Query 按键缓存**数据**，queryFn 只在没缓存时跑。
    //	于是谁先跑谁定形状，另一个组件读到的就是错的类型 ——
    //	这里会拿到一个对象，`list.find(...)` 直接抛
    //	「list.find is not a function」，整个弹窗白屏。
    //
    // ⚠️ 触发条件很隐蔽：在云账号页新增一个项目会 invalidate 这个键，
    //	之后切到集群页打开「从云账号发现」就崩 —— 而刷新一下页面又好了
    //	（因为刷新后是这个组件先跑）。表现成"偶发"，实际是必然的。
    queryKey: ['cloud-accounts', 'flattened-projects'],
    queryFn: async () => {
      const accounts = await apiGet<
        { id: number; name: string; projects?: { project_id: string; has_cred: boolean }[] }[]
      >('/api/cloud-accounts')
      return accounts.flatMap((a) =>
        (a.projects ?? []).map(
          (p): CloudProject => ({
            account_id: a.id,
            project_id: p.project_id,
            account_name: a.name,
            has_cred: p.has_cred,
          }),
        ),
      )
    },
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

/**
 * 从云账号里发现 GKE 集群。
 *
 * ⚠️ 后端在出错时返回的是 **HTTP 200 + `{ok:false, error}`**（不是 4xx），
 * 所以这里必须用 apiAction —— 只看状态码会把"该项目未配 SA key"当成成功，
 * 然后渲染出一个空列表，看起来就是"这个项目下没有集群"。
 * 这个坑在集群连通性测试上已经踩过一次。
 */
export function DiscoverDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const projects = useCloudProjects()
  const save = useSaveCluster()
  const [picked, setPicked] = useState('')
  const [adopted, setAdopted] = useState<string[]>([])

  const list = projects.data ?? []
  const current = list.find((p) => `${p.account_id}:${p.project_id}` === picked)

  const discover = useMutation({
    mutationFn: () =>
      apiAction<{ ok?: boolean; error?: string; clusters?: DiscoveredCluster[] }>(
        '/api/k8s/clusters/discover',
        'POST',
        { cloud_account_id: current?.account_id, project_id: current?.project_id },
      ),
  })

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('clusters:discover.title')}
      description={t('clusters:discover.desc')}
      closeLabel={t('common:action.close')}
      width={720}
      footer={
        <>
          <WriteButton perm={PERM} size="sm" onClick={onClose}>
            {t('common:action.close')}
          </WriteButton>
          <WriteButton
            perm={PERM}
            variant="primary"
            size="sm"
            loading={discover.isPending}
            blockedReason={current ? undefined : t('clusters:discover.pickProject')}
            onClick={() => discover.mutate()}
          >
            {t('clusters:discover.scan')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        {projects.isPending ? (
          <Skeleton className="h-5 w-[40%]" />
        ) : list.length === 0 ? (
          <Banner tone="warn">
            <span className="font-medium">{t('clusters:discover.noProject')}</span>
            <span className="mt-0.5 block">{t('clusters:discover.noProjectHint')}</span>
          </Banner>
        ) : (
          <Select<string>
            label={t('clusters:discover.project')}
            value={picked}
            onChange={setPicked}
            options={list.map((p) => ({
              value: `${p.account_id}:${p.project_id}`,
              // 没配凭据的项目扫不出东西，在选之前就标出来
              label: `${p.project_id} · ${p.account_name}${p.has_cred ? '' : ` (${t('clusters:discover.noCred')})`}`,
            }))}
            className="self-start"
          />
        )}

        {discover.isError ? (
          <Banner tone="bad">
            <span className="font-medium">{t('clusters:discover.failed')}</span>
            {/* 后端的原话最有用：多半是"该云账号项目未配 SA key" */}
            <span className="mt-0.5 block">{toErrorInfo(discover.error).detail}</span>
          </Banner>
        ) : null}

        {discover.isSuccess ? (
          (discover.data.clusters ?? []).length === 0 ? (
            <Banner tone="info">
              <span>{t('clusters:discover.none')}</span>
            </Banner>
          ) : (
            <div className="flex flex-col rounded-[var(--radius)] border border-border">
              {(discover.data.clusters ?? []).map((c) => (
                <div
                  key={`${c.name}-${c.location}`}
                  className="flex items-center gap-3 border-b border-border px-3 py-2 text-[13px] last:border-0"
                >
                  <span className="min-w-0 flex-1 truncate font-medium">{c.name}</span>
                  <span className="text-xs text-muted-foreground">{c.location}</span>
                  <code className="font-mono text-[11px]">{c.version}</code>
                  {/* 空通道要显式说出来：它决定自动升级按哪一列排期，
                      留白会让人以为"没有自动升级" */}
                  <Badge tone={c.release_channel ? 'info' : 'warn'}>
                    {c.release_channel || t('clusters:discover.noChannel')}
                  </Badge>
                  <span className="tabular text-xs text-muted-foreground">
                    {t('clusters:discover.nodes', { count: c.node_count })}
                  </span>
                  {adopted.includes(c.name) ? (
                    <Badge tone="ok">{t('clusters:discover.adopted')}</Badge>
                  ) : (
                    <WriteButton
                      perm={PERM}
                      size="sm"
                      onClick={() =>
                        save.mutate(
                          {
                            name: c.name,
                            display_name: c.name,
                            environment: '',
                            provider: 'gke',
                            location: c.location,
                            // GKE 不需要 kubeconfig：用发现它的那个云账号的 SA key 就能连。
                            //
                            // ⚠️ 这四个字段必须一起带上 —— 后端判连接方式看的是
                            // `provider=="gke" && cloud_account_id>0 && endpoint!="" && ca_data!=""`，
                            // 缺一个就退回"未配置连接方式"，用户被迫再手配一次 kubeconfig，
                            // 而这些信息在发现那一刻**本来就已经拿到了**。
                            cloud_account_id: current?.account_id,
                            project_id: current?.project_id,
                            endpoint: c.endpoint,
                            ca_data: c.ca,
                            // 留空 = 不配 kubeconfig。手填 kubeconfig 是**覆盖**用的，
                            // 不是必填项
                            kubeconfig: '',
                            prom_cluster_value: '',
                            enabled: 1,
                          },
                          { onSuccess: () => setAdopted((a) => [...a, c.name]) },
                        )
                      }
                    >
                      {t('clusters:discover.adopt')}
                    </WriteButton>
                  )}
                </div>
              ))}
            </div>
          )
        ) : null}

        {adopted.length > 0 ? (
          <Banner tone="warn">
            <span className="font-medium">{t('clusters:discover.afterAdoptTitle')}</span>
            <span className="mt-0.5 block">{t('clusters:discover.afterAdoptBody')}</span>
          </Banner>
        ) : null}
      </div>
    </Dialog>
  )
}
