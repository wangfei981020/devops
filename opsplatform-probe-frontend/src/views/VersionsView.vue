<template>
  <div>
    <h2>Agent 版本管理</h2>
    <el-card>
      <el-button type="primary" @click="uploadDialog=true">上传文件</el-button>
      <el-button @click="importDialog=true">从镜像导入</el-button>
      <el-table :data="list" size="small" border style="margin-top:10px">
        <el-table-column prop="version" label="版本" width="120" />
        <el-table-column prop="size_bytes" label="大小" width="100">
          <template #default="{row}">{{ (row.size_bytes/1024/1024).toFixed(2) }} MB</template>
        </el-table-column>
        <el-table-column prop="sha256" label="SHA256" show-overflow-tooltip />
        <el-table-column prop="source" label="来源" width="100" />
        <el-table-column prop="changelog" label="变更说明" show-overflow-tooltip />
        <el-table-column prop="uploaded_by" label="上传人" width="120" />
        <el-table-column prop="uploaded_at" label="上传时间" width="170" />
        <el-table-column label="操作" width="120">
          <template #default="{row}">
            <el-button size="small" type="danger" @click="del(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="uploadDialog" title="上传二进制" width="500px">
      <el-form label-width="80px">
        <el-form-item label="版本号"><el-input v-model="upload.version" placeholder="v1.2.0" /></el-form-item>
        <el-form-item label="变更"><el-input v-model="upload.changelog" type="textarea" /></el-form-item>
        <el-form-item label="文件">
          <el-upload :auto-upload="false" :on-change="onFile" :limit="1" :show-file-list="true">
            <el-button>选择 probe-agent</el-button>
          </el-upload>
        </el-form-item>
        <el-form-item label="签名">
          <el-input v-model="upload.signature" type="textarea" :rows="3" placeholder="ed25519 base64 签名 (来自 probe-sign sign 命令). 中心端配置 REQUIRE_SIGNED_UPLOADS=true 时必填" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="uploadDialog=false">取消</el-button>
        <el-button type="primary" :loading="uploading" @click="doUpload">上传</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="importDialog" title="从镜像导入" width="500px">
      <el-form label-width="100px">
        <el-form-item label="镜像地址"><el-input v-model="imp.image" placeholder="marks26/opsplatform-probe-agent:v1.2.0" /></el-form-item>
        <el-form-item label="二进制路径"><el-input v-model="imp.binary_path" placeholder="app/probe-agent" /></el-form-item>
        <el-form-item label="版本号"><el-input v-model="imp.version" placeholder="留空则用镜像 tag" /></el-form-item>
        <el-form-item label="变更"><el-input v-model="imp.changelog" type="textarea" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="importDialog=false">取消</el-button>
        <el-button type="primary" :loading="importing" @click="doImport">导入</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '../api/client'
import { ElMessage, ElMessageBox } from 'element-plus'

const list = ref([])
const uploadDialog = ref(false)
const importDialog = ref(false)
const upload = ref({})
const imp = ref({ binary_path: 'app/probe-agent' })
const uploading = ref(false)
const importing = ref(false)
let selectedFile = null

async function load() {
  const r = await api.get('/versions')
  list.value = r.data || []
}
function onFile(file) { selectedFile = file.raw }

async function doUpload() {
  if (!selectedFile || !upload.value.version) {
    ElMessage.warning('请选择文件并填写版本号')
    return
  }
  uploading.value = true
  try {
    const fd = new FormData()
    fd.append('file', selectedFile)
    fd.append('version', upload.value.version)
    fd.append('changelog', upload.value.changelog || '')
    if (upload.value.signature) fd.append('signature', upload.value.signature.trim())
    await api.post('/versions/upload', fd, { headers: { 'Content-Type': 'multipart/form-data' } })
    uploadDialog.value = false
    upload.value = {}; selectedFile = null
    ElMessage.success('上传成功')
    load()
  } finally { uploading.value = false }
}
async function doImport() {
  if (!imp.value.image) { ElMessage.warning('请填写镜像地址'); return }
  importing.value = true
  try {
    await api.post('/versions/import-from-image', imp.value)
    importDialog.value = false
    imp.value = { binary_path: 'app/probe-agent' }
    ElMessage.success('导入成功')
    load()
  } finally { importing.value = false }
}
async function del(row) {
  await ElMessageBox.confirm(`删除版本 ${row.version}？`, '提示', { type: 'warning' })
  await api.delete(`/versions/${row.id}`)
  load()
}
onMounted(load)
</script>
