<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">版本与升级</span>
      <span class="muted" style="margin-left:10px">
        把「GKE 悄悄自动升级」变成日程表上的计划项——提前看见升级窗口，主动挡住，按自己的时间升。
      </span>
      <el-button size="small" :icon="Refresh" :loading="loading" style="margin-left:auto" @click="reload">刷新</el-button>
    </div>

    <!-- 汇总条：先回答「现在要不要紧」，再让人往下看细节 -->
    <div class="sum-bar" v-if="sum">
      <span class="chip">{{ sum.clusters }} 个 GKE 集群</span>
      <span class="chip" :class="sum.due_30d ? 'critical' : ''">{{ sum.due_30d }} 个 30 天内自动升级</span>
      <span class="chip" v-if="sum.blocked">{{ sum.blocked }} 个已挡住</span>
      <span class="chip critical" v-if="sum.most_urgent">
        最紧急：{{ sum.most_urgent }} 还有 {{ sum.most_urgent_days }} 天
      </span>
      <span class="muted" style="margin-left:auto">
        官网排期表 {{ sum.schedule_rows }} 行
        <template v-if="sum.schedule_synced_at">· 最后同步 {{ sum.schedule_synced_at }}</template>
        <el-tag v-if="!sum.schedule_rows" size="small" type="danger" style="margin-left:6px">未同步</el-tag>
      </span>
      <el-button size="small" :loading="syncing.schedule" style="margin-left:8px"
        @click="runTask('gke_schedule_sync', 'schedule')">同步排期表</el-button>
      <el-button size="small" :loading="syncing.upgrade" @click="runTask('gke_upgrade_sync', 'upgrade')">采集集群</el-button>
    </div>

    <el-card shadow="never">
      <el-tabs v-model="tab">
        <!-- ------------------------------------------------ 升级看板 -->
        <el-tab-pane :label="`升级看板${clusters.length ? ' (' + clusters.length + ')' : ''}`" name="board">
          <el-alert v-if="!loading && !clusters.length" type="info" :closable="false" show-icon style="margin-bottom:12px">
            <template #title>还没有纳管 GKE 集群</template>
            这个页面的数据来自 GKE 的 container API。请先在「K8s → 集群管理」纳管 GKE 集群，
            并在「系统管理 → 云账号」为对应的 GCP 项目配置 SA key，然后点上方「采集集群」。
          </el-alert>

          <el-table :data="clusters" size="small" v-loading="loading" row-key="cluster_id"
            :expand-row-keys="expanded" @expand-change="onExpand">
            <el-table-column type="expand">
              <template #default="{ row }">
                <div class="detail">
                  <!-- 判断结论放最上面：看板的价值是给结论，不是给一堆字段 -->
                  <el-alert v-if="row.verdict" :type="verdictType(row)" :closable="false" show-icon
                    style="margin-bottom:10px">{{ row.verdict }}</el-alert>

                  <div class="kv">
                    <span><b>自动升级状态</b> {{ row.auto_upgrade_status || '—' }}</span>
                    <span><b>扩展支持至</b> {{ row.eos_extended_at || '—' }}</span>
                    <span><b>采集时间</b> {{ row.synced_at || '未采集' }}</span>
                  </div>
                  <!-- 暂停原因必须带解释：MAINTENANCE_WINDOW 是常态节流，不等于「已挡住」，
                       只显示枚举名会让人误以为升级被拦下了 -->
                  <div class="kv" v-if="row.paused_reason">
                    <span :class="row.pause_kind === 'excluded' ? 'warn' : ''">
                      <b>暂停原因</b> {{ row.paused_reason }}
                      <span class="muted" v-if="row.pause_note"> —— {{ row.pause_note }}</span>
                    </span>
                  </div>
                  <div class="kv" v-if="maintWindow(row)">
                    <span><b>维护策略</b> {{ maintWindow(row) }}</span>
                  </div>

                  <el-table :data="row.pools" size="small" style="margin-top:8px">
                    <el-table-column prop="name" label="节点池" min-width="180" show-overflow-tooltip />
                    <el-table-column prop="node_count" label="节点" width="66" align="right" />
                    <el-table-column prop="version" label="版本" min-width="150">
                      <template #default="{ row: p }">
                        {{ p.version }}
                        <el-tooltip v-if="p.version_skew" :content="p.version_skew">
                          <el-tag size="small" type="warning" style="margin-left:4px">偏斜</el-tag>
                        </el-tooltip>
                      </template>
                    </el-table-column>
                    <el-table-column label="自动升级" width="86" align="center">
                      <template #default="{ row: p }">
                        <el-tag size="small" :type="p.auto_upgrade ? 'warning' : 'info'">
                          {{ p.auto_upgrade ? '开' : '关' }}</el-tag>
                      </template>
                    </el-table-column>
                    <el-table-column label="自动修复" width="86" align="center">
                      <template #default="{ row: p }">
                        <el-tag size="small" :type="p.auto_repair ? 'warning' : 'info'">
                          {{ p.auto_repair ? '开' : '关' }}</el-tag>
                      </template>
                    </el-table-column>
                    <el-table-column label="升级策略" min-width="150">
                      <template #default="{ row: p }">
                        {{ p.strategy || '—' }}
                        <span class="muted" v-if="p.strategy !== 'BLUE_GREEN'">
                          +{{ p.max_surge }}/-{{ p.max_unavailable }}</span>
                      </template>
                    </el-table-column>
                    <el-table-column label="风险" min-width="200">
                      <template #default="{ row: p }">
                        <span :class="'dot ' + (p.upgrade_risk || 'green')"></span>
                        <span class="muted">{{ p.risk_note || '—' }}</span>
                      </template>
                    </el-table-column>
                    <el-table-column label="临期时刻" width="150">
                      <template #default="{ row: p }">
                        <!-- 官方只在升级即将开始时才填这个字段，有值=最后拦截机会 -->
                        <el-tag v-if="p.auto_upgrade_start_time" size="small" type="danger">
                          {{ p.auto_upgrade_start_time }}</el-tag>
                        <span v-else class="muted">—</span>
                      </template>
                    </el-table-column>
                  </el-table>
                  <el-empty v-if="!row.pools || !row.pools.length" description="未采集到节点池" :image-size="50" />
                </div>
              </template>
            </el-table-column>

            <el-table-column label="集群" min-width="190">
              <template #default="{ row }">
                {{ row.display_name || row.name }}
                <el-tag v-if="!row.synced" size="small" type="info" style="margin-left:4px">未采集</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="environment" label="环境" width="76" />
            <el-table-column label="通道" width="100">
              <template #default="{ row }">
                <span v-if="row.release_channel">{{ row.release_channel }}</span>
                <!-- 未入通道不是错误：官方规则是按 Stable 列算，但要让人看见 -->
                <el-tooltip v-else content="未加入发布通道，排期按官方规则取 Stable 列">
                  <span class="muted">未入通道</span></el-tooltip>
              </template>
            </el-table-column>
            <el-table-column prop="current_master_version" label="当前版本" min-width="150" show-overflow-tooltip />
            <el-table-column label="目标" width="90">
              <template #default="{ row }">{{ shortMinor(row.minor_target_version) || '—' }}</template>
            </el-table-column>
            <el-table-column label="预计自动升级" min-width="170">
              <template #default="{ row }">
                <template v-if="row.blocked">
                  <el-tag size="small" type="success">已挡住</el-tag>
                </template>
                <template v-else-if="row.predicted_upgrade_at">
                  <!-- 月/季度粒度绝不显示成精确日期：官网只承诺范围，显示首日会造成虚假紧迫感 -->
                  <template v-if="row.predicted_precision === 'day'">{{ row.predicted_upgrade_at }} 起</template>
                  <template v-else>{{ row.predicted_window_text }}</template>
                  <!-- 两层不确定性各自一个标签，可能同时出现（推断的版本 + 近似的日期） -->
                  <el-tooltip v-if="row.predicted_precision !== 'day'"
                    :content="`官网只给到${row.predicted_precision === 'month' ? '月' : '季度'}粒度，实际日期在 ${row.predicted_window_text}，且官方注明会变`">
                    <el-tag size="small" type="warning" style="margin-left:4px">日期近似</el-tag>
                  </el-tooltip>
                  <el-tooltip v-if="row.predicted_source === 'inferred_next_minor'"
                    content="GKE 尚未为该集群排期（minorTargetVersion 为空），目标版本按当前版本的下一个小版本推断">
                    <el-tag size="small" type="info" style="margin-left:4px">版本推断</el-tag>
                  </el-tooltip>
                </template>
                <span v-else class="muted">未知</span>
              </template>
            </el-table-column>
            <el-table-column label="倒计时" width="120">
              <template #default="{ row }">
                <span v-if="row.blocked" class="muted">—</span>
                <!-- day 粒度才是确定倒计时；月/季度只能说「最早」，否则会把 2026-08 在 7/31 说成 1 天 -->
                <span v-else-if="row.days_left !== null && row.days_left !== undefined">
                  <span :class="'dot ' + daysLevel(row.days_left)"></span>{{ daysText(row.days_left) }}</span>
                <span v-else-if="row.days_min !== null && row.days_min !== undefined">
                  <span :class="'dot ' + daysLevel(row.days_min)"></span>
                  <span class="muted">最早 </span>{{ daysText(row.days_min) }}</span>
                <span v-else class="muted">—</span>
              </template>
            </el-table-column>
            <el-table-column label="支持截止" min-width="145">
              <template #default="{ row }">
                <span v-if="!row.eos_standard_at" class="muted">—</span>
                <span v-else>
                  <span :class="'dot ' + daysLevel(row.eos_days_left)"></span>{{ row.eos_standard_at }}
                  <span class="muted" v-if="row.eos_days_left !== null">（{{ daysText(row.eos_days_left) }}）</span>
                </span>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <!-- ------------------------------------------------ 官网版本排期 -->
        <el-tab-pane :label="`官网版本排期${rows.length ? ' (' + rows.length + ')' : ''}`" name="schedule">
          <el-alert type="info" :closable="false" show-icon style="margin-bottom:10px">
            <template #title>来源 cloud.google.com/kubernetes-engine/docs/release-schedule</template>
            官网无 API，本表由页面解析而来。解析异常时会保留上次数据并打 WARN，不会静默显示错日期。
            官方注明：日期每月更新且可能变动，只给到月（2026-09）或季度（2026-Q4）的是近似值。
          </el-alert>

          <div style="margin-bottom:8px">
            <el-select v-model="chFilter" size="small" style="width:150px" placeholder="通道">
              <el-option label="全部通道" value="" />
              <el-option v-for="c in ['RAPID', 'REGULAR', 'STABLE', 'EXTENDED']" :key="c" :label="c" :value="c" />
            </el-select>
            <el-checkbox v-model="onlyAnchored" size="small" style="margin-left:10px">只看集群锚定的行</el-checkbox>
          </div>

          <el-table :data="filteredRows" size="small" v-loading="loading" max-height="560">
            <el-table-column prop="minor_version" label="版本" width="76" />
            <el-table-column prop="channel" label="通道" width="100" />
            <el-table-column label="Available" width="120">
              <template #default="{ row }">{{ row.available_raw || '—' }}</template>
            </el-table-column>
            <el-table-column label="Auto Upgrade" min-width="215">
              <template #default="{ row }">
                <b>{{ row.auto_upgrade_raw || '—' }}</b>
                <el-tag v-if="row.auto_upgrade_precision === 'month'" size="small" type="warning" style="margin-left:4px">月</el-tag>
                <el-tag v-else-if="row.auto_upgrade_precision === 'quarter'" size="small" type="warning" style="margin-left:4px">季度</el-tag>
                <span class="muted">{{ windowDays(row.auto_upgrade_days, row.auto_upgrade_days_min, row.auto_upgrade_precision) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="标准支持截止" min-width="195">
              <template #default="{ row }">
                <span :class="'dot ' + daysLevel(row.eos_standard_days ?? row.eos_standard_days_min)"></span>{{ row.eos_standard_raw || '—' }}
                <span class="muted">{{ windowDays(row.eos_standard_days, row.eos_standard_days_min, row.eos_standard_precision) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="扩展支持截止" width="130">
              <template #default="{ row }">{{ row.eos_extended_raw || '—' }}</template>
            </el-table-column>
            <el-table-column label="锚定集群" min-width="180">
              <template #default="{ row }">
                <span v-if="row.anchored_clusters && row.anchored_clusters.length">
                  ▲ {{ row.anchored_clusters.join('、') }}
                </span>
                <span v-else class="muted">—</span>
              </template>
            </el-table-column>
            <el-table-column label="" width="120" fixed="right">
              <template #default="{ row }">
                <el-tag v-if="row.is_manual" size="small" type="warning">手工</el-tag>
                <el-button link type="primary" size="small" @click="openOverride(row)">覆盖</el-button>
                <el-button v-if="row.is_manual" link type="info" size="small" @click="clearOverride(row)">还原</el-button>
              </template>
            </el-table-column>
          </el-table>
          <el-empty v-if="!loading && !rows.length" description="排期表还没同步，点上方「同步排期表」" :image-size="60" />
        </el-tab-pane>

        <!-- ------------------------------------------------ 节点健康与自动修复 -->
        <el-tab-pane :label="`节点健康${nh.length ? ' (' + nh.length + ')' : ''}`" name="health">
          <!-- 任务关着的时候「无异常」是假象，必须显著区分「没在监控」和「监控到没问题」 -->
          <el-alert v-if="nhTask && !nhTask.enabled" type="warning" :closable="false" show-icon style="margin-bottom:10px">
            <template #title>节点健康监控未开启</template>
            {{ nhTask.note }}
          </el-alert>
          <el-alert v-else-if="nhTask" type="success" :closable="false" show-icon style="margin-bottom:10px">
            <template #title>
              监控运行中（{{ nhTask.schedule }}）
              <template v-if="nhTask.last_run_at">· 最后一轮 {{ nhTask.last_run_at }}</template>
            </template>
            {{ nhTask.last_result }}
          </el-alert>

          <!-- 能提前多久是有物理上限的，写在页面上免得被当成万能预警 -->
          <div class="kv" v-if="nhTh" style="margin-bottom:10px">
            <span><b>NotReady 告警</b>{{ nhTh.not_ready_alert_after }}</span>
            <span><b>GKE 自动修复阈值</b>{{ nhTh.gke_repair_threshold }}</span>
            <span><b>磁盘预警</b>{{ nhTh.disk_predict_window }}</span>
          </div>
          <div class="muted" v-if="nhTh" style="margin-bottom:10px">{{ nhTh.note }}</div>

          <el-table :data="nh" size="small" v-loading="nhLoading">
            <el-table-column label="等级" width="80">
              <template #default="{ row }">
                <span :class="'dot ' + (row.alert_level || 'green')"></span>
                {{ row.alert_level === 'red' ? '紧急' : row.alert_level === 'yellow' ? '注意' : '—' }}
              </template>
            </el-table-column>
            <el-table-column prop="cluster" label="集群" min-width="140" show-overflow-tooltip />
            <el-table-column prop="node_name" label="节点" min-width="230" show-overflow-tooltip />
            <el-table-column label="问题" width="120">
              <template #default="{ row }">
                {{ row.alert_kind === 'not_ready' ? 'NotReady' : row.alert_kind === 'disk_full' ? '磁盘将满' : '—' }}
              </template>
            </el-table-column>
            <el-table-column label="已持续" width="120">
              <template #default="{ row }">{{ row.not_ready_text || '—' }}</template>
            </el-table-column>
            <el-table-column label="距自动修复" width="130">
              <template #default="{ row }">
                <span v-if="row.repair_in_text" :class="row.repair_in_text === '已超阈值' ? 'warn' : ''">
                  {{ row.repair_in_text }}</span>
                <span v-else class="muted">—</span>
              </template>
            </el-table-column>
            <el-table-column label="磁盘" width="90">
              <template #default="{ row }">
                <span v-if="row.disk_pct > 0">{{ row.disk_pct.toFixed(1) }}%</span>
                <span v-else class="muted">—</span>
              </template>
            </el-table-column>
            <el-table-column prop="last_alert_at" label="上次告警" width="165" />
          </el-table>
          <el-empty v-if="!nhLoading && !nh.length"
            :description="nhTask && !nhTask.enabled
              ? '监控未开启，所以这里没有数据——不代表节点没问题'
              : '当前没有异常节点'" :image-size="60" />
        </el-tab-pane>

        <!-- ------------------------------------------------ 升级与修复历史 -->
        <el-tab-pane label="升级与修复历史" name="history">
          <!-- 保留期说明必须显示：GCP operations 保留期很短，实测三个 project 合计仅 9 条、
               自动修复 0 条。空结果读成「没发生过」是错的。 -->
          <el-alert v-if="cov" :type="cov.total ? 'info' : 'warning'" :closable="false" show-icon style="margin-bottom:10px">
            <template #title>
              历史覆盖范围：{{ cov.total }} 条
              <template v-if="cov.earliest">· 最早 {{ cov.earliest }} · 最新 {{ cov.latest }}</template>
            </template>
            {{ cov.note }}
            <div v-if="cov.reason_note" style="margin-top:4px">{{ cov.reason_note }}</div>
          </el-alert>

          <div style="margin-bottom:8px;display:flex;gap:10px;align-items:center;flex-wrap:wrap">
            <el-radio-group v-model="hKind" size="small" @change="loadHistory">
              <el-radio-button value="upgrade">升级记录</el-radio-button>
              <el-radio-button value="repair">节点自动修复</el-radio-button>
            </el-radio-group>
            <el-select v-model="hCluster" size="small" style="width:190px" placeholder="全部集群" clearable @change="loadHistory">
              <el-option v-for="c in clusters" :key="c.cluster_id"
                :label="c.display_name || c.name" :value="c.cluster_id" />
            </el-select>
            <template v-if="hKind === 'upgrade'">
              <el-select v-model="hStartType" size="small" style="width:150px" placeholder="全部方式" clearable @change="loadHistory">
                <el-option label="🤖 自动升级" value="AUTOMATIC" />
                <el-option label="👤 手动升级" value="MANUAL" />
              </el-select>
              <span class="muted" v-if="hStat">
                自动 {{ hStat.AUTOMATIC || 0 }} · 手动 {{ hStat.MANUAL || 0 }}
                · 来源无方式 {{ hStat.UNKNOWN || 0 }}
              </span>
              <!-- 失败的升级最该被看见，所以给一个可点的筛选入口而不是只显示个数字 -->
              <el-button v-if="hStat && hStat.FAILED" size="small"
                :type="onlyFailed ? 'danger' : 'default'" @click="toggleFailed">
                {{ onlyFailed ? '✕ 取消只看失败' : `⚠ 失败 ${hStat.FAILED} 条` }}
              </el-button>
            </template>
          </div>

          <!-- 升级记录 -->
          <el-table v-if="hKind === 'upgrade'" :data="hRows" size="small" v-loading="hLoading" max-height="520">
            <el-table-column prop="started_at" label="开始" width="165" />
            <el-table-column prop="cluster" label="集群" min-width="150" show-overflow-tooltip />
            <el-table-column label="对象" min-width="170">
              <template #default="{ row }">
                <el-tag size="small" :type="row.scope === 'control_plane' ? 'primary' : 'info'">
                  {{ row.scope === 'control_plane' ? '控制面' : '节点池' }}</el-tag>
                <span v-if="row.pool" style="margin-left:5px">{{ row.pool }}</span>
              </template>
            </el-table-column>
            <el-table-column label="方式" width="110">
              <template #default="{ row }">
                <span v-if="row.start_type === 'AUTOMATIC'">🤖 自动</span>
                <span v-else-if="row.start_type === 'MANUAL'">👤 手动</span>
                <!-- operations 来源没有 startType，如实标「未知」不假装 -->
                <el-tooltip v-else content="该记录来自 operations.list，此来源不提供 startType 字段">
                  <span class="muted">未知</span></el-tooltip>
              </template>
            </el-table-column>
            <el-table-column label="版本变化" min-width="230">
              <template #default="{ row }">
                <span v-if="row.initial_version">{{ row.initial_version }} → {{ row.target_version }}</span>
                <span v-else class="muted">—</span>
              </template>
            </el-table-column>
            <el-table-column label="结果" width="90">
              <template #default="{ row }">
                <el-tag size="small" :type="stateType(row.state)">{{ row.state || '—' }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="duration" label="耗时" width="80" />
            <el-table-column prop="source" label="来源" width="120" />
          </el-table>

          <!-- 节点自动修复 -->
          <el-table v-else :data="hRows" size="small" v-loading="hLoading" max-height="520">
            <el-table-column prop="started_at" label="开始" width="165" />
            <el-table-column prop="cluster" label="集群" min-width="140" show-overflow-tooltip />
            <el-table-column prop="pool" label="节点池" min-width="150" show-overflow-tooltip />
            <el-table-column label="原因" min-width="180">
              <template #default="{ row }">
                <span v-if="row.repair_reason">{{ row.repair_reason }}</span>
                <el-tooltip v-else content="REST v1 的 Operation 没有 operationReason 字段，从文本也没提取到；原文见详情列">
                  <span class="muted">未解析出</span></el-tooltip>
              </template>
            </el-table-column>
            <el-table-column label="结果" width="90">
              <template #default="{ row }">
                <el-tag size="small" :type="row.status === 'DONE' ? 'success' : 'warning'">{{ row.status || '—' }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="duration" label="耗时" width="80" />
            <el-table-column prop="detail" label="详情" min-width="220" show-overflow-tooltip />
          </el-table>

          <el-empty v-if="!hLoading && !hRows.length"
            :description="hKind === 'repair'
              ? '没有采集到自动修复记录——这不代表没发生过，见上方覆盖范围说明'
              : '没有采集到升级记录，先运行「采集集群」'" :image-size="60" />
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <!-- 手工覆盖：官网页面改版导致解析错时的兜底 -->
    <el-dialog v-model="ovDlg" title="手工覆盖自动升级日期" width="440px" :close-on-click-modal="false">
      <div class="muted" style="margin-bottom:10px">
        覆盖后该行标记为「手工」，定时同步不再冲掉它。仅在官网解析出错时使用。
      </div>
      <el-form label-width="110px" size="small">
        <el-form-item label="版本 / 通道">
          <span>{{ ovRow.minor_version }} · {{ ovRow.channel }}</span>
        </el-form-item>
        <el-form-item label="原文">
          <el-input v-model="ovForm.auto_upgrade_raw" placeholder="如 2026-10-31 或 2026-Q4" />
        </el-form-item>
        <el-form-item label="归一化日期">
          <el-date-picker v-model="ovForm.auto_upgrade_at" type="date" value-format="YYYY-MM-DD"
            placeholder="用于排序和倒计时" style="width:100%" />
        </el-form-item>
        <el-form-item label="粒度">
          <el-select v-model="ovForm.auto_upgrade_precision" style="width:100%">
            <el-option label="精确到日" value="day" />
            <el-option label="只到月（近似）" value="month" />
            <el-option label="只到季度（近似）" value="quarter" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="ovDlg = false">关闭</el-button>
        <el-button type="primary" :loading="saving" @click="saveOverride">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { useAppStore } from '../stores/app'
import {
  gkeUpgradeOverview, gkeVersionSchedule, gkeOverrideSchedule,
  gkeClearScheduleOverride, runScheduledTask, gkeUpgradeHistory, gkeRepairHistory,
  gkeNodeHealth,
} from '../api/cmdb'

const app = useAppStore()
const tab = ref('board')
const loading = ref(false)
const saving = ref(false)
const syncing = reactive({ schedule: false, upgrade: false })
const sum = ref(null)
const clusters = ref([])
const rows = ref([])
const expanded = ref([])
const chFilter = ref('')
const onlyAnchored = ref(false)
const ovDlg = ref(false)
const ovRow = ref({})
const ovForm = reactive({ auto_upgrade_raw: '', auto_upgrade_at: '', auto_upgrade_precision: 'day' })

// 历史 Tab
const hKind = ref('upgrade')
const hCluster = ref(null)
const hStartType = ref(null)
const hRows = ref([])
const hStat = ref(null)
const cov = ref(null)
const hLoading = ref(false)
const onlyFailed = ref(false)

// 节点健康 Tab
const nh = ref([])
const nhTask = ref(null)
const nhTh = ref(null)
const nhLoading = ref(false)

const filteredRows = computed(() => rows.value.filter((r) =>
  (!chFilter.value || r.channel === chFilter.value) &&
  (!onlyAnchored.value || (r.anchored_clusters && r.anchored_clusters.length))))

async function reload() {
  loading.value = true
  try {
    const [ov, sc] = await Promise.all([gkeUpgradeOverview(), gkeVersionSchedule()])
    if (ov.ok) { sum.value = ov.summary; clusters.value = ov.clusters || [] }
    if (sc.ok) rows.value = sc.rows || []
  } catch (e) {
    ElMessage.error('加载失败：' + (e?.response?.data?.error || e.message))
  } finally { loading.value = false }
}

async function runTask(key, flag) {
  syncing[flag] = true
  try {
    await runScheduledTask(key)
    ElMessage.success('已触发，约数秒后刷新查看结果')
    setTimeout(() => { reload(); syncing[flag] = false }, 6000)
  } catch (e) {
    syncing[flag] = false
    ElMessage.error('触发失败：' + (e?.response?.data?.error || e.message))
  }
}

function onExpand(row, rowsExpanded) {
  expanded.value = rowsExpanded.map((r) => r.cluster_id)
}

async function loadNodeHealth() {
  nhLoading.value = true
  try {
    const r = await gkeNodeHealth()
    if (r.ok) { nh.value = r.rows || []; nhTask.value = r.task; nhTh.value = r.thresholds }
  } catch (e) {
    ElMessage.error('加载节点健康失败：' + (e?.response?.data?.error || e.message))
  } finally { nhLoading.value = false }
}

function toggleFailed() {
  onlyFailed.value = !onlyFailed.value
  loadHistory()
}

async function loadHistory() {
  hLoading.value = true
  try {
    const params = {}
    if (hCluster.value) params.cluster_id = hCluster.value
    if (hKind.value === 'upgrade') {
      if (hStartType.value) params.start_type = hStartType.value
      const r = await gkeUpgradeHistory(params)
      if (r.ok) {
        // stat 始终按全量算，否则筛选后「失败 N 条」的入口会自己消失
        hStat.value = r.stat; cov.value = r.coverage
        hRows.value = onlyFailed.value
          ? (r.rows || []).filter((x) => x.state === 'FAILED')
          : (r.rows || [])
      }
    } else {
      const r = await gkeRepairHistory(params)
      if (r.ok) { hRows.value = r.rows || []; hStat.value = null; cov.value = r.coverage }
    }
  } catch (e) {
    ElMessage.error('加载历史失败：' + (e?.response?.data?.error || e.message))
  } finally { hLoading.value = false }
}

function stateType(s) {
  if (s === 'SUCCEEDED' || s === 'DONE') return 'success'
  if (s === 'FAILED') return 'danger'
  if (s === 'RUNNING') return 'warning'
  return 'info'
}

function openOverride(row) {
  ovRow.value = row
  ovForm.auto_upgrade_raw = row.auto_upgrade_raw || ''
  ovForm.auto_upgrade_at = row.auto_upgrade_at || ''
  ovForm.auto_upgrade_precision = row.auto_upgrade_precision === 'unknown' ? 'day' : row.auto_upgrade_precision
  ovDlg.value = true
}

async function saveOverride() {
  saving.value = true
  try {
    const r = await gkeOverrideSchedule(ovRow.value.id, { ...ovForm })
    if (r.ok) { ElMessage.success('已覆盖'); ovDlg.value = false; reload() }
    else ElMessage.error(r.error || '保存失败')
  } catch (e) {
    ElMessage.error('保存失败：' + (e?.response?.data?.error || e.message))
  } finally { saving.value = false }
}

async function clearOverride(row) {
  try {
    await app.showConfirm(`还原 ${row.minor_version} · ${row.channel} 的手工覆盖？下次同步会用官网值覆盖回来。`)
    const r = await gkeClearScheduleOverride(row.id)
    if (r.ok) { ElMessage.success(r.msg || '已还原'); reload() }
  } catch { /* 用户取消 */ }
}

function shortMinor(v) {
  if (!v) return ''
  const m = String(v).replace(/^v/, '').match(/^\d+\.\d+/)
  return m ? m[0] : v
}

function daysText(d) {
  if (d === null || d === undefined) return '—'
  if (d < 0) return `已过 ${-d} 天`
  if (d === 0) return '今天'
  return `${d} 天`
}

// windowDays 排期表里的天数展示。
// day 粒度才给确定数字；月/季度只能说「最早 N 天」——官网只承诺范围，
// 拿归一化后的首日当确定日期会系统性提前（季度粒度最多 89 天）。
function windowDays(days, daysMin, precision) {
  if (precision === 'day' && days !== null && days !== undefined) return ` · ${daysText(days)}`
  if (daysMin !== null && daysMin !== undefined) return ` · 最早 ${daysText(daysMin)}`
  return ''
}

// 红黄绿：30 天是提醒锚点，7 天是最后窗口
function daysLevel(d) {
  if (d === null || d === undefined) return ''
  if (d < 0 || d <= 7) return 'red'
  if (d <= 30) return 'yellow'
  return 'green'
}

function verdictType(row) {
  if (!row.synced) return 'info'
  if (row.eos_days_left !== null && row.eos_days_left <= 30) return 'error'
  if (row.blocked) return 'success'
  if (row.days_left !== null && row.days_left <= 30) return 'warning'
  return 'info'
}

// maintWindow 把 maintenancePolicy JSON 提炼成一句人话；解析不了就原样不显示，不猜
function maintWindow(row) {
  if (!row.maintenance_policy_json) return ''
  try {
    const w = JSON.parse(row.maintenance_policy_json)?.window
    if (!w) return ''
    if (w.dailyMaintenanceWindow) return `每日维护窗口 ${w.dailyMaintenanceWindow.startTime}（时长 ${w.dailyMaintenanceWindow.duration || '—'}）`
    if (w.recurringWindow) {
      const ex = Object.keys(w.maintenanceExclusions || {}).length
      return `周期性维护窗口 ${w.recurringWindow.recurrence || ''}` + (ex ? ` · ${ex} 个维护排除` : '')
    }
  } catch { /* JSON 结构变了就不显示，别瞎猜 */ }
  return ''
}

onMounted(async () => { await reload(); loadHistory(); loadNodeHealth() })
</script>

<style scoped>
.sum-bar { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; flex-wrap: wrap; }
.chip { background: var(--el-fill-color-light); border-radius: 10px; padding: 2px 10px; font-size: 12px; }
.chip.critical { background: var(--el-color-danger-light-9); color: var(--el-color-danger); }
.detail { padding: 6px 10px 10px; }
.kv { display: flex; gap: 18px; flex-wrap: wrap; font-size: 12px; margin-bottom: 4px; }
.kv b { color: var(--el-text-color-secondary); font-weight: 500; margin-right: 4px; }
.kv .warn { color: var(--el-color-warning); }
.muted { color: var(--el-text-color-secondary); font-size: 12px; }
.dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 5px; }
.dot.red { background: var(--el-color-danger); }
.dot.yellow { background: var(--el-color-warning); }
.dot.green { background: var(--el-color-success); }
</style>
