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
      <el-button v-if="canUpgrade" size="small" :loading="syncing.upgrade" @click="runTask('gke_upgrade_sync', 'upgrade')">采集集群</el-button>
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

          <div style="margin-bottom:8px;display:flex;gap:8px;align-items:center;flex-wrap:wrap" v-if="clusters.length">
            <el-select v-model="envFilter" size="small" style="width:120px" placeholder="全部环境" clearable>
              <el-option v-for="e in envOptions" :key="e" :label="e" :value="e" />
            </el-select>
            <el-input v-model="boardQ" size="small" style="width:180px" clearable placeholder="搜集群 / 节点池名" />
            <!-- 直接对应「哪些集群要动手」：脱队节点池不会自己跟上，是 EOS 到期时的真实风险源 -->
            <el-checkbox v-model="onlyStranded" size="small">只看有节点池脱队的</el-checkbox>
            <span class="muted" style="margin-left:auto">{{ boardRows.length }} / {{ clusters.length }} 个集群</span>
          </div>

          <el-table :data="boardRows" size="small" v-loading="loading" row-key="cluster_id"
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
                    <!-- 节点池自己的支持截止：集群「支持截止」列取的就是这里的最早值 -->
                    <el-table-column label="支持截止" min-width="150">
                      <template #default="{ row: p }">
                        <span v-if="!p.eos_standard_at" class="muted">—</span>
                        <span v-else>
                          <span :class="'dot ' + daysLevel(p.eos_days_left)"></span>{{ p.eos_standard_at }}
                          <span class="muted" v-if="p.eos_days_left !== null">（{{ daysText(p.eos_days_left) }}）</span>
                        </span>
                      </template>
                    </el-table-column>
                    <el-table-column label="自动升级" width="100" align="center">
                      <template #default="{ row: p }">
                        <!-- 落后控制面 + 自动升级关 = 这个池永远不会自己跟上，是 EOS 的真实风险源 -->
                        <el-tooltip v-if="p.stranded" content="落后控制面且自动升级已关，节点池不会自己跟上，必须人工升级">
                          <el-tag size="small" type="danger">关 · 脱队</el-tag>
                        </el-tooltip>
                        <el-tag v-else size="small" :type="p.auto_upgrade ? 'warning' : 'info'">
                          {{ p.auto_upgrade ? '开' : '关' }}</el-tag>
                      </template>
                    </el-table-column>
                    <!-- 自动修复关 = 节点坏了 GKE 不会自动重建，得人工上。
                         这和「自动升级关」是同等重要的开关，呈现规格要一致，不能只中性显示「关」 -->
                    <el-table-column label="自动修复" width="100" align="center">
                      <template #default="{ row: p }">
                        <el-tooltip v-if="p.repair_off"
                          content="自动修复已关：节点故障时 GKE 不会自动 drain 重建，需人工介入">
                          <el-tag size="small" type="danger">关 · 需人工</el-tag>
                        </el-tooltip>
                        <el-tag v-else size="small" type="success">开</el-tag>
                      </template>
                    </el-table-column>
                    <el-table-column label="升级策略" min-width="150">
                      <template #default="{ row: p }">
                        {{ p.strategy || '—' }}
                        <span class="muted" v-if="p.strategy !== 'BLUE_GREEN'">
                          +{{ p.max_surge }}/-{{ p.max_unavailable }}</span>
                      </template>
                    </el-table-column>
                    <el-table-column label="风险 / 排期影响" min-width="280">
                      <template #default="{ row: p }">
                        <span :class="'dot ' + (p.upgrade_risk || 'green')"></span>
                        <span class="muted">{{ p.risk_note || rollingNote(p) }}</span>
                      </template>
                    </el-table-column>
                    <el-table-column label="临期时刻" width="150">
                      <template #default="{ row: p }">
                        <!-- 官方只在升级即将开始时才填这个字段，有值=最后拦截机会 -->
                        <el-tag v-if="p.auto_upgrade_start_time" size="small" type="danger">
                          {{ p.auto_upgrade_start_time }}</el-tag>
                        <el-tooltip v-else
                          content="GKE 只在升级即将开始时才填这个字段。为空=尚未临近，属正常；一旦有值就是最后的拦截机会">
                          <span class="muted">—</span></el-tooltip>
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
            <el-table-column label="目标" width="120">
              <template #default="{ row }">
                <span v-if="row.minor_target_version">{{ shortMinor(row.minor_target_version) }}</span>
                <!-- 目标为空但预计日期有值时，日期是按这个推断版本算的，必须显示出来 -->
                <span v-else-if="row.inferred_target_version">
                  {{ row.inferred_target_version }}
                  <el-tag size="small" type="info" style="margin-left:2px">推断</el-tag>
                </span>
                <span v-else class="muted">—</span>
              </template>
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
            <!-- 用「控制面与所有节点池里最早的」EOS。只看控制面会漏掉最危险的情况：
                 控制面已升到新版本、但节点池还跑在几天后就到期的旧版本 -->
            <el-table-column label="支持截止" min-width="190">
              <template #default="{ row }">
                <span v-if="!row.effective_eos_at" class="muted">—</span>
                <span v-else>
                  <span :class="'dot ' + daysLevel(row.effective_eos_days)"></span>{{ row.effective_eos_at }}
                  <span class="muted" v-if="row.effective_eos_days !== null">（{{ daysText(row.effective_eos_days) }}）</span>
                  <el-tooltip v-if="row.effective_eos_source && row.effective_eos_source !== '控制面'"
                    :content="`最早到期的是${row.effective_eos_source}；控制面本身是 ${row.eos_standard_at || '未知'}`">
                    <el-tag size="small" type="danger" style="margin-left:4px">节点池</el-tag>
                  </el-tooltip>
                  <!-- EXTENDED 通道的硬期限是扩展支持，不标出来会以为看的是标准支持 -->
                  <el-tooltip v-if="row.eos_basis === '扩展支持'"
                    content="该集群订阅 EXTENDED 通道，标准支持结束后仍可继续使用，硬期限是扩展支持截止">
                    <el-tag size="small" type="success" style="margin-left:4px">扩展支持</el-tag>
                  </el-tooltip>
                </span>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <!-- ------------------------------------------------ 升级预案 -->
        <!-- 排一次升级需要的东西全在这一屏：要多久、要多少配额、会断什么、卡在哪、怎么点、事后怎么验。
             CMDB 只出方案不执行——执行在 GCP 控制台，所以给的是页面步骤而不是命令。 -->
        <el-tab-pane label="升级预案" name="plan">
          <div style="margin-bottom:10px;display:flex;gap:8px;align-items:center;flex-wrap:wrap">
            <el-select v-model="planCluster" size="small" style="width:200px" placeholder="选择集群" @change="onPlanCluster">
              <el-option v-for="c in clusters" :key="c.cluster_id"
                :label="c.display_name || c.name" :value="c.cluster_id" />
            </el-select>
            <!-- 版本从 GKE getServerConfig 采来，是该区域真实可选的那份清单。
                 仍允许自己输（allow-create）：清单还没采到时不能把人卡死 -->
            <el-select v-model="planTarget" size="small" style="width:280px" clearable filterable
              allow-create default-first-option
              :placeholder="verOpts.length ? '选择目标版本' : '目标版本（清单未采集，需手输）'">
              <el-option v-for="v in verOpts" :key="v" :label="v" :value="v" />
            </el-select>
            <el-button size="small" type="primary" :loading="planLoading" @click="loadPlan">生成预案</el-button>
            <span class="muted" v-if="plan">生成于 {{ plan.generated_at }}</span>
          </div>

          <!-- 失败必须占住版面：只弹个 toast 的话，toast 消失后页面看起来就像「这个集群没风险」 -->
          <el-alert v-if="planError" type="error" :closable="false" show-icon style="margin-bottom:10px">
            <template #title>预案生成失败，当前页面不代表该集群没有风险</template>
            {{ planError }}
          </el-alert>

          <el-alert v-if="!planCluster && !planError" type="info" :closable="false" show-icon>
            <template #title>选一个集群生成升级预案</template>
            预案会算出分池耗时、配额需求、会中断的服务、会卡住 drain 的 PDB，并留下升级前基线快照。
            <b>CMDB 不执行升级</b>——预案给的是 GCP 控制台的点击步骤。
          </el-alert>

          <template v-if="plan">
            <!-- 先回答「要多久」，这是排窗口时唯一先看的数字 -->
            <div class="sum-bar" style="margin-bottom:12px">
              <span class="chip critical">
                预计总耗时 {{ fmtMin(plan.total_estimate.min_minutes) }} ~ {{ fmtMin(plan.total_estimate.max_minutes) }}
              </span>
              <span class="chip" :class="plan.total_estimate.measured ? 'ok' : ''">
                {{ plan.total_estimate.measured ? '基于实测' : '含经验区间' }}
              </span>
              <span class="chip" v-if="plan.target_version">目标 {{ plan.target_version }}</span>
              <span class="chip" v-if="totalExtraNodes">升级峰值多占 {{ totalExtraNodes }} 台</span>
              <span class="chip critical" v-if="plan.blocking_pdbs && plan.blocking_pdbs.length">
                {{ plan.blocking_pdbs.length }} 个 PDB 会卡住 drain
              </span>
              <span class="chip critical" v-if="!plan.pdbs_collected">PDB 未采集</span>
            </div>

            <el-alert v-for="(w, i) in plan.warnings" :key="'pw' + i" type="warning"
              :closable="false" show-icon style="margin-bottom:6px" :title="w" />

            <!-- 预估的可信度：缺了哪些参数必须让人看见，否则区间会被当定值用 -->
            <el-alert v-if="plan.total_estimate.incomplete && plan.total_estimate.incomplete.length"
              type="warning" :closable="false" show-icon style="margin:6px 0 12px">
              <template #title>耗时预估有 {{ plan.total_estimate.incomplete.length }} 项参数缺失，排停机窗口请按上限算</template>
              <div v-for="(s, i) in plan.total_estimate.incomplete" :key="'inc' + i" style="font-size:12px">· {{ s }}</div>
            </el-alert>

            <!-- 控制面 -->
            <el-card shadow="never" class="plan-card">
              <div class="plan-card-head">
                <b>控制面</b>
                <span class="muted">{{ plan.control_plane.current_version }} → {{ plan.control_plane.target_version || '待定' }}</span>
                <el-tag size="small" type="danger" v-if="plan.control_plane.minor_jump > 1">
                  跨 {{ plan.control_plane.minor_jump }} 个小版本
                </el-tag>
                <span style="margin-left:auto">
                  <b>{{ fmtMin(plan.control_plane.estimate.min_minutes) }} ~ {{ fmtMin(plan.control_plane.estimate.max_minutes) }}</b>
                </span>
              </div>
              <div class="muted" style="font-size:12px">依据：{{ plan.control_plane.estimate.basis }}</div>
              <div v-for="(w, i) in plan.control_plane.warnings" :key="'cw' + i" class="plan-warn">⚠ {{ w }}</div>
            </el-card>

            <!-- 各节点池 -->
            <el-card v-for="p in plan.pools" :key="p.name" shadow="never" class="plan-card">
              <div class="plan-card-head">
                <b>{{ p.name }}</b>
                <span class="muted">{{ p.node_count }} 台 · {{ p.current_version }}</span>
                <el-tag size="small" :type="p.strategy === 'BLUE_GREEN' ? 'warning' : 'info'">{{ p.strategy || '策略未知' }}</el-tag>
                <el-tag size="small" type="danger" v-if="p.auto_repair_off">autoRepair 已关</el-tag>
                <span style="margin-left:auto">
                  <b>{{ fmtMin(p.estimate.min_minutes) }} ~ {{ fmtMin(p.estimate.max_minutes) }}</b>
                  <span class="muted" v-if="p.estimate.batches"> · {{ p.estimate.batches }} 批</span>
                </span>
              </div>
              <div class="muted" style="font-size:12px">依据：{{ p.estimate.basis }}</div>

              <!-- BLUE_GREEN 参数：null 显示「未配(用GKE默认)」而不是 0——
                   显示成 0 会让人以为观察期不存在，而默认观察期通常是一小时量级 -->
              <div class="plan-params" v-if="p.strategy === 'BLUE_GREEN'">
                <span>批次：{{ p.batch_node_count ?? (p.batch_percentage ? (p.batch_percentage * 100) + '%' : '未配（用 GKE 默认）') }}</span>
                <span>每批观察期：{{ fmtSoak(p.batch_soak_sec) }}</span>
                <span>整池观察期：{{ fmtSoak(p.node_pool_soak_sec) }}</span>
                <span v-if="p.rollout_policy">策略：{{ p.rollout_policy }}</span>
              </div>

              <div class="plan-quota">📦 {{ p.quota_note }}</div>
              <div v-for="(w, i) in p.warnings" :key="'pw' + i" class="plan-warn">⚠ {{ w }}</div>

              <el-collapse v-if="p.single_replica_workloads.length || p.concentrated_nodes.length" style="margin-top:8px">
                <el-collapse-item v-if="p.single_replica_workloads.length"
                  :title="`必然中断的单副本服务 (${p.single_replica_workloads.length})`" :name="p.name + '-sr'">
                  <el-table :data="p.single_replica_workloads" size="small" border>
                    <el-table-column prop="namespace" label="命名空间" width="180" />
                    <el-table-column prop="kind" label="类型" width="110" />
                    <el-table-column prop="name" label="名称" />
                    <el-table-column prop="node" label="所在节点" show-overflow-tooltip />
                  </el-table>
                </el-collapse-item>
                <el-collapse-item v-if="p.concentrated_nodes.length"
                  :title="`单点集中节点 (${p.concentrated_nodes.length}) —— drain 一台断多个服务`" :name="p.name + '-cn'">
                  <div v-for="n in p.concentrated_nodes" :key="n.node" class="conc-node">
                    <div><b>{{ n.node }}</b> — {{ n.count }} 个有状态/单副本 Pod</div>
                    <div class="muted" style="font-size:12px">{{ n.pods.join('、') }}</div>
                    <div class="plan-warn">⚠ {{ n.risk_note }}</div>
                  </div>
                </el-collapse-item>
              </el-collapse>
            </el-card>

            <!-- PDB 阻塞 -->
            <el-card shadow="never" class="plan-card">
              <div class="plan-card-head"><b>drain 阻塞风险（PodDisruptionBudget）</b></div>
              <el-alert :type="plan.pdbs_collected ? (plan.blocking_pdbs.length ? 'warning' : 'success') : 'error'"
                :closable="false" show-icon :title="plan.pdb_note" style="margin-bottom:8px" />
              <el-table v-if="plan.blocking_pdbs.length" :data="plan.blocking_pdbs" size="small" border>
                <el-table-column prop="namespace" label="命名空间" width="180" />
                <el-table-column prop="name" label="PDB" width="220" />
                <el-table-column label="所在节点池" width="220">
                  <template #default="{ row }">
                    <el-tooltip :content="row.pools_note" placement="top">
                      <span>{{ row.pools.join('、') || '—' }} <span class="muted">(近似)</span></span>
                    </el-tooltip>
                  </template>
                </el-table-column>
                <el-table-column prop="risk_note" label="为什么卡 / 怎么办" show-overflow-tooltip />
              </el-table>
            </el-card>

            <!-- 升级前基线 -->
            <el-card shadow="never" class="plan-card">
              <div class="plan-card-head">
                <b>升级前基线快照</b>
                <span class="muted">{{ plan.baseline.taken_at }}</span>
                <!-- 这一步是升级前的必做动作：不存快照，升完就没有可比对的东西了 -->
                <el-button size="small" type="primary" plain style="margin-left:auto"
                  :loading="savingBaseline" :disabled="!plan.baseline.pods_collected"
                  @click="saveBaseline">保存为基线</el-button>
              </div>
              <div class="sum-bar" style="margin:0 0 8px">
                <span class="chip">节点 {{ plan.baseline.nodes }}</span>
                <span class="chip">Pod {{ plan.baseline.pods }}</span>
                <span class="chip ok">Running {{ plan.baseline.running }}</span>
                <span class="chip" :class="plan.baseline.failed ? 'critical' : ''">Failed {{ plan.baseline.failed }}</span>
                <span class="chip" :class="plan.baseline.pending ? 'critical' : ''">Pending {{ plan.baseline.pending }}</span>
                <span class="chip">工作负载 {{ plan.baseline.workloads }}</span>
              </div>
              <!-- 没采过 Pod 时数字全是 0，看起来像「空集群」——必须显式报错而不是让人自己看出来 -->
              <el-alert v-if="!plan.baseline.pods_collected" type="error" :closable="false" show-icon
                style="margin-bottom:8px" :title="plan.baseline.note" />
              <div v-else class="muted" style="font-size:12px;margin-bottom:8px">{{ plan.baseline.note }}</div>
              <el-collapse v-if="plan.baseline.known_bad && plan.baseline.known_bad.length">
                <el-collapse-item :title="`升级前已存在的异常 (${plan.baseline.known_bad.length}) —— 升级后原样出现即与升级无关`" name="kb">
                  <el-table :data="plan.baseline.known_bad" size="small" border max-height="360">
                    <el-table-column prop="namespace" label="命名空间" width="180" />
                    <el-table-column prop="pod" label="Pod" show-overflow-tooltip />
                    <el-table-column prop="phase" label="状态" width="100" />
                    <el-table-column prop="restarts" label="重启" width="80" />
                    <el-table-column prop="reason" label="说明" show-overflow-tooltip />
                  </el-table>
                </el-collapse-item>
              </el-collapse>

              <!-- 存过的快照列表：升完后要拿哪一份来比对，得看得见 -->
              <el-collapse v-if="baselines.length" style="margin-top:8px">
                <el-collapse-item :title="`已保存的基线快照 (${baselines.length})`" name="bl">
                  <el-table :data="baselines" size="small" border max-height="260">
                    <el-table-column prop="taken_at" label="保存时刻" width="170" />
                    <el-table-column prop="target_version" label="当时的目标版本" min-width="180" show-overflow-tooltip />
                    <el-table-column prop="nodes" label="节点" width="70" />
                    <el-table-column prop="pods" label="Pod" width="80" />
                    <el-table-column prop="running" label="Running" width="90" />
                    <el-table-column prop="failed" label="Failed" width="80" />
                    <el-table-column prop="pending" label="Pending" width="90" />
                    <el-table-column prop="known_bad" label="已知异常" width="90" />
                  </el-table>
                  <div class="muted" style="font-size:12px;margin-top:6px">
                    升级后拿<b>升级前那一份</b>与当前状态比对。和新生成的基线比没有意义——新基线就是升级后的状态。
                  </div>
                </el-collapse-item>
              </el-collapse>
            </el-card>

            <!-- 执行步骤 + 验证清单 -->
            <el-card shadow="never" class="plan-card">
              <div class="plan-card-head"><b>GCP 控制台执行步骤</b>
                <span class="muted">顺序是硬约束不是建议</span>
              </div>
              <ol class="plan-steps">
                <li v-for="(s, i) in plan.console_steps" :key="'cs' + i">{{ s }}</li>
              </ol>
            </el-card>

            <el-card shadow="never" class="plan-card">
              <div class="plan-card-head"><b>升级后验证清单</b></div>
              <ul class="plan-steps">
                <li v-for="(s, i) in plan.verification" :key="'vf' + i">{{ s }}</li>
              </ul>
            </el-card>

            <!-- 实测节奏：升完之后这里自动出数，是外推生产窗口的依据 -->
            <el-card shadow="never" class="plan-card">
              <div class="plan-card-head">
                <b>实测节奏</b>
                <span class="muted">从节点增删事件还原，用于外推其他集群</span>
                <el-select v-model="progHours" size="small" style="width:130px;margin-left:auto" @change="loadProgress">
                  <el-option label="最近 24 小时" :value="24" />
                  <el-option label="最近 3 天" :value="72" />
                  <el-option label="最近 30 天" :value="720" />
                </el-select>
              </div>
              <template v-if="prog">
                <el-alert v-for="(w, i) in prog.warnings" :key="'gw' + i" type="info"
                  :closable="false" show-icon style="margin-bottom:6px" :title="w" />
                <el-table v-if="prog.pools.length" :data="prog.pools" size="small" border style="margin-bottom:8px">
                  <el-table-column prop="pool" label="节点池" show-overflow-tooltip />
                  <el-table-column prop="at" label="升级时间" width="160" />
                  <el-table-column prop="nodes" label="换掉" width="70" />
                  <el-table-column prop="batches" label="批次" width="70" />
                  <el-table-column prop="batch_size" label="并行度" width="80" />
                  <el-table-column label="单批(中位/最慢)" width="140">
                    <template #default="{ row }">{{ row.median_batch_minutes }} / {{ row.slowest_batch_minutes }} 分</template>
                  </el-table-column>
                  <el-table-column prop="total_minutes" label="整池(分)" width="90" />
                  <el-table-column prop="note" label="说明" show-overflow-tooltip />
                </el-table>
                <!-- 控制面与节点在同一条时间线上，但精度差一个数量级，逐行标出来 -->
                <el-collapse v-if="prog.events && prog.events.length" style="margin-bottom:8px">
                  <el-collapse-item :title="`版本变更时间线 (${prog.events.length})`" name="tl">
                    <el-table :data="prog.events" size="small" border max-height="360">
                      <el-table-column prop="detected_at" label="采集到的时刻" width="160" />
                      <el-table-column label="对象" min-width="200">
                        <template #default="{ row }">
                          <el-tag v-if="row.scope === 'control_plane'" size="small" type="warning">控制面</el-tag>
                          <span v-else>{{ row.node }}<span class="muted" v-if="row.pool"> · {{ row.pool }}</span></span>
                        </template>
                      </el-table-column>
                      <el-table-column prop="event" label="事件" width="130" />
                      <el-table-column label="版本" min-width="240">
                        <template #default="{ row }">
                          <span class="muted" v-if="row.from_version">{{ row.from_version }} →</span>
                          {{ row.to_version || '（已移除）' }}
                        </template>
                      </el-table-column>
                      <el-table-column prop="precision" label="时间精度" min-width="240" show-overflow-tooltip />
                    </el-table>
                  </el-collapse-item>
                </el-collapse>

                <pre class="extrapolate">{{ prog.extrapolate }}</pre>
                <div class="muted" style="font-size:12px">
                  {{ prog.precision }}<br />采集状态：{{ prog.collection_note }}
                </div>
              </template>
            </el-card>
          </template>
        </el-tab-pane>

        <!-- ------------------------------------------------ 官网版本排期 -->
        <el-tab-pane :label="`官网版本排期${rows.length ? ' (' + rows.length + ')' : ''}`" name="schedule">
          <el-alert type="info" :closable="false" show-icon style="margin-bottom:10px">
            <template #title>来源 cloud.google.com/kubernetes-engine/docs/release-schedule</template>
            官网无 API，本表由页面解析而来。解析异常时会保留上次数据并打 WARN，不会静默显示错日期。
            官方注明：日期每月更新且可能变动，只给到月（2026-09）或季度（2026-Q4）的是近似值。
          </el-alert>

          <div style="margin-bottom:8px;display:flex;gap:8px;align-items:center;flex-wrap:wrap">
            <el-select v-model="chFilter" size="small" style="width:130px" placeholder="全部通道"
              clearable @change="schedPage = 1">
              <el-option v-for="c in ['RAPID', 'REGULAR', 'STABLE', 'EXTENDED']" :key="c" :label="c" :value="c" />
            </el-select>
            <el-select v-model="eosFilter" size="small" style="width:150px" placeholder="全部支持状态"
              clearable @change="schedPage = 1">
              <el-option label="30 天内到期" value="30" />
              <el-option label="90 天内到期" value="90" />
              <el-option label="已过期" value="expired" />
            </el-select>
            <el-input v-model="schedQ" size="small" style="width:140px" clearable
              placeholder="版本号，如 1.35" @input="schedPage = 1" />
            <!-- 1.30~1.32 全过期了，12 行噪音占 43%，一个开关就能滤掉 -->
            <el-checkbox v-model="hideExpired" size="small" @change="schedPage = 1">只看未过期</el-checkbox>
            <el-checkbox v-model="onlyAnchored" size="small" @change="schedPage = 1">只看集群锚定的行</el-checkbox>
            <span class="muted" style="margin-left:auto">{{ filteredRows.length }} / {{ rows.length }} 行</span>
          </div>

          <el-table :data="pagedSchedule" size="small" v-loading="loading" max-height="560">
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
                <el-button v-if="canUpgrade" link type="primary" size="small" @click="openOverride(row)">覆盖</el-button>
                <el-button v-if="canUpgrade && row.is_manual" link type="info" size="small" @click="clearOverride(row)">还原</el-button>
              </template>
            </el-table-column>
          </el-table>
          <div class="pager" v-if="filteredRows.length > schedPageSize">
            <el-pagination layout="total, sizes, prev, pager, next" :total="filteredRows.length"
              v-model:current-page="schedPage" v-model:page-size="schedPageSize"
              :page-sizes="[10, 20, 50]" size="small" background />
          </div>
          <el-empty v-if="!loading && !rows.length" description="排期表还没同步，点上方「同步排期表」" :image-size="60" />
          <el-empty v-else-if="!loading && !filteredRows.length" description="当前筛选没有匹配的行" :image-size="60" />
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

          <div style="margin-bottom:8px;display:flex;gap:8px;align-items:center;flex-wrap:wrap" v-if="nh.length">
            <el-select v-model="nhLevel" size="small" style="width:110px" placeholder="全部等级" clearable>
              <el-option label="紧急" value="red" />
              <el-option label="注意" value="yellow" />
            </el-select>
            <el-select v-model="nhKindF" size="small" style="width:130px" placeholder="全部问题" clearable>
              <el-option label="NotReady" value="not_ready" />
              <el-option label="磁盘将满" value="disk_full" />
            </el-select>
            <el-input v-model="nhQ" size="small" style="width:200px" clearable placeholder="搜节点名 / 集群" />
            <span class="muted" style="margin-left:auto">{{ nhRows.length }} / {{ nh.length }} 个节点</span>
          </div>

          <el-table :data="nhRows" size="small" v-loading="nhLoading">
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
            <div v-if="cov.truncated" style="margin-top:4px;color:var(--el-color-warning)">{{ cov.truncated_note }}</div>
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
              <el-select v-model="hStartType" size="small" style="width:130px" placeholder="全部方式" clearable @change="loadHistory">
                <el-option label="🤖 自动升级" value="AUTOMATIC" />
                <el-option label="👤 手动升级" value="MANUAL" />
              </el-select>
              <!-- 后端 API 早就支持 scope 参数，前端补个下拉即可 -->
              <el-select v-model="hScope" size="small" style="width:130px" placeholder="全部对象" clearable @change="loadHistory">
                <el-option label="控制面" value="control_plane" />
                <el-option label="节点池" value="nodepool" />
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
            <el-select v-model="hDays" size="small" style="width:120px" @change="hPage = 1">
              <el-option label="全部时间" :value="0" />
              <el-option label="近 7 天" :value="7" />
              <el-option label="近 30 天" :value="30" />
              <el-option label="近 90 天" :value="90" />
            </el-select>
            <el-input v-model="hQ" size="small" style="width:170px" clearable
              placeholder="搜节点池 / 版本号" @input="hPage = 1" />
            <template v-if="false">
            </template>
          </div>

          <!-- 升级记录 -->
          <el-table v-if="hKind === 'upgrade'" :data="pagedHistory" size="small" v-loading="hLoading" max-height="520">
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
          <el-table v-else :data="pagedHistory" size="small" v-loading="hLoading" max-height="520">
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

          <div class="pager" v-if="filteredHistory.length > hPageSize">
            <el-pagination layout="total, sizes, prev, pager, next" :total="filteredHistory.length"
              v-model:current-page="hPage" v-model:page-size="hPageSize"
              :page-sizes="[10, 20, 50, 100]" size="small" background />
          </div>
          <el-empty v-if="!hLoading && !hRows.length"
            :description="hKind === 'repair'
              ? '没有采集到自动修复记录——这不代表没发生过，见上方覆盖范围说明'
              : '没有采集到升级记录，先运行「采集集群」'" :image-size="60" />
          <el-empty v-else-if="!hLoading && !filteredHistory.length" description="当前筛选没有匹配的记录" :image-size="60" />
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
import { useAuthStore } from '../stores/auth'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { useAppStore } from '../stores/app'
import {
  gkeUpgradeOverview, gkeVersionSchedule, gkeOverrideSchedule,
  gkeClearScheduleOverride, runScheduledTask, gkeUpgradeHistory, gkeRepairHistory,
  gkeNodeHealth, gkeUpgradePlan, gkeUpgradeProgress, gkeAvailableVersions,
  gkeSaveBaseline, gkeListBaselines,
} from '../api/cmdb'

