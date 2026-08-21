import { toErrorInfo } from '@ops/api'
import { useTranslation } from '@ops/i18n'
import { Banner, Button, Dialog, Skeleton } from '@ops/ui'
import { useQuery } from '@tanstack/react-query'
import { Check, Copy } from 'lucide-react'
import { useState } from 'react'
import { getToken } from '../lib/auth.js'

/**
 * 看某个 K8s 对象的完整 YAML。
 *
 * # 为什么必须有这个入口
 *
 * CMDB 采集落的是**字段**（镜像、副本数、状态……），而排障时要看的是**原文** ——
 * 哪个 initContainer 用了 root、亲和性怎么写的、annotation 上挂了什么。
 * 没有这一层，人查到一半还是得回命令行 `kubectl get -o yaml`，
 * 那 CMDB「不登录服务器排障」的立项目标就断在最后一步。
 *
 * 后端 `/api/k8s/manifest` 一直都在（OPSCMDB-023 第一档），前端没接。
 *
 * # ⚠️ 三件必须照实说的事
 *
 * 1. **返回的是纯文本不是 JSON**：后端直接 `c.Data(text/plain)`，
 *    用 `apiGet` 解析会当场抛「不是合法 JSON」。这里走裸 fetch。
 * 2. **脱敏是有损的**：后端把敏感值换成 `***REDACTED***` 并在头部写明脱敏了几处。
 *    那行注释**不能省** —— 否则人会把 `***REDACTED***` 当成配置里真实写着的值。
 * 3. **这是发数据的接口**，后端记了审计（`view_manifest:<kind>`）。
 *    别加自动预取或轮询：一次点击 = 一条审计，预取会把审计灌成噪音。
 */
export function ManifestDialog({
  clusterId,
  kind,
  namespace,
  name,
  onClose,
}: {
  clusterId: number
  /** pod / deployment / service …… 后端容忍复数写法 */
  kind: string
  /** 集群级资源（node、pv、namespace）不传 */
  namespace?: string
  name: string
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)

  const q = useQuery({
    queryKey: ['manifest', clusterId, kind, namespace ?? '', name],
    // ⚠️ 只在弹窗打开时请求一次，不重取：每次请求都会写一条审计
    staleTime: Number.POSITIVE_INFINITY,
    refetchOnWindowFocus: false,
    retry: false,
    queryFn: async () => {
      const qs = new URLSearchParams({
        cluster_id: String(clusterId),
        kind,
        name,
        ...(namespace ? { namespace } : {}),
      })
      const res = await fetch(`/api/k8s/manifest?${qs}`, {
        headers: { Authorization: `Bearer ${getToken()}` },
      })
      // 失败时后端返的是 JSON（{error}），成功时是 text/plain。
      // 不能一律按文本读 —— 那样错误信息会变成一坨看不懂的 YAML 似的东西。
      if (!res.ok) {
        const j = (await res.json().catch(() => ({}))) as { error?: string; supported?: string[] }
        throw new Error(
          j.error
            ? j.supported
              ? `${j.error}（支持：${j.supported.join(', ')}）`
              : j.error
            : `HTTP ${res.status}`,
        )
      }
      return res.text()
    },
  })

  const yaml = q.data ?? ''
  // 后端把「脱敏了几处」写在头部注释里。把它拎出来单独强调 ——
  // 混在 YAML 第一行里会被当成普通注释划过去
  const redactedNote = yaml
    .split('\n')
    .find((l) => l.startsWith('#') && l.includes('脱敏'))

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('manifest:title')}
      description={`${kind} ${namespace ? `${namespace}/` : ''}${name}`}
      closeLabel={t('common:action.close')}
      width={920}
      footer={
        <>
          <Button
            size="sm"
            disabled={!yaml}
            onClick={() => {
              void navigator.clipboard?.writeText(yaml).then(() => {
                setCopied(true)
                setTimeout(() => setCopied(false), 1500)
              })
            }}
          >
            {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
            {t(copied ? 'manifest:copied' : 'manifest:copy')}
          </Button>
          <Button variant="primary" size="sm" onClick={onClose}>
            {t('common:action.close')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        {q.isPending ? <Skeleton className="h-5 w-[40%]" /> : null}

        {q.isError ? (
          <Banner tone="bad">
            <span className="font-medium">{t('manifest:failed')}</span>
            <span className="mt-0.5 block break-all">
              {q.error instanceof Error ? q.error.message : toErrorInfo(q.error).detail}
            </span>
          </Banner>
        ) : null}

        {/* ⚠️ 脱敏提示单独摆出来。少了这一句，`***REDACTED***` 会被当成真实配置值 */}
        {redactedNote ? (
          <Banner tone="warn">
            <span>{redactedNote.replace(/^#\s*/, '')}</span>
          </Banner>
        ) : null}

        {yaml ? (
          <pre className="max-h-[60vh] overflow-auto rounded-[var(--radius)] border border-border bg-secondary/40 p-3 font-mono text-[11px] leading-relaxed text-foreground">
            {yaml}
          </pre>
        ) : null}
      </div>
    </Dialog>
  )
}
