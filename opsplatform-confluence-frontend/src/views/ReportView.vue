<template>
  <div class="report-page">
    <div class="page-header">
      <h2>生成报告</h2>
      <p class="page-desc">从 Confluence 获取升级/变更数据，从 Jira 获取故障统计，导出为 Word 文档</p>
    </div>

    <!-- Date Range & Fetch -->
    <div class="card source-card">
      <div class="card-top">
        <h3>数据来源</h3>
        <div class="source-tags">
          <select v-if="confluenceConnections.length > 1" class="input input-sm" v-model="selectedConfluenceConn" @change="onConfluenceConnChange" style="max-width:200px">
            <option v-for="c in confluenceConnections" :key="c.id" :value="String(c.id)">{{ c.name }}</option>
          </select>
          <span class="source-tag">{{ SPACE_KEY || '...' }} / {{ ROOT_PAGE_TITLE }}</span>
        </div>
      </div>

      <div class="date-section">
        <div class="filter-row">
          <div class="date-field" style="max-width:180px">
            <label>项目筛选</label>
            <select class="input" v-model="filterProject">
              <option value="">全部项目</option>
              <option v-for="p in projectList" :key="p" :value="p">{{ p }}</option>
            </select>
          </div>
          <div class="date-field" style="max-width:180px" v-if="jiraFaultProjectList.length">
            <label>Jira 故障项目</label>
            <select class="input" v-model="filterJiraProject">
              <option value="">不查询故障</option>
              <option v-for="p in jiraFaultProjectList" :key="p" :value="p">{{ p }}</option>
            </select>
          </div>
          <div class="date-field" style="max-width:180px" v-if="jiraChangeProjectList.length">
            <label>Jira 变更项目</label>
            <select class="input" v-model="filterJiraChangeProject">
              <option value="">不查询变更单</option>
              <option v-for="p in jiraChangeProjectList" :key="p" :value="p">{{ p }}</option>
            </select>
          </div>
          <div class="quick-dates" style="align-self:flex-end">
            <button v-for="q in quickDates" :key="q.label"
              :class="['btn', 'btn-sm', { 'btn-primary': activeQuick === q.label }]"
              @click="applyQuickDate(q)">
              {{ q.label }}
            </button>
          </div>
        </div>
        <div class="date-range">
          <div class="date-field">
            <label>开始日期</label>
            <input type="date" class="input" v-model="startDate" />
          </div>
          <span class="date-sep">~</span>
          <div class="date-field">
            <label>结束日期</label>
            <input type="date" class="input" v-model="endDate" />
          </div>
          <button class="btn btn-primary" @click="fetchAndParse" :disabled="fetching">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/><path d="M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16"/><path d="M16 16h5v5"/></svg>
            {{ fetching ? '获取中...' : '获取数据' }}
          </button>
        </div>
      </div>

      <!-- Status -->
      <div v-if="fetching" class="fetch-status">
        <div class="spinner"></div>
        <span>{{ fetchStatus }}</span>
      </div>
      <div v-else-if="fetchDone" class="fetch-result">
        <span v-if="matchedPages.length" class="result-ok">
          匹配 {{ matchedPages.length }} 个页面{{ skippedCount ? `（跳过 ${skippedCount} 个非生产/不匹配项目）` : '' }}，解析出升级 {{ upgradeRows.length }} 条、变更 {{ changeRows.length }} 条{{ faultRows.length ? `、故障 ${faultRows.length} 条` : '' }}
        </span>
        <span v-else class="result-empty">该日期范围内未找到发布说明页面</span>
        <div v-if="matchedPages.length" class="matched-list">
          <span class="matched-tag" v-for="p in matchedPages" :key="p.id" @click="$router.push('/content/' + p.id)">{{ p.title }}</span>
        </div>
      </div>
    </div>

    <!-- Report Meta -->
    <div class="card meta-card">
      <div class="meta-row">
        <div class="meta-field">
          <label>报告标题</label>
          <input type="text" class="input" v-model="reportTitle" placeholder="自动生成，可修改" />
        </div>
      </div>
      <div class="meta-row" v-if="screenshotTasks.length" style="margin-top:12px">
        <div class="meta-field">
          <label>Grafana 监控截图（导出到 Word）</label>
          <div class="screenshot-task-list">
            <label v-for="t in screenshotTasks" :key="t.id" class="screenshot-task-check">
              <input type="checkbox" :value="t.id" v-model="selectedTaskIds" />
              <span>{{ t.name }}</span>
            </label>
          </div>
        </div>
      </div>
    </div>

    <!-- 统计摘要 -->
    <div class="card summary-card" v-if="upgradeRows.length || changeRows.length || faultRows.length">
      <p class="summary-text" v-if="faultRows.length">故障统计：{{ faultSummaryText }}，累计影响时长 {{ faultTotalDuration }} 分钟。共处理故障 <strong>{{ faultRows.length }}</strong> 起。</p>
      <p class="summary-text">本周共执行 <strong>{{ upgradeRows.length }}</strong> 次升级，<strong>{{ changeRows.length }}</strong> 次变更单处理，{{ allApproved ? '全部通过审批。' : '部分未通过审批。' }}</p>
    </div>

    <!-- 故障管理 -->
    <div class="card table-card" v-if="faultRows.length || filterJiraProject">
      <div class="card-top">
        <h3>故障管理 <span class="row-count" v-if="faultRows.length">({{ faultRows.length }})</span></h3>
        <button class="btn btn-sm btn-primary" @click="addFaultRow">+ 添加行</button>
      </div>
      <div class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th style="min-width:60px">故障项目</th>
              <th style="min-width:120px">故障标题</th>
              <th style="min-width:60px">故障级别</th>
              <th style="min-width:120px">故障影响</th>
              <th style="min-width:110px">故障开始时间</th>
              <th style="min-width:110px">故障发现时间</th>
              <th style="min-width:110px">故障解决时间</th>
              <th style="min-width:60px">故障时长</th>
              <th style="min-width:120px">故障原因</th>
              <th style="min-width:80px">故障归属</th>
              <th style="min-width:80px">故障发现方式</th>
              <th style="width:60px"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, i) in faultRows" :key="i">
              <td><input class="cell-input" v-model="row.faultProject" /></td>
              <td><input class="cell-input" v-model="row.title" /></td>
              <td><input class="cell-input" v-model="row.level" /></td>
              <td><input class="cell-input" v-model="row.impact" /></td>
              <td><input class="cell-input" v-model="row.startTime" /></td>
              <td><input class="cell-input" v-model="row.discoverTime" /></td>
              <td><input class="cell-input" v-model="row.resolveTime" /></td>
              <td><input class="cell-input" v-model="row.duration" /></td>
              <td><input class="cell-input" v-model="row.cause" /></td>
              <td><input class="cell-input" v-model="row.owner" /></td>
              <td><input class="cell-input" v-model="row.discoverMethod" /></td>
              <td class="action-cell">
                <button class="btn-icon danger" @click="faultRows.splice(i, 1)" title="删除">&times;</button>
              </td>
            </tr>
            <tr v-if="!faultRows.length">
              <td colspan="11" class="no-data">无故障记录</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 升级管理 -->
    <div class="card table-card">
      <div class="card-top">
        <h3>升级管理 <span class="row-count" v-if="upgradeRows.length">({{ upgradeRows.length }})</span></h3>
        <button class="btn btn-sm btn-primary" @click="addUpgradeRow">+ 添加行</button>
      </div>
      <div class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th style="width:90px">项目</th>
              <th style="width:120px">升级分类</th>
              <th>升级内容</th>
              <th style="width:110px">升级方式</th>
              <th style="width:100px">升级单</th>
              <th style="width:100px">升级状态</th>
              <th style="width:50px"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, i) in upgradeRows" :key="i">
              <td><input class="cell-input" v-model="row.project" /></td>
              <td><input class="cell-input" v-model="row.category" /></td>
              <td><input class="cell-input" v-model="row.content" /></td>
              <td><input class="cell-input" v-model="row.method" /></td>
              <td><input class="cell-input" v-model="row.ticket" /></td>
              <td>
                <select class="cell-input" :class="'status-' + statusClass(row.status)" v-model="row.status">
                  <option value="">请选择</option>
                  <option>成功</option>
                  <option>下周处理</option>
                  <option>进行中</option>
                  <option>失败</option>
                </select>
              </td>
              <td class="action-cell">
                <button class="btn-icon danger" @click="upgradeRows.splice(i, 1)" title="删除">&times;</button>
              </td>
            </tr>
            <tr v-if="!upgradeRows.length">
              <td colspan="7" class="empty-row">暂无数据</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 变更管理 -->
    <div class="card table-card">
      <div class="card-top">
        <h3>变更管理 <span class="row-count" v-if="changeRows.length">({{ changeRows.length }})</span></h3>
        <button class="btn btn-sm btn-primary" @click="addChangeRow">+ 添加行</button>
      </div>
      <div class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th style="width:90px">项目</th>
              <th style="width:110px">变更类型</th>
              <th>概要</th>
              <th>变更目的</th>
              <th style="width:100px">变更单</th>
              <th style="width:100px">变更方式</th>
              <th style="width:100px">变更状态</th>
              <th style="width:50px"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, i) in changeRows" :key="i">
              <td><input class="cell-input" v-model="row.project" /></td>
              <td><input class="cell-input" v-model="row.type" /></td>
              <td><input class="cell-input" v-model="row.summary" /></td>
              <td><input class="cell-input" v-model="row.purpose" /></td>
              <td><input class="cell-input" v-model="row.ticket" /></td>
              <td><input class="cell-input" v-model="row.method" /></td>
              <td>
                <select class="cell-input" :class="'status-' + statusClass(row.status)" v-model="row.status">
                  <option value="">请选择</option>
                  <option>成功</option>
                  <option>待执行</option>
                  <option>进行中</option>
                  <option>失败</option>
                </select>
              </td>
              <td class="action-cell">
                <button class="btn-icon danger" @click="changeRows.splice(i, 1)" title="删除">&times;</button>
              </td>
            </tr>
            <tr v-if="!changeRows.length">
              <td colspan="8" class="empty-row">暂无数据</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 二、业务和知识库持续建设 -->
    <div class="card table-card">
      <div class="card-top">
        <h3>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 19.5v-15A2.5 2.5 0 0 1 6.5 2H20v20H6.5a2.5 2.5 0 0 1 0-5H20"/></svg>
          二、业务和知识库持续建设
        </h3>
      </div>
      <div class="sub-section">
        <div class="card-top" style="margin-bottom:8px">
          <label class="sub-title" style="margin:0">1. 故障问题处理方案</label>
          <button class="btn btn-sm btn-primary" @click="faultPlanRows.push({ content: '' })">+ 添加行</button>
        </div>
        <div class="table-wrapper">
          <table>
            <thead><tr><th>内容</th><th style="width:50px"></th></tr></thead>
            <tbody>
              <tr v-for="(row, i) in faultPlanRows" :key="i">
                <td><input class="cell-input" v-model="row.content" placeholder="填写故障问题处理方案..." /></td>
                <td class="action-cell"><button class="btn-icon danger" @click="faultPlanRows.splice(i, 1)" title="删除">&times;</button></td>
              </tr>
              <tr v-if="!faultPlanRows.length"><td colspan="2" class="empty-row">暂无数据</td></tr>
            </tbody>
          </table>
        </div>
      </div>
      <div class="sub-section" style="margin-top:16px">
        <div class="card-top" style="margin-bottom:8px">
          <label class="sub-title" style="margin:0">2. 故障经验分享</label>
          <button class="btn btn-sm btn-primary" @click="faultShareRows.push({ content: '' })">+ 添加行</button>
        </div>
        <div class="table-wrapper">
          <table>
            <thead><tr><th>内容</th><th style="width:50px"></th></tr></thead>
            <tbody>
              <tr v-for="(row, i) in faultShareRows" :key="i">
                <td><input class="cell-input" v-model="row.content" placeholder="填写故障经验分享..." /></td>
                <td class="action-cell"><button class="btn-icon danger" @click="faultShareRows.splice(i, 1)" title="删除">&times;</button></td>
              </tr>
              <tr v-if="!faultShareRows.length"><td colspan="2" class="empty-row">暂无数据</td></tr>
            </tbody>
          </table>
        </div>
      </div>
      <div class="sub-section" style="margin-top:16px">
        <div class="card-top" style="margin-bottom:8px">
          <label class="sub-title" style="margin:0">3. 优化工作</label>
          <button class="btn btn-sm btn-primary" @click="optimizationRows.push({ content: '' })">+ 添加行</button>
        </div>
        <div class="table-wrapper">
          <table>
            <thead><tr><th>内容</th><th style="width:50px"></th></tr></thead>
            <tbody>
              <tr v-for="(row, i) in optimizationRows" :key="i">
                <td><input class="cell-input" v-model="row.content" placeholder="填写优化工作内容..." /></td>
                <td class="action-cell"><button class="btn-icon danger" @click="optimizationRows.splice(i, 1)" title="删除">&times;</button></td>
              </tr>
              <tr v-if="!optimizationRows.length"><td colspan="2" class="empty-row">暂无数据</td></tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- 三、问题处理 -->
    <div class="card table-card" v-if="Object.keys(issueRows).length">
      <div class="card-top">
        <h3>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22c5.523 0 10-4.477 10-10S17.523 2 12 2 2 6.477 2 12s4.477 10 10 10z"/><path d="m9 12 2 2 4-4"/></svg>
          四、问题处理（目标：100%）
        </h3>
      </div>
      <div v-for="(rows, project) in issueRows" :key="project" class="project-group">
        <div class="project-label">- {{ project }}：</div>
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th style="min-width:260px">本周进展</th>
                <th style="min-width:120px">完成度</th>
                <th style="min-width:260px">备注</th>
                <th style="width:50px"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, i) in rows" :key="i">
                <td><input class="cell-input" v-model="row.progress" /></td>
                <td><input class="cell-input" v-model="row.completion" /></td>
                <td><input class="cell-input" v-model="row.remark" /></td>
                <td class="action-cell">
                  <button class="btn-icon danger" @click="removeIssueRow(project, i)" title="删除">&times;</button>
                </td>
              </tr>
              <tr v-if="!rows.length">
                <td colspan="4" class="empty-row">暂无数据</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div style="padding:8px 0 16px">
          <button class="btn btn-sm btn-primary" @click="addIssueRow(project)">+ 添加行</button>
        </div>
      </div>
    </div>

    <!-- Actions -->
    <div class="action-bar">
      <button class="btn" @click="clearAll">清空全部</button>
      <div class="action-right">
        <button class="btn" @click="openPreview">预览</button>
        <button v-if="canExport" class="btn btn-primary" @click="exportWord" :disabled="exporting">
          {{ exporting ? (screenshotLoading ? `截图中 ${screenshotProgress.total ? Math.round(screenshotProgress.current / screenshotProgress.total * 100) : 0}% (${screenshotProgress.current}/${screenshotProgress.total})` : '导出中...') : '导出 Word' }}
        </button>
      </div>
    </div>

    <!-- Preview Modal -->
    <div v-if="showPreview" class="modal-overlay">
      <div class="modal-content">
        <div class="modal-header">
          <h3>报告预览</h3>
          <button class="btn-icon" @click="showPreview = false">&times;</button>
        </div>
        <div class="preview-body">
          <h2 class="preview-title">{{ reportTitle || '运维报告' }}</h2>
          <p class="preview-date">{{ startDate }} ~ {{ endDate }}</p>

          <h3 class="section-title">一、项目稳定性保障（目标：99.9%）</h3>
          <h4 style="margin:8px 0;font-size:14px">1. 可用性与故障情况</h4>
          <template v-if="faultRows.length">
            <p class="summary-text">－ 本周整体可用性：{{ availability }}%（目标 {{ SLO_TARGET }}%）。</p>
            <p class="summary-text">－ 故障统计：{{ faultSummaryText }}，累计影响时长 {{ faultTotalDuration }} 分钟。</p>
            <p class="summary-text">－ 共处理故障 <strong>{{ faultRows.length }}</strong> 起，平均响应时间 {{ faultAvgResponse }}，平均恢复时间 {{ faultAvgRecovery }}，<strong :style="{ color: sloMet ? 'var(--success)' : 'var(--danger)' }">{{ sloMet ? '满足' : '未满足' }} SLO</strong>。</p>
          </template>
          <p v-else class="summary-text">－ 本周无故障，可用性 100%，满足 SLO。</p>

          <table class="preview-table" v-if="faultRows.length">
            <thead><tr><th>故障项目</th><th>故障标题</th><th>级别</th><th>故障影响</th><th>开始时间</th><th>发现时间</th><th>解决时间</th><th>时长</th><th>原因</th><th>归属</th><th>发现方式</th></tr></thead>
            <tbody>
              <tr v-for="(row, i) in faultRows" :key="i">
                <td>{{ row.faultProject }}</td><td>{{ row.title }}</td><td>{{ row.level }}</td><td>{{ row.impact }}</td><td>{{ row.startTime }}</td><td>{{ row.discoverTime }}</td><td>{{ row.resolveTime }}</td><td>{{ row.duration }}</td><td>{{ row.cause }}</td><td>{{ row.owner }}</td><td>{{ row.discoverMethod }}</td>
              </tr>
            </tbody>
          </table>

          <h4 style="margin:16px 0 8px;font-size:14px">2. 升级上线与生产变更质量管控</h4>
          <p class="summary-text">－ 本周共执行 <strong>{{ upgradeRows.length }}</strong> 次升级，<strong>{{ changeRows.length }}</strong> 次变更单处理，{{ allApproved ? '全部通过审批。' : '部分未通过审批。' }}</p>
          <table class="preview-table" v-if="upgradeRows.length">
            <thead><tr><th>项目</th><th>升级分类</th><th>升级内容</th><th>升级方式</th><th>升级单</th><th>升级状态</th></tr></thead>
            <tbody>
              <tr v-for="(row, i) in upgradeRows" :key="i">
                <td>{{ row.project }}</td><td>{{ row.category }}</td><td>{{ row.content }}</td><td>{{ row.method }}</td><td>{{ row.ticket }}</td><td>{{ row.status }}</td>
              </tr>
            </tbody>
          </table>
          <p v-else class="no-data">无升级记录</p>

          <h3 class="section-title">变更管理</h3>
          <table class="preview-table" v-if="changeRows.length">
            <thead><tr><th>项目</th><th>变更类型</th><th>概要</th><th>变更目的</th><th>变更单</th><th>变更方式</th><th>变更状态</th></tr></thead>
            <tbody>
              <tr v-for="(row, i) in changeRows" :key="i">
                <td>{{ row.project }}</td><td>{{ row.type }}</td><td>{{ row.summary }}</td><td>{{ row.purpose }}</td><td>{{ row.ticket }}</td><td>{{ row.method }}</td><td>{{ row.status }}</td>
              </tr>
            </tbody>
          </table>
          <p v-else class="no-data">无变更记录</p>

          <h3 class="section-title">二、业务和知识库持续建设</h3>
          <h4 style="margin:8px 0;font-size:14px">1. 故障问题处理方案</h4>
          <template v-if="faultPlanRows.length">
            <p v-for="(row, i) in faultPlanRows" :key="'fp-' + i" class="summary-text">- {{ row.content || '无' }}</p>
          </template>
          <p v-else class="summary-text">- 无</p>
          <h4 style="margin:12px 0 8px;font-size:14px">2. 故障经验分享</h4>
          <template v-if="faultShareRows.length">
            <p v-for="(row, i) in faultShareRows" :key="'fs-' + i" class="summary-text">- {{ row.content || '无' }}</p>
          </template>
          <p v-else class="summary-text">- 无</p>
          <h4 style="margin:12px 0 8px;font-size:14px">3. 优化工作</h4>
          <template v-if="optimizationRows.length">
            <p v-for="(row, i) in optimizationRows" :key="'opt-' + i" class="summary-text">- {{ row.content || '无' }}</p>
          </template>
          <p v-else class="summary-text">- 无</p>

          <!-- Grafana 监控截图 -->
          <template v-if="selectedTaskIds.length">
            <h3 class="section-title">三、项目巡检</h3>
            <div v-if="previewScreenshotLoading" class="screenshot-progress-card">
              <div class="progress-header">
                <span class="progress-percent">{{ screenshotProgress.total ? Math.round(screenshotProgress.current / screenshotProgress.total * 100) : 0 }}%</span>
                <span class="progress-count">{{ screenshotProgress.current }}/{{ screenshotProgress.total }}</span>
              </div>
              <div class="progress-bar-wrap">
                <div class="progress-bar-fill" :style="{ width: screenshotProgress.total ? (screenshotProgress.current / screenshotProgress.total * 100) + '%' : '0%' }"></div>
              </div>
              <div class="progress-detail">
                <span v-if="screenshotProgress.taskName">{{ screenshotProgress.taskName }}</span>
                <span v-if="screenshotProgress.panelName" style="color:var(--text-muted)"> · {{ screenshotProgress.panelName }}</span>
              </div>
              <div class="progress-time">
                <span>已用时: {{ screenshotProgress.elapsed }}</span>
                <span>预计剩余: {{ screenshotProgress.eta }}</span>
              </div>
            </div>
            <template v-else-if="previewScreenshots.length">
              <div v-for="(task, ti) in previewScreenshots" :key="ti" style="margin-bottom:20px">
                <h4 style="margin:12px 0 8px;font-size:14px">{{ task.taskName }}</h4>
                <p v-if="task.error" style="color:var(--danger)">截图失败: {{ task.error }}</p>
                <template v-for="(dash, di) in task.dashboards" :key="di">
                  <p v-if="dash.error" style="color:var(--danger)">{{ dash.title || dash.uid }}: {{ dash.error }}</p>
                  <div v-for="(panel, pi) in (dash.panels || [])" :key="pi" style="margin:8px 0">
                    <p style="font-size:12px;color:var(--text-muted);margin-bottom:4px">{{ dash.title }}{{ panel.title && panel.title !== dash.title ? ' - ' + panel.title : '' }}</p>
                    <img v-if="panel.base64" :src="panel.base64" style="max-width:100%;border-radius:6px;border:1px solid rgba(255,255,255,0.08)" />
                  </div>
                </template>
              </div>
            </template>
          </template>

          <h3 class="section-title">四、问题处理（目标：100%）</h3>
          <template v-for="(rows, project) in issueRows" :key="'preview-' + project">
            <h4 style="margin:10px 0 6px;font-size:14px">- {{ project }}：</h4>
            <table class="preview-table" v-if="rows.length">
              <thead><tr><th>本周进展</th><th>完成度</th><th>备注</th></tr></thead>
              <tbody>
                <tr v-for="(row, i) in rows" :key="i">
                  <td>{{ row.progress || '无' }}</td><td>{{ row.completion || '无' }}</td><td>{{ row.remark || '无' }}</td>
                </tr>
              </tbody>
            </table>
            <p v-else class="no-data">暂无数据</p>
          </template>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, inject, onMounted, watch } from 'vue'
