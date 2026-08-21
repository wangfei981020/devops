import { useTranslation } from '@ops/i18n'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { ApiError, api } from '../api/client.js'
import type { App, ListOf } from '../api/types.js'

interface Group {
  id: number
  code: string
  name: string
}

/**
 * 接入方式。顺序按「改造成本从高到低」——大多数老系统只能用网关代管。
 *
 * ⚠️ `ready: false` 的**照样列出来但不可选**，并写清为什么。
 * 直接从下拉里拿掉的话，人会以为这个产品不支持；
 * 而让他选中一个走不通的选项更糟 —— saml 选了之后什么都不会发生，
 * formfill 选了之后网关直接 503（凭据根本没有地方可配）。
 * 两者都是「界面提供了一个无法完成的选项」，比缺功能更伤信任。
 */
const CONNECT = [
  { key: 'oidc', ready: true },
  { key: 'gateway', ready: true },
  { key: 'formfill', ready: false },
  { key: 'saml', ready: false },
] as const

/**
 * 接入一个应用。
 *
 * # 为什么这一屏必须有
 *
 * 在它之前，界面只能**看**应用不能**接** —— 装上这套系统的第一步就卡住，
 * 只能去调接口。一个访问控制平面，连"把第一个系统接进来"都做不到，
 * 等于装上去就是个空壳。
 *
 * # 为什么接入方式要写清代价
 *
 * 客户最常问的一句是"我这个老系统能接吗"。四种方式的**改造成本差一个量级**：
 * OIDC 要应用自己支持，网关代管零改造。只列四个英文枚举值，
 * 选的人只能靠猜，猜错的代价是接完发现登不进去。
 */