const auth = useAuthStore()
const canUpgrade = computed(() => auth.hasButton('manage_upgrade'))
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
const hideExpired = ref(false)
const eosFilter = ref('')
const schedQ = ref('')
const schedPage = ref(1); const schedPageSize = ref(10)
// 看板筛选
const envFilter = ref('')
const boardQ = ref('')
const onlyStranded = ref(false)
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
const hScope = ref(null)
const hQ = ref('')
const hDays = ref(0)
const hPage = ref(1); const hPageSize = ref(20)

// 升级预案 Tab
const planCluster = ref(null)
const planTarget = ref('')
const plan = ref(null)
const planLoading = ref(false)
const planError = ref('')
const prog = ref(null)
const progHours = ref(24)
const verOpts = ref([])
const baselines = ref([])
const savingBaseline = ref(false)

async function loadBaselines() {
  if (!planCluster.value) { baselines.value = []; return }
  try { baselines.value = (await gkeListBaselines({ cluster_id: planCluster.value })).items || [] }
  catch (e) { baselines.value = []; reportError(e, '读取基线快照失败') }
}

async function saveBaseline() {
  if (!plan.value) return
  savingBaseline.value = true
  try {
    const params = { cluster_id: planCluster.value }
    if (plan.value.target_version) params.target_version = plan.value.target_version
    await gkeSaveBaseline(params)
    ElMessage.success('基线已存档，升级后拿它来比对')
    await loadBaselines()
  } catch (e) { reportError(e, '保存基线失败') }
  finally { savingBaseline.value = false }
}