import { Document, Packer, Paragraph, Table, TableRow, TableCell, TextRun, ImageRun, WidthType, AlignmentType, BorderStyle } from 'docx'
import { saveAs } from 'file-saver'
import api from '@/api'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()
const canExport = computed(() => authStore.hasPermission('confluence:export_report'))

const toast = inject('toast')
const confirmDialog = inject('confirm')

const SPACE_KEY = ref('')
const ROOT_PAGE_TITLE = ref('发布说明')
const projectList = ref([])
const jiraFaultProjectList = ref([])
const jiraChangeProjectList = ref([])
const jiraFaultType = ref('故障')
const jiraChangeType = ref('')

// 多连接支持
const confluenceConnections = ref([])
const jiraConnections = ref([])
const selectedConfluenceConn = ref('')
const selectedJiraConn = ref('')

onMounted(async () => {
  try {
    // 加载连接列表
    const connRes = await api.get('/api/connections/public')
    const conns = connRes.data.data || []
    confluenceConnections.value = conns.filter(c => c.type === 'confluence')
    jiraConnections.value = conns.filter(c => c.type === 'jira')

    // 默认选中默认连接
    const defConf = confluenceConnections.value.find(c => c.is_default) || confluenceConnections.value[0]
    if (defConf) {
      selectedConfluenceConn.value = String(defConf.id)
      const cfg = typeof defConf.config === 'string' ? JSON.parse(defConf.config || '{}') : (defConf.config || {})
      const spaces = (cfg.space_key || '').split(',').map(s => s.trim()).filter(Boolean)
      if (spaces.length) SPACE_KEY.value = spaces[0]
      const rootPage = (cfg.root_page || '').trim()
      if (rootPage) ROOT_PAGE_TITLE.value = rootPage.split(',')[0].trim()
    }

    const defJira = jiraConnections.value.find(c => c.is_default) || jiraConnections.value[0]
    if (defJira) {
      selectedJiraConn.value = String(defJira.id)
      const cfg = typeof defJira.config === 'string' ? JSON.parse(defJira.config || '{}') : (defJira.config || {})
      // 故障项目列表（兼容旧字段 projects）
      const faultProjects = (cfg.fault_projects || cfg.projects || '').split(',').map(s => s.trim()).filter(Boolean)
      jiraFaultProjectList.value = faultProjects
      if (faultProjects.length) filterJiraProject.value = faultProjects[0]
      // 变更项目列表
      const changeProjects = (cfg.change_projects || '').split(',').map(s => s.trim()).filter(Boolean)
      jiraChangeProjectList.value = changeProjects
      if (changeProjects.length) filterJiraChangeProject.value = changeProjects[0]
      if (cfg.fault_issuetype) jiraFaultType.value = cfg.fault_issuetype
      if (cfg.change_issuetype) jiraChangeType.value = cfg.change_issuetype
    }

    // 公共配置
    const settingsRes = await api.get('/api/settings/public')
    const config = settingsRes.data.data || {}
    const projects = (config.confluence_projects || '').split(',').map(s => s.trim()).filter(Boolean)
    projectList.value = projects

    // Fallback: 如果没有连接，从旧配置读取
    if (!defConf) {
      const spaces = (config.confluence_allowed_spaces || '').split(',').map(s => s.trim()).filter(Boolean)
      if (spaces.length) SPACE_KEY.value = spaces[0]
      const rootPage = (config.confluence_root_page || '').trim()
      if (rootPage) ROOT_PAGE_TITLE.value = rootPage.split(',')[0].trim()
    }
    // 加载截图任务列表
    loadScreenshotTasks()
  } catch (e) { /* ignore */ }
})

