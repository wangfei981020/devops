<template>
  <div :class="{ page: !embedded }">
    <div v-if="!embedded" class="page-head">
      <span class="page-title">云账号</span>
      <span class="muted" style="margin-left:10px">全局共享：主机 / K8s / 成本 都用它。账号=业务分组；凭据(SA key)配在项目层，每 GCP project 一份</span>
      <el-button type="primary" size="small" style="float:right" @click="openAcct()">+ 添加云账号</el-button>
    </div>
    <!-- 嵌进「接入管理」的 tab 时不重复显示页标题，但按钮和说明要留着 -->
    <div v-else style="margin-bottom:10px">
      <el-button type="primary" size="small" @click="openAcct()">+ 添加云账号</el-button>
      <span class="muted" style="margin-left:10px">全局共享：主机 / K8s / 成本 都用它。账号=业务分组；凭据(SA key)配在项目层，每 GCP project 一份</span>
    </div>
    <LoadError :error="error" title="云账号未加载" @retry="load" />
    <el-card :shadow="embedded ? 'never' : 'never'" :body-style="embedded ? { padding: '0' } : {}">
      <el-table :data="accPaged" size="small" row-key="id" v-loading="loading"
        :expand-row-keys="expanded" @expand-change="onExpand">
        <el-table-column type="expand">
          <template #default="{ row }">
            <div style="padding:8px 40px">
              <div style="display:flex;align-items:center;margin-bottom:8px">
                <b>项目（凭据在此配）</b>
                <el-button link type="primary" size="small" style="margin-left:10px" @click="openProj(row)">+ 添加项目</el-button>
              </div>
              <el-table :data="row.projects" size="small">
                <el-table-column prop="name" label="业务名" width="140" />
                <el-table-column prop="project_id" label="GCP Project" min-width="180" />
                <el-table-column label="凭据(SA)" width="90"><template #default="{ row: p }"><el-tag size="small" :type="p.has_cred?'success':'danger'">{{ p.has_cred?'已配':'未配' }}</el-tag></template></el-table-column>
                <el-table-column prop="host_count" label="主机数" width="80" />
                <el-table-column prop="last_sync_at" label="最近同步" width="140"><template #default="{ row: p }">{{ p.last_sync_at || '—' }}</template></el-table-column>
                <el-table-column label="操作" width="180"><template #default="{ row: p }">
                  <el-button link type="success" size="small" :loading="syncing['p'+p.id]" @click="syncProject(row.id, p.id)">同步</el-button>
                  <span v-if="projProg[p.id]" class="muted" style="font-size:11px">{{ progressText(projProg[p.id]) }}</span>
                  <el-button link type="primary" size="small" @click="openProj(row, p)">编辑</el-button>
                  <el-button link type="danger" size="small" @click="delProj(p)">删除</el-button>
                </template></el-table-column>
              </el-table>
              <el-empty v-if="!row.projects?.length" description="还没项目，点上面「添加项目」并配 SA key" :image-size="50" />
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="name" label="账号名" min-width="160" />
        <el-table-column prop="provider" label="厂商" width="90"><template #default="{ row }"><el-tag size="small">{{ row.provider }}</el-tag></template></el-table-column>
        <el-table-column label="项目数" width="90"><template #default="{ row }">{{ row.projects?.length || 0 }}</template></el-table-column>
        <el-table-column label="操作" width="220" fixed="right"><template #default="{ row }">
          <el-button link type="success" size="small" :loading="syncing['a'+row.id]" @click="syncAccount(row.id)">同步全部</el-button>
          <span v-if="acctProg[row.id]" class="muted" style="font-size:12px">同步中 {{ progressText(acctProg[row.id]) }}</span>
          <el-button link type="primary" size="small" @click="openAcct(row)">编辑</el-button>
          <el-button link type="danger" size="small" @click="delAcct(row)">删除</el-button>
        </template></el-table-column>
      </el-table>
      <Pager :total="accounts.length" v-model:page="page" v-model:page-size="pageSize" />
      <el-empty v-if="!loading && !accounts.length" description="还没云账号，点右上添加" />
    </el-card>

    <!-- 账号弹窗 -->
    <el-dialog :close-on-click-modal="false" v-model="acctDlg" :title="acctEdit?'编辑云账号':'添加云账号'" width="480px">
      <el-form :model="acctForm" label-width="120px">
        <el-form-item label="账号名"><el-input v-model="acctForm.name" placeholder="如 公司GCP" /></el-form-item>
        <el-form-item label="厂商"><el-input v-model="acctForm.provider" placeholder="gcp" :disabled="acctEdit" /></el-form-item>
        <el-form-item label="账单 dataset"><el-input v-model="acctForm.billing_export_dataset" placeholder="可选,BigQuery 账单导出(二期)" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="acctDlg=false">取消</el-button><el-button type="primary" @click="saveAcct">保存</el-button></template>
    </el-dialog>

    <!-- 项目弹窗 -->
    <el-dialog :close-on-click-modal="false" v-model="projDlg" :title="projEdit?'编辑项目':'添加项目'" width="560px">
      <el-form :model="projForm" label-width="110px">
        <el-form-item label="业务名"><el-input v-model="projForm.name" placeholder="如 生产/测试" /></el-form-item>
        <el-form-item label="GCP Project"><el-input v-model="projForm.project_id" placeholder="csc5002-public-uat" :disabled="projEdit" /></el-form-item>
        <el-form-item label="SA key JSON">
          <el-input v-model="projForm.cred_json" type="textarea" :rows="6" :placeholder="projEdit?'留空=保留原值':'粘贴 service account JSON(只读:Compute Viewer + Kubernetes Engine Viewer)'" />
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="projDlg=false">取消</el-button><el-button type="primary" @click="saveProj">保存</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useLoadState } from '../composables/useLoadState'
import LoadError from '../components/LoadError.vue'
import { listCloudAccounts, createCloudAccount, updateCloudAccount, deleteCloudAccount,
  createCloudProject, updateCloudProject, deleteCloudProject } from '../api/cmdb'
