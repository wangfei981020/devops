import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, Badge, EmptyState, Skeleton, fromQuery } from '@ops/ui'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { WriteButton } from '../components/WriteButton.js'
import { get, makeLoadError } from '../lib/api.js'
import { KIND_KEYS } from './Rules.js'
import { RuleFromTemplateDialog } from './RuleFromTemplate.js'

/**
 * 场景模板库。
 *
 * 建规则的入口有两条路，这一页是**默认那条**：
 *   场景模板 —— 回答几个业务问题，查询语句由平台生成
 *   手写规则 —— 直接写 LogQL，给知道自己要什么的人
 *
 * ⚠️ 每个模板都要说清「什么时候用它」和「它会生成什么」。
 * 只列一个名字的模板库等于让人挨个点开试 —— 而试错成本是一条建错的规则。
 */

interface TplField {
  key: string
  type: 'text' | 'tags' | 'number'
  required?: boolean
  help?: string
  example?: string
}

// ⚠️ 没有 name/when/label：文案全部按 key 从语言包取（后端只回结构）。
// 后端回文案的话，英文界面下这一页会整片中文。
interface Template {
  key: string
  kind: string
  fields: TplField[]
}

interface Rule {
  id: number
  name: string
  template?: string
}

// 场景 → i18n key。**复用规则页那一份**（KIND_KEYS），不要在这里再发明一套：
// 我第一版凭空写了 opsalert:rules.kindSpike 之类的 key，语言包里根本没有 ——
// i18next 取不到 key 时会把 key 本身当文案渲染，界面上就出现一个写着
// "opsalert:rules.kindSpike" 的徽章。是 check-i18n-usage 拦下来的。

export function TemplatesPage() {
  const { t } = useTranslation()
  const [picked, setPicked] = useState<string | null>(null)

  const templates = useQuery({
    queryKey: ['rule-templates'],
    queryFn: () => get<{ items: Template[] }>('/rules/templates'),
  })
  // 每个模板建过几条规则。没有这个数字，模板库只是一份说明书，
  // 看不出哪些模板真的在用、哪些从来没人点过
  const rules = useQuery({
    queryKey: ['rules'],
    queryFn: () => get<{ items: Rule[] }>('/rules'),
  })

  const usedBy = (key: string) => (rules.data?.items ?? []).filter((r) => r.template === key)

  return (
    <div className="flex flex-col gap-3">
      <RuleFromTemplateDialog
        open={picked !== null}
        initialTemplate={picked}
        onClose={() => {
          setPicked(null)
          void rules.refetch()
        }}
      />

      <p className="text-xs text-muted-foreground">{t('opsalert:tplPage.hint')}</p>

      <AsyncBoundary
        state={fromQuery(templates, (d) => d.items.length === 0, makeLoadError(t))}
        pending={<Skeleton className="h-64 w-full" />}
        empty={
          <EmptyState
            title={t('opsalert:tplPage.emptyTitle')}
            reason={t('opsalert:tplPage.emptyReason')}
            action={{ label: t('action.retry'), onClick: () => void templates.refetch() }}
          />
        }
        // 四态必须齐：模板取不到时要能看出是"加载失败"而不是"平台没有模板"
        errorTitle={t('opsalert:tplPage.loadError')}
        retryLabel={t('action.retry')}
        onRetry={() => templates.refetch()}
      >
        {(d) => (
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {d.items.map((tpl) => {
              const used = usedBy(tpl.key)
              return (
                <section
                  key={tpl.key}
                  className="flex flex-col gap-2 rounded-lg border border-border bg-card p-4"
                >
                  <div className="flex items-start gap-2">
                    <h3 className="flex-1 text-sm font-semibold">{t(`opsalert:tplDef.${tpl.key}.name`)}</h3>
                    <Badge tone="info">{t(`opsalert:rules.kind.${KIND_KEYS[tpl.kind] ?? ''}`, tpl.kind)}</Badge>
                  </div>

                  <p className="text-xs leading-relaxed text-muted-foreground">
                    {t(`opsalert:tplDef.${tpl.key}.when`)}
                  </p>

                  {/* 把「要问你什么」摆出来：点进去才知道要填十项，是最劝退的体验 */}
                  <div className="mt-1">
                    <div className="text-xs font-medium">{t('opsalert:tplPage.asks')}</div>
                    <ul className="mt-1 flex flex-col gap-0.5">
                      {tpl.fields.map((f) => (
                        <li key={f.key} className="text-xs text-muted-foreground">
                          · {t(`opsalert:tplDef.${tpl.key}.f.${f.key}.label`)}
                          {f.required ? <span className="text-danger"> *</span> : null}
                        </li>
                      ))}
                    </ul>
                  </div>

                  <div className="mt-auto flex items-center gap-2 pt-2">
                    <span className="text-xs text-muted-foreground">
                      {used.length > 0
                        ? t('opsalert:tplPage.usedBy', { count: used.length })
                        : t('opsalert:tplPage.unused')}
                    </span>
                    <WriteButton
                      perm="alert:manage_rules"
                      size="sm"
                      className="ml-auto"
                      onClick={() => setPicked(tpl.key)}
                    >
                      {t('opsalert:tplPage.use')}
                    </WriteButton>
                  </div>
                </section>
              )
            })}
          </div>
        )}
      </AsyncBoundary>
    </div>
  )
}
