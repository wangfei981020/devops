<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">主机</span>
      <div>
        <el-button :icon="Money" @click="openRates">成本费率</el-button>
        <el-button :icon="Cloudy" @click="openAccounts">云账号</el-button>
      </div>
    </div>
    <div class="muted" style="margin-bottom:12px">GCP Compute 主机（只读同步，不能改/删）。成本为<b>估算·目录价</b>（可在「成本费率」调整）；真实账单待接 BigQuery。</div>

    <el-card shadow="never" style="margin-bottom:12px">
      <div class="filter">
        <el-input v-model="f.kw" placeholder="搜索 实例名/IP" clearable :prefix-icon="Search" style="width:200px" />
        <el-select v-model="f.provider" clearable placeholder="厂商" style="width:120px"><el-option v-for="p in opts.provider" :key="p" :label="plabel(p)" :value="p" /></el-select>
        <el-select v-model="f.project" clearable placeholder="项目" style="width:150px"><el-option v-for="p in opts.project" :key="p" :label="p" :value="p" /></el-select>
        <el-select v-model="f.zone" clearable placeholder="区域" style="width:150px"><el-option v-for="z in opts.zone" :key="z" :label="z" :value="z" /></el-select>
        <el-select v-model="f.status" clearable placeholder="状态" style="width:130px"><el-option v-for="s in opts.status" :key="s" :label="s" :value="s" /></el-select>
        <span class="muted" style="margin-left:auto">共 {{ filtered.length }} / {{ rows.length }} 台　月估合计 ${{ monthSum }}</span>
      </div>
    </el-card>

    <el-card shadow="never">
      <el-table :data="paged" size="small" v-loading="loading">
        <el-table-column label="厂商" width="80"><template #default="{ row }"><el-tag :style="providerStyle(row.provider)" size="small">{{ plabel(row.provider) }}</el-tag></template></el-table-column>
        <el-table-column label="项目" min-width="120"><template #default="{ row }"><el-tag :style="projectStyle(projName(row))" size="small">{{ projName(row) }}</el-tag></template></el-table-column>
        <el-table-column label="区域" width="130"><template #default="{ row }"><el-tag :style="regionStyle(row.zone)" size="small">{{ row.zone }}</el-tag></template></el-table-column>
        <el-table-column label="实例名" min-width="170"><template #default="{ row }">
          <span :class="{ stale: row.stale }">{{ row.name }}</span>
          <el-tag v-if="row.stale" type="warning" size="small" style="margin-left:6px">已删</el-tag>
          <el-tooltip v-if="row.preemptible" content="抢占式/Spot 实例：价格低但 GCP 可随时回收，勿跑关键有状态服务"><el-tag type="warning" size="small" effect="dark" style="margin-left:6px">Spot</el-tag></el-tooltip>
        </template></el-table-column>
        <el-table-column label="CPU" width="70" align="right"><template #default="{ row }">{{ row.vcpu }}核</template></el-table-column>
        <el-table-column label="内存" width="80" align="right"><template #default="{ row }">{{ gb(row.mem_mb) }}G</template></el-table-column>
        <el-table-column label="磁盘" width="90" align="right"><template #default="{ row }">{{ row.disk_total_gb }}G</template></el-table-column>
        <el-table-column label="内网IP" width="130"><template #default="{ row }"><span class="mono">{{ row.internal_ip || '—' }}</span></template></el-table-column>
        <el-table-column label="外网IP" width="130"><template #default="{ row }"><span class="mono">{{ row.external_ip || '—' }}</span></template></el-table-column>
        <el-table-column label="状态" width="90"><template #default="{ row }"><el-tag :type="stTag(row.status)" size="small">{{ stLabel(row.status) }}</el-tag></template></el-table-column>
        <el-table-column label="日均($)" width="90" align="right"><template #default="{ row }">${{ row.cost_daily }}</template></el-table-column>
        <el-table-column label="本月估($)" width="100" align="right"><template #default="{ row }">${{ row.cost_month }}</template></el-table-column>
        <el-table-column label="累计($)" width="100" align="right"><template #default="{ row }">${{ row.cost_total }}</template></el-table-column>
        <el-table-column label="操作" width="70" fixed="right"><template #default="{ row }">
          <el-tooltip content="查看详情"><el-button link type="primary" :icon="View" @click="openDetail(row)" /></el-tooltip>
        </template></el-table-column>
      </el-table>
      <el-pagination v-model:current-page="page" v-model:page-size="size" :page-sizes="[10,20,50,100]"
        :total="filtered.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>

    <!-- 主机详情 -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="dDlg" title="主机详情" width="620px">
      <template v-if="detail">
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item label="项目">{{ detail.host.project_name || detail.host.project }}</el-descriptions-item>
          <el-descriptions-item label="项目ID"><span class="mono">{{ detail.host.project }}</span></el-descriptions-item>
          <el-descriptions-item label="实例名">{{ detail.host.name }}</el-descriptions-item>
          <el-descriptions-item label="主机名">{{ detail.host.hostname || '—' }}</el-descriptions-item>
          <el-descriptions-item label="区域">{{ detail.host.zone }}</el-descriptions-item>
          <el-descriptions-item label="状态"><el-tag :type="stTag(detail.host.status)" size="small">{{ stLabel(detail.host.status) }}</el-tag></el-descriptions-item>
          <el-descriptions-item label="CPU">{{ detail.host.vcpu }} vCPU</el-descriptions-item>
          <el-descriptions-item label="内存">{{ gb(detail.host.mem_mb) }} GB</el-descriptions-item>
          <el-descriptions-item label="机型">{{ detail.host.machine_type }}</el-descriptions-item>
          <el-descriptions-item label="CPU平台">{{ detail.host.cpu_platform || '—' }}</el-descriptions-item>
          <el-descriptions-item label="镜像">{{ detail.host.image || '—' }}</el-descriptions-item>
          <el-descriptions-item label="操作系统">{{ detail.host.os || '—' }}</el-descriptions-item>
          <el-descriptions-item label="计费形态">
            <el-tag v-if="detail.host.preemptible" type="warning" size="small" effect="dark">抢占式/Spot</el-tag>
            <span v-else>标准</span>
          </el-descriptions-item>
          <el-descriptions-item label="删除保护">{{ detail.host.deletion_protection ? '✅ 已开' : '未开' }}</el-descriptions-item>
          <el-descriptions-item label="创建时间">{{ detail.host.gcp_created_at || '—' }}</el-descriptions-item>
          <el-descriptions-item label="云账号">{{ detail.host.account_name }}</el-descriptions-item>
          <el-descriptions-item label="内网IP"><span class="mono">{{ detail.host.internal_ip || '—' }}</span></el-descriptions-item>
          <el-descriptions-item label="外网IP"><span class="mono">{{ detail.host.external_ip || '—' }}</span></el-descriptions-item>
          <el-descriptions-item label="VPC">{{ detail.host.vpc || '—' }}</el-descriptions-item>
          <el-descriptions-item label="子网">{{ detail.host.subnet || '—' }}</el-descriptions-item>
          <el-descriptions-item label="防火墙标签" :span="2">
            <el-tag v-for="t in detail.host.network_tags" :key="t" size="small" effect="plain" style="margin:2px 4px 2px 0">{{ t }}</el-tag>
            <span v-if="!detail.host.network_tags || !detail.host.network_tags.length" class="muted">—</span>
          </el-descriptions-item>
          <el-descriptions-item label="服务账号" :span="2">
            <span v-if="detail.host.service_accounts && detail.host.service_accounts.length" class="mono">{{ detail.host.service_accounts.join('、') }}</span>
            <span v-else class="muted">—</span>
          </el-descriptions-item>
          <el-descriptions-item label="标签" :span="2">
            <el-tag v-for="(v,k) in detail.host.labels" :key="k" size="small" style="margin:2px 4px 2px 0">{{ k }}:{{ v }}</el-tag>
            <span v-if="!detail.host.labels || !Object.keys(detail.host.labels).length" class="muted">—</span>
          </el-descriptions-item>
        </el-descriptions>

        <h4 class="sec">磁盘（{{ detail.disks.length }} 块，合计 {{ detail.host.disk_total_gb }}G）</h4>
        <el-table :data="detail.disks" size="small" max-height="200">
          <el-table-column prop="name" label="名称" min-width="140" />
          <el-table-column label="大小" width="90" align="right"><template #default="{ row: d }">{{ d.size_gb }}G</template></el-table-column>
          <el-table-column label="类型" width="120"><template #default="{ row: d }"><el-tag size="small" effect="plain">{{ d.type }}</el-tag></template></el-table-column>
          <el-table-column label="启动盘" width="80"><template #default="{ row: d }"><span v-if="d.is_boot">✓</span><span v-else class="muted">—</span></template></el-table-column>
        </el-table>

        <h4 class="sec">🔗 关联业务域名（源站IP 命中）</h4>
        <div v-if="detail.related_domains.length" style="display:flex;flex-wrap:wrap;gap:6px">
          <el-tag v-for="(r,i) in detail.related_domains" :key="i" size="small" type="success" effect="plain">{{ r.fqdn }}</el-tag>
        </div>
        <div v-else class="muted">无（没有业务域名的源站IP 指向此主机）</div>

        <h4 class="sec">💰 成本估算（USD · 目录价）　<el-date-picker v-model="asOf" type="date" size="small" value-format="YYYY-MM-DD" placeholder="累计到" style="width:150px" @change="reloadDetail" /></h4>
        <el-descriptions :column="3" border size="small">
          <el-descriptions-item label="命中费率" :span="3">
            <span class="mono">{{ detail.rate_matched }}</span>
            <span class="muted" style="margin-left:8px">vCPU ${{ detail.rate_vcpu_hour }}/h · 内存 ${{ detail.rate_ram_gb_hour }}/GB/h</span>
          </el-descriptions-item>
          <el-descriptions-item label="时价">${{ detail.cost_hourly }}/h</el-descriptions-item>
          <el-descriptions-item label="日均">${{ detail.host.cost_daily }}/天</el-descriptions-item>
          <el-descriptions-item label="本月估">${{ detail.host.cost_month }}</el-descriptions-item>
          <el-descriptions-item label="累计" :span="3">
            <b>${{ detail.host.cost_total }}</b>
            <span class="muted" style="margin-left:8px">（{{ detail.host.gcp_created_at }} 创建 → {{ detail.as_of }}）</span>
          </el-descriptions-item>
        </el-descriptions>
        <div class="muted" style="margin-top:6px;font-size:12px">※ 估算基于目录价，未含持续使用/承诺折扣，也未精确按实际停机时段计。真实账单待接 BigQuery。</div>
      </template>
      <template #footer><el-button @click="dDlg=false">关闭</el-button></template>
    </el-dialog>

    <!-- 云账号管理（账号=分组，展开看其下多个 project，凭据在 project 层） -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="acctDlg" title="云账号" width="820px">
      <div class="muted" style="margin-bottom:10px">账号是业务分组；<b>凭据配在项目层</b>——同一账号下每个 GCP project 一份 service account。展开账号管理其下项目。</div>
      <div style="text-align:right;margin-bottom:10px"><el-button type="primary" size="small" :icon="Plus" @click="openAcctForm()">添加云账号</el-button></div>
      <el-table :data="accounts" size="small" row-key="id">
        <el-table-column type="expand">
          <template #default="{ row: acc }">
            <div style="padding:6px 16px 12px">
              <div style="display:flex;align-items:center;margin-bottom:8px">
                <b>项目（每个 project 独立凭据）</b>
                <el-button size="small" :icon="Plus" style="margin-left:auto" @click="openProjForm(acc)">添加项目</el-button>
              </div>
              <el-table :data="acc.projects" size="small">
                <el-table-column prop="name" label="自定义名" min-width="110" />
                <el-table-column label="project ID" min-width="150"><template #default="{ row: p }"><span class="mono">{{ p.project_id }}</span></template></el-table-column>
                <el-table-column label="凭据" width="80"><template #default="{ row: p }">
                  <el-tag :type="p.has_cred?'success':'info'" size="small">{{ p.has_cred?'已配':'未配' }}</el-tag>
                </template></el-table-column>
                <el-table-column label="主机" width="60" align="right"><template #default="{ row: p }">{{ p.host_count }}</template></el-table-column>
                <el-table-column label="最近同步" width="130"><template #default="{ row: p }">
                  {{ p.last_sync_at || '—' }}
                  <div v-if="p.last_result" class="muted" style="font-size:11px">{{ p.last_result }}</div>
                </template></el-table-column>
                <el-table-column label="操作" width="150"><template #default="{ row: p }">
                  <div style="display:flex;gap:4px;align-items:center">
                    <el-button link type="primary" :icon="Refresh" :loading="syncing['p'+p.id]" @click="syncProj(p)">同步</el-button>
                    <el-tooltip content="编辑"><el-button link type="primary" :icon="Edit" @click="openProjForm(acc, p)" /></el-tooltip>
                    <el-tooltip content="删除（连主机）"><el-button link type="danger" :icon="Delete" @click="delProj(p)" /></el-tooltip>
                  </div>
                </template></el-table-column>
              </el-table>
              <el-empty v-if="!acc.projects.length" description="还没有项目，点右上「添加项目」并配置凭据" :image-size="40" />
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="name" label="账号名" min-width="130" />
        <el-table-column label="厂商" width="80"><template #default="{ row }"><el-tag size="small">{{ row.provider }}</el-tag></template></el-table-column>
        <el-table-column label="项目数" width="80" align="right"><template #default="{ row }">{{ row.projects.length }}</template></el-table-column>
        <el-table-column label="操作" width="220" fixed="right"><template #default="{ row }">
          <div style="display:flex;gap:6px;align-items:center">
            <el-button link type="primary" :icon="Refresh" :loading="syncing['a'+row.id]" @click="syncAcct(row)">同步全部</el-button>
            <el-tooltip content="编辑账号"><el-button link type="primary" :icon="Edit" @click="openAcctForm(row)" /></el-tooltip>
            <el-tooltip content="删除（连项目和主机）"><el-button link type="danger" :icon="Delete" @click="delAcct(row)" /></el-tooltip>
          </div>
        </template></el-table-column>
      </el-table>
      <el-empty v-if="!accounts.length" description="还没有云账号，点右上添加" :image-size="50" />
    </el-dialog>

    <!-- 账号表单（分组：名称 + 计费集） -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="acctForm.dlg" :title="acctForm.id?'编辑云账号':'添加云账号'" width="500px">
      <el-form :model="acctForm" label-width="120px">
        <el-form-item label="账号名"><el-input v-model="acctForm.name" placeholder="如 公司GCP" style="width:260px" /></el-form-item>
        <el-form-item label="BigQuery账单集">
          <el-input v-model="acctForm.billing_export_dataset" placeholder="（预留，二期真实账单用，可留空）" style="width:300px" />
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="acctForm.dlg=false">取消</el-button><el-button type="primary" @click="saveAcct">保存</el-button></template>
    </el-dialog>

    <!-- 项目表单（凭据在这层） -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="projForm.dlg" :title="projForm.id?'编辑项目':'添加项目'" width="560px">
      <el-form :model="projForm" label-width="110px">
        <el-form-item label="归属账号"><span>{{ projForm.accountName }}</span></el-form-item>
        <el-form-item label="自定义名"><el-input v-model="projForm.name" placeholder="如 生产 / 测试（主机列表显示这个）" style="width:320px" /></el-form-item>
        <el-form-item label="project ID"><el-input v-model="projForm.project_id" placeholder="g32-prod-8821" style="width:320px" /></el-form-item>
        <el-form-item label="SA JSON 凭据">
          <el-input v-model="projForm.cred_json" type="textarea" :rows="4" :placeholder="projForm.id ? '留空=不改凭据' : '该 project 的 service account JSON key（只读 compute.viewer 即可）'" />
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="projForm.dlg=false">取消</el-button><el-button type="primary" @click="saveProj">保存</el-button></template>
    </el-dialog>

    <!-- 成本费率（分档：计算费率 + 磁盘费率） -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="rateDlg" title="成本费率（USD · 目录价估算）" width="760px">
      <div class="muted" style="margin-bottom:10px">
        主机按自己的<b>「区域 + 机型族」</b>命中计算费率、<b>「区域 + 磁盘类型」</b>命中磁盘费率，命中不到用 <code>default</code> 兜底。
        <span style="color:#e6a23c">⚠️ us-central1 为官方确认价；asia-east1/asia-east2 为保守溢价估算（偏高），请对照 GCP 控制台核对后改，改过即标「已核对」。</span>
      </div>
      <el-tabs v-model="rateTab">
        <el-tab-pane label="计算费率（vCPU + 内存）" name="compute">
          <div style="text-align:right;margin-bottom:8px"><el-button size="small" :icon="Plus" @click="openRateForm('compute')">添加档位</el-button></div>
          <el-table :data="computeRates" size="small" max-height="380">
            <el-table-column prop="region" label="区域" min-width="120" />
            <el-table-column prop="machine_family" label="机型族" width="100" />
            <el-table-column label="vCPU/时($)" width="130"><template #default="{ row }"><span class="mono">{{ row.vcpu_hour_usd }}</span></template></el-table-column>
            <el-table-column label="内存GB/时($)" width="130"><template #default="{ row }"><span class="mono">{{ row.ram_gb_hour_usd }}</span></template></el-table-column>
            <el-table-column label="来源" width="100"><template #default="{ row }"><el-tag size="small" :type="noteType(row.note)" effect="plain">{{ noteLabel(row.note) }}</el-tag></template></el-table-column>
            <el-table-column label="操作" width="90"><template #default="{ row }">
              <el-button link type="primary" :icon="Edit" @click="openRateForm('compute', row)" />
              <el-button v-if="row.region!=='default'" link type="danger" :icon="Delete" @click="delRate('compute', row)" />
            </template></el-table-column>
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="磁盘费率" name="disk">
          <div style="text-align:right;margin-bottom:8px"><el-button size="small" :icon="Plus" @click="openRateForm('disk')">添加档位</el-button></div>
          <el-table :data="diskRates" size="small" max-height="380">
            <el-table-column prop="region" label="区域" min-width="120" />
            <el-table-column prop="disk_type" label="磁盘类型" width="130" />
            <el-table-column label="GB/月($)" width="130"><template #default="{ row }"><span class="mono">{{ row.gb_month_usd }}</span></template></el-table-column>
            <el-table-column label="来源" width="100"><template #default="{ row }"><el-tag size="small" :type="noteType(row.note)" effect="plain">{{ noteLabel(row.note) }}</el-tag></template></el-table-column>
            <el-table-column label="操作" width="90"><template #default="{ row }">
              <el-button link type="primary" :icon="Edit" @click="openRateForm('disk', row)" />
              <el-button v-if="row.region!=='default'" link type="danger" :icon="Delete" @click="delRate('disk', row)" />
            </template></el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>
      <template #footer><el-button @click="rateDlg=false">关闭</el-button></template>
    </el-dialog>

    <!-- 费率行 编辑/添加 -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="rateForm.dlg" :title="rateForm.id?'编辑费率':'添加费率'" width="440px">
      <el-form :model="rateForm" label-width="110px">
        <el-form-item label="区域"><el-input v-model="rateForm.region" :disabled="!!rateForm.id" placeholder="如 asia-east1 / default" style="width:220px" /></el-form-item>
        <template v-if="rateForm.kind==='compute'">
          <el-form-item label="机型族"><el-input v-model="rateForm.machine_family" :disabled="!!rateForm.id" placeholder="e2 / n2 / c2 / custom / default" style="width:220px" /></el-form-item>
          <el-form-item label="vCPU/时($)"><el-input-number v-model="rateForm.vcpu_hour_usd" :precision="6" :step="0.001" :controls="false" style="width:160px" /></el-form-item>
          <el-form-item label="内存GB/时($)"><el-input-number v-model="rateForm.ram_gb_hour_usd" :precision="6" :step="0.001" :controls="false" style="width:160px" /></el-form-item>
        </template>
        <template v-else>
          <el-form-item label="磁盘类型"><el-input v-model="rateForm.disk_type" :disabled="!!rateForm.id" placeholder="pd-ssd / pd-balanced / pd-standard / default" style="width:220px" /></el-form-item>
          <el-form-item label="GB/月($)"><el-input-number v-model="rateForm.gb_month_usd" :precision="6" :step="0.01" :controls="false" style="width:160px" /></el-form-item>
        </template>
      </el-form>
      <template #footer><el-button @click="rateForm.dlg=false">取消</el-button><el-button type="primary" @click="saveRate">保存</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search, View, Refresh, Edit, Delete, Plus, Money, Cloudy } from '@element-plus/icons-vue'
