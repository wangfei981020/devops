import { useTranslation } from '@ops/i18n'
import { Pwd } from './Pwd.js'
import { useMutation } from '@tanstack/react-query'
import { LayoutGrid, LogOut, Settings2, ShieldCheck } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { ApiError, api } from '../api/client.js'
import type { Identity } from '../api/types.js'

/**
 * 顶栏右上角的「我是谁」。
 *
 * # 为什么必须有
 *
 * 之前顶栏只有主题/语言/偏好三个图标，**页面上没有任何地方写着当前是谁**。
 * 在一个专门管访问控制的系统里，这是最不该缺的一条信息：
 * 排障时第一句话就是"你现在是用哪个账号登的"，而看的人自己答不上来。
 *
 * # 为什么显示用户名而不只是显示名
 *
 * 显示名会重（三个"张伟"），用户名不会。走查时被专门挑出来过：
 * 界面上写着"张伟"，而策略里配的是 `zhangwei3`，对不上号。
 *
 * # 管理入口为什么放在这里
 *
 * 门户和控制台是两个入口、同一个域名。管理员**先是员工再是管理员**，
 * 他从门户落地，需要一个明确的门进控制台 —— 不给入口的话，
 * 他只能靠记住 /console 这个路径，而记不住的人会以为自己没权限。
 *
 * ⚠️ 这个入口按 `is_admin` 显隐，那只是**体验**。真正的拦截在后端
 * adminOnly 中间件里：改一个 JS 变量能让按钮出现，但接口照样 403。
 */
export function UserMenu({ me, showConsoleLink }: { me: Identity; showConsoleLink?: boolean }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [pwdOpen, setPwdOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false)
    }
    const onEsc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onEsc)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onEsc)
    }
  }, [open])

  const logout = useMutation({
    mutationFn: () => api.post('/auth/logout', {}),
    // 成功失败都回登录页：失败多半是会话本来就没了，
    // 停在原地让人以为还登着，比直接回登录页糟。
    onSettled: () => window.location.assign('/'),
  })

  const initial = (me.display_name || me.username).slice(0, 1).toUpperCase()

  return (
    <div className="relative" ref={box}>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
        className="flex cursor-pointer items-center gap-2 rounded-[var(--radius)] px-1.5 py-1 hover:bg-secondary"
      >
        <span className="grid size-6.5 shrink-0 place-items-center rounded-full bg-brand-bg text-[11px] font-semibold text-brand">
          {initial}
        </span>
        {/* 窄屏只留头像：顶栏放不下就横向挤，挤到菜单点不着 */}
        <span className="hidden text-[13px] sm:inline">{me.display_name || me.username}</span>
      </button>

      {open ? (
        <div
          role="menu"
          className="absolute right-0 z-20 mt-1.5 w-60 overflow-hidden rounded-[var(--radius-md)] border border-border bg-card shadow-lg"
        >
          <div className="border-b border-border px-3.5 py-3">
            <b className="block text-[13px] font-medium">{me.display_name || me.username}</b>
            {/* 用户名单独一行、等宽字体：它是配策略时要填的那个值 */}
            <span className="mt-0.5 block font-mono text-[11px] text-muted-foreground">
              {me.username}
            </span>
            <div className="mt-1.5 flex flex-wrap gap-1">
              <span className="rounded-[var(--radius-sm)] bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                {me.is_admin ? t('sso:user.roleAdmin') : t('sso:user.roleMember')}
              </span>
              <span className="rounded-[var(--radius-sm)] bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                {t(`sso:user.source.${me.source}`, { defaultValue: me.source })}
              </span>
              {me.is_break_glass ? (
                // 应急账号必须一眼可辨：它绕开的正是这套系统本身
                <span className="rounded-[var(--radius-sm)] bg-warning-bg px-1.5 py-0.5 text-[10px] text-warning">
                  {t('sso:user.breakGlass')}
                </span>
              ) : null}
            </div>
          </div>

          {showConsoleLink && me.is_admin ? (
            <a
              href="/console"
              className="flex items-center gap-2 px-3.5 py-2.5 text-[13px] hover:bg-muted"
            >
              <ShieldCheck className="size-4 text-muted-foreground" />
              {t('sso:user.toConsole')}
            </a>
          ) : null}
          {!showConsoleLink ? (
            <a href="/" className="flex items-center gap-2 px-3.5 py-2.5 text-[13px] hover:bg-muted">
              <LayoutGrid className="size-4 text-muted-foreground" />
              {t('sso:user.toPortal')}
            </a>
          ) : null}

          <button
            type="button"
            onClick={() => {
              setOpen(false)
              setPwdOpen(true)
            }}
            className="flex w-full cursor-pointer items-center gap-2 px-3.5 py-2.5 text-left text-[13px] hover:bg-muted"
          >
            <Settings2 className="size-4 text-muted-foreground" />
            {t('sso:user.changePassword')}
          </button>

          <button
            type="button"
            disabled={logout.isPending}
            onClick={() => logout.mutate()}
            className="flex w-full cursor-pointer items-center gap-2 border-t border-border px-3.5 py-2.5 text-left text-[13px] hover:bg-muted disabled:opacity-60"
          >
            <LogOut className="size-4 text-muted-foreground" />
            {logout.isPending ? t('sso:user.loggingOut') : t('sso:user.logout')}
          </button>
        </div>
      ) : null}

      {pwdOpen ? <ChangePasswordDialog onClose={() => setPwdOpen(false)} /> : null}
    </div>
  )
}

