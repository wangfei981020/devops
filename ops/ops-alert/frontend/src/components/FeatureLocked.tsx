import { useTranslation } from '@ops/i18n'
import { Lock } from 'lucide-react'
import type { ReactNode } from 'react'
import { useLicense } from '../routes/license/queries.js'

/**
 * 企业版功能未授权时的占位。
 *
 * # 为什么不能沿用错误态
 *
 * 402 走通用错误处理的话，界面上是「加载失败：feature_not_licensed」——
 * 一句机器码，读起来像系统坏了。客户会去报障，而正确的动作是联系采购。
 *
 * # 为什么也不能沿用空态
 *
 * 回放实验室原来就是这样：接口 402，页面照常渲染出完整表单，
 * 右侧写着「还没有发起回放」—— 读作"只是还没跑过"。
 * **把"没授权"显示成"暂时没数据"**，正是这个产品自己要根治的那类误读。
 */
export function FeatureLocked({ feature, children }: { feature: string; children: ReactNode }) {
  const { t } = useTranslation()
  const lic = useLicense()

  // ⚠️ 授权还没查回来时**照常渲染内容**，不显示锁。
  // 显示锁的话，每次刷新都会先闪一下"需要授权"再变回正常页面 ——
  // 那一瞬间足以让人以为授权出了问题。
  if (!lic.data) return <>{children}</>

  const st = lic.data.features[feature]
  if (st?.has) return <>{children}</>

  // 三态：没买 / 买了但这版没做。两者的下一步动作完全不同，
  // 合并成一句"不可用"会让已经买了的客户去催采购
  const notImplemented = st?.granted && !st.implemented
  return (
    <div className="flex flex-col items-start gap-3 rounded-lg border border-border bg-card px-6 py-8">
      <Lock className="size-5 text-muted-foreground" aria-hidden="true" />
      <h2 className="text-sm font-semibold">
        {t(`opsalert:license.f.${feature}`, feature)}
        <span className="ml-2 rounded bg-secondary px-1 text-[9px] tracking-wide text-muted-foreground uppercase">
          EE
        </span>
      </h2>
      <p className="max-w-[52ch] text-xs leading-relaxed text-muted-foreground">
        {notImplemented
          ? t('opsalert:license.lockedNotImplemented')
          : t('opsalert:license.lockedNeedsLicense')}
      </p>
      {!notImplemented && (
        <p className="text-2xs text-muted-foreground">{t('opsalert:license.lockedWhere')}</p>
      )}
    </div>
  )
}