import { listHosts, getHost, listCloudAccounts, createCloudAccount, updateCloudAccount, deleteCloudAccount,
  syncCloudAccount, createCloudProject, updateCloudProject, deleteCloudProject, syncCloudProject,
  listComputeRates, createComputeRate, updateComputeRate, deleteComputeRate,
  listDiskRates, createDiskRate, updateDiskRate, deleteDiskRate } from '../api/cmdb'
import { useAppStore } from '../stores/app'
import { providerLabel as plabel, providerStyle, projectStyle, regionStyle } from '../utils/cloud'

const app = useAppStore()
const rows = ref([]), loading = ref(false)
const f = ref({ kw: '', provider: null, project: null, zone: null, status: null })
const page = ref(1), size = ref(10)
const dDlg = ref(false), detail = ref(null), detailCiid = ref(null), asOf = ref('')
const acctDlg = ref(false), accounts = ref([]), syncing = ref({})
const acctForm = ref({ dlg: false })
const projForm = ref({ dlg: false })
const rateDlg = ref(false), rateTab = ref('compute'), computeRates = ref([]), diskRates = ref([])
const rateForm = ref({ dlg: false })

function gb(mb) { return mb ? Math.round(mb / 1024 * 10) / 10 : 0 }
function stLabel(s) { return ({ RUNNING: '运行', TERMINATED: '停止', STOPPING: '停止中', PROVISIONING: '创建中', STAGING: '启动中' }[s] || s || '—') }
function stTag(s) { return s === 'RUNNING' ? 'success' : (s === 'TERMINATED' || s === 'STOPPING' ? 'info' : 'warning') }