export function NewAppForm({ onDone, app }: { onDone: () => void; app?: App }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  // 传了 app 就是改，没传就是建。同一套字段渲染两次迟早分叉 ——
  // 分叉的表现是"建的时候能填的东西改的时候没有"，而那正是人最想改的字段。
  const editing = app !== undefined

  const [code, setCode] = useState(app?.code ?? '')
  const [name, setName] = useState(app?.name ?? '')
  const [env, setEnv] = useState(app?.env ?? '')
  const [connect, setConnect] = useState<string>(app?.connect_type ?? 'gateway')
  const [baseURL, setBaseURL] = useState(app?.base_url ?? '')
  const [showWhenDenied, setShow] = useState(app?.show_when_denied ?? false)
  const [groupIDs, setGroupIDs] = useState<number[]>(app?.group_ids ?? [])
  const [redirects, setRedirects] = useState('')
  const [err, setErr] = useState<string | null>(null)
  // 建完之后停在这一屏显示密钥 —— 明文只有这一次
  const [done, setDone] = useState<CreateResult | null>(null)

  const groups = useQuery({
    queryKey: ['app-groups'],
    queryFn: () => api.get<ListOf<Group>>('/app-groups'),
  })

  const wantOIDC = connect === 'oidc'
  const redirectList = redirects
    .split('\n')
    .map((v) => v.trim())
    .filter((v) => v !== '')

  const m = useMutation({
    mutationFn: () => {
      const body = {
        code: code.trim(),
        name: name.trim(),
        env: env.trim(),
        connect_type: connect,
        base_url: baseURL.trim(),
        show_when_denied: showWhenDenied,
        // 图标不让人填：多一栏要填的东西，换来的只是一个色块。
        // 但也**不能留空** —— 空的图标位在列表里是一块空白，看起来像渲染坏了，
        // 而它旁边的应用都有色块，对比之下更像故障。
        icon_text: iconTextOf(name),
        icon_color: iconColorOf(code),
        group_ids: groupIDs,
        // 只选了一个组时它自然就是主组；多选时取第一个，
        // 主组决定门户里这个应用归在哪一栏（一个应用只能出现在一栏里）
        primary_group_id: groupIDs[0] ?? 0,
        // 选了 OIDC 就顺带把客户端建了。client_id 由后端生成 ——
        // 让人自己填，填出来的一定是 harbor / prod / test 这种好猜的值。
        // 改的时候不带这一段：改应用不该顺手再发一个客户端出来。
        oidc: !editing && wantOIDC ? { redirect_uris: redirectList, public_client: false } : undefined,
      }
      return editing
        ? api.put<CreateResult>(`/apps/${app.id}`, body)
        : api.post<CreateResult>('/apps', body)
    },
    onSuccess: (r) => {
      setErr(null)
      void qc.invalidateQueries({ queryKey: ['apps'] })
      void qc.invalidateQueries({ queryKey: ['overview'] })
      void qc.invalidateQueries({ queryKey: ['oidc-clients'] })
      // 有密钥要交给人，就不能直接关掉表单 —— 关掉 = 密钥永远拿不回来了。
      // 没密钥（网关接入）则照旧直接收起，不平白多一次点击。
      if (r.client_secret || r.oidc_error) setDone(r)
      else onDone()
    },
    onError: (e) =>
      setErr(
        e instanceof ApiError
          ? t(`errors.${e.code}`, { ...e.params, defaultValue: e.code })
          : t('error.unreachable'),
      ),
  })

  // OIDC 必须有回调地址：没有回调地址的客户端跑不完任何一次登录，
  // 后端会直接拒。按钮先禁掉，别让人填完一屏再吃一个报错。
  // 改的时候不建客户端，所以不受这条限制。
  const ready =
    code.trim() !== '' &&
    name.trim() !== '' &&
    env.trim() !== '' &&
    (editing || !wantOIDC || redirectList.length > 0)

  if (done) return <Created r={done} name={name} onDone={onDone} />

  return (
    <div className="rounded-[var(--radius-md)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold">
        {editing ? t('sso:apps.editTitle', { name: app.name }) : t('sso:apps.newTitle')}
      </h2>
      <p className="mt-1 mb-3 max-w-[80ch] text-[12px] text-muted-foreground">
        {editing ? t('sso:apps.editDesc') : t('sso:apps.newDesc')}
      </p>

      <div className="grid gap-3 sm:grid-cols-3">
        <Field label={t('sso:apps.fCode')} hint={t('sso:apps.fCodeHint')}>
          <input
            value={code}
            // code 是标识不是名字：空格和大写会让下游配置里对不上，直接在输入时挡掉
            onChange={(e) => setCode(e.target.value.toLowerCase().replace(/[^a-z0-9_-]/g, ''))}
            placeholder="harbor"
            // ⚠️ 改的时候锁死：下游配置里填的就是它，改了等于对方那一行
            // 指向一个不存在的应用，而下游不会有任何提示。后端也不接受这个字段。
            disabled={editing}
            className={`${inputCls} ${editing ? 'cursor-not-allowed opacity-60' : ''}`}
          />
        </Field>
        <Field label={t('sso:apps.fName')}>
          <input value={name} onChange={(e) => setName(e.target.value)} className={inputCls} />
        </Field>
        <Field label={t('sso:apps.fEnv')} hint={t('sso:apps.fEnvHint')}>
          <input
            value={env}
            onChange={(e) => setEnv(e.target.value)}
            placeholder="PROD"
            className={inputCls}
          />
        </Field>
      </div>

      <div className="mt-3">
        <span className="mb-1.5 block text-[12px] text-muted-foreground">
          {t('sso:apps.fConnect')}
        </span>
        {editing ? (
          // 改接入方式不会让已经建好的客户端或路由消失，只会让界面写着一套、
          // 实际跑着另一套。真要换，先把旧的挂件删干净再重建。
          <p className="mb-2 rounded-[var(--radius)] bg-muted px-2.5 py-1.5 text-[11px] text-muted-foreground">
            {t('sso:apps.connectLocked', { type: connect })}
          </p>
        ) : null}
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
          {CONNECT.map((c) => (
            <button
              key={c.key}
              type="button"
              disabled={!c.ready || editing}
              onClick={() => setConnect(c.key)}
              aria-pressed={connect === c.key}
              className={[
                'rounded-[var(--radius)] border p-2.5 text-left',
                c.ready && !editing ? 'cursor-pointer' : 'cursor-not-allowed opacity-60',
                connect === c.key ? 'border-brand bg-brand-bg' : 'border-border',
                c.ready && !editing && connect !== c.key ? 'hover:bg-muted' : '',
              ].join(' ')}
            >
              <b className="block text-[13px] font-medium">
                {t(`sso:apps.connect.${c.key}`)}
                {!c.ready ? (
                  <span className="ml-1.5 rounded-[var(--radius-sm)] bg-muted px-1.5 py-0.5 text-[10px] font-normal text-muted-foreground">
                    {t('sso:apps.notReady')}
                  </span>
                ) : null}
              </b>
              {/* 改造成本必须写出来：客户是照着这行判断"我这个系统能不能接"。
                  未实现的则写清缺的是哪一环，而不是只标一个灰。 */}
              <span className="mt-0.5 block text-[11px] text-muted-foreground">
                {t(c.ready ? `sso:apps.connectCost.${c.key}` : `sso:apps.notReadyWhy.${c.key}`)}
              </span>
            </button>
          ))}
        </div>
      </div>

      <div className="mt-3 grid gap-3 sm:grid-cols-2">
        <Field label={t('sso:apps.fBaseURL')} hint={t('sso:apps.fBaseURLHint')}>
          <input
            value={baseURL}
            onChange={(e) => setBaseURL(e.target.value)}
            placeholder="https://harbor.example.com"
            className={inputCls}
          />
        </Field>
        <Field label={t('sso:apps.fGroups')} hint={t('sso:apps.fGroupsHint')}>
          {groups.isError ? (
            // 分组读不出来 ≠ 没有分组。后者可以继续建应用，前者说明接口有问题，
            // 显示成"没有分组"会让人以为要先去建一个，白跑一趟。
            <p className="text-[12px] text-warning">{t('sso:apps.groupsUnavailable')}</p>
          ) : (
            <div className="flex flex-wrap gap-1.5">
              {(groups.data?.items ?? []).map((g) => {
                const on = groupIDs.includes(g.id)
                return (
                  <button
                    key={g.id}
                    type="button"
                    onClick={() =>
                      setGroupIDs((v) => (on ? v.filter((x) => x !== g.id) : [...v, g.id]))
                    }
                    className={[
                      'cursor-pointer rounded-[var(--radius-sm)] border px-2 py-0.5 text-[12px]',
                      on ? 'border-brand bg-brand-bg text-brand' : 'border-border',
                    ].join(' ')}
                  >
                    {g.name}
                  </button>
                )
              })}
              {(groups.data?.items ?? []).length === 0 && !groups.isPending ? (
                <span className="text-[12px] text-muted-foreground">{t('sso:apps.noGroups')}</span>
              ) : null}
            </div>
          )}
        </Field>
      </div>

      {wantOIDC && !editing ? (
        <div className="mt-3 rounded-[var(--radius)] border border-border bg-background p-3">
          <b className="block text-[13px] font-medium">{t('sso:apps.oidcTitle')}</b>
          {/* client_id / client_secret 不出现在表单里：都是建的时候生成的。
              人要填的只有回调地址 —— 而这一个恰恰是最容易填错、
              且填错之后对方只会报一句 "invalid redirect_uri" 的。 */}
          <p className="mt-0.5 mb-2 max-w-[80ch] text-[11px] text-muted-foreground">
            {t('sso:apps.oidcDesc')}
          </p>
          <span className="mb-1 block text-[12px] text-muted-foreground">
            {t('sso:apps.fRedirects')}
          </span>
          <textarea
            value={redirects}
            onChange={(e) => setRedirects(e.target.value)}
            rows={2}
            placeholder="https://harbor.example.com/c/oidc/callback"
            className={`${inputCls} font-mono text-[12px]`}
          />
          <p className="mt-1 text-[11px] text-muted-foreground">{t('sso:apps.fRedirectsHint')}</p>
          {/* 常见系统的回调路径直接给出来。这几个路径都是各自文档里写死的，
              照抄一遍不难，但抄错一个字符的代价是接入当场失败且报错很含糊。 */}
          {baseURL.trim() !== '' ? (
            <div className="mt-2 flex flex-wrap items-center gap-1.5">
              <span className="text-[11px] text-muted-foreground">
                {t('sso:apps.callbackPreset')}
              </span>
              {CALLBACKS.map((cb) => (
                <button
                  key={cb.key}
                  type="button"
                  onClick={() =>
                    setRedirects((v) =>
                      [...v.split('\n').filter((x) => x.trim() !== ''), trimSlash(baseURL) + cb.path].join(
                        '\n',
                      ),
                    )
                  }
                  className="cursor-pointer rounded-[var(--radius-sm)] border border-border px-1.5 py-0.5 text-[11px] hover:bg-muted"
                >
                  {cb.key}
                </button>
              ))}
            </div>
          ) : null}
        </div>
      ) : null}

      <label className="mt-3 flex cursor-pointer items-start gap-2 text-[12px]">
        <input
          type="checkbox"
          checked={showWhenDenied}
          onChange={(e) => setShow(e.target.checked)}
          className="mt-0.5"
        />
        <span>
          <b className="font-medium">{t('sso:apps.fShowWhenDenied')}</b>
          {/* 这个开关是把一份系统清单暴露给全员，代价必须写在勾选框旁边 */}
          <span className="block text-muted-foreground">{t('sso:apps.fShowWhenDeniedHint')}</span>
        </span>
      </label>

      {err ? (
        <p className="mt-3 rounded-[var(--radius)] bg-danger-bg px-3 py-2 text-[12px] text-danger">
          {err}
        </p>
      ) : null}

      <div className="mt-4 flex items-center gap-2">
        <button
          type="button"
          disabled={!ready || m.isPending}
          onClick={() => m.mutate()}
          className="cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground disabled:cursor-default disabled:opacity-50"
        >
          {m.isPending
            ? t(editing ? 'sso:apps.saving' : 'sso:apps.creating')
            : t(editing ? 'sso:apps.save' : 'sso:apps.create')}
        </button>
        <button
          type="button"
          onClick={onDone}
          className="cursor-pointer rounded-[var(--radius)] border border-border px-3 py-1.5 text-[13px] hover:bg-secondary"
        >
          {t('sso:apps.cancel')}
        </button>
        {/* 接完还进不去是必然的：默认拒绝。不写这句，第一个接入的人一定会来问 */}
        {editing ? null : (
          <span className="text-[11px] text-muted-foreground">{t('sso:apps.afterCreate')}</span>
        )}
      </div>
    </div>
  )
}

