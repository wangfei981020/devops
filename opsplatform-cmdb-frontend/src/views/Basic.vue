<template>
  <div class="page">
    <div class="page-head"><span class="page-title">基础配置</span></div>

    <!-- 项目 -->
    <el-card shadow="never" style="margin-bottom:14px">
      <template #header><b>项目</b><span class="muted" style="margin-left:8px">录入域名/证书时下拉选</span>
        <el-button type="primary" size="small" style="float:right" @click="openProj()">+ 添加项目</el-button></template>
      <el-table :data="pPaged" size="small">
        <el-table-column label="项目名" min-width="160"><template #default="{ row }"><el-tag size="small" effect="plain" :style="chip(row.color)">{{ row.name }}</el-tag></template></el-table-column>
        <el-table-column label="状态" min-width="150"><template #default="{ row }">
          <el-tag v-if="row.status" size="small" effect="plain" :style="chip(app.statusColor(row.status))">{{ row.status }}</el-tag>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column prop="remark" label="备注" min-width="200" />
        <el-table-column prop="sort_order" label="排序" width="80" />
        <el-table-column label="操作" width="100" fixed="right"><template #default="{ row }"><div style="display:flex;gap:8px;align-items:center"><el-tooltip content="编辑"><el-button link type="primary" :icon="Edit" @click="openProj(row)" /></el-tooltip><el-tooltip content="删除"><el-button link type="danger" :icon="Delete" @click="delProj(row)" /></el-tooltip></div></template></el-table-column>
      </el-table>
      <el-pagination v-if="projects.length > pSize" v-model:current-page="pPage" v-model:page-size="pSize" :page-sizes="[10,20,50,100]"
        :total="projects.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
      <el-empty v-if="!projects.length" description="还没有项目，点右上添加" :image-size="60" />
    </el-card>

    <!-- 环境 -->
    <el-card shadow="never">
      <template #header><b>环境</b><span class="muted" style="margin-left:8px">PROD/UAT/TEST/DEV 等，可自定义</span>
        <el-button type="primary" size="small" style="float:right" @click="openEnv()">+ 添加环境</el-button></template>
      <el-table :data="ePaged" size="small">
        <el-table-column label="环境" width="150"><template #default="{ row }"><el-tag v-if="row.color" size="small" effect="plain" :style="chip(row.color)">{{ row.code }}</el-tag><el-tag v-else :type="row.tag_type" size="small">{{ row.code }}</el-tag></template></el-table-column>
        <el-table-column prop="name" label="名称" min-width="160" />
        <el-table-column prop="sort_order" label="排序" width="80" />
        <el-table-column label="操作" width="100" fixed="right"><template #default="{ row }"><div style="display:flex;gap:8px;align-items:center"><el-tooltip content="编辑"><el-button link type="primary" :icon="Edit" @click="openEnv(row)" /></el-tooltip><el-tooltip content="删除"><el-button link type="danger" :icon="Delete" @click="delEnv(row)" /></el-tooltip></div></template></el-table-column>
      </el-table>
      <el-pagination v-if="envs.length > eSize" v-model:current-page="ePage" v-model:page-size="eSize" :page-sizes="[10,20,50,100]"
        :total="envs.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>

    <!-- CDN -->
    <el-card shadow="never" style="margin-top:14px">
      <template #header><b>CDN 厂商</b><span class="muted" style="margin-left:8px">录入解析时下拉选，可留空</span>
        <el-button type="primary" size="small" style="float:right" @click="openCdn()">+ 添加 CDN</el-button></template>
      <el-table :data="cPaged" size="small">
        <el-table-column prop="name" label="CDN 名称" min-width="200" />
        <el-table-column prop="sort_order" label="排序" width="80" />
        <el-table-column label="操作" width="100" fixed="right"><template #default="{ row }"><div style="display:flex;gap:8px;align-items:center"><el-tooltip content="编辑"><el-button link type="primary" :icon="Edit" @click="openCdn(row)" /></el-tooltip><el-tooltip content="删除"><el-button link type="danger" :icon="Delete" @click="delCdn(row)" /></el-tooltip></div></template></el-table-column>
      </el-table>
      <el-pagination v-if="cdns.length > cSize" v-model:current-page="cPage" v-model:page-size="cSize" :page-sizes="[10,20,50,100]"
        :total="cdns.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
      <el-empty v-if="!cdns.length" description="还没有 CDN，点右上添加" :image-size="60" />
    </el-card>

    <!-- 生命周期状态字典 -->
    <el-card shadow="never" style="margin-top:14px">
      <template #header><b>生命周期状态</b><span class="muted" style="margin-left:8px">项目上线状态 / 主域名使用状态，可自定义增删改</span>
        <el-button type="primary" size="small" style="float:right" @click="openStatus()">+ 添加状态</el-button></template>
      <el-radio-group v-model="sScope" size="small" style="margin-bottom:10px">
        <el-radio-button value="project">项目状态</el-radio-button>
        <el-radio-button value="domain">主域名状态</el-radio-button>
      </el-radio-group>
      <el-table :data="scopedStatuses" size="small">
        <el-table-column label="状态" min-width="180"><template #default="{ row }"><el-tag size="small" effect="plain" :style="chip(row.color)">{{ row.label }}</el-tag></template></el-table-column>
        <el-table-column label="颜色" width="90"><template #default="{ row }"><span :style="{ display:'inline-block', width:'16px', height:'16px', borderRadius:'4px', background: row.color || '#909399', verticalAlign:'middle' }" /></template></el-table-column>
        <el-table-column prop="sort_order" label="排序" width="80" />
        <el-table-column label="操作" width="100" fixed="right"><template #default="{ row }"><div style="display:flex;gap:8px;align-items:center"><el-tooltip content="编辑"><el-button link type="primary" :icon="Edit" @click="openStatus(row)" /></el-tooltip><el-tooltip content="删除"><el-button link type="danger" :icon="Delete" @click="delStatus(row)" /></el-tooltip></div></template></el-table-column>
      </el-table>
      <el-empty v-if="!scopedStatuses.length" description="该类别还没有状态，点右上添加" :image-size="60" />
    </el-card>

    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="sDlg" :title="sEdit?'编辑状态':'添加状态'" width="440px">
      <el-form :model="sForm" label-width="80px">
        <el-form-item label="类别"><el-radio-group v-model="sForm.scope" :disabled="sEdit"><el-radio value="project">项目状态</el-radio><el-radio value="domain">主域名状态</el-radio></el-radio-group></el-form-item>
        <el-form-item label="状态名"><el-input v-model="sForm.label" placeholder="如 已上线 / 备用" /></el-form-item>
        <el-form-item label="颜色"><el-color-picker v-model="sForm.color" :predefine="COOL" /></el-form-item>
        <el-form-item label="排序"><el-input-number v-model="sForm.sort_order" :min="0" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="sDlg=false">取消</el-button><el-button type="primary" @click="saveStatus">保存</el-button></template>
    </el-dialog>

    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="cDlg" :title="cEdit?'编辑 CDN':'添加 CDN'" width="440px">
      <el-form :model="cForm" label-width="80px">
        <el-form-item label="CDN 名称"><el-input v-model="cForm.name" placeholder="如 阿里云CDN" /></el-form-item>
        <el-form-item label="排序"><el-input-number v-model="cForm.sort_order" :min="0" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="cDlg=false">取消</el-button><el-button type="primary" @click="saveCdn">保存</el-button></template>
    </el-dialog>

    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="pDlg" :title="pEdit?'编辑项目':'添加项目'" width="440px">
      <el-form :model="pForm" label-width="70px">
        <el-form-item label="项目名"><el-input v-model="pForm.name" /></el-form-item>
        <el-form-item label="状态">
          <el-select v-model="pForm.status" clearable placeholder="上线状态（可自定义）" style="width:240px">
            <el-option v-for="s in app.projectStatuses" :key="s.id" :label="s.label" :value="s.label">
              <span :style="{ display:'inline-block', width:'8px', height:'8px', borderRadius:'50%', background: s.color || '#909399', marginRight:'6px' }" />{{ s.label }}
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item label="备注"><el-input v-model="pForm.remark" /></el-form-item>
        <el-form-item label="颜色"><el-color-picker v-model="pForm.color" :predefine="COOL" /><span class="muted" style="margin-left:8px">域名台账里项目标签的颜色</span></el-form-item>
        <el-form-item label="排序"><el-input-number v-model="pForm.sort_order" :min="0" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="pDlg=false">取消</el-button><el-button type="primary" @click="saveProj">保存</el-button></template>
    </el-dialog>

    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="eDlg" :title="eEdit?'编辑环境':'添加环境'" width="440px">
      <el-form :model="eForm" label-width="80px">
        <el-form-item label="代码"><el-input v-model="eForm.code" placeholder="PROD / UAT / ..." :disabled="eEdit" /></el-form-item>
        <el-form-item label="名称"><el-input v-model="eForm.name" /></el-form-item>
        <el-form-item label="颜色"><el-color-picker v-model="eForm.color" :predefine="COOL" /><span class="muted" style="margin-left:8px">建议用冷色，红橙留给证书告警</span></el-form-item>
        <el-form-item label="排序"><el-input-number v-model="eForm.sort_order" :min="0" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="eDlg=false">取消</el-button><el-button type="primary" @click="saveEnv">保存</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Edit, Delete } from '@element-plus/icons-vue'
