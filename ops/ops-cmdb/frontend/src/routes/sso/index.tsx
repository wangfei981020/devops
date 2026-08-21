import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  Banner,
  Field,
  type LoadError,
  SecretInput,
  Select,
  Skeleton,
  Switch,
  TextInput,
  fromQuery,
} from '@ops/ui'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { roleOptions, useRoles } from '../../lib/roles.js'
import { type IdPConfig, useDiscover, useIdPConfig, useSaveIdP } from './queries.js'

const PERM = 'cmdb:manage_users'

/**
 * 单点登录接入。
 *
 * 一整页只做一件事：让 CMDB 去连一个已有的 IdP。
 * 所以不做列表、不做多身份源 —— 绝大多数客户只有一个，
 * 多出来的那层"先选身份源"对所有人都是负担。
 */
export function SSOPage() {
  const { t } = useTranslation()
  const query = useIdPConfig()

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="mx-auto max-w-[840px] p-5">
      <div className="mb-4 flex items-baseline gap-2.5">
        <h1 className="text-sm font-semibold text-foreground">{t('ssoconnect:title')}</h1>
        <p className="min-w-0 flex-1 text-xs text-muted-foreground">{t('ssoconnect:hint')}</p>
      </div>

      <AsyncBoundary
        state={fromQuery<IdPConfig>(query, () => false, toLoadError)}
        errorTitle={t('ssoconnect:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={<Skeleton className="h-5 w-[60%]" />}
        empty={null}
      >
        {(d) => <SSOForm initial={d} />}
      </AsyncBoundary>
    </div>
  )
}

function SSOForm({ initial }: { initial: IdPConfig }) {
  const { t } = useTranslation()
  const [f, setF] = useState({ ...initial, client_secret: '' })
  const save = useSaveIdP()
  const discover = useDiscover()
  // 角色清单从接口取，不写死：写死过一次，界面上给出的两个角色数据库里根本没有
  const roles = roleOptions(useRoles().data)

  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) =>
    setF((p) => ({ ...p, [k]: v }))

  return (
    <div className="flex flex-col gap-5">
      {/* 逃生通道。这句话必须在页面顶部，不能藏在保存按钮旁边的小字里：
          客户开 SSO 之前最怕的就是"配错了以后谁也进不来"，
          而这个担心会直接拖住他不敢开 */}
      <Banner tone="info">
        <span className="font-medium">{t('ssoconnect:localAlwaysOn.title')}</span>
        <span className="mt-0.5 block">{t('ssoconnect:localAlwaysOn.body')}</span>
      </Banner>

      {/* 最近一次失败。放在最上面而不是折叠起来：
          管理员打开这一页，十有八九就是因为有人登不进来 */}
      {initial.last_error ? (
        <Banner tone="bad">
          <span className="font-medium">{t('ssoconnect:lastError.title')}</span>
          <span className="mt-0.5 block">
            {t(`ssoconnect:login.reason.${initial.last_error}`, {
              defaultValue: t('ssoconnect:login.reason.unknown'),
            })}
          </span>
          <span className="mt-1 block text-[11px] opacity-80">
            {t('ssoconnect:lastError.at')} {initial.last_error_at}
            {initial.last_error_user ? ` · ${t('ssoconnect:lastError.who')} ${initial.last_error_user}` : ''}
          </span>
          {initial.last_claims?.length ? (
            <div className="mt-2">
              <div className="text-[11px] font-medium">{t('ssoconnect:lastError.claimsTitle')}</div>
              <div className="mt-1 flex flex-wrap gap-1">
                {initial.last_claims.map((c) => (
                  // 可点：点一下直接填进「用户名取自」。
                  // 只列出来的话，管理员还得手打一遍，打错了又是一轮往返
                  <button
                    key={c}
                    type="button"
                    onClick={() => set('username_claim', c)}
                    className="cursor-pointer rounded border border-current/30 px-1.5 py-0.5 font-mono text-[11px] hover:bg-current/10"
                  >
                    {c}
                  </button>
                ))}
              </div>
              <div className="mt-1 text-[11px] opacity-80">{t('ssoconnect:lastError.claimsHint')}</div>
            </div>
          ) : null}
        </Banner>
      ) : null}

      <section className="flex flex-col gap-3 rounded-md border border-border p-4">
        <div className="flex items-center justify-between">
          <div className="text-xs font-medium text-foreground">{t('ssoconnect:section.connect')}</div>
          {initial.enabled ? (
            <Badge tone="ok">{t('ssoconnect:state.on')}</Badge>
          ) : (
            <Badge tone="mute">{t('ssoconnect:state.off')}</Badge>
          )}
        </div>

        <Field label={t('ssoconnect:field.issuer')} hint={t('ssoconnect:field.issuerHint')}>
          <div className="flex gap-2">
            <TextInput
              value={f.issuer}
              onChange={(e) => set('issuer', e.target.value)}
              placeholder="https://example.okta.com"
            />
            <WriteButton
              perm={PERM}
              size="sm"
              loading={discover.isPending}
              blockedReason={f.issuer ? undefined : t('ssoconnect:field.issuerHint')}
              onClick={() => discover.mutate({ issuer: f.issuer, allow_private: f.allow_private })}
            >
              {t('ssoconnect:action.discover')}
            </WriteButton>
          </div>
        </Field>

        {/* 探测结果。成功要把**取到的端点**列出来，而不是只回一句"连通"：
            issuer 打错一个字母也可能连上另一个真实存在的 IdP，
            只有看到端点域名对不对，才谈得上确认 */}
        {discover.isSuccess ? (
          <Banner tone="info">
            <span className="font-medium">{t('ssoconnect:discover.ok')}</span>
            <div className="mt-1 flex flex-col gap-0.5 font-mono text-[11px]">
              <span>authorization: {discover.data.authorization_endpoint}</span>
              <span>token: {discover.data.token_endpoint}</span>
              <span>jwks: {discover.data.jwks_uri}</span>
            </div>
          </Banner>
        ) : null}
        {discover.isError ? (
          <Banner tone="bad">
            <span className="font-medium">
              {tError(t, toErrorInfo(discover.error).messageKey, toErrorInfo(discover.error).params)}
            </span>
            <span className="mt-0.5 block">{toErrorInfo(discover.error).detail}</span>
          </Banner>
        ) : null}

        {/* 内网开关紧挨着 issuer：它改变的正是上面那一行的判定规则。
            放到页面底部"高级设置"里的话，被拒的人不会想到往下翻 */}
        <div className="flex flex-col gap-1.5">
          <Switch
            checked={f.allow_private}
            onChange={(v) => set('allow_private', v)}
            label={t('ssoconnect:field.allowPrivate')}
          />
          <span className="text-xs leading-relaxed text-muted-foreground">
            {t('ssoconnect:field.allowPrivateHint')}
          </span>
        </div>

        <Field label={t('ssoconnect:field.clientId')}>
          <TextInput value={f.client_id} onChange={(e) => set('client_id', e.target.value)} />
        </Field>

        <Field
          label={t('ssoconnect:field.clientSecret')}
          hint={initial.has_secret ? t('common:write.secretKeep') : t('ssoconnect:field.secretHint')}
        >
          <SecretInput
            configured={initial.has_secret}
            placeholderConfigured="••••••••" 
            placeholderEmpty=""
            showLabel={t('common:action.showSecret')}
            hideLabel={t('common:action.hideSecret')}
            value={f.client_secret}
            onChange={(e) => set('client_secret', e.target.value)}
          />
        </Field>

        {/*
          回调地址由后端按当前访问地址算好。让客户自己拼的话，
          拼错的代价是一句 redirect_uri_mismatch —— 那句话既不说期望值
          也不说实际值，是接 OIDC 最常卡住的一步。

          # 🔴 但后端推导可能是错的，而这一页必须能看出来

          后端从请求头推 scheme（优先 X-Forwarded-Proto）。TLS 终止在入口网关、
          而网关**没有**传这个头时，它会拼出 `http://`，
          而页面本身跑在 `https://` 上（OPSCMDB-031 P0-22 实测就是这样）。

          照着一个 http 的值去登记，得到的正是这段说明所警告的那个错误 ——
          说明书亲手把人引到了坑里。

          浏览器知道真实协议（location.protocol），后端不知道。
          所以这个比对**只能在前端做**：对不上就直接把正确值给出来。
        */}
        {/*
          🔴 输入框里显示**修正后**的值，而不是后端那个可能错的值。

          原来的做法是：框里放后端算的值，下面用红字提示"协议不对，正确的是 xxx"。
          那仍然不够 —— 人复制的是**框里**那个（只读框天然让人以为它就是答案），
          红字在框的下面，复制动作发生在读到它之前。
          于是「已经提示了」和「客户照着错的登记了」可以同时成立。

          实测：生产上后端返回的仍是 http://（v0.109.0 里代码已经修了三条退路，
          说明运行时那三条头一条都没命中——根因未查清，见 OPSCMDB-031 P0-22）。
          在根因查清之前，**至少不能让这一页把人引向错的那个值**。

          ⚠️ 红字提示保留：值被悄悄改掉而不说，人就不知道后端配置还有问题。
        */}
        <Field label={t('ssoconnect:field.redirectUri')} hint={t('ssoconnect:field.redirectHint')}>
          <TextInput value={correctedRedirectURI(f.redirect_uri)} readOnly />
        </Field>
        <RedirectSchemeCheck value={f.redirect_uri} t={t} />

        <Field label={t('ssoconnect:field.scopes')} hint={t('ssoconnect:field.scopesHint')}>
          <TextInput value={f.scopes} onChange={(e) => set('scopes', e.target.value)} />
        </Field>
      </section>

      <section className="flex flex-col gap-3 rounded-md border border-border p-4">
        <div className="text-xs font-medium text-foreground">{t('ssoconnect:section.mapping')}</div>
        <Field label={t('ssoconnect:field.usernameClaim')} hint={t('ssoconnect:field.usernameClaimHint')}>
          <TextInput value={f.username_claim} onChange={(e) => set('username_claim', e.target.value)} />
        </Field>
        <Field label={t('ssoconnect:field.nameClaim')}>
          <TextInput value={f.name_claim} onChange={(e) => set('name_claim', e.target.value)} />
        </Field>
      </section>

      <section className="flex flex-col gap-3 rounded-md border border-border p-4">
        <div className="text-xs font-medium text-foreground">{t('ssoconnect:section.jit')}</div>
        <Switch
          checked={f.jit_enabled}
          onChange={(v) => set('jit_enabled', v)}
          label={t('ssoconnect:field.jit')}
        />
        {/* 席位提醒只在打开时出现。常态化的警告会被无视，
            而这句话恰恰是打开这个开关时唯一需要知道的后果 */}
        {f.jit_enabled ? (
          <Banner tone="warn">
            <span className="font-medium">{t('ssoconnect:jit.seatsTitle')}</span>
            <span className="mt-0.5 block">{t('ssoconnect:jit.seatsBody')}</span>
          </Banner>
        ) : null}
        <div className="flex flex-col gap-1.5">
          <Select<string>
            label={t('ssoconnect:field.jitRole')}
            value={f.jit_role_code}
            onChange={(v) => set('jit_role_code', v)}
            options={roles}
            className="self-start"
          />
          <span className="text-xs leading-relaxed text-muted-foreground">
            {t('ssoconnect:field.jitRoleHint')}
          </span>
        </div>
      </section>

      <div className="flex items-center gap-3">
        <Switch
          checked={f.enabled}
          onChange={(v) => set('enabled', v)}
          label={t('ssoconnect:field.enable')}
        />
        <div className="ml-auto flex items-center gap-2">
          {save.isError ? (
            <span className="max-w-[420px] truncate text-xs text-danger" title={toErrorInfo(save.error).detail}>
              {tError(t, toErrorInfo(save.error).messageKey, toErrorInfo(save.error).params)}
            </span>
          ) : null}
          {save.isSuccess ? (
            <span className="text-xs text-success">{t('common:write.saved')}</span>
          ) : null}
          <WriteButton
            perm={PERM}
            variant="primary"
            size="sm"
            loading={save.isPending}
            onClick={() =>
              save.mutate({
                enabled: f.enabled,
                display_name: f.display_name,
                issuer: f.issuer,
                client_id: f.client_id,
                // 空串 = 不动原密钥。这一条必须传空而不是 undefined，
                // 否则 JSON 里字段消失，后端分不清"没改"和"没传"
                client_secret: f.client_secret,
                scopes: f.scopes,
                username_claim: f.username_claim,
                name_claim: f.name_claim,
                jit_enabled: f.jit_enabled,
                jit_role_code: f.jit_role_code,
                allow_private: f.allow_private,
              })
            }
          >
            {t('common:write.save')}
          </WriteButton>
        </div>
      </div>
    </div>
  )
}

