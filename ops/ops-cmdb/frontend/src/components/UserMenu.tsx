import { useTranslation } from '@ops/i18n'
import { MenuItem, MenuSeparator, Popover, cn } from '@ops/ui'
import { ChevronDown, LogOut, RefreshCw, UserCog } from 'lucide-react'

export interface CurrentUser {
  name: string
  /** 登录名 / 邮箱，显示在菜单里用于确认「我现在是谁」 */
  account: string
  /** 头像 URL。没有就退回首字母 —— 不要用占位图，占位图看起来像加载失败 */
  avatarUrl?: string
}

/**
 * 右上角用户区。
 *
 * 头像**加用户名**一起显示，而不是只放头像：
 * 运维平台常有多账号切换（自己的号 / 值班号 / 只读号），
 * 只有头像的话得先点开才知道当前是谁，而误用错账号做写操作的代价很高。
 */
export function UserMenu({
  user,
  onSignOut,
  onChangePassword,
  onRefreshPerms,
}: {
  user: CurrentUser
  onSignOut: () => void
  /**
   * 改密码。传 undefined = 不显示这一项。
   *
   * ⚠️ 非本地账号（走运维平台/SSO 登录）必须传 undefined：
   * 后端会拒绝，而且理由是"你的密码不在这儿" —— 给一个点了必然报错的入口
   * 比没有入口更糟。
   */
  onChangePassword?: () => void
  /**
   * 刷新我的权限。传 undefined = 不显示（本地账号没有可刷的）。
   *
   * 🔴 管理员在运维平台改完角色后，用户这边**要等会话过期才生效** ——
   * 中间那段时间菜单是错的：撤了权限的还看得见，加了权限的还看不见。
   * 后端 /api/refresh-permissions 一直都在，只是没有入口（OPSCMDB-023 第三档）。
   */
  onRefreshPerms?: () => void
}) {
  const { t } = useTranslation()
  const initials = user.name.slice(0, 2).toUpperCase()

  return (
    <Popover
      align="end"
      panelClassName="w-[212px] py-1"
      trigger={(p) => (
        <button
          type="button"
          {...p}
          className={cn(
            'flex cursor-pointer items-center gap-2 rounded-[var(--radius)] py-1 pr-1.5 pl-1',
            'transition-colors duration-150 hover:bg-secondary',
            p['aria-expanded'] && 'bg-secondary',
          )}
        >
          <Avatar name={initials} url={user.avatarUrl} />
          <span className="max-w-[112px] truncate text-[13px] text-foreground">{user.name}</span>
          <ChevronDown className="size-3.5 text-muted-foreground" />
        </button>
      )}
    >
      <div className="border-b border-border px-3 py-2">
        <p className="text-[11px] text-muted-foreground">{t('user.signedInAs')}</p>
        <p className="truncate text-[13px] font-medium text-foreground">{user.name}</p>
        <p className="truncate font-mono text-[11px] text-muted-foreground">{user.account}</p>
      </div>

      {/* ⚠️ 原来这里是 `<MenuItem icon={<UserCog />}>{t('user.profile')}</MenuItem>` ——
          **没有 onClick**。一个点了什么都不发生的菜单项，不报错、不变灰，
          看上去和能用的一模一样。 */}
      {onChangePassword ? (
        <MenuItem icon={<UserCog />} onClick={onChangePassword}>
          {t('user.pw.entry')}
        </MenuItem>
      ) : null}
      {onRefreshPerms ? (
        <MenuItem icon={<RefreshCw />} onClick={onRefreshPerms}>
          {t('user.refreshPerms')}
        </MenuItem>
      ) : null}
      <MenuSeparator />
      <MenuItem icon={<LogOut />} danger onClick={onSignOut}>
        {t('user.logout')}
      </MenuItem>
    </Popover>
  )
}

function Avatar({ name, url }: { name: string; url?: string }) {
  if (url) {
    return (
      <img
        src={url}
        alt=""
        className="size-6 rounded-full object-cover"
        // 头像加载失败时退回首字母，而不是留一个碎图标 ——
        // 碎图会让人以为整个页面出问题了
        onError={(e) => {
          e.currentTarget.style.display = 'none'
        }}
      />
    )
  }
  return (
    <span className="grid size-6 place-items-center rounded-full bg-secondary text-[10px] font-semibold text-foreground">
      {name}
    </span>
  )
}
