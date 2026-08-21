import { useTranslation } from '@ops/i18n'
import { useLicense } from '../routes/license/queries.js'
import { can, useSession } from './session.js'

/**
 * 写操作能不能做，以及**不能做的时候是为什么**。
 *
 * 三种拦截原因在界面上必须分开，因为用户的下一步完全不同：
 *   没权限    去找管理员要权限码
 *   只读降级  去续费 / 重新采购（授权页）
 *   还没加载完 等一下就好，别急着点
 *
 * ⚠️ 把它们统一成"按钮置灰"是最省事也最糟的做法：
 * 一个有权限的人看到自己的按钮点不了，第一反应是"系统坏了"。
 */
export type WriteBlock = 'none' | 'pending' | 'no-permission' | 'read-only'

export function useWriteGate(permCode: string): {
  allowed: boolean
  block: WriteBlock
  /** 直接可渲染的原因文案；allowed 时为空串 */
  reason: string
} {
  const { t } = useTranslation()
  const me = useSession()
  const lic = useLicense()

  if (me.isPending || lic.isPending) {
    // ⚠️ 加载中**不能**当成"有权限"：那一瞬间点下去会打到后端，
    // 而后端会按真实权限拒绝 —— 用户看到的是一个随机失败的按钮
    return { allowed: false, block: 'pending', reason: '' }
  }
  if (!can(me.data, permCode)) {
    return { allowed: false, block: 'no-permission', reason: t('common:write.noPermission', { code: permCode }) }
  }
  if (lic.data?.readOnly) {
    return { allowed: false, block: 'read-only', reason: t('common:write.readOnly') }
  }
  return { allowed: true, block: 'none', reason: '' }
}
