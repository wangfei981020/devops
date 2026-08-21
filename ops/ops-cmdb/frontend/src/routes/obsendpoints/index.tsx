import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  Button,
  Dialog,
  EmptyState,
  Field,
  type LoadError,
  SecretInput,
  Select,
  Skeleton,
  Switch,
  TextInput,
  fromQuery,
} from '@ops/ui'
import { History, Info, Pencil, Radio, Trash2, Zap } from 'lucide-react'
import { useState } from 'react'
import { ObjectHistoryDialog } from '../../components/ObjectHistory.js'
import { RowMenu } from '../../components/RowMenu.js'
import { useEnvOptions } from '../../lib/dicts.js'
import { WriteButton } from '../../components/WriteButton.js'
import {
  CLUSTER_LABEL_PRESETS,
  OBS_TYPES,
  type ObsEndpoint,
  useLabelNames,
  useDeleteObs,
  useObsEndpoints,
  useSaveObs,
  useTestObs,
  type ObsTestResult,
} from './queries.js'

const PERM = 'cmdb:manage_integrations'

export function ObsEndpointsPage() {
  const { t } = useTranslation()
  const query = useObsEndpoints()
  const [editing, setEditing] = useState<ObsEndpoint | 'new' | null>(null)
  const [delFor, setDelFor] = useState<ObsEndpoint | null>(null)
  const [historyFor, setHistoryFor] = useState<ObsEndpoint | null>(null)

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    // ⚠️ 从 max-w-[1000px] 放宽。
    //
    //	实测表格总宽 984px、视口 1728px，**右边空着 744px**，
    //	却把最需要空间的「集群隔离标签」列压到 72px —— 15 个字折成 5 行，
    //	行高被撑到 98px（其它行 58px）。宽度不是不够，是没给到需要的地方（P1-62）。
    <div className="mx-auto max-w-[1400px] p-5">
      <div className="mb-4 flex items-baseline gap-2.5">
        <h1 className="text-sm font-semibold text-foreground">{t('obsendpoints:title')}</h1>
        <p className="min-w-0 flex-1 text-xs text-muted-foreground">{t('obsendpoints:hint')}</p>
        <WriteButton perm={PERM} variant="primary" size="sm" onClick={() => setEditing('new')}>
          {t('common:write.add')}
        </WriteButton>
      </div>

      <AsyncBoundary
        state={fromQuery<{ items: ObsEndpoint[] }>(query, (d) => d.items.length === 0, toLoadError)}
        errorTitle={t('obsendpoints:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={<Skeleton className="h-5 w-[60%]" />}
        empty={
          <EmptyState
            icon={<Radio />}
            title={t('obsendpoints:empty.title')}
            reason={t('obsendpoints:empty.reason')}
            action={{ label: t('obsendpoints:empty.action'), onClick: () => setEditing('new') }}
          />
        }
      >
        {(d) => (
          <div className="overflow-x-auto rounded-[var(--radius)] border border-border">
            <table className="w-full text-[13px]">
              <thead>
                <tr className="border-b border-border bg-card text-left text-xs text-muted-foreground">
                  <th className="px-3 py-2 font-medium">{t('obsendpoints:col.source')}</th>
                  <th className="px-3 py-2 font-medium">{t('obsendpoints:field.type')}</th>
                  <th className="px-3 py-2 font-medium">{t('obsendpoints:field.env')}</th>
                  {/* ⚠️ 说明放表头，出现**一次**。
                      原来「留空（该源只有一个集群的数据）」这句话被填进每一行的单元格，
                      5 行重复 5 次 —— 那不是数据，是说明（P1-63）。
                      把说明当数据填进单元格，既撑爆行高又淹没真正的差异。 */}
                  <th className="w-[200px] px-3 py-2 font-medium">
                    <span className="inline-flex items-center gap-1">
                      {t('obsendpoints:field.clusterLabel')}
                      <span title={t('obsendpoints:field.clusterLabelHelp')} className="inline-flex">
                        <Info className="size-3 cursor-help text-muted-foreground" />
                      </span>
                    </span>
                  </th>
                  <th className="px-3 py-2" />
                </tr>
              </thead>
              <tbody>
                {d.items.map((o) => (
                  <Row
                    key={o.id}
                    ep={o}
                    t={t}
                    onEdit={() => setEditing(o)}
                    onDelete={() => setDelFor(o)}
                    onHistory={() => setHistoryFor(o)}
                  />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </AsyncBoundary>

      {editing ? (
        <EditDialog t={t} ep={editing === 'new' ? null : editing} onClose={() => setEditing(null)} />
      ) : null}
      {delFor ? <DeleteDialog t={t} ep={delFor} onClose={() => setDelFor(null)} /> : null}
      {historyFor ? (
        <ObjectHistoryDialog
          table="obs_endpoints"
          pk={String(historyFor.id)}
          title={historyFor.name}
          onClose={() => setHistoryFor(null)}
        />
      ) : null}
    </div>
  )
}

/**
 * 一行数据源。
 *
 * 🔴 这里原本是个裸 div 列表，每行挂三个按钮、删除还是个大红按钮。
 * 三个问题，都在 CONVENTIONS §2.7 里有规定：
 *   1. 列表页统一用表格结构（有表头），不是自由排版的 div —— 没有表头时
 *      用户不知道那串灰字是什么，也没法按列扫
 *   2. 破坏性操作不做常驻按钮。删除是这页最少用的操作，却是视觉最重的元素，
 *      而且离「编辑」只有几十像素 —— 收进 ⋯ 菜单
 *   3. **环境和集群标签根本没显示**，而这两个恰恰决定了这个源会被谁用到。
 *      配错时列表上完全看不出来
 */
function Row({
  ep,
  t,
  onEdit,
  onDelete,
  onHistory,
}: {
  ep: ObsEndpoint
  t: (k: string, o?: Record<string, unknown>) => string
  onEdit: () => void
  onDelete: () => void
  onHistory: () => void
}) {
  const test = useTestObs()
  return (
    <tr className="border-b border-border last:border-b-0">
      <td className="px-3 py-2.5 align-top">
        <div className="flex min-w-0 flex-col gap-0.5">
          <div className="flex items-baseline gap-2">
            <span className="truncate font-medium text-foreground">{ep.name}</span>
            {ep.enabled !== 1 ? <Badge tone="mute">{t('obsendpoints:disabled')}</Badge> : null}
          </div>
          <span className="truncate font-mono text-xs text-muted-foreground" title={ep.url}>
            {ep.url}
          </span>
        </div>
      </td>
      <td className="px-3 py-2.5 align-top">
        <Badge tone="mute">{ep.type}</Badge>
      </td>
      {/* ⚠️ whitespace-nowrap：这一列只有 45px 宽，「不限环境」四个字
          会被折成「不限 / 环境」两行（OPSCMDB-031 P2-58，与 P1-62 同源）。
          环境值本来就短，不折行比压窄更重要 */}
      <td className="px-3 py-2.5 align-top text-xs whitespace-nowrap">
        {/* 空 = 不限环境，是个**有意义的配置**，不是缺数据。
            用 — 会让人以为没配 */}
        {ep.env ? (
          <span className="text-foreground">{ep.env}</span>
        ) : (
          <span className="text-muted-foreground">{t('obsendpoints:field.envAny')}</span>
        )}
      </td>
      <td className="px-3 py-2.5 align-top text-xs">
        {ep.cluster_label ? (
          <span className="font-mono text-foreground">{ep.cluster_label}</span>
        ) : (
          // 只显示结论，完整说明在表头的 ? 里
          <span className="text-muted-foreground">{t('obsendpoints:field.clusterLabelNoneShort')}</span>
        )}
      </td>
      <td className="px-3 py-2.5 text-right align-top">
        <div className="flex items-center justify-end gap-2">
          {/* 测连通的结果就地显示。配错地址的数据源和没配一样，
              但界面上它看着是好的 —— 所以这个按钮比"保存成功"重要 */}
          {/* 测试连通性：只读、高频、⚡ 语义公认 —— 符合 iconOnly 的三个条件 */}
          <WriteButton
            perm={PERM}
            size="sm"
            iconOnly
            icon={<Zap className="size-3.5" />}
            aria-label={t('common:write.test')}
            title={t('common:write.test')}
            loading={test.isPending}
            onClick={() => test.mutate(ep.id)}
          />
          <RowMenu
            perm={PERM}
            items={[
              {
                key: 'edit',
                label: t('common:write.edit'),
                icon: <Pencil className="size-3.5" />,
                onClick: onEdit,
              },
              {
                // 🔴 「这个端点昨天还好好的，今天怎么查不到数据了」——
                //	审计日志页有全量记录，但那是按**时间**排的一大列表，
                //	要在里面找出"谁动过这一条"得先知道动的时刻，
                //	而来问的人恰恰只知道对象、不知道时刻（OPSCMDB-023 第三档）
                key: 'history',
                label: t('audit:objectHistory.entry'),
                icon: <History className="size-3.5" />,
                onClick: onHistory,
              },
              {
                key: 'delete',
                label: t('common:write.delete'),
                icon: <Trash2 className="size-3.5" />,
                onClick: onDelete,
                danger: true,
              },
            ]}
          />
        </div>
        {/*
          ⚠️ 结果放在按钮**下方**，不挤在旁边。
          原来它和按钮、⋯ 菜单挤在同一个 flex 行里，被压成 **12px 宽**——
          正好一个汉字的宽度，「连通」竖排成「连 / 通」两行，几乎无法辨认（P1-64）。
        */}
        <TestResultLine test={test} t={t} />
      </td>
    </tr>
  )
}

/**
 * 连通性测试结果。
 *
 * ⚠️ 这个按钮是确认端点可用的**唯一入口**（数据源页的说明就是指引用户来点它的），
 * 所以它的输出应当是这条链路上信息最全的一环。原来只产出「连通」两个字：
 *
 *   ❌ 没有耗时 —— 「连通」和「连通但花了 8 秒」是两种健康度，
 *      后者会拖慢每个用到它的页面，而界面上它俩长得一样
 *   ❌ 没有探通的路径 —— 地址填错时（少了/多了前缀）这一条最能说明问题
 *   ❌ 失败时没有细节 —— 只说"不通"，和巡检页「不说哪个数据源失败」同病
 *
 * 后端其实**全都给了**（status / path / duration_ms / tried），只是前端没用。
 */
function TestResultLine({
  test,
  t,
}: {
  test: {
    isSuccess: boolean
    isError: boolean
    data?: ObsTestResult
    error?: unknown
  }
  t: (k: string, o?: Record<string, unknown>) => string
}) {
  if (!test.isSuccess && !test.isError) return null
  const d = test.data
  // ⚠️ mutation 成功不等于探通：后端对"没探通"也返回 200 + ok:false
  const ok = test.isSuccess && d?.ok === true
  if (ok) {
    return (
      <div className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px]">
        <span className="text-success">{t('common:write.testOk')}</span>
        {d?.duration_ms != null ? (
          // 慢也要说：超过 2 秒的"通"值得单独看一眼
          <span className={d.duration_ms > 2000 ? 'text-warning' : 'text-muted-foreground'}>
            {t('obsendpoints:test.took', { ms: d.duration_ms })}
          </span>
        ) : null}
        {d?.path ? (
          <span className="font-mono text-muted-foreground">
            {t('obsendpoints:test.via', { path: d.path })}
          </span>
        ) : null}
      </div>
    )
  }
  // 失败：把后端给的原因和每条尝试都摊开 —— 这正是排障要看的
  const reason = d?.error || (test.error ? toErrorInfo(test.error).detail : '')
  return (
    <div className="mt-1.5 flex flex-col gap-0.5 text-[11px]">
      <span className="text-danger">{t('common:write.testFail')}</span>
      {reason ? <span className="break-words text-danger/85">{reason}</span> : null}
      {(d?.tried ?? []).map((x) => (
        <span key={`${x.path}-${x.status ?? x.error}`} className="font-mono text-muted-foreground">
          {x.path} → {x.status ?? x.error}
        </span>
      ))}
    </div>
  )
}

function EditDialog({
  t,
  ep,
  onClose,
}: {
  t: (k: string, o?: Record<string, unknown>) => string
  ep: ObsEndpoint | null
  onClose: () => void
}) {
  const [name, setName] = useState(ep?.name ?? '')
  const [type, setType] = useState(ep?.type ?? 'prometheus')
  const [url, setUrl] = useState(ep?.url ?? '')
  const [env, setEnv] = useState(ep?.env ?? '')
  const [label, setLabel] = useState(ep?.cluster_label ?? '')
  const [token, setToken] = useState('')
  const [enabled, setEnabled] = useState((ep?.enabled ?? 1) === 1)
  const save = useSaveObs()
  const envs = useEnvOptions()
  const probe = useLabelNames(ep?.id ?? null, type)

  // 下拉选项 = 留空 + 探测到的 + 候选 + 当前值（如果都不在里面）。
  // ⚠️ 当前值必须保留：老记录里可能是个非常规标签名，
  // 不放进选项的话，一打开编辑框它就被悄悄改成了别的值
  const probed = probe.data?.ok ? (probe.data.names ?? []) : []
  const labelOptions = [
    { value: '', label: t('obsendpoints:field.clusterLabelNone') },
    ...Array.from(new Set([...probed, ...CLUSTER_LABEL_PRESETS, ...(label ? [label] : [])]))
      .filter((v) => v !== '')
      .map((v) => ({
        value: v,
        label: probed.includes(v) ? t('obsendpoints:field.labelProbed', { name: v }) : v,
      })),
  ]
  const labelHint =
    type !== 'prometheus'
      ? t('obsendpoints:field.labelNotApplicable')
      : !ep
        ? t('obsendpoints:field.labelProbeAfterSave')
        : probe.isPending
          ? t('obsendpoints:field.labelProbing')
          : probe.data?.ok
            ? // 🔴 note_key 优先：note 是后端拼的**中文**原句，留给 MCP / 直接调 API 的人，
              //	英文界面直接渲染它就是一句中文（OPSCMDB-054）
              probe.data.note_key
              ? t(probe.data.note_key, probe.data.note_params)
              : (probe.data.note ?? '')
            : ''

  return (
    <Dialog
      open
      onClose={onClose}
      title={ep ? t('obsendpoints:editTitle', { name: ep.name }) : t('obsendpoints:addTitle')}
      description={t('obsendpoints:addDesc')}
      closeLabel={t('common:action.close')}
      width={560}
      footer={
        <>
          <Button size="sm" onClick={onClose}>{t('common:write.cancel')}</Button>
          <Button
            size="sm"
            variant="primary"
            loading={save.isPending}
            disabled={name.trim() === '' || url.trim() === ''}
            onClick={() =>
              save.mutate(
                {
                  id: ep?.id,
                  name: name.trim(),
                  type,
                  url: url.trim(),
                  env: env.trim(),
                  cluster_id: ep?.cluster_id ?? 0,
                  cluster_label: label.trim(),
                  token,
                  enabled: enabled ? 1 : 0,
                },
                { onSuccess: onClose },
              )
            }
          >
            {t('common:write.save')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label={t('obsendpoints:field.name')} required>
          <TextInput value={name} onChange={(e) => setName(e.target.value)} autoFocus />
        </Field>
        <Field label={t('obsendpoints:field.type')} hint={t('obsendpoints:field.typeHint')}>
          {/* 类型只能从白名单选。打错一个字母会建出一条永远不工作的记录，
              而界面上它显示"已启用" */}
          <Select<string>
            label={t('obsendpoints:field.type')}
            value={type}
            onChange={setType}
            options={OBS_TYPES.map((v) => ({ value: v, label: t(`obsendpoints:types.${v}`) }))}
          />
        </Field>
        <Field label={t('obsendpoints:field.url')} hint={t('obsendpoints:field.urlHint')} required>
          <TextInput value={url} onChange={(e) => setUrl(e.target.value)} placeholder="http://prometheus:9090" />
        </Field>
        <Field label={t('obsendpoints:field.env')} hint={t('obsendpoints:field.envHint')}>
          {/* 环境从字典取，不手输。手输会打错，而打错的表现是这个源
              在按环境查找时永远匹配不上——同样是静默失效 */}
          <Select<string>
            label={t('obsendpoints:field.env')}
            value={env}
            onChange={setEnv}
            options={[
              { value: '', label: t('obsendpoints:field.envAny') },
              ...(envs.data ?? []).map((e) => ({ value: e.code, label: `${e.code} · ${e.name}` })),
            ]}
          />
          {envs.isError ? (
            // 字典拉不到时不能装作"只有不限环境这一个选项"
            <p className="mt-1 text-xs text-danger">{t('obsendpoints:field.envLoadFailed')}</p>
          ) : null}
        </Field>
        <Field
          label={t('obsendpoints:field.clusterLabel')}
          hint={t('obsendpoints:field.clusterLabelHint')}
        >
          <Select<string>
            label={t('obsendpoints:field.clusterLabel')}
            value={label}
            onChange={setLabel}
            options={labelOptions}
          />
          {/* 探测的三种结局分开说，不能都渲染成"没有可选项" */}
          {labelHint ? <p className="mt-1 text-xs text-muted-foreground">{labelHint}</p> : null}
          {probe.data && probe.data.ok === false ? (
            <p className="mt-1 text-xs text-warning">
              {probe.data.error_key
                ? t(probe.data.error_key, probe.data.error_params)
                : probe.data.error}
            </p>
          ) : null}
        </Field>
        <Field label={t('obsendpoints:field.token')} hint={t('obsendpoints:field.tokenHint')}>
          <SecretInput
            configured={ep?.has_token ?? false}
            placeholderConfigured={t('common:write.secretKeep')}
            placeholderEmpty={t('common:write.secretEmpty')}
            showLabel={t('common:action.showSecret')}
            hideLabel={t('common:action.hideSecret')}
            value={token}
            onChange={(e) => setToken(e.target.value)}
          />
        </Field>
        <Switch checked={enabled} onChange={setEnabled} label={t('obsendpoints:field.enabled')} />
        {save.isError ? <p className="text-xs text-danger">{t(toErrorInfo(save.error).messageKey)}</p> : null}
      </div>
    </Dialog>
  )
}

function DeleteDialog({
  t,
  ep,
  onClose,
}: {
  t: (k: string, o?: Record<string, unknown>) => string
  ep: ObsEndpoint
  onClose: () => void
}) {
  const del = useDeleteObs()
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('common:write.deleteConfirm', { name: ep.name })}
      description={t('common:write.deleteHint')}
      closeLabel={t('common:action.close')}
      footer={
        <>
          <Button size="sm" onClick={onClose}>{t('common:write.cancel')}</Button>
          <Button size="sm" variant="danger" loading={del.isPending} onClick={() => del.mutate(ep.id, { onSuccess: onClose })}>
            {t('common:write.delete')}
          </Button>
        </>
      }
    >
      <p className="text-[13px] leading-relaxed text-foreground">{t('obsendpoints:deleteImpact')}</p>
      {del.isError ? <p className="mt-2 text-xs text-danger">{t(toErrorInfo(del.error).messageKey)}</p> : null}
    </Dialog>
  )
}