// 换集群时先拉该区域的可选版本，再生成预案。
// 版本清单是按「project+区域」存的，换集群可能换区域，不能沿用上一个的。
async function onPlanCluster() {
  verOpts.value = []
  planTarget.value = ''
  if (!planCluster.value) return
  try {
    const r = await gkeAvailableVersions({ cluster_id: planCluster.value })
    verOpts.value = r.versions || []
    // 清单没采到时给出可操作的提示，而不是留个空下拉框让人以为没版本可升
    if (r.note) ElMessage.warning(r.note)
  } catch (e) {
    reportError(e, '读取可用版本失败')
  }
  await loadPlan()
}

// 升级峰值一共要多占几台——BLUE_GREEN 按池翻倍，配额不够会在深夜窗口里升到一半失败
const totalExtraNodes = computed(() =>
  (plan.value?.pools || []).reduce((s, p) => s + (p.extra_nodes_needed || 0), 0))

// 分钟转人读。排窗口时「310 分钟」远不如「5 小时 10 分」直观
function fmtMin(m) {
  if (m === null || m === undefined) return '—'
  if (m < 60) return m + ' 分'
  const h = Math.floor(m / 60); const r = m % 60
  return r ? `${h} 小时 ${r} 分` : `${h} 小时`
}

// 观察期：null 是「API 没给，GKE 会用自己的默认值」，不是 0。
// 显示成 0 会让人以为不用等，而默认观察期通常是一小时量级——这个区别直接决定窗口排多长。
function fmtSoak(sec) {
  if (sec === null || sec === undefined) return '未配（用 GKE 默认，未计入预估）'
  if (sec === 0) return '0（不等待）'
  return sec >= 60 ? Math.round(sec / 60) + ' 分钟' : sec + ' 秒'
}