/**
 * 自助改密。
 *
 * 要验旧口令：会话被盗时，攻击者能做的最有价值的一件事就是改密 ——
 * 那样受害者连自己都进不去了。这一层由后端强制，这里只是把错误说清楚。
 */
function ChangePasswordDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const [oldPwd, setOld] = useState('')
  const [newPwd, setNew] = useState('')
  const [again, setAgain] = useState('')
  const [err, setErr] = useState<string | null>(null)
  const [done, setDone] = useState(false)

  const m = useMutation({
    mutationFn: () => api.post('/auth/password', { old_password: oldPwd, new_password: newPwd }),
    onSuccess: () => {
      setErr(null)
      setDone(true)
    },
    onError: (e) => {
      // 旧口令错 / 太弱 / 和原来一样，三种的下一步完全不同，
      // 统一说"改密失败"等于让人瞎试。
      setErr(
        e instanceof ApiError
          ? t(`errors.${e.code}`, { ...e.params, defaultValue: e.code })
          : t('error.unreachable'),
      )
    },
  })

  const mismatch = again !== '' && newPwd !== again

  return (
    // 点遮罩不关：输了一半的表单被一次误触清空，是最招人烦的一种交互
    <div className="fixed inset-0 z-30 grid place-items-center bg-overlay p-4">
      <div className="w-full max-w-sm rounded-[var(--radius-md)] border border-border bg-card p-5">
        <h2 className="text-sm font-semibold">{t('sso:user.changePassword')}</h2>

        {done ? (
          <>
            <p className="mt-3 rounded-[var(--radius)] bg-success-bg px-3 py-2 text-[12px] text-success">
              {t('sso:user.pwdChanged')}
            </p>
            <button
              type="button"
              onClick={onClose}
              className="mt-3 w-full cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground"
            >
              {t('sso:user.close')}
            </button>
          </>
        ) : (
          <>
            <div className="mt-3 grid gap-2.5">
              <Input label={t('sso:user.oldPwd')} value={oldPwd} onChange={setOld} />
              <Input label={t('sso:user.newPwd')} value={newPwd} onChange={setNew} />
              <Input label={t('sso:user.newPwdAgain')} value={again} onChange={setAgain} />
            </div>
            <p className="mt-2 text-[11px] text-muted-foreground">{t('sso:user.pwdRule')}</p>
            {mismatch ? (
              <p className="mt-2 text-[12px] text-danger">{t('sso:user.pwdMismatch')}</p>
            ) : null}
            {err ? (
              <p className="mt-2 rounded-[var(--radius)] bg-danger-bg px-2 py-1.5 text-[12px] text-danger">
                {err}
              </p>
            ) : null}
            <div className="mt-4 flex gap-2">
              <button
                type="button"
                onClick={onClose}
                className="flex-1 cursor-pointer rounded-[var(--radius)] border border-border px-3 py-1.5 text-[13px] hover:bg-secondary"
              >
                {t('sso:user.cancel')}
              </button>
              <button
                type="button"
                disabled={!oldPwd || !newPwd || mismatch || m.isPending}
                onClick={() => m.mutate()}
                className="flex-1 cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground disabled:cursor-default disabled:opacity-50"
              >
                {m.isPending ? t('sso:user.saving') : t('sso:user.save')}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

function Input({
  label,
  value,
  onChange,
}: {
  label: string
  value: string
  onChange: (v: string) => void
}) {
  return (
    <label className="block text-[12px]">
      <span className="mb-1 block text-muted-foreground">{label}</span>
      <Pwd
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="border-border"
      />
    </label>
  )
}
