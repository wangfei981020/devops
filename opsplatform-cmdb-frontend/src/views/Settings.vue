<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">设置</span>
      <span class="muted" style="margin-left:10px">
        注册商 / 云账号 / CDN / 数据源 / ACME 的凭据已统一移到「接入管理」
        <el-link type="primary" style="margin-left:6px;vertical-align:baseline" @click="$router.push('/integrations')">去接入管理</el-link>
      </span>
    </div>

    <el-card shadow="never">
      <template #header><b>指标导出</b><span class="muted" style="margin-left:8px">飞书通知 / 到期提醒 / 通知人 在「通知」页</span></template>
      <el-form :model="cfg" label-width="160px" style="max-width:760px">
        <el-form-item label="可导出 label 白名单">
          <el-input v-model="cfg.export_label_whitelist" placeholder="project,env,module,name,ca,registrar,team" />
          <div class="muted">只有列入白名单的自定义 label 才会进 Prometheus（控高基数，防 VM 写爆）</div>
        </el-form-item>
        <el-button type="primary" @click="saveCfg">保存设置</el-button>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { getSettings, updateSettings } from '../api/cmdb'

const cfg = ref({})

async function load() { cfg.value = await getSettings() }
async function saveCfg() {
  try { await updateSettings({ export_label_whitelist: cfg.value.export_label_whitelist || '' }); ElMessage.success('已保存') }
  catch (e) { ElMessage.error('保存失败') }
}
onMounted(load)
</script>

<style scoped>
.muted { color: #909399; font-size: 12px; }
</style>