// http.js 的响应拦截器（若已启用）会把错误规范化并全局弹一次 toast，规范化后的错误带 __cmdb 标记。
// 这里据此决定要不要再弹：带标记说明全局已经提示过，重复弹就是同一个错误两条红条；
// 没有标记（拦截器未启用时）则由本页负责提示，两种情况都不会静默失败。
function reportError(e, prefix) {
  const msg = e?.__cmdb ? e.message : (e?.response?.data?.error || e?.message || '未知错误')
  if (!e?.__cmdb) ElMessage.error(prefix + '：' + msg)
  return msg
}

async function loadPlan() {
  if (!planCluster.value) return
  planLoading.value = true
  planError.value = ''
  try {
    const params = { cluster_id: planCluster.value }
    if (planTarget.value.trim()) params.target_version = planTarget.value.trim()
    plan.value = await gkeUpgradePlan(params)
    await loadProgress()
    await loadBaselines()
  } catch (e) {
    // 预案查询失败必须清空并报出来：残留上一次的预案会被当成本次结果，
    // 而空白页会被读成「没有风险」——两种都比报错危险
    plan.value = null
    planError.value = reportError(e, '生成预案失败')
  } finally {
    planLoading.value = false
  }
}

async function loadProgress() {
  if (!planCluster.value) return
  try {
    prog.value = await gkeUpgradeProgress({ cluster_id: planCluster.value, hours: progHours.value })
  } catch (e) {
    prog.value = null
    reportError(e, '读取实测节奏失败')
  }
}