/**
 * 图标文字：中文取前两字，英文取前两个字母大写。
 * 取两个而不是一个：一个字母的重复率太高，一屏里三个「H」分不出谁是谁。
 */
function iconTextOf(name: string): string {
  const n = name.trim()
  if (!n) return '?'
  // eslint-disable-next-line no-control-regex -- 判断是不是 ASCII，故意用码点范围
  const ascii = /^[\x00-\x7F]+$/.test(n[0] ?? '')
  return ascii ? n.slice(0, 2).toUpperCase() : n.slice(0, 2)
}

/**
 * 图标底色：从标识算出来，**同一个应用永远同一个颜色**。
 *
 * 随机取色的话，每次重新接入同一个系统颜色都不一样，
 * 而人是靠颜色在门户里一眼找到常用系统的。
 * 色相取自设计系统的强调色附近，避开红/绿 —— 那两个在这套界面里
 * 已经被"拒绝/放行"占用了，用作图标底色会误导。
 */
function iconColorOf(code: string): string {
  let h = 0
  for (const ch of code) h = (h * 31 + ch.charCodeAt(0)) % 360
  const hue = 200 + (h % 140) // 200–340：蓝 → 紫 → 品红
  return `oklch(0.62 0.14 ${hue})`
}

const inputCls =
  'w-full rounded-[var(--radius)] border border-border bg-background px-2.5 py-1.5 text-[13px]'