import { useAppStore } from '../stores/app'
import { useHostSync } from '../composables/useHostSync'
import { usePager } from '../composables/usePager'
import Pager from '../components/Pager.vue'

// embedded=true 时作为「接入管理」的一个 tab 渲染：不显示页级标题，去掉外层留白
defineProps({ embedded: { type: Boolean, default: false } })

const app = useAppStore()
const { loading, error, run } = useLoadState()
const accounts = ref([]); const expanded = ref([])
// 与「主机」页共用同一份同步实现：这里原本只发请求、不轮询进度、错误一律吞成「失败」，
// 而同步失败发生在后台协程里，不轮询就永远看不到原因（比如 GCP 权限 403）。
const { syncing, acctProg, projProg, syncAccount, syncProject, progressText } =
  useHostSync(() => load())
const { page, pageSize, paged: accPaged } = usePager(accounts)
const acctDlg = ref(false); const acctEdit = ref(false); const acctForm = reactive({ id: 0, name: '', provider: 'gcp', billing_export_dataset: '' })
const projDlg = ref(false); const projEdit = ref(false); const projAcct = ref(0)
const projForm = reactive({ id: 0, name: '', project_id: '', cred_json: '' })

// 失败留在页面上：原来只弹 toast，3 秒后消失，列表显示为空，
// 看起来像"还没配任何云账号"——而云账号是主机/K8s/成本三块的共同前提（CMDB-013）
async function load() { await run(async () => { accounts.value = await listCloudAccounts() }) }
function onExpand(row, rows) { expanded.value = rows.map(r => r.id) }

function openAcct(row) { acctEdit.value = !!row; Object.assign(acctForm, row ? { ...row } : { id: 0, name: '', provider: 'gcp', billing_export_dataset: '' }); acctDlg.value = true }
async function saveAcct() {
  if (!acctForm.name) { ElMessage.warning('账号名必填'); return }
  try { if (acctEdit.value) await updateCloudAccount(acctForm.id, acctForm); else await createCloudAccount(acctForm); ElMessage.success('已保存'); acctDlg.value = false; load() }
  catch (e) { ElMessage.error(e.response?.data?.error || '保存失败') }
}
async function delAcct(row) { try { await app.showConfirm(`删除云账号「${row.name}」？其下项目+同步来的主机一并清除。`); await deleteCloudAccount(row.id); ElMessage.success('已删除'); load() } catch (e) { if (e !== 'cancel') ElMessage.error('失败') } }


function openProj(acct, p) { projEdit.value = !!p; projAcct.value = acct.id; Object.assign(projForm, p ? { ...p, cred_json: '' } : { id: 0, name: '', project_id: '', cred_json: '' }); projDlg.value = true }
async function saveProj() {
  if (!projForm.project_id) { ElMessage.warning('GCP Project 必填'); return }
  try { if (projEdit.value) await updateCloudProject(projForm.id, projForm); else await createCloudProject(projAcct.value, projForm); ElMessage.success('已保存'); projDlg.value = false; load() }
  catch (e) { ElMessage.error(e.response?.data?.error || '保存失败') }
}
async function delProj(p) { try { await app.showConfirm(`删除项目「${p.project_id}」？`); await deleteCloudProject(p.id); ElMessage.success('已删除'); load() } catch (e) { if (e !== 'cancel') ElMessage.error('失败') } }


onMounted(load)
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
</style>
