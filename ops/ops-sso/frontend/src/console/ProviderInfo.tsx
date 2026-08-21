import { useTranslation } from '@ops/i18n'
import { useQuery } from '@tanstack/react-query'
import { Check, Copy } from 'lucide-react'
import { useState } from 'react'
import { api } from '../api/client.js'

interface Info {
  issuer: string
  discovery_url: string
  jwks_url: string
  is_local: boolean
  run_mode: string
}

/**
 * 接入信息：接入方要填的那几行。
 *
 * # 为什么值得占一块地方
 *
 * 「issuer 填什么」是每个接入方问的第一个问题，而唯一可信的答案是
 * **这个进程里实际生效的值** —— 不是部署手册（可能过期），
 * 不是某份 values.yaml（可能不是这套环境的）。让人去猜，
 * 最常见的结果是填了个末尾多一个斜杠的版本，然后在下游得到
 * 一句「iss 不匹配」，两边各查半天。
 *
 * 只读。issuer 是部署期配置（环境变量 OAP_ISSUER），不在界面上改：
 * 改它等于让所有已接入的下游同时验签失败。
 */
export function ProviderInfo() {
  const { t } = useTranslation()
  const q = useQuery({
    queryKey: ['provider-info'],
    queryFn: () => api.get<Info>('/provider-info'),
  })

  // 取不到就整块不显示：它是辅助信息，不该在应用列表上方杵一个错误框。
  // 真需要时点重试也没有意义 —— 这个值来自进程启动参数，不会自己变好。
  if (!q.data) return null
  const d = q.data

  return (
    <section className="mb-5 rounded-[var(--radius-md)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold">{t('sso:provider.title')}</h2>
      <p className="mt-1 mb-3 max-w-[80ch] text-[12px] text-muted-foreground">
        {t('sso:provider.desc')}
      </p>

      {d.is_local ? (
        // 本机地址能跑通本地联调，但任何真实下游从**它自己的容器**里
        // 拉这个地址都会失败 —— 而报错出现在下游，没人会怀疑到这里。
        <p className="mb-3 rounded-[var(--radius)] border border-warning bg-warning-bg px-3 py-2 text-[12px]">
          {t('sso:provider.isLocal')}
        </p>
      ) : null}

      <dl className="grid gap-2">
        <Row label={t('sso:provider.issuer')} value={d.issuer} />
        <Row label={t('sso:provider.discovery')} value={d.discovery_url} />
        <Row label={t('sso:provider.jwks')} value={d.jwks_url} />
      </dl>

      <p className="mt-3 text-[11px] leading-relaxed text-muted-foreground">
        {d.run_mode === 'prod' ? t('sso:provider.prodChecked') : t('sso:provider.devNotChecked')}
      </p>
    </section>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // 剪贴板在非 https 下会被浏览器拒掉。不弹错 —— 值本身就在屏幕上，
      // 手工选中复制即可；为一个可有可无的便利功能弹一个错误框更烦人。
    }
  }

  return (
    <div className="flex items-center gap-2">
      <dt className="w-28 shrink-0 text-[12px] text-muted-foreground">{label}</dt>
      <dd className="min-w-0 flex-1">
        {/* 必须能整串看见：截断的地址复制出来是错的，
            而"看起来对"的错地址比明显的错更难查 */}
        <code className="block overflow-x-auto rounded-[var(--radius-sm)] bg-muted px-2 py-1 font-mono text-[12px] whitespace-nowrap">
          {value}
        </code>
      </dd>
      <button
        type="button"
        onClick={() => void copy()}
        title={t('sso:provider.copy')}
        className="shrink-0 cursor-pointer rounded-[var(--radius)] border border-border p-1.5 text-muted-foreground hover:bg-secondary"
      >
        {copied ? <Check className="size-3.5 text-success" /> : <Copy className="size-3.5" />}
      </button>
    </div>
  )
}