import { listProjects, createProject, updateProject, deleteProject, listEnvironments, createEnvironment, updateEnvironment, deleteEnvironment, listCdns, createCdn, updateCdn, deleteCdn,
  listStatuses, createStatus, updateStatus, deleteStatus } from '../api/cmdb'
import { useAppStore } from '../stores/app'
import { usePaged } from '../composables/usePaged'

const app = useAppStore()
const COOL = ['#3b7dd8', '#5b8ff9', '#269a99', '#5ad8a6', '#6dc8ec', '#9270ca', '#5d7092', '#0e7a6e', '#7d5fd6', '#2f9e8f']
function chip(c) { return c ? { color: c, borderColor: c + '66', background: c + '14' } : {} }
const projects = ref([]), envs = ref([]), cdns = ref([])
const { page: pPage, size: pSize, paged: pPaged } = usePaged(projects)
const { page: ePage, size: eSize, paged: ePaged } = usePaged(envs)
const { page: cPage, size: cSize, paged: cPaged } = usePaged(cdns)
const pDlg = ref(false), pEdit = ref(false), pForm = ref({})
const eDlg = ref(false), eEdit = ref(false), eForm = ref({})
const cDlg = ref(false), cEdit = ref(false), cForm = ref({})
const statuses = ref([]), sScope = ref('project')
const sDlg = ref(false), sEdit = ref(false), sForm = ref({})
const scopedStatuses = computed(() => statuses.value.filter((s) => s.scope === sScope.value))

