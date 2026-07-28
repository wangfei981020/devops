import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { syncCloudAccount, syncCloudProject, cloudAccountSyncStatus } from '../api/cmdb'

// 云主机同步：触发 + 进度轮询 + 结果提示。
//
// 抽出来是因为「主机」和「接入管理」两个页面都要用，而之前是两套独立实现：
// 主机页有完整的轮询/错误透传/完成刷新，接入管理页只发了个请求就结束——
// 触发后没有进度、错误被 catch 成一句「失败」、完成也不刷新，用起来就像没反应。
// 更要命的是同步失败（比如权限 403）发生在后台协程里，HTTP 早就返回 202 了，
// **只有轮询才拿得到这个错误**，所以不轮询的那个页面永远看不到失败原因。
//
// 进度按项目分开存：以前项目行读的是账号级计数，同一账号下几个项目
// 在界面上会显示完全相同的数字（哪怕其中一个一台主机都没有、还失败了）。
//
// onFinished: 一轮同步结束后的回调，用于刷新列表。
export function useHostSync(onFinished) {
  const syncing = ref({})  // 'a<accountId>' / 'p<projectId>' -> bool
  const acctProg = ref({}) // accountId -> { total, done }
  const projProg = ref({}) // projectId -> { total, done, running, error }

  // 已经提示过完成的项目，避免轮询期间重复弹同一条消息
  let notified = new Set()

  const POLL_MS = 2500

  function clearAcct(accId, pids = []) {
    const { [accId]: _a, ...restA } = acctProg.value
    acctProg.value = restA
    const restP = { ...projProg.value }
    for (const pid of pids) delete restP[pid]
    projProg.value = restP
    const s = { ...syncing.value, ['a' + accId]: false }
    for (const pid of pids) s['p' + pid] = false
    syncing.value = s
  }

  async function poll(accId) {
    let s
    try {
      s = await cloudAccountSyncStatus(accId)
    } catch (e) {
      // 轮询本身失败（网络抖动/后端重启）不该终止——真结束时状态接口会返回 running=false
      setTimeout(() => poll(accId), POLL_MS)
      return
    }

    acctProg.value = { ...acctProg.value, [accId]: { total: s.total || 0, done: s.done || 0 } }

    const seen = []
    const np = { ...projProg.value }
    for (const p of s.projects || []) {
      const pid = p.project_id
      seen.push(pid)
      np[pid] = { total: p.total || 0, done: p.done || 0, running: p.running, error: p.error }
      // 单个项目跑完就立刻给结果，不必等整批——失败的那个尤其要马上说
      if (!p.running && !notified.has(pid)) {
        notified.add(pid)
        syncing.value = { ...syncing.value, ['p' + pid]: false }
        if (p.error) ElMessage.error(`${p.project || '项目'} 同步失败：${p.error}`)
        else ElMessage.success(`${p.project || '项目'} 同步完成：${p.synced} 台，失效 ${p.stale}`)
      }
    }
    projProg.value = np

    if (s.running) {
      setTimeout(() => poll(accId), POLL_MS)
      return
    }
    clearAcct(accId, seen)
    notified = new Set()
    if (typeof onFinished === 'function') onFinished()
  }

  async function syncAccount(accId) {
    syncing.value = { ...syncing.value, ['a' + accId]: true }
    acctProg.value = { ...acctProg.value, [accId]: { total: 0, done: 0 } }
    notified = new Set()
    try {
      const r = await syncCloudAccount(accId)
      // 后端会跳过正在同步中的项目并在 msg 里说明，这句要让用户看到
      ElMessage.success(r?.msg || '已触发同步')
      poll(accId)
    } catch (e) {
      clearAcct(accId)
      ElMessage.error(e.response?.data?.error || '同步启动失败')
    }
  }

  async function syncProject(accId, pid) {
    syncing.value = { ...syncing.value, ['p' + pid]: true }
    projProg.value = { ...projProg.value, [pid]: { total: 0, done: 0, running: true } }
    notified.delete(pid)
    try {
      await syncCloudProject(pid)
      poll(accId)
    } catch (e) {
      const { [pid]: _p, ...rest } = projProg.value
      projProg.value = rest
      syncing.value = { ...syncing.value, ['p' + pid]: false }
      // 409（该项目正在同步中）等后端原文要原样带出，不能吞成一句「失败」
      ElMessage.error(e.response?.data?.error || '同步启动失败')
    }
  }

  // 进度文案：total 还没算出来时显示省略号，避免闪一下 0/0
  function progressText(p) {
    if (!p) return ''
    return `${p.done}/${p.total || '…'}`
  }

  return { syncing, acctProg, projProg, syncAccount, syncProject, progressText }
}
