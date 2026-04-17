<template>
  <div>
    <h2>审计日志</h2>
    <el-card>
      <el-form inline>
        <el-form-item label="操作"><el-input v-model="filters.action" clearable /></el-form-item>
        <el-form-item label="用户"><el-input v-model="filters.username" clearable /></el-form-item>
        <el-form-item><el-button type="primary" @click="load">查询</el-button></el-form-item>
      </el-form>
      <el-table :data="list" size="small" border>
        <el-table-column prop="created_at" label="时间" width="170" />
        <el-table-column prop="username" label="用户" width="120" />
        <el-table-column prop="auth_source" label="来源" width="80" />
        <el-table-column prop="action" label="操作" width="160" />
        <el-table-column prop="target_type" label="类型" width="100" />
        <el-table-column prop="target_name" label="对象" width="140" />
        <el-table-column prop="detail" label="详情" show-overflow-tooltip />
        <el-table-column prop="ip" label="IP" width="130" />
      </el-table>
      <el-pagination v-model:current-page="page" :total="total" :page-size="50" @current-change="load" style="margin-top:10px" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '../api/client'

const list = ref([])
const filters = ref({})
const page = ref(1)
const total = ref(0)

async function load() {
  const params = { page: page.value, page_size: 50, ...filters.value }
  const r = await api.get('/audit-logs', { params })
  list.value = r.data.list || []
  total.value = r.data.total || 0
}
onMounted(load)
</script>