// 节点健康 Tab
const nh = ref([])
const nhTask = ref(null)
const nhTh = ref(null)
const nhLoading = ref(false)
const nhLevel = ref('')
const nhKindF = ref('')
const nhQ = ref('')

const filteredRows = computed(() => rows.value.filter((r) => {
  if (chFilter.value && r.channel !== chFilter.value) return false
  if (onlyAnchored.value && !(r.anchored_clusters && r.anchored_clusters.length)) return false
  if (schedQ.value && !String(r.minor_version).includes(schedQ.value.trim())) return false
  // 支持状态用 days_min（区间最早端），与倒计时口径一致
  const d = r.eos_standard_days ?? r.eos_standard_days_min
  if (hideExpired.value && d !== null && d !== undefined && d < 0) return false
  if (eosFilter.value) {
    if (d === null || d === undefined) return false
    if (eosFilter.value === 'expired' && d >= 0) return false
    if (eosFilter.value !== 'expired' && (d < 0 || d > Number(eosFilter.value))) return false
  }
  return true
}))
const pagedSchedule = computed(() =>
  filteredRows.value.slice((schedPage.value - 1) * schedPageSize.value, schedPage.value * schedPageSize.value))

// ---- 升级看板筛选 ----
const envOptions = computed(() => [...new Set(clusters.value.map((c) => c.environment).filter(Boolean))])
const boardRows = computed(() => clusters.value.filter((c) => {
  if (envFilter.value && c.environment !== envFilter.value) return false
  if (onlyStranded.value && !(c.pools || []).some((p) => p.stranded)) return false
  if (boardQ.value) {
    const q = boardQ.value.trim().toLowerCase()
    const hit = `${c.display_name || ''}${c.name || ''}`.toLowerCase().includes(q) ||
      (c.pools || []).some((p) => (p.name || '').toLowerCase().includes(q))
    if (!hit) return false
  }
  return true
}))