const projName = (r) => r.project_name || r.project
const opts = computed(() => ({
  provider: [...new Set(rows.value.map((r) => r.provider).filter(Boolean))].sort(),
  project: [...new Set(rows.value.map(projName).filter(Boolean))].sort(),
  zone: [...new Set(rows.value.map((r) => r.zone).filter(Boolean))].sort(),
  status: [...new Set(rows.value.map((r) => r.status).filter(Boolean))].sort(),
}))
const filtered = computed(() => rows.value.filter((r) => {
  const kw = f.value.kw?.toLowerCase()
  return (!kw || r.name.toLowerCase().includes(kw) || (r.internal_ip || '').includes(kw) || (r.external_ip || '').includes(kw)) &&
    (!f.value.provider || r.provider === f.value.provider) &&
    (!f.value.project || projName(r) === f.value.project) &&
    (!f.value.zone || r.zone === f.value.zone) &&
    (!f.value.status || r.status === f.value.status)
}))
const paged = computed(() => { const s = (page.value - 1) * size.value; return filtered.value.slice(s, s + size.value) })
const monthSum = computed(() => Math.round(filtered.value.reduce((n, r) => n + (r.cost_month || 0), 0)))

async function load() {
  loading.value = true
  try { rows.value = await listHosts() } catch (e) {} finally { loading.value = false }
}
async function openDetail(row) { detailCiid.value = row.ci_id; asOf.value = ''; detail.value = await getHost(row.ci_id); dDlg.value = true }
async function reloadDetail() { if (detailCiid.value) detail.value = await getHost(detailCiid.value, asOf.value || undefined) }

