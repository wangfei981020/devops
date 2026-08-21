import { useTranslation } from '@ops/i18n'
import { Button, Dialog, Select } from '@ops/ui'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '../../lib/api.js'
import { ProjectsSection, useProjects } from './ProjectsSection.js'
import {
  AUTH_BY_PROVIDER,
  ENVS,
  type EnvRow,
  type Org,
  PROVIDERS,
  emptyEnv,
  fromLines,
  toLines,
  normLines,
} from './types.js'

interface Props {
  org: Org | null
  onClose: () => void
  onSaved: () => void
  onToast: (msg: string, kind: 'ok' | 'err') => void
}

/**
 * 组织新增/编辑。
 *
 * 🔴 凭据字段留空 = **不改动已有凭据**，不是清空。
 * 因为读接口从不回显凭据，编辑时这些框本来就是空的 ——
 * 若把空当成清空，用户改个备注就会把密码抹掉，
 * 然后采集开始报认证失败，而界面上看不出任何异常。
 */
export function OrgForm({ org, onClose, onSaved, onToast }: Props) {
  const { t } = useTranslation()
  const isEdit = !!org
  const [f, setF] = useState({
    name: org?.name ?? '',
    provider_type: org?.provider_type ?? 'kite',
    auth_type: org?.auth_type ?? 'password',
    endpoint: org?.endpoint ?? '',
    harbor_host: org?.harbor_host ?? '',
    harbor_project: org?.harbor_project ?? '',
    is_self: org?.is_self ?? false,
    enabled: org?.enabled ?? true,
    username: '',
    password: '',
    api_key: '',
    insecure_tls: false,
    datasource_id: org?.datasource_id ?? 0,
  })

  // ─── 数据源 ───
  //
  // 🔴 引用数据源后，地址和凭据由数据源提供，这个表单里不再填一遍。
  //    这是「同一个 Rancher 被 N 家公司共用」的正解 ——
  //    改一次密码只改一处，而不是 N 处里漏掉一处。
  const dsQ = useQuery({
    queryKey: ['datasources'],
    queryFn: () => api<{ id: number; name: string; provider_type: string; endpoint: string }[]>(
      '/api/datasources'),
  })
  const dsList = dsQ.data ?? []
  const usingDS = f.datasource_id > 0
  // ⚠️ 名字优先用列表里查到的（最新）；查不到（数据源被删了）回落到平台上带回来的那份，
  //    再不行才显示 id —— 显示空白会让人以为「没引用」，而其实引用着一个不存在的东西。
  const dsHit = dsList.find((d) => d.id === f.datasource_id)
  const dsName = dsHit?.name ?? org?.datasource_name ?? `#${f.datasource_id}`
  const dsEndpoint = dsHit?.endpoint ?? org?.ds_endpoint ?? ''

  const projQ = useProjects(org?.id ?? 0)
  const projects = (projQ.data ?? []).filter((p) => p.enabled)
  const [envs, setEnvs] = useState<EnvRow[]>(
    org?.envs?.length ? org.envs : [emptyEnv()],
  )
  const [busy, setBusy] = useState(false)

  const set = (k: keyof typeof f, v: unknown) => setF((p) => ({ ...p, [k]: v }))
  const setEnv = (i: number, patch: Partial<EnvRow>) =>
    setEnvs((p) => p.map((e, n) => (n === i ? { ...e, ...patch } : e)))

  async function pullClusters(i: number) {
    if (!org) {
      onToast(t('opsversion:org.saveFirst'), 'err')
      return
    }
    try {
      const list = await api<{ id: string; name: string }[]>(
        `/api/orgs/${org.id}/clusters?env=${encodeURIComponent(envs[i]?.env ?? '')}`,
      )
      if (!list.length) {
        onToast(t('opsversion:org.noClusters'), 'err')
        return
      }
      setEnv(i, { cluster_refs: list.map((c) => c.id) })
      onToast(t('opsversion:org.pulled', { n: list.length }), 'ok')
    } catch (e) {
      onToast((e as Error).message, 'err')
    }
  }

  async function save() {
    if (!f.name.trim()) {
      onToast(t('opsversion:org.nameRequired'), 'err')
      return
    }
    setBusy(true)
    try {
      // 🔴 多行框在输入期保留原样（否则敲不出第二行，见 types.ts 的 toLines），
      //    所以清理必须在这里做一次 —— 漏了的话空行会变成一条 "" 规则匹配所有 ns。
      const cleanEnvs = envs.map((e) => ({
        ...e,
        cluster_refs: normLines(e.cluster_refs),
        ns_include: normLines(e.ns_include),
        ns_exclude: normLines(e.ns_exclude),
        workload_include: normLines(e.workload_include),
        workload_exclude: normLines(e.workload_exclude),
      }))
      await api(isEdit ? `/api/orgs/${org.id}` : '/api/orgs', {
        method: isEdit ? 'PUT' : 'POST',
        body: JSON.stringify({ ...f, envs: cleanEnvs }),
      })
      onSaved()
    } catch (e) {
      onToast((e as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const field = (label: string, node: React.ReactNode, hint?: string) => (
    <div className="mb-3">
      <div className="flex items-start gap-2">
        <span className="w-24 shrink-0 pt-1.5 text-right text-xs text-muted-foreground">{label}</span>
        <div className="min-w-0 flex-1">{node}</div>
      </div>
      {hint && <div className="ml-26 pl-2 text-[11px] text-muted-foreground">{hint}</div>}
    </div>
  )

  const input = (v: string, on: (s: string) => void, ph = '', type = 'text') => (
    <input
      type={type}
      value={v}
      placeholder={ph}
      onChange={(e) => on(e.target.value)}
      className="w-full rounded-md border border-input bg-background px-2.5 py-1.5 text-xs text-foreground"
    />
  )

  // 高度跟着内容走：2 行起、10 行封顶。
  // ⚠️ 固定 rows={2} 时，客户环境动辄十几个 ns，
  //    填完只能在两行高的框里上下滚，看不全也就核对不了。
  // 单串凭据（Kite 的 api_key / ArgoCD 的 token）走同一个输入框。
  // ⚠️ 只判 'api_key' 的话，选了 ArgoCD 的 token 会掉进用户名密码那一支 ——
  //    界面上要你填账号密码，而后端等的是一串 token。
  const isKeyAuth = (t: string) => t === 'api_key' || t === 'token'

  // 认证方式跟着数据源走：ArgoCD 没有 api_key，Kite 没有 token。
  // ⚠️ 列出对方不支持的方式，人填完才发现连不上 —— 而报错只会说"凭据无效"。
  const authOptions = (provider: string) =>
    (AUTH_BY_PROVIDER[provider] ?? []).map((a) => ({
      value: a,
      label: t(`opsversion:authType.${a}`),
    }))

  const area = (v: string, on: (s: string) => void, ph = '') => (
    <textarea
      rows={Math.min(10, Math.max(2, v.split('\n').length))}
      value={v}
      placeholder={ph}
      onChange={(e) => on(e.target.value)}
      className="w-full resize-y rounded-md border border-input bg-background px-2.5 py-1.5 font-mono text-[11px] text-foreground"
    />
  )

  return (
    <Dialog
      open
      onClose={onClose}
      title={isEdit ? `${t('opsversion:org.edit')} · ${org.name}` : t('opsversion:org.add')}
      closeLabel={t('common:action.close')}
      width={880}
      footer={
        <>
          <Button onClick={onClose}>{t('common:action.cancel')}</Button>
          <Button variant="primary" onClick={save} disabled={busy}>
            {isEdit ? t('common:action.save') : t('common:action.confirm')}
          </Button>
        </>
      }
    >
      <div className="mb-2 border-b border-border pb-1.5 text-xs font-semibold text-foreground">
        {t('opsversion:org.secBasic')}
      </div>
      {field(t('opsversion:org.name'), input(f.name, (v) => set('name', v)))}
      {field(
        'Harbor',
        <div className="flex gap-2">
          {input(f.harbor_host, (v) => set('harbor_host', v), t('opsversion:org.harborHostPh'))}
          {input(f.harbor_project, (v) => set('harbor_project', v), t('opsversion:org.harborProjPh'))}
        </div>,
        t('opsversion:org.harborHint'),
      )}
      {field(
        '',
        <div className="space-y-1.5">
          <label className="flex items-center gap-1.5 text-xs text-foreground">
            <input type="checkbox" checked={f.is_self} onChange={(e) => set('is_self', e.target.checked)} />
            {t('opsversion:org.isSelf')}
          </label>
          {/* 🔴 停用开关。原来只有后端字段、没有界面入口，
              于是停用的平台**再也启用不回来**（OPSVER-016）。 */}
          <label className="flex items-center gap-1.5 text-xs text-foreground">
            <input type="checkbox" checked={f.enabled} onChange={(e) => set('enabled', e.target.checked)} />
            {t('opsversion:org.enabled')}
          </label>
          <div className="text-[11px] text-muted-foreground">{t('opsversion:org.enabledHint')}</div>
        </div>,
      )}

      <div className="mb-2 mt-4 border-b border-border pb-1.5 text-xs font-semibold text-foreground">
        {t('opsversion:org.secConn')}
      </div>
      {/* 🔴 引用数据源。选了之后，地址和凭据都由数据源提供，这里**不再填一遍** ——
          填两遍就有两份真相：改密码时改一处漏一处，
          表现是某个平台悄悄采集失败，而错误写着「认证失败」，
          没人会想到是「另外那处忘了改」。这正是把数据源独立出来的全部意义。 */}
      {field(
        t('opsversion:org.datasource'),
        <Select
          label={t('opsversion:org.datasource')}
          value={String(f.datasource_id)}
          onChange={(v) => {
            const id = Number(v)
            const ds = dsList.find((d) => d.id === id)
            setF((p) => ({
              ...p,
              datasource_id: id,
              // 引用了就跟着数据源的类型走 —— 两者不一致的话，
              // 会拿 rancher 的解析逻辑去读 argocd 的响应，解出来一片空
              provider_type: ds ? ds.provider_type : p.provider_type,
              // ⚠️ 引用后清掉本平台自己填的地址和凭据：留着的话优先级高于数据源
              //    （见后端 OrgEnv.Conn 的三层优先级），等于「选了数据源却没生效」
              endpoint: ds ? '' : p.endpoint,
              username: ds ? '' : p.username,
              password: ds ? '' : p.password,
              api_key: ds ? '' : p.api_key,
            }))
          }}
          options={[
            { value: '0', label: t('opsversion:org.dsNone') },
            ...dsList.map((d) => ({ value: String(d.id), label: `${d.name}（${d.provider_type}）` })),
          ]}
        />,
        usingDS
          ? t('opsversion:org.dsUsing', { name: dsName, endpoint: dsEndpoint })
          : t('opsversion:org.dsHint'),
      )}

      {/* 引用数据源时，下面这些全部由数据源提供，不显示 */}
      {!usingDS && field(
        t('opsversion:org.provider'),
        <div className="flex gap-2">
          <Select
            label={t('opsversion:org.provider')}
            value={f.provider_type}
            onChange={(v) => set('provider_type', v)}
            options={PROVIDERS.map((p) => ({ value: p, label: p }))}
          />
          <Select
            label={t('opsversion:org.authType')}
            value={f.auth_type}
            onChange={(v) => {
              // 🔴 切换认证方式时清掉另一种已填的值。
              //    不清的话：填了密码又改成 api_key，提交时后端同时收到两种凭据，
              //    用哪个取决于实现细节 —— 而人以为自己只配了一种。
              setF((p) => ({
                ...p,
                auth_type: v,
                username: v === 'password' ? p.username : '',
                password: v === 'password' ? p.password : '',
                api_key: isKeyAuth(v) ? p.api_key : '',
              }))
            }}
            options={authOptions(f.provider_type)}
          />
        </div>,
        f.provider_type === 'argocd' ? t('opsversion:org.argocdScopeHint') : undefined,
      )}
      {!usingDS && field(t('opsversion:org.endpoint'), input(f.endpoint, (v) => set('endpoint', v), 'https://kite.example.com'))}
      {/* 🔴 只显示当前认证方式用得上的那几栏。
          三栏always显示时，选了密码认证却还看着一个 API Key 输入框 ——
          人会犹豫「是不是两个都要填」，填了又不知道哪个生效。 */}
      {!usingDS && field(
        t('opsversion:org.credential'),
        isKeyAuth(f.auth_type) ? (
          input(f.api_key, (v) => set('api_key', v), 'API Key', 'password')
        ) : (
          <div className="flex gap-2">
            {input(f.username, (v) => set('username', v), t('opsversion:login.username'))}
            {input(f.password, (v) => set('password', v), t('opsversion:login.password'), 'password')}
          </div>
        ),
        org?.has_credential ? t('opsversion:org.credKeep') : t('opsversion:org.credNone'),
      )}
      {field(
        '',
        <label className="flex items-center gap-1.5 text-xs text-foreground">
          <input type="checkbox" checked={f.insecure_tls} onChange={(e) => set('insecure_tls', e.target.checked)} />
          {t('opsversion:org.insecureTls')}
        </label>,
      )}

      {/* 项目：一家公司下的多个项目。对比表的一列 = 项目 × 环境。
          ⚠️ 放在环境之前 —— 环境要挂到项目下，先有项目才谈得上归属。 */}
      <div className="mb-2 mt-4 border-b border-border pb-1.5 text-xs font-semibold text-foreground">
        {t('opsversion:proj.section')}
      </div>
      <ProjectsSection orgId={org?.id ?? 0} />

      <div className="mb-2 mt-4 border-b border-border pb-1.5 text-xs font-semibold text-foreground">
        {t('opsversion:org.secEnv')}
      </div>
      {/* 🔴 多项目时按项目**分组**渲染，每个项目下各自加环境。
          原来是一个平铺列表 + 每行一个「所属项目」下拉，人看不出
          「这个项目已经配了哪些环境」，于是想给第二个项目配 UAT 时
          直接在末尾又加了一行 UAT —— 撞唯一键（OPSVER-024 就是这么来的）。
          先选项目、再在它下面加环境，才是这件事本来的形状。 */}
      {projects.length > 1 &&
        projects.map((pr) => {
          const mine = envs
            .map((e, i) => ({ e, i }))
            .filter(({ e }) => (e.project_id || projects[0]?.id) === pr.id)
          return (
            <div key={pr.id} className="mb-3 rounded-md border border-border-strong p-2.5">
              <div className="mb-2 flex items-center gap-2">
                <span className="text-xs font-semibold text-foreground">{pr.name}</span>
                <span className="text-[11px] text-muted-foreground">
                  {t('opsversion:org.envOfProject', { n: mine.length })}
                </span>
                <div className="flex-1" />
                <Button
                  onClick={() =>
                    setEnvs((p) => [...p, { ...emptyEnv(), project_id: pr.id }])
                  }
                >
                  {t('opsversion:org.addEnvTo', { name: pr.name })}
                </Button>
              </div>
              {mine.length === 0 ? (
                <div className="rounded-md border border-dashed border-border px-3 py-4 text-center text-[11px] text-muted-foreground">
                  {t('opsversion:org.noEnvYet')}
                </div>
              ) : (
                mine.map(({ i }) => envCard(i))
              )}
            </div>
          )
        })}
      {/* 单项目平台：不分组，就是一个平铺列表 —— 分组框在这里是纯噪音 */}
      {projects.length <= 1 && envs.map((_, i) => envCard(i))}
      {projects.length <= 1 && (
        <Button onClick={() => setEnvs((p) => [...p, emptyEnv()])}>
          {t('opsversion:org.addEnv')}
        </Button>
      )}
    </Dialog>
  )

  function envCard(i: number) {
    const e = envs[i]
    if (!e) return null
    return (
        <div key={i} className="mb-2.5 rounded-md border border-border bg-background p-2.5">
          <div className="mb-2 flex flex-wrap items-center gap-2">
            <Select
              label={t('opsversion:org.env')}
              value={e.env}
              onChange={(v) => setEnv(i, { env: v })}
              options={ENVS.map((x) => ({ value: x, label: x }))}
            />
            {/* 「所属项目」下拉去掉了：环境已经按项目分组渲染，
                归属由它在哪个分组框里决定。留着等于同一件事有两个入口，
                两处不一致时谁也说不清以哪个为准。
                要换项目 = 删掉再到目标项目下加一个（跨项目搬配置本来就该是显式动作）。 */}
            <label className="flex items-center gap-1.5 text-xs text-foreground">
              <input
                type="checkbox"
                checked={e.compare_enabled}
                onChange={(ev) => setEnv(i, { compare_enabled: ev.target.checked })}
              />
              {t('opsversion:org.compareEnabled')}
            </label>
            <span className="text-[11px] text-muted-foreground">{t('opsversion:org.compareHint')}</span>
            <div className="flex-1" />
            <Button onClick={() => pullClusters(i)}>{t('opsversion:org.pullClusters')}</Button>
            {envs.length > 1 && (
              <Button onClick={() => setEnvs((p) => p.filter((_, n) => n !== i))}>
                {t('common:action.delete')}
              </Button>
            )}
          </div>
          {field(t('opsversion:org.clusters'), area(fromLines(e.cluster_refs), (v) => setEnv(i, { cluster_refs: toLines(v) }), t('opsversion:org.clustersPh')))}
          {field(
                t('opsversion:org.nsInclude'),
                area(fromLines(e.ns_include), (v) => setEnv(i, { ns_include: toLines(v) }), t('opsversion:org.nsIncludePh')),
                // ⚠️ 说清楚"留空"的前提：project 级账号读不了全部 ns，
                //    而那正是 Rancher 只读账号的常态 —— 不说的话人会一直卡在 403
                t('opsversion:org.nsScopeHint'),
              )}
          {field(t('opsversion:org.nsExclude'), area(fromLines(e.ns_exclude), (v) => setEnv(i, { ns_exclude: toLines(v) }), t('opsversion:org.nsExcludePh')))}
          {/* workload 规则：ns 太粗。一个 ns 里几十个服务，而各家部署的
              服务集合并不相同 —— 我方 UAT 有的，对方可能压根没有 */}
          {field(t('opsversion:org.wlInclude'), area(fromLines(e.workload_include), (v) => setEnv(i, { workload_include: toLines(v) }), t('opsversion:org.wlIncludePh')))}
          {field(t('opsversion:org.wlExclude'), area(fromLines(e.workload_exclude), (v) => setEnv(i, { workload_exclude: toLines(v) }), t('opsversion:org.wlExcludePh')))}
          {/* 🔴 环境级连接覆盖：客户常有两套 Rancher（UAT 一个、PROD 一个）。
              **整组覆盖不逐字段回落** —— 所以填了地址就必须把认证方式和凭据一起填，
              只填地址的话凭据是空的，采集必然认证失败，而界面上看地址明明填了。 */}
          {field(t('opsversion:org.override'), input(e.endpoint, (v) => setEnv(i, { endpoint: v }), t('opsversion:org.overridePh')), t('opsversion:org.overrideHint'))}
          {/* 🔴 认证方式与凭据**常显**，不藏在「填了地址才出现」的条件里。
              藏起来的结果是功能等于不存在：用户看不到入口，就以为
              「一个环境一个 Rancher」这种场景不支持 —— 实测被这么反馈过。
              留空继承平台级，填了就必须整组填（见下面的提示）。 */}
          {(
            <>
              {field(
                t('opsversion:org.authType'),
                <div className="flex flex-wrap gap-2">
                  <Select
                    label={t('opsversion:org.authType')}
                    value={e.auth_type || 'password'}
                    onChange={(v) =>
                      // 切换时清掉另一种，理由同平台级：两种凭据同时送到后端，
                      // 用哪个取决于实现细节，而人以为自己只配了一种
                      setEnv(i, {
                        auth_type: v,
                        username: v === 'password' ? e.username : '',
                        password: v === 'password' ? e.password : '',
                        api_key: isKeyAuth(v) ? e.api_key : '',
                      })
                    }
                    options={authOptions(f.provider_type)}
                  />
                </div>,
              )}
              {field(
                t('opsversion:org.credential'),
                isKeyAuth(e.auth_type || 'password') ? (
                  input(e.api_key, (v) => setEnv(i, { api_key: v }), 'API Key', 'password')
                ) : (
                  <div className="flex gap-2">
                    {input(e.username, (v) => setEnv(i, { username: v }), t('opsversion:login.username'))}
                    {input(e.password, (v) => setEnv(i, { password: v }), t('opsversion:login.password'), 'password')}
                  </div>
                ),
                e.has_credential
                  ? t('opsversion:org.credKeep')
                  : e.endpoint.trim() !== ''
                    ? t('opsversion:org.envCredRequired')
                    : t('opsversion:org.envCredInherit'),
              )}
            </>
          )}
        </div>
    )
  }
}