function Field({
  label,
  hint,
  children,
}: {
  label: string
  hint?: string
  children: React.ReactNode
}) {
  return (
    <label className="block text-[12px]">
      <span className="mb-1 block text-muted-foreground">{label}</span>
      {children}
      {hint ? <span className="mt-1 block text-[11px] text-muted-foreground">{hint}</span> : null}
    </label>
  )
}

interface CreateResult {
  id: number
  client_id?: string
  client_secret?: string
  /** 应用建好了但客户端没建出来 —— 必须如实说，不能显示成"接入成功" */
  oidc_error?: boolean
}

const CALLBACKS = [
  { key: 'Harbor', path: '/c/oidc/callback' },
  { key: 'Grafana', path: '/login/generic_oauth' },
  { key: 'Argo CD', path: '/auth/callback' },
  { key: 'GitLab', path: '/users/auth/openid_connect/callback' },
]

function trimSlash(v: string) {
  return v.trim().replace(/\/+$/, '')
}

/**
 * 建完之后的凭据交接屏。
 *
 * # 为什么要单独一屏
 *
 * client_secret 明文**只在这一次响应里出现**（库里存的是哈希）。
 * 直接把表单收起来，等于把密钥丢了 —— 补救只能重建客户端，
 * 而重建意味着对方系统里已经填好的配置全部作废。
 *
 * 所以这一屏不给"自动关闭"，必须人点一下确认拿到了。
 */