async function openAccounts() { accounts.value = await listCloudAccounts(); acctDlg.value = true }
async function refreshAccounts() { accounts.value = await listCloudAccounts() }
function openAcctForm(row) {
  acctForm.value = row
    ? { dlg: true, id: row.id, name: row.name, billing_export_dataset: row.billing_export_dataset }
    : { dlg: true, id: null, name: '', billing_export_dataset: '' }
}
async function saveAcct() {
  if (!acctForm.value.name) { ElMessage.warning('账号名必填'); return }
  try {
    const b = { name: acctForm.value.name, billing_export_dataset: acctForm.value.billing_export_dataset }
    acctForm.value.id ? await updateCloudAccount(acctForm.value.id, b) : await createCloudAccount({ ...b, provider: 'gcp' })
    ElMessage.success('已保存'); acctForm.value.dlg = false; refreshAccounts()
  } catch (e) { ElMessage.error(e.response?.data?.error || '失败') }
}
async function delAcct(row) {
  try { await app.showConfirm(`删除云账号 ${row.name}？其下项目和同步来的主机一并清除`); await deleteCloudAccount(row.id); refreshAccounts(); load() }
  catch (e) { if (e !== 'cancel') ElMessage.error('失败') }
}
async function syncAcct(row) {
  syncing.value = { ...syncing.value, ['a' + row.id]: true }
  try {
    const r = await syncCloudAccount(row.id)
    if (r.failures && r.failures.length) ElMessage.warning(`同步 ${r.synced} 台，${r.failures.length} 个项目失败：${r.failures[0]}`)
    else ElMessage.success(`同步完成：${r.synced} 台，失效 ${r.stale}`)
    refreshAccounts(); load()
  } catch (e) { ElMessage.error(e.response?.data?.error || '同步失败') }
  finally { syncing.value = { ...syncing.value, ['a' + row.id]: false } }
}

