import { formatList } from '@ops/i18n'
import { Badge } from '@ops/ui'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { useState } from 'react'
import { NAV } from '../../layouts/nav.js'
import type { Role } from '../../lib/roles.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

/**
 * 角色清单 + 每个角色**具体能做什么**。
 *
 * # 为什么这个面板必须存在
 *
 * 页面叫「用户与角色」，但原来只有用户列表 —— 6 个内置角色只在
 * 「添加用户」弹窗的下拉里出现一次，**看不到任何一个角色能做什么**。
 * 「CMDB 资产管理员」和「CMDB 集群管理员」的区别，界面上无从得知，
 * 于是分配角色只能靠名字猜（OPSCMDB-031 P1-67）。
 *
 * 猜错的后果是不对称的：猜窄了，对方每个页面 403 会来找你；
 * 猜宽了，**没有人会来告诉你** —— 多出来的那部分权限就一直挂在那儿。
 *
 * # ⚠️ 权限码的中文名从哪来
 *
 * `menu:*` 一律从 `nav.ts` 反查 —— 那是菜单的唯一数据源，权限码本来
 * 就是从那儿定义的。在这里再写一份「码 → 菜单名」的表，两份必然分叉，
 * 而分叉的表现是**这个面板说的和用户实际看到的菜单不一样**，
 * 比没有这个面板更糟。
 *
 * `cmdb:*`（动作权限）没有菜单可反查，只能查语言包。
 *
 * ⚠️ 查不到的码**照原样显示**并标出来，不能悄悄丢掉：
 * 丢掉的话，一个新加的权限码会让这个角色看起来比实际权限小，
 * 而"看起来权限小"正是这个面板要防的那件事的反面。
 */
export function RolePanel({ roles, t }: { roles: Role[]; t: TFn }) {
  const [open, setOpen] = useState(false)

  return (
    <section className="mt-6 rounded-[var(--radius)] border border-border">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full cursor-pointer items-baseline gap-2 px-3 py-2.5 text-left"
      >
        {open ? (
          <ChevronDown className="size-3.5 shrink-0 translate-y-0.5 text-muted-foreground" />
        ) : (
          <ChevronRight className="size-3.5 shrink-0 translate-y-0.5 text-muted-foreground" />
        )}
        <span className="text-[13px] font-medium text-foreground">{t('users:roles.title')}</span>
        <span className="min-w-0 flex-1 text-xs text-muted-foreground">
          {t('users:roles.hint', { count: roles.length })}
        </span>
      </button>

      {open ? (
        <div className="flex flex-col border-t border-border">
          {roles.map((r) => (
            <RoleRow key={r.code} role={r} t={t} />
          ))}
        </div>
      ) : null}
    </section>
  )
}

function RoleRow({ role, t }: { role: Role; t: TFn }) {
  const groups = groupPerms(role.permissions ?? [], t)

  return (
    <div className="border-b border-border px-3 py-2.5 last:border-b-0">
      <div className="flex items-baseline gap-2">
        <span className="text-[13px] font-medium text-foreground">{role.name}</span>
        <code className="font-mono text-[11px] text-muted-foreground">{role.code}</code>
        {role.is_builtin ? <Badge tone="mute">{t('users:roles.builtin')}</Badge> : null}
      </div>
      {role.description ? (
        <p className="mt-1 text-xs text-muted-foreground">{role.description}</p>
      ) : null}

      {/* 🔴 管理员这一档必须单独说清楚：它的权限**不是**靠权限码生效的。
          把它和别的角色一样列一串码，会让人以为"这些码就是它的全部权限"，
          于是想收窄它 —— 而收窄不掉，因为拦截根本不看这些码 */}
      {role.unrestricted ? (
        <p className="mt-1.5 text-xs text-warning">{t('users:roles.unrestrictedNote')}</p>
      ) : groups.length === 0 ? (
        // 空权限的角色是有意义的（比如刚建出来还没配），但它必须看得出来
        // 是"确实一条都没有"，而不是"没取到"
        <p className="mt-1.5 text-xs text-muted-foreground">{t('users:roles.noPerms')}</p>
      ) : (
        <dl className="mt-1.5 flex flex-col gap-1">
          {groups.map((g) => (
            <div key={g.key} className="flex gap-2 text-xs">
              <dt className="w-[72px] shrink-0 text-muted-foreground">{g.label}</dt>
              {/* whitespace-normal：这一串可能很长，必须折行 */}
              <dd className="min-w-0 flex-1 whitespace-normal break-words text-foreground">
                {formatList(t, g.names)}
              </dd>
            </div>
          ))}
        </dl>
      )}
    </div>
  )
}