/**
 * 校验后端推出来的回调地址，协议和当前页面对不对得上。
 *
 * ⚠️ 只在**对不上**时出现。协议一致时不占版面 ——
 * 一个永远显示的"检查通过"会被当成装饰，真出问题时反而看不见。
 */
function RedirectSchemeCheck({
  value,
  t,
}: {
  value: string
  t: (k: string, p?: Record<string, unknown>) => string
}) {
  if (!value) return null
  // 浏览器里 location.protocol 带冒号（"https:"）
  const pageScheme = window.location.protocol.replace(':', '')
  const uriScheme = value.split('://')[0]
  if (!uriScheme || uriScheme === pageScheme) return null

  // 把正确的那一行直接给出来：只说"协议不对"还要人自己改，改的时候又会出错
  const corrected = `${pageScheme}://${value.split('://')[1] ?? ''}`
  return (
    <Banner tone="bad">
      <span className="font-medium">{t('ssoconnect:redirectMismatch.title')}</span>
      <span className="mt-0.5 block">
        {t('ssoconnect:redirectMismatch.body', { page: pageScheme, uri: uriScheme })}
      </span>
      <span className="mt-1 block font-mono text-[11px] text-foreground">{corrected}</span>
      <span className="mt-1 block">{t('ssoconnect:redirectMismatch.fix')}</span>
    </Banner>
  )
}


/**
 * 把后端算出的回调地址纠正成**页面实际使用的协议**。
 *
 * 🔴 浏览器知道真实协议，后端不知道（它只能从请求头猜，而入口网关可能不传
 * X-Forwarded-Proto）。所以这个纠正只能在前端做。
 *
 * ⚠️ 只改 scheme，host 和 path 原样保留 —— 那两段后端算得对，
 *	而"顺手多改一点"会引入新的不一致。
 * ⚠️ 空值原样返回：没值时返回一个拼出来的地址会让人以为后端给了答案。
 */
function correctedRedirectURI(v: string): string {
  if (!v || typeof window === 'undefined') return v
  const i = v.indexOf('://')
  if (i <= 0) return v
  return window.location.protocol.replace(':', '') + v.slice(i)
}