// 项目（凭据层）
function openProjForm(acc, p) {
  projForm.value = p
    ? { dlg: true, id: p.id, accountId: acc.id, accountName: acc.name, name: p.name, project_id: p.project_id, cred_json: '' }
    : { dlg: true, id: null, accountId: acc.id, accountName: acc.name, name: '', project_id: '', cred_json: '' }
}
async function saveProj() {
  if (!projForm.value.project_id) { ElMessage.warning('project ID 必填'); return }
  try {
    const b = { name: projForm.value.name, project_id: projForm.value.project_id, cred_json: projForm.value.cred_json }
    projForm.value.id ? await updateCloudProject(projForm.value.id, b) : await createCloudProject(projForm.value.accountId, b)
    ElMessage.success('已保存'); projForm.value.dlg = false; refreshAccounts()
  } catch (e) { ElMessage.error(e.response?.data?.error || '失败') }
}
async function delProj(p) {
  try { await app.showConfirm(`删除项目 ${p.name}（${p.project_id}）？其下同步来的主机一并清除`); await deleteCloudProject(p.id); refreshAccounts(); load() }
  catch (e) { if (e !== 'cancel') ElMessage.error('失败') }
}
async function syncProj(p) {
  syncing.value = { ...syncing.value, ['p' + p.id]: true }
  try { const r = await syncCloudProject(p.id); ElMessage.success(`同步完成：${r.synced} 台，失效 ${r.stale}`); refreshAccounts(); load() }
  catch (e) { ElMessage.error(e.response?.data?.error || '同步失败') }
  finally { syncing.value = { ...syncing.value, ['p' + p.id]: false } }
}