// Date range
const today = new Date().toISOString().split('T')[0]
const startDate = ref(today)
const endDate = ref(today)
const activeQuick = ref('今天')

const quickDates = [
  { label: '今天', days: 0 },
  { label: '近7天', days: 7 },
  { label: '近30天', days: 30 },
  { label: '本周', type: 'week' },
  { label: '本月', type: 'month' },
]

function applyQuickDate(q) {
  activeQuick.value = q.label
  const now = new Date()
  endDate.value = now.toISOString().split('T')[0]

  if (q.type === 'week') {
    const day = now.getDay() || 7
    const mon = new Date(now)
    mon.setDate(now.getDate() - day + 1)
    startDate.value = mon.toISOString().split('T')[0]
  } else if (q.type === 'month') {
    startDate.value = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-01`
  } else {
    const d = new Date(now)
    d.setDate(now.getDate() - q.days)
    startDate.value = d.toISOString().split('T')[0]
  }
}

// Data
const filterProject = ref('')
const filterJiraProject = ref('')
const filterJiraChangeProject = ref('')

function onConfluenceConnChange() {
  const conn = confluenceConnections.value.find(c => String(c.id) === selectedConfluenceConn.value)
  if (!conn) return
  const cfg = typeof conn.config === 'string' ? JSON.parse(conn.config || '{}') : (conn.config || {})
  const spaces = (cfg.space_key || '').split(',').map(s => s.trim()).filter(Boolean)
  if (spaces.length) SPACE_KEY.value = spaces[0]
  const rootPage = (cfg.root_page || '').trim()
  if (rootPage) ROOT_PAGE_TITLE.value = rootPage.split(',')[0].trim()
}
const faultRows = ref([])
const reportTitle = ref('')

// 新增章节数据
const optimizationRows = ref([])
const faultPlanRows = ref([])
const faultShareRows = ref([])
const issueRows = ref({})  // { projectName: [{ progress, completion, remark }] }

// 初始化问题处理行（按选中的项目）
function initIssueRows() {
  const selected = filterProject.value
  const projects = selected ? [selected] : projectList.value
  if (!projects.length) { issueRows.value = {}; return }
  const current = issueRows.value
  const newRows = {}
  for (const p of projects) {
    newRows[p] = current[p] || [{ progress: '', completion: '', remark: '' }]
  }
  issueRows.value = newRows
}
function addIssueRow(project) {
  if (!issueRows.value[project]) issueRows.value[project] = []
  issueRows.value[project].push({ progress: '', completion: '', remark: '' })
}
function removeIssueRow(project, index) {
  issueRows.value[project].splice(index, 1)
}
const showPreview = ref(false)
const exporting = ref(false)

// 截图任务
const screenshotTasks = ref([])
const selectedTaskIds = ref([])
const screenshotLoading = ref(false)
const previewScreenshots = ref([])
const previewScreenshotLoading = ref(false)
const screenshotProgress = ref({ current: 0, total: 0, taskName: '', panelName: '', startTime: 0, elapsed: '', eta: '' })

// 更新耗时和预估
function updateProgressTime() {
  const p = screenshotProgress.value
  if (!p.startTime || p.total === 0) return
  const elapsed = (Date.now() - p.startTime) / 1000
  const mins = Math.floor(elapsed / 60)
  const secs = Math.floor(elapsed % 60)
  p.elapsed = mins > 0 ? `${mins}分${secs}秒` : `${secs}秒`
  if (p.current > 0) {
    const avgPerItem = elapsed / p.current
    const remaining = (p.total - p.current) * avgPerItem
    const remMins = Math.floor(remaining / 60)
    const remSecs = Math.floor(remaining % 60)
    p.eta = remMins > 0 ? `约${remMins}分${remSecs}秒` : `约${remSecs}秒`
  } else {
    p.eta = '计算中...'
  }
}
let progressTimer = null

async function openPreview() {
  showPreview.value = true
  if (selectedTaskIds.value.length) {
    previewScreenshotLoading.value = true
    previewScreenshots.value = []
    screenshotProgress.value = { current: 0, total: selectedTaskIds.value.length, taskName: '' }
    try {
      previewScreenshots.value = await captureSelectedTaskScreenshots()
    } finally {
      previewScreenshotLoading.value = false
    }
  }
}

async function loadScreenshotTasks() {
  try {
    const res = await api.get('/api/screenshot-tasks')
    screenshotTasks.value = (res.data.data || []).filter(t => t.enabled)
  } catch { screenshotTasks.value = [] }
}

// 项目筛选变化时，自动选中匹配的截图任务（如 G01 → G01 PROD）
watch(filterProject, (proj) => {
  if (!proj || !screenshotTasks.value.length) {
    selectedTaskIds.value = []
    return
  }
  const matched = screenshotTasks.value.filter(t =>
    t.name.toUpperCase().startsWith(proj.toUpperCase())
  )
  selectedTaskIds.value = matched.map(t => t.id)
})

// 项目筛选变化时初始化问题处理行
watch([filterProject, projectList], () => initIssueRows())

// 将多个面板图片按 gridPos 拼成一张完整 Dashboard 图
async function stitchPanelsToImage(panels, panelInfos, dashWidth = 1920) {
  const gridUnitH = 30 // Grafana 每个网格单元约30px高
  // 计算画布总高度
  let maxBottom = 0
  for (const info of panelInfos) {
    const bottom = (info.y + info.h) * gridUnitH
    if (bottom > maxBottom) maxBottom = bottom
  }
  if (maxBottom === 0) return null

  const canvas = document.createElement('canvas')
  canvas.width = dashWidth
  canvas.height = maxBottom
  const ctx = canvas.getContext('2d')
  ctx.fillStyle = '#181b1f' // Grafana dark theme 背景色
  ctx.fillRect(0, 0, dashWidth, maxBottom)

  // 逐面板绘制
  for (let i = 0; i < panels.length; i++) {
    const panel = panels[i]
    const info = panelInfos[i]
    if (!panel || !panel.base64 || !info) continue
    try {
      const img = await loadImage(panel.base64)
      const x = Math.round(dashWidth * info.x / 24)
      const y = info.y * gridUnitH
      const w = Math.round(dashWidth * info.w / 24)
      const h = info.h * gridUnitH
      ctx.drawImage(img, x, y, w, h)
    } catch { /* skip failed panels */ }
  }
  return canvas.toDataURL('image/png')
}

function loadImage(src) {
  return new Promise((resolve, reject) => {
    const img = new Image()
    img.onload = () => resolve(img)
    img.onerror = reject
    img.src = src
  })
}

function isLongRange() {
  if (!startDate.value || !endDate.value) return false
  const diff = (new Date(endDate.value) - new Date(startDate.value)) / (1000 * 86400)
  return diff > 8
}

async function captureSelectedTaskScreenshots() {
  if (!selectedTaskIds.value.length) return []

  // 启动计时器
  screenshotProgress.value = { current: 0, total: 0, taskName: '', panelName: '', startTime: Date.now(), elapsed: '0秒', eta: '计算中...' }
  progressTimer = setInterval(updateProgressTime, 1000)

  try {
    return await captureByFullPage()
  } finally {
    clearInterval(progressTimer)
    progressTimer = null
  }
}

// 短时间范围：整页截图
async function captureByFullPage() {
  const total = selectedTaskIds.value.length
  screenshotProgress.value.total = total
  const results = []
  for (let i = 0; i < total; i++) {
    const taskId = selectedTaskIds.value[i]
    const task = screenshotTasks.value.find(t => t.id === taskId)
    const taskName = task?.name || `任务${taskId}`
    screenshotProgress.value.current = i
    screenshotProgress.value.taskName = taskName
    screenshotProgress.value.panelName = ''
    try {
      const params = {}
      if (startDate.value) params.from = startDate.value
      if (endDate.value) params.to = endDate.value
      const res = await api.get(`/api/screenshot-tasks/${taskId}/preview`, { params, timeout: 600000 })
      results.push({ taskName, dashboards: res.data.data || [] })
    } catch (e) {
      results.push({ taskName, dashboards: [], error: e.message })
    }
    screenshotProgress.value.current = i + 1
  }
  return results
}

// 长时间范围：逐面板截图
async function captureByPanels() {
  const results = []
  // 先获取所有任务的面板列表，计算总面板数
  const taskPanelsList = []
  let totalPanels = 0
  for (const taskId of selectedTaskIds.value) {
    const task = screenshotTasks.value.find(t => t.id === taskId)
    const taskName = task?.name || `任务${taskId}`
    try {
      const res = await api.get(`/api/screenshot-tasks/${taskId}/panels`)
      const dashboards = res.data.data || []
      let panelCount = 0
      for (const d of dashboards) panelCount += (d.panels || []).length
      taskPanelsList.push({ taskId, taskName, dashboards, panelCount })
      totalPanels += panelCount
    } catch (e) {
      taskPanelsList.push({ taskId, taskName, dashboards: [], panelCount: 0, error: e.message })
    }
  }

  screenshotProgress.value.total = totalPanels
  screenshotProgress.value.current = 0

  // 使用 Asia/Shanghai 时区计算时间戳（和后端一致）
  const fromMs = new Date(startDate.value + 'T00:00:00+08:00').getTime()
  const toMs = new Date(endDate.value + 'T23:59:59+08:00').getTime()

  for (const tp of taskPanelsList) {
    screenshotProgress.value.taskName = tp.taskName
    if (tp.error) {
      results.push({ taskName: tp.taskName, dashboards: [], error: tp.error })
      continue
    }

    const CONCURRENCY = 5
    const taskDashboards = []
    for (const dash of tp.dashboards) {
      const panels = dash.panels || []
      const dashResult = { uid: dash.uid, title: dash.title, panels: new Array(panels.length).fill(null) }

      for (let i = 0; i < panels.length; i += CONCURRENCY) {
        const batch = panels.slice(i, i + CONCURRENCY)
        screenshotProgress.value.panelName = batch.map(p => p.title || `Panel ${p.id}`).join(', ')

        const renderPanel = async (panel, idx) => {
          const pw = Math.round(1920 * panel.w / 24)
          const ph = Math.max(panel.h * 30, 200)
          const params = { panelId: panel.id, dashUid: dash.uid, from: fromMs, to: toMs, width: pw, height: ph }
          for (let retry = 0; retry < 3; retry++) {
            try {
              const res = await api.get(`/api/screenshot-tasks/${tp.taskId}/render-panel`, { params, timeout: 600000 })
              dashResult.panels[idx] = { panel_id: panel.id, title: panel.title, base64: res.data.data?.base64 || '' }
              return
            } catch (e) {
              if (e.response?.status === 429 && retry < 2) {
                await new Promise(r => setTimeout(r, 3000 * (retry + 1)))
                continue
              }
              dashResult.panels[idx] = { panel_id: panel.id, title: panel.title, base64: '', error: e.message }
            }
          }
        }
        const promises = batch.map((panel, bi) => renderPanel(panel, i + bi))
        await Promise.all(promises)
        screenshotProgress.value.current += batch.length
      }
      // 过滤掉可能的 null
      dashResult.panels = dashResult.panels.filter(Boolean)

      // 拼成一张完整 Dashboard 布局图
      const panelInfos = panels.map(p => ({ x: p.x, y: p.y, w: p.w, h: p.h }))
      const stitchedBase64 = await stitchPanelsToImage(dashResult.panels, panelInfos)
      if (stitchedBase64) {
        dashResult.panels = [{ panel_id: 0, title: dash.title, base64: stitchedBase64 }]
      }
      taskDashboards.push(dashResult)
    }
    results.push({ taskName: tp.taskName, dashboards: taskDashboards })
  }
  return results
}
const fetching = ref(false)
const fetchDone = ref(false)
const fetchStatus = ref('')
const matchedPages = ref([])
const skippedCount = ref(0)
const upgradeRows = ref([])
const changeRows = ref([])

// 是否全部通过审批（状态为"成功"视为通过）
const allApproved = computed(() => {
  const all = [...upgradeRows.value, ...changeRows.value]
  if (all.length === 0) return true
  return all.every(r => !r.status || r.status === '成功')
})

// 故障统计摘要
const faultLevelCounts = computed(() => {
  const counts = {}
  for (const r of faultRows.value) {
    const lvl = (r.level || '未知').toLowerCase()
    counts[lvl] = (counts[lvl] || 0) + 1
  }
  return counts
})

const faultSummaryText = computed(() => {
  const counts = faultLevelCounts.value
  // 收集所有出现的等级并排序
  const allLevels = Object.keys(counts).sort()
  if (allLevels.length === 0) return ''
  return allLevels.map(l => `${l.toUpperCase()} ${counts[l]} 次`).join('，')
})

// 影响 SLO 的等级：P0-P3，P4 不影响
const SLO_LEVELS = ['p0', 'p1', 'p2', 'p3']
function isSloLevel(level) {
  return SLO_LEVELS.includes((level || '').toLowerCase())
}

function parseDurationMins(d) {
  if (!d) return 0
  let mins = 0
  const dayMatch = d.match(/([\d.]+)d/)
  const hourMatch = d.match(/(\d+)h/)
  const minMatch = d.match(/(\d+)m/)
  if (dayMatch) mins += parseFloat(dayMatch[1]) * 1440
  if (hourMatch) mins += parseInt(hourMatch[1]) * 60
  if (minMatch) mins += parseInt(minMatch[1])
  return mins
}

// 全部故障总时长
const faultTotalDuration = computed(() => {
  let totalMins = 0
  for (const r of faultRows.value) {
    totalMins += parseDurationMins(r.duration)
  }
  return totalMins
})

// 影响 SLO 的故障时长（仅 P0-P3）
const sloFaultDuration = computed(() => {
  let totalMins = 0
  for (const r of faultRows.value) {
    if (isSloLevel(r.level)) totalMins += parseDurationMins(r.duration)
  }
  return totalMins
})

const faultAvgRecovery = computed(() => {
  const rows = faultRows.value.filter(r => r.duration)
  if (rows.length === 0) return '-'
  const total = faultTotalDuration.value
  const avg = total / rows.length
  if (avg >= 1440) return `${(avg / 1440).toFixed(1)} d`
  if (avg >= 60) return `${(avg / 60).toFixed(1)} h`
  return `${Math.round(avg)} 分钟`
})

const faultAvgResponse = computed(() => {
  let totalMins = 0
  let count = 0
  for (const r of faultRows.value) {
    if (r.startTime && r.discoverTime) {
      const diff = new Date(r.discoverTime.replace(' ', 'T')) - new Date(r.startTime.replace(' ', 'T'))
      if (diff >= 0) {
        totalMins += diff / 60000
        count++
      }
    }
  }
  if (count === 0) return '-'
  const avg = totalMins / count
  if (avg >= 60) return `${(avg / 60).toFixed(1)} h`
  return `${Math.round(avg)} 分钟`
})

// 可用性计算：(总时间 - P0~P3故障时长) / 总时间，P4不影响SLO
const SLO_TARGET = 99.9
const availability = computed(() => {
  const days = Math.max(1, Math.round((new Date(endDate.value) - new Date(startDate.value)) / 86400000) + 1)
  const totalMins = days * 24 * 60
  const faultMins = sloFaultDuration.value
  if (faultMins === 0) return 100
  return parseFloat(((totalMins - faultMins) / totalMins * 100).toFixed(3))
})

const sloMet = computed(() => availability.value >= SLO_TARGET)

// CQL 搜索根页面
async function findRootPageId() {
  const cql = `type=page AND title="${ROOT_PAGE_TITLE.value}" AND space="${SPACE_KEY.value}"`
  const res = await api.get(`/api/confluence/search?cql=${encodeURIComponent(cql)}&limit=5`)
  const results = res.data.data?.results || []
  const match = results.find(r => (r.content?.title || r.title)?.trim() === ROOT_PAGE_TITLE.value)
  if (!match) return null
  return match.content?.id || match.id
}

// CQL 搜索根页面的所有后代页面（服务端过滤，高效）
async function fetchDescendantPages(rootPageId) {
  const allPages = []
  let startAt = 0
  const limit = 200

  while (true) {
    const cql = `ancestor=${rootPageId} AND type=page`
    const res = await api.get(`/api/confluence/search?cql=${encodeURIComponent(cql)}&start=${startAt}&limit=${limit}`)
    const data = res.data.data
    const results = data?.results || []
    results.forEach(r => {
      allPages.push(r.content || r)
    })
    if (results.length < limit) break
    startAt += results.length
  }

  return allPages
}

// Main fetch flow
async function fetchAndParse() {
  fetching.value = true
  fetchDone.value = false
  faultRows.value = []
  upgradeRows.value = []
  changeRows.value = []
  matchedPages.value = []
  skippedCount.value = 0

  try {
    if (!SPACE_KEY.value) {
      toast('请先在系统设置中配置允许的空间', 'error')
      fetching.value = false
      return
    }

    // Step 1: CQL 查找根页面
    fetchStatus.value = '查找发布说明页面...'
    const rootPageId = await findRootPageId()
    if (!rootPageId) {
      toast(`未找到页面: ${ROOT_PAGE_TITLE.value}`, 'error')
      fetchDone.value = true
      fetching.value = false
      return
    }

    // Step 2: CQL 获取所有后代页面（1-2次请求）
    fetchStatus.value = '获取后代页面...'
    const descendants = await fetchDescendantPages(rootPageId)

    // Step 3: 按日期过滤
    const filtered = filterByDateRange(descendants)

    if (!filtered.length) {
      fetchDone.value = true
      fetching.value = false
      return
    }

    // Auto-set report title
    const projLabel = filterProject.value ? ` [${filterProject.value}]` : ''
    if (startDate.value === endDate.value) {
      reportTitle.value = `${startDate.value} 运维日报${projLabel}`
    } else {
      reportTitle.value = `${startDate.value} ~ ${endDate.value} 运维报告${projLabel}`
    }

    // Step 4: 获取并解析每个匹配页面的内容
    const included = []
    let skipped = 0
    for (let i = 0; i < filtered.length; i++) {
      fetchStatus.value = `解析页面 (${i + 1}/${filtered.length}): ${filtered[i].title}`
      const res = await api.get(`/api/confluence/content/${filtered[i].id}`)
      const pageData = res.data.data
      const bodyHtml = pageData?.body?.storage?.value || pageData?.body?.view?.value || ''
      const result = parseHtmlTables(bodyHtml, filtered[i].title)

      // 过滤：只保留生产环境
      if (result.environment && !result.environment.includes('生产')) {
        skipped++
        continue
      }
      // 过滤：项目筛选
      if (filterProject.value && result.projectName) {
        if (!result.projectName.toLowerCase().includes(filterProject.value.toLowerCase())) {
          skipped++
          continue
        }
      }

      included.push(filtered[i])
      upgradeRows.value.push(...result.upgrades)
      changeRows.value.push(...result.changes)
    }

    matchedPages.value = included
    skippedCount.value = skipped

    // Step 5: 从 Jira 获取故障工单
    if (filterJiraProject.value) {
      fetchStatus.value = '从 Jira 获取故障工单...'
      await fetchJiraFaults()
    }

    // Step 6: 从 Jira 获取变更单
    if (filterJiraChangeProject.value && jiraChangeType.value) {
      fetchStatus.value = '从 Jira 获取变更单...'
      await fetchJiraChanges()
    }

    fetchDone.value = true
  } catch (e) {
    console.error('Fetch failed:', e)
    fetchStatus.value = '获取失败: ' + e.message
  } finally {
    fetching.value = false
  }
}

// 从 Jira 获取故障工单
async function fetchJiraFaults() {
  try {
    const project = filterJiraProject.value
    const issueType = jiraFaultType.value || '故障'
    // JQL: 按项目、类型、日期范围查询
    const jql = `project = "${project}" AND issuetype = "${issueType}" AND created >= "${startDate.value}" AND created <= "${endDate.value} 23:59" ORDER BY priority ASC, created DESC`
    const res = await api.get(`/api/jira/search?jql=${encodeURIComponent(jql)}&maxResults=100`)
    const data = res.data.data
    const issues = data?.issues || []

    for (const issue of issues) {
      const fields = issue.fields || {}
      // 故障等级 - 优先使用自定义字段 customfield_10919
      const level = fields.customfield_10919?.value || fields.customfield_10919 || fields.priority?.name || ''

      // 时间字段 - 优先使用自定义字段
      const created = fields.created ? fields.created.replace('T', ' ').substring(0, 16) : ''
      const discoverTime = fields.customfield_10936 ? fields.customfield_10936.replace('T', ' ').substring(0, 16) : created
      const resolveTime = fields.customfield_10942 ? fields.customfield_10942.replace('T', ' ').substring(0, 16)
        : (fields.resolutiondate ? fields.resolutiondate.replace('T', ' ').substring(0, 16) : '')

      // 故障时长 - 优先使用自定义字段（分钟）
      let duration = ''
      if (fields.customfield_11134) {
        const mins = Number(fields.customfield_11134)
        if (mins >= 1440) duration = `${(mins / 1440).toFixed(1)}d`
        else if (mins >= 60) duration = `${Math.floor(mins / 60)}h${mins % 60}m`
        else duration = `${mins}m`
      } else if (fields.created && fields.resolutiondate) {
        const diffMs = new Date(fields.resolutiondate) - new Date(fields.created)
        const diffH = Math.floor(diffMs / 3600000)
        const diffM = Math.floor((diffMs % 3600000) / 60000)
        if (diffH > 24) duration = `${(diffH / 24).toFixed(1)}d`
        else if (diffH > 0) duration = `${diffH}h${diffM}m`
        else duration = `${diffM}m`
      }

      // 跳过 P4 及以下等级（不影响 SLO，不展示在报告中）
      if (level.toLowerCase() === 'p4' || level.toLowerCase() === 'p5') continue

      // 故障影响范围、根本原因、归属 - 使用自定义字段
      const impact = fields.customfield_10938 || (fields.description ? fields.description.substring(0, 100) : '')
      const cause = fields.customfield_10939 || fields.resolution?.name || ''
      const faultOwner = fields.customfield_11141?.value || fields.assignee?.displayName || ''

      faultRows.value.push({
        faultProject: fields.customfield_11140?.value || '',
        title: fields.customfield_10935 || fields.summary || '',
        level: level,
        impact: impact,
        startTime: created,
        discoverTime: discoverTime,
        resolveTime: resolveTime,
        duration: duration,
        cause: cause,
        owner: faultOwner,
        discoverMethod: fields.customfield_10937?.value || '',
      })
    }
    // 按项目筛选过滤（filterProject 对应 customfield_11140 故障项目）
    if (filterProject.value) {
      faultRows.value = faultRows.value.filter(r =>
        !r.faultProject || r.faultProject.toLowerCase().includes(filterProject.value.toLowerCase())
      )
    }
  } catch (e) {
    console.error('Jira fault fetch failed:', e)
    toast('Jira 故障查询失败: ' + (e.response?.data?.error || e.message), 'error')
  }
}

// 从 Jira 获取变更单并合并到 changeRows
async function fetchJiraChanges() {
  try {
    const project = filterJiraChangeProject.value
    const issueType = jiraChangeType.value

    // Step 1: 获取字段定义，建立 description/name → customfield_id 映射
    let fieldMap = {}
    try {
      const fieldsRes = await api.get('/api/jira/fields')
      const allFields = fieldsRes.data.data || []
      for (const f of allFields) {
        // 用 schema.customId 或 description 或 name 做 key
        if (f.id?.startsWith('customfield_')) {
          const desc = (f.description || '').toLowerCase().trim()
          const name = (f.name || '').toLowerCase().trim()
          fieldMap[desc] = f.id
          fieldMap[name] = f.id
        }
      }
    } catch (e) {
      console.warn('获取 Jira 字段定义失败，使用默认映射:', e.message)
    }

    // 自定义字段映射：通过 description/name 查找 customfield ID
    const cfProject = fieldMap['业务运维项目'] || fieldMap['business_project'] || ''
    const cfChangeType = fieldMap['变更类型'] || fieldMap['change_type'] || ''
    const cfChangePurpose = fieldMap['变更目的'] || fieldMap['change_purpose'] || ''
    const cfChangeMethod = fieldMap['变更方式'] || fieldMap['change_method'] || ''

    // Step 2: 查询变更单
    const jql = `project = "${project}" AND issuetype = "${issueType}" AND created >= "${startDate.value}" AND created <= "${endDate.value} 23:59" ORDER BY created DESC`
    const res = await api.get(`/api/jira/search?jql=${encodeURIComponent(jql)}&maxResults=200`)
    const issues = res.data.data?.issues || []

    for (const issue of issues) {
      const f = issue.fields || {}
      const statusName = f.status?.name || ''

      // 状态映射
      let status = '成功'
      const statusLower = statusName.toLowerCase()
      if (['进行中', 'in progress', '处理中'].some(s => statusLower.includes(s.toLowerCase()))) status = '进行中'
      else if (['待处理', 'to do', 'open', '待执行'].some(s => statusLower.includes(s.toLowerCase()))) status = '待执行'
      else if (['变更运维审批', '变更审批'].some(s => statusLower.includes(s.toLowerCase()))) status = '待执行'
      else if (['已取消', 'cancelled'].some(s => statusLower.includes(s.toLowerCase()))) status = '失败'

      // 提取自定义字段值
      const getFieldValue = (fieldId) => {
        if (!fieldId || !f[fieldId]) return ''
        const val = f[fieldId]
        if (typeof val === 'string') return val
        if (val.value) return val.value  // select 类型
        if (val.name) return val.name
        return String(val)
      }

      // 业务运维项目
      const bizProject = getFieldValue(cfProject) || f.project?.key || project

      // 变更类型
      const changeType = getFieldValue(cfChangeType) || f.issuetype?.name || issueType

      // 变更目的
      const changePurpose = getFieldValue(cfChangePurpose)
        || (f.description ? String(f.description).replace(/<[^>]*>/g, '').substring(0, 100) : '')

      // 变更方式
      const changeMethod = getFieldValue(cfChangeMethod) || '-'

      // 按项目筛选过滤
      if (filterProject.value && bizProject && !bizProject.toLowerCase().includes(filterProject.value.toLowerCase())) {
        continue
      }

      changeRows.value.push({
        project: bizProject,
        type: changeType,
        summary: f.summary || '',
        purpose: changePurpose,
        ticket: issue.key || '',
        method: changeMethod,
        status,
      })
    }
  } catch (e) {
    console.error('Jira change fetch failed:', e)
    toast('Jira 变更单查询失败: ' + (e.response?.data?.error || e.message), 'error')
  }
}

// Filter pages by date in title (look for YYYY-MM-DD pattern)
function filterByDateRange(pages) {
  const start = startDate.value
  const end = endDate.value

  return pages.filter(p => {
    const title = p.title || ''
    // Try to extract date from title
    const dateMatch = title.match(/(\d{4}-\d{2}-\d{2})/)
    if (dateMatch) {
      const pageDate = dateMatch[1]
      return pageDate >= start && pageDate <= end
    }
    // Also try YYYYMMDD format
    const dateMatch2 = title.match(/(\d{4})(\d{2})(\d{2})/)
    if (dateMatch2) {
      const pageDate = `${dateMatch2[1]}-${dateMatch2[2]}-${dateMatch2[3]}`
      return pageDate >= start && pageDate <= end
    }
    return false
  })
}

// Parse Confluence upgrade ticket template
// The page structure has: 变更基本信息 table, Jira table, 升级清单 tables, 变更结果 table
function parseHtmlTables(html, pageTitle) {
  const upgrades = []
  const changes = []
  const parser = new DOMParser()
  const doc = parser.parseFromString(html, 'text/html')

  // Build a key-value map from 项目/内容 style tables
  // Returns { text: {key: textValue}, cells: {key: cellElement} }
  function readKvTable(table) {
    const map = {}
    const cellMap = {}
    const rows = table.querySelectorAll('tr')
    for (let i = 1; i < rows.length; i++) {
      const cells = rows[i].querySelectorAll('th, td')
      if (cells.length >= 2) {
        const key = cells[0].textContent.trim()
        map[key] = cells[1].textContent.trim()
        cellMap[key] = cells[1]
      }
    }
    map._cells = cellMap
    return map
  }

  // Extract checked items from a table cell element (handles Confluence ac:task-list checkboxes)
  function getCheckedFromCell(cell, options) {
    const cellHtml = cell.innerHTML || ''
    const checked = []
    // Confluence storage format: <ac:task><ac:task-status>complete</ac:task-status><ac:task-body>功能升级</ac:task-body></ac:task>
    const taskRegex = /<ac:task-status>\s*complete\s*<\/ac:task-status>\s*<ac:task-body[^>]*>(.*?)<\/ac:task-body>/gi
    let m
    while ((m = taskRegex.exec(cellHtml)) !== null) {
      const body = m[1].replace(/<[^>]*>/g, '').trim()
      for (const opt of options) {
        if (body.includes(opt)) checked.push(opt)
      }
    }
    if (checked.length > 0) return checked
    // Fallback: if no ac:task found, try text matching (plain text cells)
    const text = cell.textContent.trim()
    for (const opt of options) {
      if (text.includes(opt)) return [opt]
    }
    return [text]
  }

  const tables = doc.querySelectorAll('table')
  const allText = doc.body?.textContent || ''

  // Try to detect the Confluence upgrade ticket template
  // Look for key markers: 变更基本信息, Jira Issue, 变更结果
  const isUpgradeTicket = allText.includes('变更基本信息') || allText.includes('Jira Issue') || allText.includes('升级清单')

  if (isUpgradeTicket && tables.length >= 2) {
    // Parse template-style page
    let systemName = '', changeType = '', jiraTicket = '', upgradeResult = ''
    let background = '', method = '', projectName = '', environment = ''
    let checkedTypes = []

    for (const table of tables) {
      const kv = readKvTable(table)
      const rows = table.querySelectorAll('tr')
      // 读取表头（第一行），用于识别横向表格（如 项目|内容 格式）
      const headerCells = rows[0]?.querySelectorAll('th, td') || []
      const headers = Array.from(headerCells).map(c => c.textContent.trim())
      const headerStr = headers.join('|')

      // 变更基本信息 table（纵向 key-value 格式）
      const isInfoTable = !!(kv['系统 / 产品名称 / 项目'] || kv['系统/产品名称/项目'] || kv['变更类型'])
      if (isInfoTable) {
        systemName = kv['系统 / 产品名称 / 项目'] || kv['系统/产品名称/项目'] || ''
        // 项目代号：取第一个词（如 "G01 客户端" → "G01"）
        if (systemName && !projectName) {
          const code = systemName.trim().split(/\s+/)[0]
          projectName = code || systemName
        }
        // 变更类型：从 checkbox 中提取已勾选项
        const typeCell = kv._cells?.['变更类型']
        const types = ['功能升级', '版本升级', '配置变更', '架构调整', '安全修复']
        if (typeCell) {
          checkedTypes = getCheckedFromCell(typeCell, types)
          changeType = checkedTypes[0] || ''
        } else {
          const typeText = kv['变更类型'] || ''
          checkedTypes = types.filter(t => typeText.includes(t))
          changeType = checkedTypes[0] || typeText
        }
        // 影响环境：从 checkbox 中提取已勾选项
        const envCell = kv._cells?.['影响环境']
        const envOptions = ['预发', '生产']
        if (envCell) {
          const checkedEnvs = getCheckedFromCell(envCell, envOptions)
          environment = checkedEnvs.join(',')
        } else {
          const envText = kv['影响环境'] || ''
          environment = envText
        }
      }

      // Jira 横向表格（表头: 项目|内容，数据行: G32|YX-168）
      // 排除变更基本信息表（它的表头也可能是"项目|内容"但数据行是长 key-value）
      if (!isInfoTable && headerStr.includes('项目') && (headerStr.includes('内容') || headerStr.includes('Jira'))) {
        const projIdx = headers.findIndex(h => h === '项目')
        const contentIdx = headers.findIndex(h => h.includes('内容') || h.includes('Jira'))
        for (let i = 1; i < rows.length; i++) {
          const cells = rows[i].querySelectorAll('th, td')
          const vals = Array.from(cells).map(c => c.textContent.trim())
          if (projIdx >= 0 && vals[projIdx] && !projectName) {
            projectName = vals[projIdx]
          }
          if (contentIdx >= 0 && vals[contentIdx]) {
            const m = vals[contentIdx].match(/([A-Z]+-\d+)/)
            if (m && !jiraTicket) jiraTicket = m[1]
          }
        }
      }

      // Jira 纵向 key-value 格式（Jira Issue: YX-168）
      if (kv['Jira Issue'] || kv['Jira']) {
        const jiraText = kv['Jira Issue'] || kv['Jira'] || ''
        const jiraMatch = jiraText.match(/([A-Z]+-\d+)/)
        if (jiraMatch && !jiraTicket) jiraTicket = jiraMatch[1]
      }

      // 纵向格式的项目名
      if (kv['项目'] && !projectName) {
        projectName = kv['项目']
      }

      // 变更结果 table
      if (kv['升级结果'] !== undefined) {
        const resultText = kv['升级结果'] || ''
        upgradeResult = resultText.includes('成功') ? '成功' : resultText.includes('回滚') ? '回滚' : resultText
      }
    }

    // Fallback: 从整个页面 HTML 中扫描 Jira 单号链接
    if (!jiraTicket) {
      const links = doc.querySelectorAll('a')
      for (const a of links) {
        const text = a.textContent.trim()
        const m = text.match(/^([A-Z]+-\d+)$/)
        if (m) { jiraTicket = m[1]; break }
      }
    }
    // Fallback: 从全文扫描 Jira 单号
    if (!jiraTicket) {
      const m = allText.match(/([A-Z]+-\d+)/)
      if (m) jiraTicket = m[1]
    }

    // 分类判断：只有勾选了"配置变更"才归入变更管理，其他全部归升级管理
    const hasConfigChange = checkedTypes.includes('配置变更')

    // Determine upgrade method
    if (allText.includes('滚动升级') || allText.includes('滚动')) method = '滚动升级'
    else if (allText.includes('重启')) method = '重启'
    else if (allText.includes('热更') || allText.includes('热加载')) method = '热修复'
    else method = '滚动升级'

    // Extract background text: 找"三、升级/变更背景"标题后面的内容
    const allElements = doc.querySelectorAll('h1, h2, h3, h4, h5, p, li, ac\\:rich-text-body')
    let foundBgSection = false
    for (const el of allElements) {
      const t = el.textContent.trim()
      // 找到"三、"或"升级/变更背景"标题
      if (!foundBgSection && (t.includes('三、') || t.includes('升级') && t.includes('背景') || t.includes('变更背景'))) {
        foundBgSection = true
        // 如果标题本身包含内容（如 "三、xxx: 实际内容"），提取冒号后面的
        continue
      }
      // 标题后的第一个有效内容
      if (foundBgSection && t && !t.includes('全部勾选') && !t.includes('方可开始') && t.length > 3) {
        background = t
        break
      }
    }
    // Fallback: 遍历所有段落找有效内容（排除模板提示文字）
    if (!background) {
      const allP = doc.querySelectorAll('p, li')
      for (const p of allP) {
        const t = p.textContent.trim()
        if (t && !t.includes('全部勾选') && !t.includes('方可开始') && !t.includes('方可执行')
            && !t.includes('确认无误') && t.length > 5 && t.length < 200) {
          background = t
          break
        }
      }
    }

    const content = `${systemName} ${background}`.trim() || pageTitle

    // 升级类型和变更类型独立判断，可同时出现在两个表中
    const upgradeTypes = checkedTypes.filter(t => t !== '配置变更')
    if (upgradeTypes.length > 0) {
      upgrades.push({
        project: projectName,
        category: upgradeTypes.filter(t => ['功能升级', '版本升级', '架构调整', '安全修复'].includes(t)).join('/') || '业务上线需求',
        content: content,
        method: method,
        ticket: jiraTicket,
        status: upgradeResult || '成功',
      })
    }
    if (hasConfigChange) {
      changes.push({
        project: projectName,
        type: '配置变更',
        summary: systemName || pageTitle,
        purpose: background || content,
        ticket: jiraTicket,
        method: method === '滚动升级' ? '重启' : method,
        status: upgradeResult || '成功',
      })
    }

    return { upgrades, changes, projectName, environment }
  }

  // Fallback: try to parse standard report-style tables (升级分类/升级内容 headers)
  for (const table of tables) {
    const rows = table.querySelectorAll('tr')
    if (rows.length < 2) continue

    const headerCells = rows[0].querySelectorAll('th, td')
    const headers = Array.from(headerCells).map(c => c.textContent.trim())
    const headerStr = headers.join('|')

    if (headerStr.includes('升级分类') || headerStr.includes('升级内容')) {
      const colMap = {}
      headers.forEach((h, idx) => {
        if (h.includes('项目') && !h.includes('名称')) colMap.project = idx
        else if (h.includes('升级分类')) colMap.category = idx
        else if (h.includes('升级内容')) colMap.content = idx
        else if (h.includes('升级方式')) colMap.method = idx
        else if (h.includes('升级单')) colMap.ticket = idx
        else if (h.includes('升级状态')) colMap.status = idx
      })
      for (let i = 1; i < rows.length; i++) {
        const cells = rows[i].querySelectorAll('td')
        const vals = Array.from(cells).map(c => c.textContent.trim())
        if (vals.every(v => !v)) continue
        upgrades.push({
          project: colMap.project !== undefined ? vals[colMap.project] || '' : '',
          category: vals[colMap.category ?? 0] || '',
          content: vals[colMap.content ?? 1] || '',
          method: vals[colMap.method ?? 2] || '',
          ticket: vals[colMap.ticket ?? 3] || '',
          status: vals[colMap.status ?? 4] || '',
        })
      }
    } else if (headerStr.includes('变更类型') || headerStr.includes('变更目的')) {
      const colMap = {}
      headers.forEach((h, idx) => {
        if (h.includes('项目') && !h.includes('名称')) colMap.project = idx
        else if (h.includes('变更类型')) colMap.type = idx
        else if (h.includes('概要')) colMap.summary = idx
        else if (h.includes('变更目的')) colMap.purpose = idx
        else if (h.includes('变更单')) colMap.ticket = idx
        else if (h.includes('变更方式')) colMap.method = idx
        else if (h.includes('变更状态')) colMap.status = idx
      })
      for (let i = 1; i < rows.length; i++) {
        const cells = rows[i].querySelectorAll('td')
        const vals = Array.from(cells).map(c => c.textContent.trim())
        if (vals.every(v => !v)) continue
        changes.push({
          project: colMap.project !== undefined ? vals[colMap.project] || '' : '',
          type: vals[colMap.type ?? 0] || '',
          summary: vals[colMap.summary ?? 1] || '',
          purpose: vals[colMap.purpose ?? 2] || '',
          ticket: vals[colMap.ticket ?? 3] || '',
          method: vals[colMap.method ?? 4] || '',
          status: vals[colMap.status ?? 5] || '',
        })
      }
    }
  }

  return { upgrades, changes }
}

function addFaultRow() {
  faultRows.value.push({ faultProject: '', title: '', level: '', impact: '', startTime: '', discoverTime: '', resolveTime: '', duration: '', cause: '', owner: '', discoverMethod: '' })
}
function addUpgradeRow() {
  upgradeRows.value.push({ project: '', category: '', content: '', method: '', ticket: '', status: '' })
}
function addChangeRow() {
  changeRows.value.push({ project: '', type: '', summary: '', purpose: '', ticket: '', method: '', status: '' })
}
async function clearAll() {
  const ok = await confirmDialog({ title: '确认清空', message: '确定要清空所有报告数据吗？', type: 'warning', okText: '清空' })
  if (!ok) return
  faultRows.value = []
  upgradeRows.value = []
  changeRows.value = []
  matchedPages.value = []
  reportTitle.value = ''
  fetchDone.value = false
  optimizationRows.value = []
  faultPlanRows.value = []
  faultShareRows.value = []
  initIssueRows()
}

function statusClass(s) {
  if (s === '成功') return 'success'
  if (s === '失败') return 'danger'
  if (s === '进行中' || s === '待执行') return 'warning'
  if (s === '下周处理') return 'info'
  return ''
}

// Word export
function cell(text, opts = {}) {
  const b = { top: { style: BorderStyle.SINGLE, size: 1 }, bottom: { style: BorderStyle.SINGLE, size: 1 }, left: { style: BorderStyle.SINGLE, size: 1 }, right: { style: BorderStyle.SINGLE, size: 1 } }
  return new TableCell({
    borders: b,
    width: opts.width ? { size: opts.width, type: WidthType.DXA } : undefined,
    children: [new Paragraph({ alignment: AlignmentType.CENTER, spacing: { before: 60, after: 60 }, children: [new TextRun({ text: text || '', size: 20, font: '微软雅黑', bold: opts.bold || false })] })],
    verticalAlign: 'center',
  })
}

async function exportWord() {
  exporting.value = true
  try {
    const children = []
    const title = reportTitle.value || '运维报告'
    children.push(
      new Paragraph({ alignment: AlignmentType.CENTER, spacing: { after: 200 }, children: [new TextRun({ text: title, size: 32, bold: true, font: '微软雅黑' })] }),
      new Paragraph({ alignment: AlignmentType.CENTER, spacing: { after: 300 }, children: [new TextRun({ text: `${startDate.value} ~ ${endDate.value}`, size: 22, font: '微软雅黑', color: '666666' })] }),
    )

    // 一、项目稳定性保障
    children.push(new Paragraph({ spacing: { before: 300, after: 100 }, children: [new TextRun({ text: '一、项目稳定性保障（目标：99.9%）', size: 24, bold: true, font: '微软雅黑' })] }))
    children.push(new Paragraph({ spacing: { before: 100, after: 50 }, children: [new TextRun({ text: '1. 可用性与故障情况', size: 22, bold: true, font: '微软雅黑' })] }))

    // 故障统计文字
    if (faultRows.value.length) {
      children.push(new Paragraph({ spacing: { before: 50, after: 50 }, children: [new TextRun({ text: `－ 本周整体可用性：${availability.value}%（目标 ${SLO_TARGET}%）。`, size: 22, font: '微软雅黑' })] }))
      children.push(new Paragraph({ spacing: { before: 50, after: 50 }, children: [new TextRun({ text: `－ 故障统计：${faultSummaryText.value}，累计影响时长 ${faultTotalDuration.value} 分钟。`, size: 22, font: '微软雅黑' })] }))
      children.push(new Paragraph({ spacing: { before: 50, after: 100 }, children: [new TextRun({ text: `－ 共处理故障 ${faultRows.value.length} 起，平均响应时间 ${faultAvgResponse.value}，平均恢复时间 ${faultAvgRecovery.value}，${sloMet.value ? '满足' : '未满足'} SLO。`, size: 22, font: '微软雅黑' })] }))
    } else {
      children.push(new Paragraph({ spacing: { before: 50, after: 100 }, children: [new TextRun({ text: '－ 本周无故障，可用性 100%，满足 SLO。', size: 22, font: '微软雅黑' })] }))
    }

    // 故障表格
    if (faultRows.value.length) {
      const fH = new TableRow({ children: [cell('故障项目', { bold: true, width: 700 }), cell('故障标题', { bold: true, width: 1200 }), cell('级别', { bold: true, width: 500 }), cell('故障影响', { bold: true, width: 1100 }), cell('开始时间', { bold: true, width: 1000 }), cell('发现时间', { bold: true, width: 1000 }), cell('解决时间', { bold: true, width: 1000 }), cell('时长', { bold: true, width: 500 }), cell('原因', { bold: true, width: 1000 }), cell('归属', { bold: true, width: 700 }), cell('发现方式', { bold: true, width: 700 })] })
      const fR = faultRows.value.map(r => new TableRow({ children: [cell(r.faultProject, { width: 700 }), cell(r.title, { width: 1200 }), cell(r.level, { width: 500 }), cell(r.impact, { width: 1100 }), cell(r.startTime, { width: 1000 }), cell(r.discoverTime, { width: 1000 }), cell(r.resolveTime, { width: 1000 }), cell(r.duration, { width: 500 }), cell(r.cause, { width: 1000 }), cell(r.owner, { width: 700 }), cell(r.discoverMethod, { width: 700 })] }))
      children.push(new Table({ width: { size: 9800, type: WidthType.DXA }, rows: [fH, ...fR] }))
    }

    // 2. 升级上线与生产变更质量管控
    children.push(new Paragraph({ spacing: { before: 300, after: 100 }, children: [new TextRun({ text: '2. 升级上线与生产变更质量管控', size: 22, bold: true, font: '微软雅黑' })] }))
    children.push(new Paragraph({ spacing: { before: 50, after: 100 }, children: [new TextRun({ text: `－ 本周共执行 ${upgradeRows.value.length} 次升级，${changeRows.value.length} 次变更单处理，${allApproved.value ? '全部通过审批。' : '部分未通过审批。'}`, size: 22, font: '微软雅黑' })] }))
    const uH = new TableRow({ children: [cell('项目', { bold: true, width: 1200 }), cell('升级分类', { bold: true, width: 1600 }), cell('升级内容', { bold: true, width: 3200 }), cell('升级方式', { bold: true, width: 1200 }), cell('升级单', { bold: true, width: 1200 }), cell('升级状态', { bold: true, width: 1200 })] })
    const uR = upgradeRows.value.length
      ? upgradeRows.value.map(r => new TableRow({ children: [cell(r.project, { width: 1200 }), cell(r.category, { width: 1600 }), cell(r.content, { width: 3200 }), cell(r.method, { width: 1200 }), cell(r.ticket, { width: 1200 }), cell(r.status, { width: 1200 })] }))
      : [new TableRow({ children: [cell(''), cell(''), cell(''), cell(''), cell(''), cell('')] })]
    children.push(new Table({ width: { size: 9800, type: WidthType.DXA }, rows: [uH, ...uR] }))

    // 变更管理
    children.push(new Paragraph({ spacing: { before: 400, after: 150 }, children: [new TextRun({ text: '变更管理', size: 26, bold: true, font: '微软雅黑' })] }))
    const cH = new TableRow({ children: [cell('项目', { bold: true, width: 1000 }), cell('变更类型', { bold: true, width: 1200 }), cell('概要', { bold: true, width: 1800 }), cell('变更目的', { bold: true, width: 1800 }), cell('变更单', { bold: true, width: 1000 }), cell('变更方式', { bold: true, width: 1000 }), cell('变更状态', { bold: true, width: 1000 })] })
    const cR = changeRows.value.length
      ? changeRows.value.map(r => new TableRow({ children: [cell(r.project, { width: 1000 }), cell(r.type, { width: 1200 }), cell(r.summary, { width: 1800 }), cell(r.purpose, { width: 1800 }), cell(r.ticket, { width: 1000 }), cell(r.method, { width: 1000 }), cell(r.status, { width: 1000 })] }))
      : [new TableRow({ children: [cell(''), cell(''), cell(''), cell(''), cell(''), cell(''), cell('')] })]
    children.push(new Table({ width: { size: 9800, type: WidthType.DXA }, rows: [cH, ...cR] }))

    // 二、业务和知识库持续建设
    children.push(new Paragraph({ spacing: { before: 400, after: 100 }, children: [new TextRun({ text: '二、业务和知识库持续建设', size: 24, bold: true, font: '微软雅黑' })] }))
    children.push(new Paragraph({ spacing: { before: 100, after: 50 }, children: [new TextRun({ text: '1. 故障问题处理方案', size: 22, bold: true, font: '微软雅黑' })] }))
    if (faultPlanRows.value.length) {
      for (const row of faultPlanRows.value) {
        children.push(new Paragraph({ spacing: { before: 50, after: 50 }, children: [new TextRun({ text: '- ' + (row.content || '无'), size: 22, font: '微软雅黑' })] }))
      }
    } else {
      children.push(new Paragraph({ spacing: { before: 50, after: 100 }, children: [new TextRun({ text: '- 无', size: 22, font: '微软雅黑' })] }))
    }
    children.push(new Paragraph({ spacing: { before: 100, after: 50 }, children: [new TextRun({ text: '2. 故障经验分享', size: 22, bold: true, font: '微软雅黑' })] }))
    if (faultShareRows.value.length) {
      for (const row of faultShareRows.value) {
        children.push(new Paragraph({ spacing: { before: 50, after: 50 }, children: [new TextRun({ text: '- ' + (row.content || '无'), size: 22, font: '微软雅黑' })] }))
      }
    } else {
      children.push(new Paragraph({ spacing: { before: 50, after: 100 }, children: [new TextRun({ text: '- 无', size: 22, font: '微软雅黑' })] }))
    }
    children.push(new Paragraph({ spacing: { before: 100, after: 50 }, children: [new TextRun({ text: '3. 优化工作', size: 22, bold: true, font: '微软雅黑' })] }))
    if (optimizationRows.value.length) {
      for (const row of optimizationRows.value) {
        children.push(new Paragraph({ spacing: { before: 50, after: 50 }, children: [new TextRun({ text: '- ' + (row.content || '无'), size: 22, font: '微软雅黑' })] }))
      }
    } else {
      children.push(new Paragraph({ spacing: { before: 50, after: 100 }, children: [new TextRun({ text: '- 无', size: 22, font: '微软雅黑' })] }))
    }

    // 三、项目巡检（监控截图）
    if (selectedTaskIds.value.length) {
      children.push(new Paragraph({ spacing: { before: 400, after: 150 }, children: [new TextRun({ text: '三、项目巡检', size: 24, bold: true, font: '微软雅黑' })] }))
      screenshotLoading.value = true
      try {
        const screenshotResults = await captureSelectedTaskScreenshots()
        for (const taskResult of screenshotResults) {
          children.push(new Paragraph({ spacing: { before: 200, after: 100 }, children: [new TextRun({ text: taskResult.taskName, size: 22, bold: true, font: '微软雅黑' })] }))
          if (taskResult.error) {
            children.push(new Paragraph({ children: [new TextRun({ text: `截图失败: ${taskResult.error}`, size: 20, font: '微软雅黑', color: 'FF0000' })] }))
            continue
          }
          for (const dash of taskResult.dashboards) {
            if (dash.error) {
              children.push(new Paragraph({ children: [new TextRun({ text: `${dash.title || dash.uid}: ${dash.error}`, size: 20, font: '微软雅黑', color: 'FF0000' })] }))
              continue
            }
            for (const panel of (dash.panels || [])) {
              if (!panel.base64) continue
              try {
                const base64Data = panel.base64.replace(/^data:image\/\w+;base64,/, '')
                const binaryStr = atob(base64Data)
                const bytes = new Uint8Array(binaryStr.length)
                for (let i = 0; i < binaryStr.length; i++) bytes[i] = binaryStr.charCodeAt(i)
                children.push(new Paragraph({ spacing: { before: 100, after: 50 }, children: [new TextRun({ text: dash.title + (panel.title && panel.title !== dash.title ? ` - ${panel.title}` : ''), size: 20, font: '微软雅黑', color: '666666' })] }))
                children.push(new Paragraph({ children: [new ImageRun({ data: bytes, transformation: { width: 680, height: 400 }, type: 'png' })] }))
              } catch (imgErr) {
                children.push(new Paragraph({ children: [new TextRun({ text: `图片处理失败: ${imgErr.message}`, size: 20, font: '微软雅黑', color: 'FF0000' })] }))
              }
            }
          }
        }
      } finally {
        screenshotLoading.value = false
      }
    }

    // 四、问题处理（目标：100%）
    children.push(new Paragraph({ spacing: { before: 400, after: 100 }, children: [new TextRun({ text: '四、问题处理（目标：100%）', size: 24, bold: true, font: '微软雅黑' })] }))
    for (const [project, rows] of Object.entries(issueRows.value)) {
      children.push(new Paragraph({ spacing: { before: 150, after: 80 }, children: [new TextRun({ text: `- ${project}：`, size: 22, bold: true, font: '微软雅黑' })] }))
      const iH = new TableRow({ children: [cell('本周进展', { bold: true, width: 4000 }), cell('完成度', { bold: true, width: 1800 }), cell('备注', { bold: true, width: 4000 })] })
      const iR = rows.length
        ? rows.map(r => new TableRow({ children: [cell(r.progress || '无', { width: 4000 }), cell(r.completion || '无', { width: 1800 }), cell(r.remark || '无', { width: 4000 })] }))
        : [new TableRow({ children: [cell('无'), cell('无'), cell('无')] })]
      children.push(new Table({ width: { size: 9800, type: WidthType.DXA }, rows: [iH, ...iR] }))
    }

    const doc = new Document({ sections: [{ properties: {}, children }] })
    const blob = await Packer.toBlob(doc)
    saveAs(blob, `${title}.docx`)
  } catch (e) {
    console.error('Export failed:', e)
    toast('导出失败: ' + e.message, 'error')
  } finally {
    exporting.value = false
  }
}
</script>

<style scoped>
.report-page { max-width: 1100px; }

.page-header { margin-bottom: 20px; }
.page-header h2 { font-size: 18px; font-weight: 600; color: var(--text-primary); }
.page-desc { font-size: 13px; color: var(--text-muted); margin-top: 4px; }

.source-card { margin-bottom: 16px; }
.card-top { display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; }
.card-top h3 { font-size: 15px; font-weight: 600; color: var(--text-primary); }
.source-tag { font-size: 12px; font-family: var(--font-mono); color: var(--primary-light); background: rgba(76,154,255,0.1); padding: 3px 10px; border-radius: 4px; }

.date-section { display: flex; flex-direction: column; gap: 12px; }
.filter-row { display: flex; gap: 16px; align-items: flex-end; }
.quick-dates { display: flex; gap: 6px; flex-wrap: wrap; }
.date-range { display: flex; align-items: flex-end; gap: 10px; }
.date-field { flex: 1; }
.date-field label { display: block; font-size: 11px; color: var(--text-muted); margin-bottom: 4px; }
.date-sep { color: var(--text-muted); padding-bottom: 8px; }

.fetch-status { display: flex; align-items: center; gap: 10px; margin-top: 14px; color: var(--text-secondary); font-size: 13px; }
.fetch-result { margin-top: 14px; font-size: 13px; }
.result-ok { color: var(--success); }
.result-empty { color: var(--text-muted); }
.matched-list { display: flex; gap: 6px; flex-wrap: wrap; margin-top: 8px; }
.matched-tag { font-size: 12px; background: var(--bg-tertiary); color: var(--text-primary); padding: 3px 10px; border-radius: 4px; cursor: pointer; transition: background 200ms; }
.matched-tag:hover { background: var(--bg-hover); color: var(--primary-light); }

.meta-card { margin-bottom: 16px; }
.meta-row { display: flex; gap: 16px; }
.meta-field { flex: 1; }
.meta-field label { display: block; font-size: 12px; font-weight: 500; color: var(--text-secondary); margin-bottom: 6px; }
.screenshot-task-list { display: flex; flex-wrap: wrap; gap: 10px; margin-top: 4px; }
.screenshot-task-check { display: flex; align-items: center; gap: 5px; font-size: 13px; color: var(--text-primary); cursor: pointer; padding: 4px 10px; border-radius: 6px; background: rgba(255,255,255,0.04); border: 1px solid rgba(255,255,255,0.08); }
.screenshot-task-check:hover { background: rgba(76,154,255,0.08); }
.screenshot-task-check input[type="checkbox"] { accent-color: var(--primary); }
.screenshot-progress-card { margin: 20px 0; padding: 16px 20px; background: rgba(59,130,246,0.06); border: 1px solid rgba(59,130,246,0.15); border-radius: 8px; }
.progress-header { display: flex; align-items: baseline; gap: 10px; margin-bottom: 8px; }
.progress-percent { font-size: 24px; font-weight: 700; color: var(--primary-light); }
.progress-count { font-size: 13px; color: var(--text-muted); }
.progress-bar-wrap { width: 100%; height: 8px; background: rgba(255,255,255,0.08); border-radius: 4px; overflow: hidden; margin-bottom: 10px; }
.progress-bar-fill { height: 100%; background: linear-gradient(90deg, #3b82f6, #60a5fa); border-radius: 4px; transition: width 0.4s ease; }
.progress-detail { font-size: 13px; color: var(--text-primary); margin-bottom: 6px; }
.progress-time { display: flex; justify-content: space-between; font-size: 12px; color: var(--text-muted); }

.table-card { margin-bottom: 16px; }
.summary-card { padding: 16px 20px; margin-bottom: 16px; }
.summary-text { font-size: 15px; color: #cfd8dc; margin: 0; line-height: 1.6; }
.row-count { font-size: 12px; font-weight: 400; color: var(--text-muted); }

table { cursor: default; }
table tr { cursor: default; }
table tr:hover td { background: transparent; }

.cell-input {
  width: 100%; padding: 5px 8px; background: var(--bg-input); border: 1px solid transparent;
  border-radius: 4px; color: var(--text-primary); font-size: 13px; font-family: var(--font-sans); outline: none; transition: border-color 200ms;
}
.cell-input:focus { border-color: var(--primary-light); background: var(--bg-secondary); }
select.cell-input {
  appearance: none;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='10' viewBox='0 0 24 24' fill='none' stroke='%2394A3B8' stroke-width='2'%3E%3Cpath d='m6 9 6 6 6-6'/%3E%3C/svg%3E");
  background-repeat: no-repeat; background-position: right 6px center; padding-right: 22px;
}

.status-success { color: var(--success) !important; }
.status-danger { color: var(--danger) !important; }
.status-warning { color: var(--warning) !important; }
.status-info { color: var(--info) !important; }

.empty-row { text-align: center; color: var(--text-muted); padding: 24px !important; font-size: 13px; }
.action-cell { text-align: center; padding: 6px !important; }

.btn-icon {
  display: inline-flex; align-items: center; justify-content: center; width: 28px; height: 28px;
  border: none; background: transparent; border-radius: 6px; cursor: pointer;
  color: var(--text-muted); transition: all 200ms; font-size: 18px; line-height: 1;
}
.btn-icon:hover { background: var(--bg-hover); color: var(--text-primary); }
.btn-icon.danger:hover { background: rgba(239,68,68,0.1); color: var(--danger); }

.action-bar { display: flex; align-items: center; justify-content: space-between; padding: 16px 0; }
.action-right { display: flex; gap: 10px; }

.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.6); display: flex; align-items: center; justify-content: center; z-index: 1000; backdrop-filter: blur(4px); }
.modal-content { background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius-lg); max-width: 900px; width: 90%; max-height: 85vh; display: flex; flex-direction: column; overflow: hidden; }
.modal-header { display: flex; align-items: center; justify-content: space-between; padding: 16px 20px; border-bottom: 1px solid var(--border); }
.modal-header h3 { font-size: 15px; font-weight: 600; color: var(--text-primary); }

.preview-body { padding: 30px; overflow-y: auto; background: #fff; color: #1a1a1a; }
.preview-title { font-size: 20px; font-weight: 700; text-align: center; margin-bottom: 6px; color: #111; }
.preview-date { text-align: center; font-size: 13px; color: #666; margin-bottom: 24px; }
.section-title { font-size: 15px; font-weight: 600; margin: 20px 0 10px; color: #222; }
.preview-table { width: 100%; border-collapse: collapse; font-size: 13px; margin-bottom: 16px; }
.preview-table th, .preview-table td { border: 1px solid #ccc; padding: 8px 10px; text-align: left; color: #333; background: #fff; }
.preview-table th { background: #f5f5f5; font-weight: 600; color: #222; text-transform: none; letter-spacing: 0; font-size: 13px; }
.preview-table tr:hover td { background: #fff; }
.no-data { color: #999; font-size: 13px; padding: 12px 0; }

.report-textarea {
  width: 100%; padding: 10px 14px;
  background: var(--bg-input); border: 1px solid var(--border);
  border-radius: 8px; color: var(--text-primary);
  font-size: 13px; font-family: var(--font-sans);
  resize: vertical; outline: none; transition: border-color 200ms; line-height: 1.6;
}
.report-textarea:focus { border-color: var(--primary-light); background: var(--bg-secondary); }
.report-textarea::placeholder { color: var(--text-muted); }

.sub-section label.sub-title { display: block; font-size: 13px; font-weight: 500; color: var(--text-secondary); margin-bottom: 8px; }

.project-group { margin-bottom: 16px; }
.project-group:last-child { margin-bottom: 0; }
.project-label { font-size: 14px; font-weight: 600; color: var(--text-primary); margin-bottom: 10px; }

/* 报告页表格加边框 */
.table-card table { border: 1px solid rgba(255,255,255,0.1); border-radius: 6px; overflow: hidden; }
.table-card table th { border: 1px solid rgba(255,255,255,0.1); }
.table-card table td { border: 1px solid rgba(255,255,255,0.06); }
</style>