// ---- 历史筛选 + 分页（scope/方式走后端，其余前端过滤）----
const filteredHistory = computed(() => hRows.value.filter((r) => {
  if (hDays.value) {
    const t = Date.parse((r.started_at || '').replace(' ', 'T'))
    if (!Number.isNaN(t) && Date.now() - t > hDays.value * 86400000) return false
  }
  if (hQ.value) {
    const q = hQ.value.trim().toLowerCase()
    const hay = [r.pool, r.cluster, r.node_name, r.initial_version, r.target_version, r.repair_reason]
      .filter(Boolean).join(' ').toLowerCase()
    if (!hay.includes(q)) return false
  }
  return true
}))
const pagedHistory = computed(() =>
  filteredHistory.value.slice((hPage.value - 1) * hPageSize.value, hPage.value * hPageSize.value))

// ---- 节点健康筛选 ----
const nhRows = computed(() => nh.value.filter((r) => {
  if (nhLevel.value && r.alert_level !== nhLevel.value) return false
  if (nhKindF.value && r.alert_kind !== nhKindF.value) return false
  if (nhQ.value) {
    const q = nhQ.value.trim().toLowerCase()
    if (!`${r.node_name || ''} ${r.cluster || ''}`.toLowerCase().includes(q)) return false
  }
  return true
}))

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
  hPage.value = 1
  try {
    const params = {}
    if (hCluster.value) params.cluster_id = hCluster.value
    if (hKind.value === 'upgrade') {
      if (hStartType.value) params.start_type = hStartType.value
      if (hScope.value) params.scope = hScope.value
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

// rollingNote maxUnavailable=0 时不会同时不可用，但节点越多滚完越久——
// 16 节点的池和 4 节点的池排期难度完全不同，不能都显示「—」
function rollingNote(p) {
  if (!p.node_count) return '—'
  if (p.strategy === 'BLUE_GREEN') return `蓝绿升级，${p.node_count} 节点整池切换`
  if (p.max_unavailable > 0) return '—'
  return `逐个替换不中断，${p.node_count} 个节点需滚完（节点越多窗口越长）`
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

/* ---- 升级预案 ---- */
.chip.ok { background: var(--el-color-success-light-9); color: var(--el-color-success); }
.plan-card { margin-bottom: 10px; }
.plan-card :deep(.el-card__body) { padding: 12px 14px; }
.plan-card-head { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 6px; }
.plan-params { display: flex; gap: 16px; flex-wrap: wrap; font-size: 12px; margin: 6px 0;
  color: var(--el-text-color-regular); background: var(--el-fill-color-lighter);
  padding: 6px 10px; border-radius: 4px; }
.plan-quota { font-size: 12px; margin: 6px 0; color: var(--el-text-color-regular); }
.plan-warn { font-size: 12px; color: var(--el-color-warning); margin-top: 4px; line-height: 1.6; }
.plan-steps { margin: 4px 0 0; padding-left: 22px; font-size: 13px; line-height: 1.9; }
.conc-node { padding: 6px 0; border-bottom: 1px solid var(--el-border-color-lighter); }
.conc-node:last-child { border-bottom: none; }
.extrapolate { white-space: pre-wrap; word-break: break-word; font-size: 12px; line-height: 1.8;
  background: var(--el-fill-color-lighter); padding: 10px; border-radius: 4px; margin: 0 0 8px;
  font-family: inherit; }
</style>