/**
 * 不挂在菜单上的权限码。
 *
 * ⚠️ 这一小张表是**第二份真相**，所以只放确实没有菜单项的码：
 *   menu:cmdb            产品总开关（withParent 会给每个角色加上）
 *   menu:cmdb_task_runs  执行记录，是定时任务页里的一块，不是独立菜单
 *   read:*               MCP / 观测接口那一档只读能力，没有页面
 *
 * 认不出来的仍然归「未识别」原样显示 —— 宁可多显示一条码，
 * 也不能把一条真实存在的权限从清单里抹掉。
 */
function extraLabel(code: string, t: TFn): string | null {
  const map: Record<string, string> = {
    'menu:cmdb': 'users:permx.root',
    'menu:cmdb_task_runs': 'users:permx.taskRuns',
    'read:k8s_diag': 'users:permx.k8sDiag',
    'read:loki': 'users:permx.loki',
    'read:pod_logs': 'users:permx.podLogs',
    'read:prom_query': 'users:permx.promQuery',
  }
  const key = map[code]
  if (!key) return null
  const label = t(key)
  return label === key ? null : label
}

/** 权限码 → 菜单名。同一个码可能挂在多个菜单项上（同名的只留一个） */
function menuLabels(t: TFn): Map<string, { group: string; name: string }> {
  const m = new Map<string, { group: string; name: string }>()
  for (const g of NAV) {
    for (const it of g.items) {
      if (!m.has(it.perm)) m.set(it.perm, { group: t(g.labelKey), name: t(it.labelKey) })
    }
  }
  return m
}

interface PermGroup {
  key: string
  label: string
  names: string[]
}

function groupPerms(perms: string[], t: TFn): PermGroup[] {
  const menus = menuLabels(t)
  const byGroup = new Map<string, Set<string>>()
  const actions: string[] = []
  const unknown: string[] = []

  const push = (group: string, name: string) => {
    const set = byGroup.get(group) ?? new Set<string>()
    set.add(name)
    byGroup.set(group, set)
  }

  for (const p of perms) {
    // 不挂在任何菜单上的码：产品总开关、子页面、以及 read:* 那一档。
    // 它们本来就没有 nav 项可反查，不属于"对不上"
    const extra = extraLabel(p, t)
    if (extra) {
      push(t('users:roles.otherGroup'), extra)
      continue
    }
    if (p.startsWith('menu:')) {
      const hit = menus.get(p)
      // 反查不到 = 后端有这个菜单权限但前端菜单里没有对应项。
      // 这是真的对不上，得看得见（check-perm-codes.mjs 管的是反方向）
      if (hit) push(hit.group, hit.name)
      else unknown.push(p)
      continue
    }
    if (p.startsWith('cmdb:')) {
      const key = `users:perm.${p.slice('cmdb:'.length)}`
      const label = t(key)
      // i18next 查不到 key 时原样返回 key 本身
      if (label === key) unknown.push(p)
      else actions.push(label)
      continue
    }
    unknown.push(p)
  }

  const out: PermGroup[] = []
  for (const [group, names] of byGroup) {
    out.push({ key: `m:${group}`, label: group, names: [...names] })
  }
  if (actions.length > 0) {
    out.push({ key: 'actions', label: t('users:roles.actions'), names: actions })
  }
  if (unknown.length > 0) {
    out.push({ key: 'unknown', label: t('users:roles.unknownPerm'), names: unknown })
  }
  return out
}