function noteLabel(n) { return ({ official: '官方确认', estimate: '估算待核对', confirmed: '已核对', manual: '手填', fallback: '兜底' }[n] || n || '—') }
function noteType(n) { return n === 'official' || n === 'confirmed' ? 'success' : (n === 'estimate' ? 'warning' : 'info') }
async function refreshRates() { computeRates.value = await listComputeRates(); diskRates.value = await listDiskRates() }
async function openRates() { await refreshRates(); rateTab.value = 'compute'; rateDlg.value = true }
function openRateForm(kind, row) {
  if (kind === 'compute') {
    rateForm.value = row
      ? { dlg: true, kind, id: row.id, region: row.region, machine_family: row.machine_family, vcpu_hour_usd: row.vcpu_hour_usd, ram_gb_hour_usd: row.ram_gb_hour_usd }
      : { dlg: true, kind, id: null, region: '', machine_family: '', vcpu_hour_usd: 0, ram_gb_hour_usd: 0 }
  } else {
    rateForm.value = row
      ? { dlg: true, kind, id: row.id, region: row.region, disk_type: row.disk_type, gb_month_usd: row.gb_month_usd }
      : { dlg: true, kind, id: null, region: '', disk_type: '', gb_month_usd: 0 }
  }
}
async function saveRate() {
  const rf = rateForm.value
  try {
    if (rf.kind === 'compute') {
      const b = { region: rf.region, machine_family: rf.machine_family, vcpu_hour_usd: rf.vcpu_hour_usd, ram_gb_hour_usd: rf.ram_gb_hour_usd }
      rf.id ? await updateComputeRate(rf.id, b) : await createComputeRate(b)
    } else {
      const b = { region: rf.region, disk_type: rf.disk_type, gb_month_usd: rf.gb_month_usd }
      rf.id ? await updateDiskRate(rf.id, b) : await createDiskRate(b)
    }
    ElMessage.success('已保存，成本按新费率重算'); rateForm.value.dlg = false; refreshRates(); load()
  } catch (e) { ElMessage.error(e.response?.data?.error || '失败') }
}
async function delRate(kind, row) {
  try {
    await app.showConfirm(`删除费率 ${row.region} / ${kind === 'compute' ? row.machine_family : row.disk_type}？`)
    kind === 'compute' ? await deleteComputeRate(row.id) : await deleteDiskRate(row.id)
    refreshRates(); load()
  } catch (e) { if (e !== 'cancel') ElMessage.error('失败') }
}
onMounted(load)
</script>

<style scoped>
.filter { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.mono { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12px; }
.stale { text-decoration: line-through; color: #b0b3bb; }
.muted { color: #909399; }
.sec { font-size: 14px; margin: 18px 0 8px; display: flex; align-items: center; gap: 10px; }
</style>
