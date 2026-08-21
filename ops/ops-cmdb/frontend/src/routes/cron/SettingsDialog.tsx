import { useTranslation } from '@ops/i18n'
import { Banner, Button, Dialog, Field, MultiSelect, Select, Switch, TextInput } from '@ops/ui'
import { useState } from 'react'
import { useLarkGroups, useNotifyUsers } from '../notify/queries.js'
import { type ScheduledTask, type TaskSettingsBody, useUpdateTaskSettings } from './queries.js'

/**
 * 定时任务的频率与通知设置。
 *
 * 🔴 这五项后端一直收着、库里一直生效，而界面上**既看不见也改不了**
 * （OPSCMDB-039 / OPSCMDB-028 GAP-13）。旧版 Cron.vue 全都有。
 * 「任务失败了会不会通知、发给谁」这种事只能改库，是不可接受的。
 */
export function TaskSettingsDialog({
  task,
  onClose,
}: {
  task: ScheduledTask
  onClose: () => void
}) {
  const { t } = useTranslation()
  const groups = useLarkGroups()
  const users = useNotifyUsers()
  const save = useUpdateTaskSettings()

  const [schedule, setSchedule] = useState(task.schedule)
  const [notifyEnabled, setNotifyEnabled] = useState(task.notify_enabled === 1)
  const [groupId, setGroupId] = useState(task.lark_group_id ?? '')
  const [notifyWhen, setNotifyWhen] = useState(task.notify_when || 'always')
  const [atUsers, setAtUsers] = useState<string[]>(task.at_user_ids ?? [])

  // cron 的合法性由**后端**判（它有现成的 5 字段校验并返回中文提示）。
  // 前端只挡住"字段数明显不对"这种一眼可见的错，避免把一次往返浪费在低级错误上。
  // ⚠️ 不要在前端重写一套完整校验：两套判据迟早分叉，
  //	而分叉的结果是前端放行、后端拒绝，或者更坏——前端拦下后端本来接受的写法。
  const fieldCount = schedule.trim().split(/\s+/).filter(Boolean).length
  const scheduleShapeBad = schedule.trim() !== '' && fieldCount !== 5

  /**
   * 只提交**改动过**的字段。
   *
   * ⚠️ 全量提交的话，两个人同时开着这个弹窗时，后保存的那个会把
   * 前一个的改动整体覆盖回自己打开时的旧值 —— 而且没有任何冲突提示。
   */
  const patch = (): TaskSettingsBody => {
    const p: TaskSettingsBody = {}
    if (schedule !== task.schedule) p.schedule = schedule.trim()
    if ((task.notify_enabled === 1) !== notifyEnabled) p.notify_enabled = notifyEnabled ? 1 : 0
    if (groupId !== (task.lark_group_id ?? ''))
      // 空 = 不指定群。必须发 null 而不是 0 或 ''：
      // 0 会被当成一个真实的群 id 存进去，然后通知发不出去且查不出为什么
      p.lark_group_id = groupId === '' ? null : Number(groupId)
    if (notifyWhen !== (task.notify_when || 'always')) p.notify_when = notifyWhen
    const before = [...(task.at_user_ids ?? [])].sort().join(',')
    if ([...atUsers].sort().join(',') !== before) p.at_user_ids = atUsers
    return p
  }

  const changed = Object.keys(patch()).length > 0

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('cron:settings.title', { name: task.name })}
      description={t('cron:settings.desc')}
      closeLabel={t('common:action.close')}
      width={560}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t('common:action.cancel')}
          </Button>
          <Button
            onClick={() => {
              save.mutate({ key: task.task_key, patch: patch() }, { onSuccess: onClose })
            }}
            // 没改动就不该能点：一次"保存"什么都没发出去，
            // 却弹出"保存成功"，会让人以为改动生效了
            disabled={!changed || scheduleShapeBad || save.isPending}
          >
            {save.isPending ? t('common:action.saving') : t('common:action.save')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        {/* 保存失败必须说出来。后端此前把这四个字段的错误全丢掉、照样返回 200，
            于是"保存成功"了但没存上 —— 后端已修，前端这一侧也不能吞。 */}
        {save.isError ? (
          <Banner tone="bad">
            <span>{t('cron:settings.saveFailed', { err: String(save.error) })}</span>
          </Banner>
        ) : null}

        <Field label={t('cron:settings.schedule')} hint={t('cron:settings.scheduleHint')}>
          <TextInput
            value={schedule}
            onChange={(e) => setSchedule(e.target.value)}
            invalid={scheduleShapeBad}
            placeholder="0 3 * * *"
            className="font-mono"
          />
        </Field>
        {scheduleShapeBad ? (
          <Banner tone="warn">
            <span>{t('cron:settings.scheduleShape', { n: fieldCount })}</span>
          </Banner>
        ) : null}

        <Switch
          checked={notifyEnabled}
          onChange={setNotifyEnabled}
          label={t('cron:settings.notifyEnabled')}
        />

        {/* 通知关掉时下面三项没有意义，直接不显示 ——
            留一组灰着的控件会让人以为"配了但不生效"，
            而实际是"根本不会发"。 */}
        {notifyEnabled ? (
          <>
            <Field label={t('cron:settings.larkGroup')}>
              <Select<string>
                label={t('cron:settings.larkGroup')}
                value={String(groupId)}
                onChange={setGroupId}
                options={[
                  { value: '', label: t('cron:settings.noGroup') },
                  ...(groups.data?.items ?? []).map((g) => ({ value: String(g.id), label: g.name })),
                ]}
                // 一个群都没配时说清楚原因和去哪配，而不是给一个空下拉
                disabledReason={
                  (groups.data?.items.length ?? 0) === 0 && !groups.isPending
                    ? t('cron:settings.noGroupsHint')
                    : undefined
                }
              />
            </Field>
            <Field label={t('cron:settings.notifyWhen')}>
              <Select<string>
                label={t('cron:settings.notifyWhen')}
                value={notifyWhen}
                onChange={setNotifyWhen}
                // ⚠️ 只有这两个值。后端判的是 `notifyWhen == "fail" && ok → 不发`
                //	（handlers/scheduler.go），别的值一律等同于 always
                options={[
                  { value: 'always', label: t('cron:settings.whenAlways') },
                  { value: 'fail', label: t('cron:settings.whenFail') },
                ]}
              />
            </Field>
            <Field label={t('cron:settings.atUsers')} hint={t('cron:settings.atUsersHint')}>
              <MultiSelect
                label={t('cron:settings.atUsers')}
                value={atUsers}
                onChange={setAtUsers}
                placeholder={t('cron:settings.noAtUsers')}
                summary={(n, total) => t('common:multiSelect.summary', { n, total })}
                searchPlaceholder={t('common:filter.searchPlaceholder')}
                clearLabel={t('common:filter.clearAll')}
                selectAllLabel={t('common:multiSelect.selectAll')}
                emptyLabel={t('cron:settings.noAtUsersAvailable')}
                // ⚠️ 只列**启用**的通知人：停用的人选进去不会被 @，
                //	而界面上看着像配好了
                options={(users.data ?? [])
                  .filter((u) => u.enabled === 1)
                  .map((u) => ({ value: String(u.id), label: u.name }))}
              />
            </Field>
          </>
        ) : null}
      </div>
    </Dialog>
  )
}