function Created({ r, name, onDone }: { r: CreateResult; name: string; onDone: () => void }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState<string | null>(null)

  const copy = (label: string, v: string) => {
    void navigator.clipboard?.writeText(v).then(
      () => setCopied(label),
      // 复制失败要说出来：以为复制成功了、去粘贴发现是旧内容，
      // 比"复制按钮没反应"难查得多
      () => setCopied('__fail__'),
    )
  }

  return (
    <div className="rounded-[var(--radius-md)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold">{t('sso:apps.createdTitle', { name })}</h2>

      {r.oidc_error ? (
        // 应用建好了，客户端没建成。这不是"成功"，也不是"全失败" ——
        // 说错任何一边都会让人做错下一步（重建应用 / 以为能直接接）
        <p className="mt-2 rounded-[var(--radius)] bg-warning-bg px-3 py-2 text-[12px] text-warning">
          {t('sso:apps.createdOidcFailed')}
        </p>
      ) : (
        <>
          <p className="mt-1 mb-3 max-w-[80ch] text-[12px] text-muted-foreground">
            {t('sso:apps.createdDesc')}
          </p>
          <dl className="grid gap-2">
            <Cred label={t('sso:apps.cClientID')} v={r.client_id ?? ''} onCopy={copy} />
            <Cred label={t('sso:apps.cSecret')} v={r.client_secret ?? ''} onCopy={copy} />
          </dl>
          <p className="mt-2 rounded-[var(--radius)] bg-warning-bg px-3 py-2 text-[12px] text-warning">
            {t('sso:apps.secretOnce')}
          </p>
        </>
      )}

      {copied ? (
        <p className="mt-2 text-[11px] text-muted-foreground">
          {copied === '__fail__' ? t('sso:apps.copyFailed') : t('sso:apps.copied', { field: copied })}
        </p>
      ) : null}

      <div className="mt-4 flex items-center gap-2">
        <button
          type="button"
          onClick={onDone}
          className="cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground"
        >
          {t('sso:apps.createdOk')}
        </button>
        <span className="text-[11px] text-muted-foreground">{t('sso:apps.createdNext')}</span>
      </div>
    </div>
  )
}

function Cred({
  label,
  v,
  onCopy,
}: {
  label: string
  v: string
  onCopy: (label: string, v: string) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="flex items-center gap-2">
      <dt className="w-28 shrink-0 text-[12px] text-muted-foreground">{label}</dt>
      {/* 密钥这里**故意明文显示**：这一屏的全部目的就是让人拿到它，
          再用圆点盖住等于让人复制一个自己看不见的东西 */}
      <dd className="min-w-0 flex-1 truncate rounded-[var(--radius)] border border-border bg-background px-2 py-1 font-mono text-[12px]">
        {v}
      </dd>
      <button
        type="button"
        onClick={() => onCopy(label, v)}
        className="cursor-pointer rounded-[var(--radius)] border border-border px-2 py-1 text-[11px] hover:bg-secondary"
      >
        {t('sso:apps.copy')}
      </button>
    </div>
  )
}