async function load() { projects.value = await listProjects(); envs.value = await listEnvironments(); cdns.value = await listCdns(); statuses.value = await listStatuses() }
function openProj(row) { pEdit.value = !!row; pForm.value = row ? { ...row } : { name: '', remark: '', color: '', sort_order: 0, status: '' }; pDlg.value = true }
function openStatus(row) { sEdit.value = !!row; sForm.value = row ? { ...row } : { scope: sScope.value, label: '', color: '', sort_order: 0 }; sDlg.value = true }
async function saveStatus() {
  if (!sForm.value.label) { ElMessage.warning('状态名必填'); return }
  try { sEdit.value ? await updateStatus(sForm.value.id, sForm.value) : await createStatus(sForm.value); ElMessage.success('已保存'); sDlg.value = false; load(); app.loadStatuses() }
  catch (e) { ElMessage.error(e.response?.data?.error || '失败') }
}
async function delStatus(row) { try { await app.showConfirm(`删除状态「${row.label}」？已用它的记录保留原文本`); await deleteStatus(row.id); load(); app.loadStatuses() } catch (e) { if (e !== 'cancel') ElMessage.error('失败') } }
async function saveProj() {
  try { pEdit.value ? await updateProject(pForm.value.id, pForm.value) : await createProject(pForm.value); ElMessage.success('已保存'); pDlg.value = false; load(); app.loadProjects() }
  catch (e) { ElMessage.error(e.response?.data?.error || '失败') }
}
async function delProj(row) { try { await app.showConfirm(`删除项目 ${row.name}？`); await deleteProject(row.id); load() } catch (e) { if (e !== 'cancel') ElMessage.error('失败') } }
function openEnv(row) { eEdit.value = !!row; eForm.value = row ? { ...row } : { code: '', name: '', tag_type: 'info', color: '', sort_order: 0 }; eDlg.value = true }
async function saveEnv() {
  try { eEdit.value ? await updateEnvironment(eForm.value.id, eForm.value) : await createEnvironment(eForm.value); ElMessage.success('已保存'); eDlg.value = false; load() }
  catch (e) { ElMessage.error(e.response?.data?.error || '失败') }
}
async function delEnv(row) { try { await app.showConfirm(`删除环境 ${row.code}？`); await deleteEnvironment(row.id); load() } catch (e) { if (e !== 'cancel') ElMessage.error('失败') } }
function openCdn(row) { cEdit.value = !!row; cForm.value = row ? { ...row } : { name: '', sort_order: 0 }; cDlg.value = true }
async function saveCdn() {
  try { cEdit.value ? await updateCdn(cForm.value.id, cForm.value) : await createCdn(cForm.value); ElMessage.success('已保存'); cDlg.value = false; load(); app.loadCdns() }
  catch (e) { ElMessage.error(e.response?.data?.error || '失败') }
}
async function delCdn(row) { try { await app.showConfirm(`删除 CDN ${row.name}？`); await deleteCdn(row.id); load(); app.loadCdns() } catch (e) { if (e !== 'cancel') ElMessage.error('失败') } }
onMounted(load)
</script>
