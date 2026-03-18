<template>
  <div>
    <div class="tabs">
      <button :class="['tab', { active: activeTab === 'connections' }]" @click="activeTab = 'connections'">服务连接</button>
      <button :class="['tab', { active: activeTab === 'general' }]" @click="activeTab = 'general'">通用配置</button>
      <button :class="['tab', { active: activeTab === 'users' }]" @click="activeTab = 'users'">用户管理</button>
      <button :class="['tab', { active: activeTab === 'screenshot' }]" @click="activeTab = 'screenshot'">截图任务</button>
      <button :class="['tab', { active: activeTab === 'audit' }]" @click="activeTab = 'audit'">审计日志</button>
    </div>

    <!-- 服务连接管理 -->
    <div v-if="activeTab === 'connections'">
      <!-- Confluence 连接 -->
      <div class="conn-section">
        <div class="section-header">
          <div class="section-label">
            <span class="type-icon confluence">C</span>
            <h3>Confluence 连接</h3>
          </div>
          <button v-if="canManageConns" class="btn btn-sm btn-primary" @click="openAddConn('confluence')">+ 添加</button>
        </div>
        <div class="conn-grid">
          <div class="conn-card" v-for="c in confluenceConns" :key="c.id" :class="{ default: c.is_default }">
            <div class="conn-card-header">
              <div class="conn-name">{{ c.name }}</div>
              <span v-if="c.is_default" class="default-badge">默认</span>
            </div>
            <div class="conn-url">{{ c.url }}</div>
            <div class="conn-meta">
              <span v-if="c.username">{{ c.username }}</span>
              <span v-if="c.config?.space_key" class="meta-tag">空间: {{ c.config.space_key }}</span>
              <span v-if="c.config?.root_page" class="meta-tag">根页面: {{ c.config.root_page }}</span>
            </div>
            <div class="conn-actions">
              <button class="btn btn-xs" @click="testConn(c)">{{ c._testing ? '测试中...' : '测试' }}</button>
              <button v-if="canManageConns" class="btn btn-xs" @click="openEditConn(c)">编辑</button>
              <button v-if="canManageConns" class="btn btn-xs danger" @click="deleteConn(c)">删除</button>
            </div>
            <div v-if="c._testResult" :class="['test-msg', c._testResult.ok ? 'ok' : 'fail']">{{ c._testResult.message }}</div>
          </div>
          <div class="conn-card empty" v-if="!confluenceConns.length" @click="openAddConn('confluence')">
            <span class="empty-icon">+</span>
            <span>添加 Confluence 连接</span>
          </div>
        </div>
      </div>

      <!-- Jira 连接 -->
      <div class="conn-section">
        <div class="section-header">
          <div class="section-label">
            <span class="type-icon jira">J</span>
            <h3>Jira 连接</h3>
          </div>
          <button v-if="canManageConns" class="btn btn-sm btn-primary" @click="openAddConn('jira')">+ 添加</button>
        </div>
        <div class="conn-grid">
          <div class="conn-card" v-for="c in jiraConns" :key="c.id" :class="{ default: c.is_default }">
            <div class="conn-card-header">
              <div class="conn-name">{{ c.name }}</div>
              <span v-if="c.is_default" class="default-badge">默认</span>
            </div>
            <div class="conn-url">{{ c.url }}</div>
            <div class="conn-meta">
              <span v-if="c.username">{{ c.username }}</span>
              <span v-if="c.config?.fault_projects" class="meta-tag">故障: {{ c.config.fault_projects }}</span>
              <span v-if="c.config?.change_projects" class="meta-tag">变更: {{ c.config.change_projects }}</span>
            </div>
            <div class="conn-actions">
              <button class="btn btn-xs" @click="testConn(c)">{{ c._testing ? '测试中...' : '测试' }}</button>
              <button v-if="canManageConns" class="btn btn-xs" @click="openEditConn(c)">编辑</button>
              <button v-if="canManageConns" class="btn btn-xs danger" @click="deleteConn(c)">删除</button>
            </div>
            <div v-if="c._testResult" :class="['test-msg', c._testResult.ok ? 'ok' : 'fail']">{{ c._testResult.message }}</div>
          </div>
          <div class="conn-card empty" v-if="!jiraConns.length && canManageConns" @click="openAddConn('jira')">
            <span class="empty-icon">+</span>
            <span>添加 Jira 连接</span>
          </div>
        </div>
      </div>

      <!-- Grafana 连接 -->
      <div class="conn-section">
        <div class="section-header">
          <div class="section-label">
            <span class="type-icon grafana">G</span>
            <h3>Grafana 连接</h3>
          </div>
          <button v-if="canManageConns" class="btn btn-sm btn-primary" @click="openAddConn('grafana')">+ 添加</button>
        </div>
        <div class="conn-grid">
          <div class="conn-card" v-for="c in grafanaConns" :key="c.id" :class="{ default: c.is_default }">
            <div class="conn-card-header">
              <div class="conn-name">{{ c.name }}</div>
              <span v-if="c.is_default" class="default-badge">默认</span>
            </div>
            <div class="conn-url">{{ c.url }}</div>
            <div class="conn-meta">
              <span class="meta-tag">API Token: ****</span>
            </div>
            <div class="conn-actions">
              <button class="btn btn-xs" @click="testConn(c)">{{ c._testing ? '测试中...' : '测试' }}</button>
              <button v-if="canManageConns" class="btn btn-xs" @click="openEditConn(c)">编辑</button>
              <button v-if="canManageConns" class="btn btn-xs danger" @click="deleteConn(c)">删除</button>
            </div>
            <div v-if="c._testResult" :class="['test-msg', c._testResult.ok ? 'ok' : 'fail']">{{ c._testResult.message }}</div>
          </div>
          <div class="conn-card empty" v-if="!grafanaConns.length && canManageConns" @click="openAddConn('grafana')">
            <span class="empty-icon">+</span>
            <span>添加 Grafana 连接</span>
          </div>
        </div>
      </div>
      <!-- Lark 飞书连接 -->
      <div class="conn-section">
        <div class="section-header">
          <div class="section-label">
            <span class="type-icon lark">L</span>
            <h3>飞书 Lark 连接</h3>
          </div>
          <button v-if="canManageConns" class="btn btn-sm btn-primary" @click="openAddConn('lark')">+ 添加</button>
        </div>
        <div class="conn-grid">
          <div class="conn-card" v-for="c in larkConns" :key="c.id" :class="{ default: c.is_default }">
            <div class="conn-card-header">
              <div class="conn-name">{{ c.name }}</div>
              <span v-if="c.is_default" class="default-badge">默认</span>
            </div>
            <div class="conn-url">{{ c.url?.replace(/\/[^/]{8,}$/, '/****') }}</div>
            <div class="conn-meta">
              <span class="meta-tag">Webhook 机器人</span>
            </div>
            <div class="conn-actions">
              <button class="btn btn-xs" @click="testConn(c)">{{ c._testing ? '测试中...' : '测试' }}</button>
              <button v-if="canManageConns" class="btn btn-xs" @click="openEditConn(c)">编辑</button>
              <button v-if="canManageConns" class="btn btn-xs danger" @click="deleteConn(c)">删除</button>
            </div>
            <div v-if="c._testResult" :class="['test-msg', c._testResult.ok ? 'ok' : 'fail']">{{ c._testResult.message }}</div>
          </div>
          <div class="conn-card empty" v-if="!larkConns.length && canManageConns" @click="openAddConn('lark')">
            <span class="empty-icon">+</span>
            <span>添加飞书群机器人</span>
          </div>
        </div>
      </div>

      <!-- 飞书应用配置（截图发图片） -->
      <div class="conn-section" v-if="canManageConns">
        <div class="section-header">
          <div class="section-label">
            <span class="type-icon lark">A</span>
            <h3>飞书应用配置（截图发图片）</h3>
          </div>
          <button class="btn btn-sm btn-primary" @click="saveLarkApp" :disabled="savingLarkApp">{{ savingLarkApp ? '保存中...' : '保存' }}</button>
        </div>
        <div class="form-grid" style="max-width:640px">
          <div class="field">
            <label>App ID</label>
            <input class="input" v-model="larkAppId" placeholder="飞书开放平台应用的 App ID" />
          </div>
          <div class="field">
            <label>App Secret</label>
            <input class="input" type="password" v-model="larkAppSecret" placeholder="飞书开放平台应用的 App Secret" />
          </div>
        </div>
        <p class="field-desc" style="margin-top:8px">在飞书开放平台创建应用，开通 im:resource 权限，用于上传截图到飞书群。不配置则截图消息只有文字。</p>
      </div>
    </div>

    <!-- 截图任务管理 -->
    <div v-if="activeTab === 'screenshot'" class="card">
      <div class="section-header" style="margin-bottom:18px">
        <h3 style="margin:0">Grafana 截图定时任务</h3>
        <button class="btn btn-primary btn-sm" @click="openAddTask">+ 新建任务</button>
      </div>
      <div v-if="!screenshotTasks.length" class="empty-state">暂无截图任务，点击上方按钮创建</div>
      <div class="task-list">
        <div class="task-card" v-for="t in screenshotTasks" :key="t.id">
          <div class="task-card-header">
            <div>
              <div class="task-name">{{ t.name }}</div>
              <div class="task-meta">
                <span>Cron: <code>{{ t.cron_expr }}</code></span>
                <span>时间范围: {{ formatTimeRange(t.time_range) }}</span>
                <span>{{ t.width }}×{{ t.height }}</span>
                <span>{{ t.theme === 'dark' ? '暗色' : '亮色' }}</span>
              </div>
            </div>
            <div class="task-status">
              <label class="switch">
                <input type="checkbox" :checked="t.enabled" @change="toggleTask(t)">
                <span class="slider"></span>
              </label>
            </div>
          </div>
          <div class="task-dashboards">
            <span class="dash-tag" v-for="d in parseDashboards(t.dashboards)" :key="d.uid">
              {{ d.title || d.uid }}
              <small v-if="d.panels?.length">({{ d.panels.length }}面板)</small>
              <small v-else>(全部)</small>
            </span>
          </div>
          <div class="task-lark">
            发送到: <span class="lark-tag" v-for="id in parseLarkIDs(t.lark_conn_ids)" :key="id">{{ getLarkName(id) }}</span>
          </div>
          <div v-if="t.last_run_at" class="task-last-run">
            上次执行: {{ t.last_run_at }} <span :class="['status-badge', t.last_status === 'success' ? 'ok' : 'fail']">{{ t.last_status }}</span>
          </div>
          <div class="conn-actions" style="margin-top:10px">
            <button class="btn btn-xs preview-btn" @click="previewTask(t)" :disabled="t._previewing">{{ t._previewing ? '截图中...' : '预览截图' }}</button>
            <button class="btn btn-xs" @click="runTask(t)">立即执行</button>
            <button class="btn btn-xs" @click="openEditTask(t)">编辑</button>
            <button class="btn btn-xs danger" @click="deleteTask(t)">删除</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 截图任务编辑弹窗 -->
    <div v-if="showTaskModal" class="modal-overlay">
      <div class="modal card conn-modal" style="width:640px">
        <h3>{{ editingTask.id ? '编辑截图任务' : '新建截图任务' }}</h3>
        <div class="form-grid">
          <div class="field">
            <label>任务名称</label>
            <input class="input" v-model="editingTask.name" placeholder="如：核心服务每小时截图" />
          </div>
          <div class="field">
            <label>Grafana 连接</label>
            <select class="input" v-model="editingTask.grafana_conn_id">
              <option v-for="c in grafanaConns" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select>
          </div>
        </div>
        <div class="form-grid">
          <div class="field">
            <label>Cron 表达式</label>
            <input class="input" v-model="editingTask.cron_expr" placeholder="0 * * * * (每小时)" />
            <span class="field-desc">分 时 日 月 周（如 0 * * * * = 每小时整点）</span>
          </div>
          <div class="field">
            <label>截图时间范围</label>
            <select class="input" v-model="timeRangeType">
              <option value="30m">30分钟</option>
              <option value="1h">1小时</option>
              <option value="3h">3小时</option>
              <option value="6h">6小时</option>
              <option value="12h">12小时</option>
              <option value="24h">24小时</option>
              <option value="custom">自定义时间段</option>
            </select>
            <div v-if="timeRangeType === 'custom'" class="custom-time-row">
              <input type="time" class="input time-input" v-model="customTimeFrom" />
              <span class="var-eq">~</span>
              <input type="time" class="input time-input" v-model="customTimeTo" />
            </div>
          </div>
        </div>
        <div class="form-grid">
          <div class="field">
            <label>截图宽度</label>
            <input class="input" type="number" v-model.number="editingTask.width" />
          </div>
          <div class="field">
            <label>截图高度</label>
            <input class="input" type="number" v-model.number="editingTask.height" />
          </div>
        </div>
        <div class="field">
          <label>主题</label>
          <select class="input" v-model="editingTask.theme" style="width:200px">
            <option value="light">亮色</option>
            <option value="dark">暗色</option>
          </select>
        </div>

        <div class="divider"></div>
        <h4 style="margin:0 0 8px">Dashboard 配置</h4>
        <div v-for="(dash, idx) in editingTask.dashboards" :key="idx" class="dash-config-item">
          <div class="form-grid" style="align-items:end">
            <div class="field">
              <label>Dashboard</label>
              <select class="input" v-model="dash.uid" @change="onDashboardSelect(dash, idx)">
                <option value="">请选择</option>
                <option v-for="d in availableDashboards" :key="d.uid" :value="d.uid">{{ d.title }}</option>
              </select>
            </div>
            <div class="field" style="flex-direction:row;align-items:center;gap:8px">
              <span class="field-desc" style="margin:0">{{ dash.panels?.length ? dash.panels.length + ' 个面板' : '全部面板' }}</span>
              <button class="btn btn-xs danger" @click="editingTask.dashboards.splice(idx, 1)">移除</button>
            </div>
          </div>
        </div>
        <button class="btn btn-xs" @click="editingTask.dashboards.push({ uid: '', title: '', panels: [] })" style="margin-top:8px">+ 添加 Dashboard</button>

        <div class="divider"></div>
        <h4 style="margin:0 0 8px">模板变量</h4>
        <p class="field-desc" style="margin-top:0">Dashboard 中的模板变量（如 namespace、datasource），格式为 key=value。截图时会自动传递 var-key=value 参数。</p>
        <div v-for="(_, key) in editingTask.variables" :key="key" class="var-row">
          <input class="input var-input" :value="key" @change="renameVar($event, key)" placeholder="变量名（如 namespace）" />
          <span class="var-eq">=</span>
          <input class="input var-input" v-model="editingTask.variables[key]" placeholder="值（如 opsplatform）" />
          <button class="btn btn-xs danger" @click="delete editingTask.variables[key]">删除</button>
        </div>
        <button class="btn btn-xs" @click="addVariable" style="margin-top:8px">+ 添加变量</button>

        <div class="divider"></div>
        <h4 style="margin:0 0 8px">发送到飞书群</h4>
        <div class="lark-select">
          <label v-for="c in larkConns" :key="c.id" class="lark-check">
            <input type="checkbox" :value="c.id" v-model="editingTask.lark_conn_ids" />
            {{ c.name }}
          </label>
        </div>
        <div v-if="!larkConns.length" class="field-desc">请先在「服务连接」中添加飞书机器人</div>

        <div class="field checkbox-field" style="margin-top:12px">
          <label><input type="checkbox" v-model="editingTask.enabled" /> 创建后立即启用</label>
        </div>

        <div class="form-actions">
          <button class="btn" @click="showTaskModal = false">取消</button>
          <button class="btn btn-primary" @click="saveTask" :disabled="savingTask">{{ savingTask ? '保存中...' : '保存' }}</button>
        </div>
      </div>
    </div>

    <!-- 截图预览弹窗 -->
    <div v-if="showPreviewModal" class="modal-overlay" @click.self="showPreviewModal = false">
      <div class="modal card preview-modal">
        <div class="preview-header">
          <h3>截图预览 - {{ previewTaskName }}</h3>
          <div style="display:flex;gap:8px">
            <button class="btn btn-xs" style="background:var(--primary);color:#fff" @click="testSendTask" :disabled="testSending">{{ testSending ? '发送中...' : '测试发送飞书' }}</button>
            <button class="btn btn-xs" @click="showPreviewModal = false">关闭</button>
          </div>
        </div>
        <div v-if="previewLoading" class="empty-state">正在截图，请稍候...</div>
        <div v-else-if="previewError" class="empty-state" style="color:var(--danger)">{{ previewError }}</div>
        <div v-else class="preview-content">
          <div v-for="dash in previewData" :key="dash.uid" class="preview-dashboard">
            <h4 class="preview-dash-title">{{ dash.title || dash.uid }}</h4>
            <div v-if="dash.error" class="test-msg fail">{{ dash.error }}</div>
            <div v-else class="preview-panels">
              <div v-for="p in dash.panels" :key="p.panel_id" class="preview-panel">
                <div class="preview-panel-title">{{ p.title }}</div>
                <img :src="p.base64" :alt="p.title" class="preview-img" @click="openFullImg(p.base64)" />
              </div>
            </div>
            <div v-if="!dash.error && (!dash.panels || !dash.panels.length)" class="empty-state">未获取到面板截图</div>
          </div>
        </div>
      </div>
    </div>

    <!-- 图片全屏查看 -->
    <div v-if="fullImg" class="modal-overlay" style="cursor:zoom-out" @click="fullImg = null">
      <img :src="fullImg" style="max-width:95vw;max-height:95vh;border-radius:8px" />
    </div>

    <!-- 通用配置 -->
    <div v-if="activeTab === 'general'" class="card">
      <h3>报告项目列表</h3>
      <p class="field-desc">配置可选的项目代号，用于生成报告时按项目筛选。Confluence 和 Jira 报告共用此列表。多个项目用英文逗号分隔。</p>
      <div class="form-grid">
        <div class="field">
          <label>项目列表</label>
          <input class="input" v-model="generalConfig.confluence_projects" placeholder="G01,G32,G33（逗号分隔）" />
        </div>
      </div>
      <div class="form-actions">
        <button v-if="canManageSettings" class="btn btn-primary" @click="saveGeneral" :disabled="saving">保存</button>
      </div>
    </div>

    <!-- 用户管理 -->
    <div v-if="activeTab === 'users'" class="card">
      <div class="section-header">
        <h3>用户列表</h3>
        <button v-if="canManageSettings" class="btn btn-primary btn-sm" @click="showAddUser = true">添加用户</button>
      </div>
      <div class="table-wrapper">
        <table>
          <thead><tr><th>用户名</th><th>显示名</th><th>角色</th><th>来源</th><th>状态</th><th>创建时间</th></tr></thead>
          <tbody>
            <tr v-for="u in users" :key="u.id" style="cursor:default">
              <td>{{ u.username }}</td>
              <td>{{ u.display_name }}</td>
              <td><span class="badge" :class="u.role === 'admin' ? 'admin' : ''">{{ u.role }}</span></td>
              <td>{{ u.auth_source }}</td>
              <td><span :class="['status-dot', u.status]"></span>{{ u.status }}</td>
              <td class="date-cell">{{ u.created_at }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="showAddUser" class="modal-overlay">
        <div class="modal card">
          <h3>添加用户</h3>
          <div class="field"><label>用户名</label><input class="input" v-model="newUser.username" /></div>
          <div class="field"><label>密码</label><input class="input" type="password" v-model="newUser.password" /></div>
          <div class="field"><label>显示名</label><input class="input" v-model="newUser.display_name" /></div>
          <div class="field"><label>角色</label>
            <select class="input" v-model="newUser.role"><option value="user">user</option><option value="admin">admin</option></select>
          </div>
          <div class="form-actions">
            <button class="btn" @click="showAddUser = false">取消</button>
            <button class="btn btn-primary" @click="addUser">创建</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 审计日志 -->
    <div v-if="activeTab === 'audit'" class="card">
      <h3>审计日志</h3>
      <div class="table-wrapper">
        <table>
          <thead><tr><th>时间</th><th>用户</th><th>操作</th><th>详情</th><th>IP</th></tr></thead>
          <tbody>
            <tr v-for="l in auditLogs" :key="l.id" style="cursor:default">
              <td class="date-cell">{{ l.created_at }}</td><td>{{ l.username }}</td><td>{{ l.action }}</td><td>{{ l.detail }}</td><td>{{ l.ip }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 连接编辑弹窗 -->
    <div v-if="showConnModal" class="modal-overlay">
      <div class="modal card conn-modal">
        <h3>{{ editingConn.id ? '编辑连接' : '添加连接' }}</h3>
        <div class="field">
          <label>连接名称</label>
          <input class="input" v-model="editingConn.name" :placeholder="editingConn.type === 'grafana' ? 'Grafana 生产' : editingConn.type === 'confluence' ? 'Confluence 生产' : 'Jira 生产'" />
        </div>
        <template v-if="editingConn.type === 'lark'">
          <div class="field">
            <label>Webhook URL</label>
            <input class="input" v-model="editingConn.url" placeholder="https://open.larksuite.com/open-apis/bot/v2/hook/xxxx" />
          </div>
          <p class="field-desc">在飞书群 → 设置 → 群机器人 → 添加自定义机器人，复制 Webhook 地址。</p>
        </template>
        <template v-else-if="editingConn.type === 'grafana'">
          <div class="field">
            <label>Grafana URL</label>
            <input class="input" v-model="editingConn.url" placeholder="http://grafana.example.com" />
          </div>
          <div class="field">
            <label>Service Account API Token</label>
            <input class="input" type="password" v-model="editingConn.password" placeholder="glsa_xxxx（在 Grafana → Administration → Service Accounts 中创建）" />
          </div>
          <p class="field-desc">Grafana 使用 OIDC 登录时，需要创建 Service Account 并生成 API Token。进入 Grafana → Administration → Service accounts → 创建 → 添加 Token。</p>
        </template>
        <template v-else>
          <div class="form-grid">
            <div class="field">
              <label>{{ editingConn.type === 'confluence' ? 'Confluence' : 'Jira' }} URL</label>
              <input class="input" v-model="editingConn.url" placeholder="http://localhost:8091" />
            </div>
            <div class="field">
              <label>用户名</label>
              <input class="input" v-model="editingConn.username" placeholder="admin" />
            </div>
          </div>
          <div class="field">
            <label>密码 / 令牌</label>
            <input class="input" type="password" v-model="editingConn.password" placeholder="密码或 Personal Access Token" />
          </div>
        </template>

        <!-- Confluence 额外配置 -->
        <template v-if="editingConn.type === 'confluence'">
          <div class="divider"></div>
          <div class="form-grid">
            <div class="field">
              <label>空间 Key</label>
              <input class="input" v-model="editingConn.config.space_key" placeholder="DEV,TEAM（逗号分隔）" />
            </div>
            <div class="field">
              <label>根目录页面标题</label>
              <input class="input" v-model="editingConn.config.root_page" placeholder="发布说明" />
            </div>
          </div>
        </template>

        <!-- Jira 额外配置 -->
        <template v-if="editingConn.type === 'jira'">
          <div class="divider"></div>
          <div class="form-grid">
            <div class="field">
              <label>故障项目 Key</label>
              <input class="input" v-model="editingConn.config.fault_projects" placeholder="FLT（逗号分隔）" />
            </div>
            <div class="field">
              <label>故障工单类型</label>
              <input class="input" v-model="editingConn.config.fault_issuetype" placeholder="故障" />
            </div>
            <div class="field">
              <label>变更项目 Key</label>
              <input class="input" v-model="editingConn.config.change_projects" placeholder="CHG（逗号分隔，留空则不拉取变更单）" />
            </div>
            <div class="field">
              <label>变更工单类型</label>
              <input class="input" v-model="editingConn.config.change_issuetype" placeholder="任务" />
            </div>
          </div>
        </template>

        <div class="field checkbox-field">
          <label><input type="checkbox" v-model="editingConn.is_default" /> 设为默认连接</label>
        </div>

        <div class="form-actions">
          <button class="btn" @click="showConnModal = false">取消</button>
          <button class="btn btn-primary" @click="saveConn" :disabled="savingConn">{{ savingConn ? '保存中...' : '保存' }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch, inject } from 'vue'
import api from '@/api'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()
const canManageConns = computed(() => authStore.hasPermission('confluence:manage_connections'))
const canManageSettings = computed(() => authStore.hasPermission('confluence:manage_settings'))

const toast = inject('toast')
const confirm = inject('confirm')
const activeTab = ref('connections')
const saving = ref(false)
const savingConn = ref(false)

// 连接数据
const connections = ref([])
const confluenceConns = computed(() => connections.value.filter(c => c.type === 'confluence'))
const jiraConns = computed(() => connections.value.filter(c => c.type === 'jira'))
const grafanaConns = computed(() => connections.value.filter(c => c.type === 'grafana'))
const larkConns = computed(() => connections.value.filter(c => c.type === 'lark'))

// 飞书应用配置
const larkAppId = ref('')
const larkAppSecret = ref('')
const savingLarkApp = ref(false)

async function loadLarkAppConfig() {
  try {
    const res = await api.get('/api/settings')
    const s = res.data.data || {}
    larkAppId.value = s.lark_app_id || ''
    larkAppSecret.value = s.lark_app_secret || ''
  } catch (e) { /* ignore */ }
}

async function saveLarkApp() {
  savingLarkApp.value = true
  try {
    await api.post('/api/settings', {
      lark_app_id: larkAppId.value,
      lark_app_secret: larkAppSecret.value
    })
    toast('飞书应用配置已保存', 'success')
  } catch (e) {
    toast(e.response?.data?.error || '保存失败', 'error')
  } finally {
    savingLarkApp.value = false
  }
}

// 连接编辑弹窗
const showConnModal = ref(false)
const editingConn = ref({ type: '', name: '', url: '', username: '', password: '', config: {}, is_default: false })

// 截图任务
const screenshotTasks = ref([])
const showTaskModal = ref(false)
const savingTask = ref(false)
const availableDashboards = ref([])

// 截图预览
const showPreviewModal = ref(false)
const previewTaskName = ref('')
const previewTaskId = ref(null)
const testSending = ref(false)
const previewData = ref([])
const previewLoading = ref(false)
const previewError = ref('')
const fullImg = ref(null)
const editingTask = ref({
  name: '', grafana_conn_id: 0, dashboards: [{ uid: '', title: '', panels: [] }],
  variables: {}, lark_conn_ids: [], cron_expr: '0 * * * *', time_range: '1h',
  width: 1000, height: 500, theme: 'light', enabled: true
})

// 自定义时间范围
const timeRangeType = ref('1h')
const customTimeFrom = ref('09:00')
const customTimeTo = ref('10:00')

// 同步 timeRangeType <-> editingTask.time_range
watch(timeRangeType, (val) => {
  if (val === 'custom') {
    editingTask.value.time_range = `custom:${customTimeFrom.value}-${customTimeTo.value}`
  } else {
    editingTask.value.time_range = val
  }
})
watch(customTimeFrom, (val) => {
  if (timeRangeType.value === 'custom') {
    editingTask.value.time_range = `custom:${val}-${customTimeTo.value}`
  }
})
watch(customTimeTo, (val) => {
  if (timeRangeType.value === 'custom') {
    editingTask.value.time_range = `custom:${customTimeFrom.value}-${val}`
  }
})

// 通用配置
const generalConfig = ref({ confluence_projects: '' })

// 用户
const users = ref([])
const auditLogs = ref([])
const showAddUser = ref(false)
const newUser = ref({ username: '', password: '', display_name: '', role: 'user' })

onMounted(() => { loadTabData(); loadLarkAppConfig() })
watch(activeTab, () => loadTabData())

async function loadTabData() {
  if (activeTab.value === 'connections') {
    try {
      const endpoint = canManageConns.value ? '/api/connections' : '/api/connections/public'
      const res = await api.get(endpoint)
      connections.value = (res.data.data || []).map(c => {
        if (typeof c.config === 'string') {
          try { c.config = JSON.parse(c.config) } catch { c.config = {} }
        }
        if (!c.config) c.config = {}
        return c
      })
    } catch (e) { /* ignore */ }
  } else if (activeTab.value === 'screenshot') {
    try {
      const res = await api.get('/api/screenshot-tasks')
      screenshotTasks.value = res.data.data || []
    } catch (e) { /* ignore */ }
    // 同时加载连接列表（需要 Grafana 和 Lark 连接）
    if (!connections.value.length) {
      try {
        const endpoint = canManageConns.value ? '/api/connections' : '/api/connections/public'
        const res = await api.get(endpoint)
        connections.value = (res.data.data || []).map(c => {
          if (typeof c.config === 'string') { try { c.config = JSON.parse(c.config) } catch { c.config = {} } }
          if (!c.config) c.config = {}
          return c
        })
      } catch (e) { /* ignore */ }
    }
  } else if (activeTab.value === 'general') {
    try {
      const res = await api.get('/api/settings')
      generalConfig.value = res.data.data || {}
    } catch (e) { /* ignore */ }
  } else if (activeTab.value === 'users') {
    try { const res = await api.get('/api/users'); users.value = res.data.data || [] } catch (e) { /* ignore */ }
  } else if (activeTab.value === 'audit') {
    try { const res = await api.get('/api/audit-logs'); auditLogs.value = res.data.data || [] } catch (e) { /* ignore */ }
  }
}

function openAddConn(type) {
  editingConn.value = {
    type,
    name: '',
    url: '',
    username: '',
    password: '',
    config: type === 'confluence' ? { space_key: '', root_page: '' } : type === 'grafana' ? {} : type === 'lark' ? {} : { fault_projects: '', fault_issuetype: '故障', change_projects: '', change_issuetype: '' },
    is_default: connections.value.filter(c => c.type === type).length === 0, // 第一个自动设为默认
  }
  showConnModal.value = true
}

function openEditConn(conn) {
  editingConn.value = {
    id: conn.id,
    type: conn.type,
    name: conn.name,
    url: conn.url,
    username: conn.username,
    password: conn.password || '',
    config: { ...conn.config },
    is_default: conn.is_default,
  }
  showConnModal.value = true
}

async function saveConn() {
  savingConn.value = true
  try {
    const data = {
      type: editingConn.value.type,
      name: editingConn.value.name,
      url: editingConn.value.url,
      username: editingConn.value.username,
      password: editingConn.value.password,
      config: editingConn.value.config,
      is_default: editingConn.value.is_default,
    }
    if (editingConn.value.id) {
      await api.put(`/api/connections/${editingConn.value.id}`, data)
    } else {
      await api.post('/api/connections', data)
    }
    toast('保存成功', 'success')
    showConnModal.value = false
    loadTabData()
  } catch (e) {
    toast(e.response?.data?.error || '保存失败', 'error')
  } finally { savingConn.value = false }
}

async function deleteConn(conn) {
  const ok = await confirm({ title: '确认删除', message: `删除连接「${conn.name}」？`, type: 'warning', okText: '删除' })
  if (!ok) return
  try {
    await api.delete(`/api/connections/${conn.id}`)
    toast('已删除', 'success')
    loadTabData()
  } catch (e) {
    toast('删除失败', 'error')
  }
}

async function testConn(conn) {
  conn._testing = true
  conn._testResult = null
  try {
    const res = await api.post('/api/connections/test', {
      id: conn.id, type: conn.type, url: conn.url, username: conn.username, password: conn.password,
    })
    conn._testResult = { ok: true, message: `连接成功！用户: ${res.data.data?.user || ''}` }
  } catch (e) {
    conn._testResult = { ok: false, message: e.response?.data?.error || '连接失败' }
  } finally { conn._testing = false }
}

async function saveGeneral() {
  saving.value = true
  try {
    await api.post('/api/settings', generalConfig.value)
    toast('配置已保存', 'success')
  } catch (e) {
    toast('保存失败', 'error')
  } finally { saving.value = false }
}

async function addUser() {
  try {
    await api.post('/api/users', newUser.value)
    toast('用户已创建', 'success')
    showAddUser.value = false
    newUser.value = { username: '', password: '', display_name: '', role: 'user' }
    loadTabData()
  } catch (e) { toast(e.response?.data?.error || '创建失败', 'error') }
}

// ===== 截图任务 =====
function parseDashboards(val) {
  if (Array.isArray(val)) return val
  try { return JSON.parse(val) || [] } catch { return [] }
}
function parseLarkIDs(val) {
  if (Array.isArray(val)) return val
  try { return JSON.parse(val) || [] } catch { return [] }
}
function getLarkName(id) {
  const c = larkConns.value.find(c => c.id === id)
  return c ? c.name : `ID:${id}`
}
function formatTimeRange(val) {
  if (!val) return ''
  if (val.startsWith('custom:')) return val.replace('custom:', '').replace('-', ' ~ ')
  const map = { '30m': '最近30分钟', '1h': '最近1小时', '3h': '最近3小时', '6h': '最近6小时', '12h': '最近12小时', '24h': '最近24小时' }
  return map[val] || val
}

function openAddTask() {
  editingTask.value = {
    name: '', grafana_conn_id: grafanaConns.value[0]?.id || 0,
    dashboards: [{ uid: '', title: '', panels: [] }],
    variables: {}, lark_conn_ids: larkConns.value.map(c => c.id),
    cron_expr: '0 * * * *', time_range: '1h',
    width: 1000, height: 500, theme: 'light', enabled: true
  }
  timeRangeType.value = '1h'
  customTimeFrom.value = '09:00'
  customTimeTo.value = '10:00'
  loadAvailableDashboards()
  showTaskModal.value = true
}

function openEditTask(t) {
  editingTask.value = {
    id: t.id,
    name: t.name,
    grafana_conn_id: t.grafana_conn_id,
    dashboards: parseDashboards(t.dashboards),
    variables: typeof t.variables === 'string' ? JSON.parse(t.variables || '{}') : (t.variables || {}),
    lark_conn_ids: parseLarkIDs(t.lark_conn_ids),
    cron_expr: t.cron_expr,
    time_range: t.time_range,
    width: t.width,
    height: t.height,
    theme: t.theme,
    enabled: t.enabled
  }
  // 解析自定义时间范围
  if (t.time_range && t.time_range.startsWith('custom:')) {
    timeRangeType.value = 'custom'
    const parts = t.time_range.replace('custom:', '').split('-')
    customTimeFrom.value = parts[0] || '09:00'
    customTimeTo.value = parts[1] || '10:00'
  } else {
    timeRangeType.value = t.time_range || '1h'
  }
  loadAvailableDashboards()
  showTaskModal.value = true
}

async function loadAvailableDashboards() {
  const connId = editingTask.value.grafana_conn_id
  if (!connId) return
  try {
    const res = await api.get(`/api/grafana/dashboards?conn_id=${connId}`)
    availableDashboards.value = res.data.data || []
  } catch { availableDashboards.value = [] }
}

function onDashboardSelect(dash) {
  const found = availableDashboards.value.find(d => d.uid === dash.uid)
  if (found) dash.title = found.title
}

async function saveTask() {
  savingTask.value = true
  try {
    const data = { ...editingTask.value }
    if (data.id) {
      await api.put(`/api/screenshot-tasks/${data.id}`, data)
    } else {
      await api.post('/api/screenshot-tasks', data)
    }
    toast('保存成功', 'success')
    showTaskModal.value = false
    loadTabData()
  } catch (e) {
    toast(e.response?.data?.error || '保存失败', 'error')
  } finally { savingTask.value = false }
}

async function deleteTask(t) {
  const ok = await confirm({ title: '确认删除', message: `删除截图任务「${t.name}」？`, type: 'warning', okText: '删除' })
  if (!ok) return
  try {
    await api.delete(`/api/screenshot-tasks/${t.id}`)
    toast('已删除', 'success')
    loadTabData()
  } catch (e) { toast('删除失败', 'error') }
}

async function toggleTask(t) {
  try {
    await api.put(`/api/screenshot-tasks/${t.id}/toggle`, { enabled: !t.enabled })
    toast(t.enabled ? '已禁用' : '已启用', 'success')
    loadTabData()
  } catch (e) { toast('操作失败', 'error') }
}

function addVariable() {
  const key = 'var_' + Date.now()
  editingTask.value.variables[key] = ''
}

function renameVar(event, oldKey) {
  const newKey = event.target.value.trim()
  if (!newKey || newKey === oldKey) return
  const val = editingTask.value.variables[oldKey]
  delete editingTask.value.variables[oldKey]
  editingTask.value.variables[newKey] = val
}

async function previewTask(t) {
  t._previewing = true
  previewTaskName.value = t.name
  previewTaskId.value = t.id
  previewData.value = []
  previewError.value = ''
  previewLoading.value = true
  showPreviewModal.value = true
  try {
    const res = await api.get(`/api/screenshot-tasks/${t.id}/preview`)
    previewData.value = res.data.data || []
  } catch (e) {
    previewError.value = e.response?.data?.error || '截图失败'
  } finally {
    previewLoading.value = false
    t._previewing = false
  }
}

async function testSendTask() {
  if (!previewTaskId.value) return
  testSending.value = true
  try {
    const res = await api.post(`/api/screenshot-tasks/${previewTaskId.value}/test-send`)
    toast(res.data.data?.message || '发送成功', 'success')
  } catch (e) {
    toast(e.response?.data?.error || '发送失败', 'error')
  } finally {
    testSending.value = false
  }
}

function openFullImg(src) {
  fullImg.value = src
}

async function runTask(t) {
  try {
    await api.post(`/api/screenshot-tasks/${t.id}/run`)
    toast('任务已触发执行', 'success')
  } catch (e) { toast(e.response?.data?.error || '执行失败', 'error') }
}
</script>

<style scoped>
.tabs { display: flex; gap: 0; margin-bottom: 20px; border-bottom: 1px solid var(--border); }
.tab {
  padding: 10px 20px; background: none; border: none;
  border-bottom: 2px solid transparent; color: var(--text-secondary);
  font-size: 14px; cursor: pointer; transition: all var(--transition); font-family: var(--font-sans);
}
.tab:hover { color: var(--text-primary); }
.tab.active { color: var(--primary-light); border-bottom-color: var(--primary-light); }

h3 { font-size: 16px; margin-bottom: 16px; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.field { display: flex; flex-direction: column; gap: 4px; }
.field label { font-size: 13px; color: var(--text-secondary); font-weight: 500; }
.checkbox-field label { flex-direction: row; gap: 8px; cursor: pointer; align-items: center; display: flex; }
.checkbox-field input[type="checkbox"] { width: 16px; height: 16px; accent-color: var(--primary-light); }
.form-actions { display: flex; gap: 8px; margin-top: 20px; }
.divider { height: 1px; background: var(--border); margin: 20px 0; }
.field-desc { font-size: 13px; color: var(--text-muted); margin-bottom: 12px; }

/* 连接管理 */
.conn-section { margin-bottom: 28px; }
.section-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px; }
.section-header h3 { margin: 0; }
.section-label { display: flex; align-items: center; gap: 10px; }
.type-icon {
  width: 32px; height: 32px; border-radius: 8px; display: flex; align-items: center; justify-content: center;
  font-weight: 700; font-size: 16px; color: #fff;
}
.type-icon.confluence { background: linear-gradient(135deg, #0052CC, #2684FF); }
.type-icon.jira { background: linear-gradient(135deg, #0065FF, #2684FF); }
.type-icon.grafana { background: linear-gradient(135deg, #F46800, #FFB357); }

.conn-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: 14px; }

.conn-card {
  background: var(--bg-secondary); border: 1px solid var(--border); border-radius: 10px;
  padding: 18px; transition: all 0.2s; position: relative;
}
.conn-card:hover { border-color: var(--primary-light); }
.conn-card.default { border-color: rgba(76,154,255,0.3); background: rgba(76,154,255,0.04); }
.conn-card.empty {
  border-style: dashed; display: flex; flex-direction: column; align-items: center;
  justify-content: center; gap: 8px; min-height: 120px; cursor: pointer;
  color: var(--text-muted); font-size: 14px;
}
.conn-card.empty:hover { border-color: var(--primary-light); color: var(--primary-light); }
.empty-icon { font-size: 28px; font-weight: 300; }

.conn-card-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.conn-name { font-weight: 600; font-size: 15px; color: var(--text-primary); }
.default-badge {
  font-size: 11px; padding: 2px 8px; border-radius: 4px;
  background: rgba(76,154,255,0.15); color: var(--primary-light); font-weight: 500;
}
.conn-url { font-size: 13px; color: var(--text-secondary); font-family: var(--font-mono); margin-bottom: 8px; word-break: break-all; }
.conn-meta { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 12px; }
.conn-meta span { font-size: 12px; color: var(--text-muted); }
.meta-tag { background: rgba(255,255,255,0.05); padding: 2px 8px; border-radius: 4px; }
.conn-actions { display: flex; gap: 6px; }

.btn-xs { padding: 4px 10px; font-size: 12px; }
.btn-xs.danger { color: var(--danger); border-color: var(--danger); }
.btn-xs.danger:hover { background: rgba(239,68,68,0.1); }

.test-msg { margin-top: 8px; font-size: 12px; padding: 6px 10px; border-radius: 6px; }
.test-msg.ok { background: rgba(16,185,129,0.1); color: var(--success); }
.test-msg.fail { background: rgba(239,68,68,0.1); color: var(--danger); }

/* 弹窗 */
.modal-overlay {
  position: fixed; inset: 0; background: rgba(0,0,0,0.5);
  display: flex; align-items: center; justify-content: center; z-index: 1000;
}
.modal { display: flex; flex-direction: column; gap: 12px; }
.conn-modal { width: 520px; max-height: 90vh; overflow-y: auto; }

.badge.admin { background: rgba(76,154,255,0.15); color: var(--primary-light); }
.status-dot { display: inline-block; width: 6px; height: 6px; border-radius: 50%; margin-right: 6px; }
.status-dot.active { background: var(--success); }
.status-dot.disabled { background: var(--danger); }
.date-cell { font-size: 13px; color: var(--text-secondary); white-space: nowrap; }

.test-result { margin-top: 12px; padding: 10px 14px; border-radius: var(--radius); font-size: 14px; }
.test-result.success { background: rgba(16,185,129,0.1); color: var(--success); }
.test-result.error { background: rgba(239,68,68,0.1); color: var(--danger); }

/* Lark icon */
.type-icon.lark { background: linear-gradient(135deg, #3370FF, #5B8DEF); }

/* 截图任务 */
.task-list { display: flex; flex-direction: column; gap: 14px; }
.task-card {
  background: var(--bg-secondary); border: 1px solid var(--border); border-radius: 10px;
  padding: 18px; transition: border-color 0.2s;
}
.task-card:hover { border-color: var(--primary-light); }
.task-card-header { display: flex; justify-content: space-between; align-items: flex-start; }
.task-name { font-weight: 600; font-size: 15px; color: var(--text-primary); margin-bottom: 6px; }
.task-meta { display: flex; gap: 12px; font-size: 12px; color: var(--text-muted); flex-wrap: wrap; }
.task-meta code { background: rgba(255,255,255,0.06); padding: 1px 6px; border-radius: 4px; font-size: 12px; }
.task-dashboards { margin: 10px 0 6px; display: flex; gap: 6px; flex-wrap: wrap; }
.dash-tag {
  background: rgba(76,154,255,0.1); color: var(--primary-light); padding: 3px 10px;
  border-radius: 6px; font-size: 12px; font-weight: 500;
}
.dash-tag small { opacity: 0.7; margin-left: 4px; }
.task-lark { font-size: 12px; color: var(--text-muted); margin-bottom: 4px; }
.lark-tag {
  background: rgba(51,112,255,0.1); color: #5B8DEF; padding: 2px 8px;
  border-radius: 4px; font-size: 12px; margin-left: 4px;
}
.task-last-run { font-size: 12px; color: var(--text-muted); margin-top: 6px; }
.status-badge { padding: 1px 6px; border-radius: 4px; font-size: 11px; margin-left: 6px; }
.status-badge.ok { background: rgba(16,185,129,0.15); color: var(--success); }
.status-badge.fail { background: rgba(239,68,68,0.15); color: var(--danger); }
.empty-state { text-align: center; padding: 40px; color: var(--text-muted); font-size: 14px; }

/* 开关 */
.switch { position: relative; display: inline-block; width: 40px; height: 22px; }
.switch input { opacity: 0; width: 0; height: 0; }
.slider {
  position: absolute; cursor: pointer; inset: 0; background: #3a3a4a;
  border-radius: 22px; transition: 0.3s;
}
.slider:before {
  content: ""; position: absolute; height: 16px; width: 16px; left: 3px; bottom: 3px;
  background: white; border-radius: 50%; transition: 0.3s;
}
input:checked + .slider { background: var(--primary-light); }
input:checked + .slider:before { transform: translateX(18px); }

/* Dashboard 配置行 */
.dash-config-item { background: rgba(255,255,255,0.02); border: 1px solid var(--border); border-radius: 8px; padding: 12px; margin-bottom: 8px; }

/* Lark 多选 */
.lark-select { display: flex; gap: 16px; flex-wrap: wrap; }
.lark-check { display: flex; align-items: center; gap: 6px; font-size: 13px; cursor: pointer; }
.lark-check input { accent-color: var(--primary-light); }

h4 { font-size: 14px; font-weight: 600; color: var(--text-primary); }

/* 预览按钮 */
.preview-btn { color: var(--primary-light); border-color: var(--primary-light); }
.preview-btn:hover { background: rgba(76,154,255,0.1); }

/* 预览弹窗 */
.preview-modal { width: 95vw; max-width: 1600px; max-height: 90vh; overflow-y: auto; }
.preview-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.preview-header h3 { margin: 0; }
.preview-content { display: flex; flex-direction: column; gap: 24px; }
.preview-dashboard { }
.preview-dash-title { margin: 0 0 12px; font-size: 15px; padding-bottom: 8px; border-bottom: 1px solid var(--border); }
.preview-panels { display: flex; flex-direction: column; gap: 16px; }
.preview-panel { background: var(--bg-secondary); border: 1px solid var(--border); border-radius: 8px; overflow: hidden; }
.preview-panel-title { padding: 8px 12px; font-size: 13px; color: var(--text-secondary); border-bottom: 1px solid var(--border); }
.preview-img { width: 100%; display: block; cursor: zoom-in; }

/* 变量配置 */
.var-row { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.var-input { flex: 1; }
.var-eq { color: var(--text-muted); font-weight: 600; }

/* 自定义时间范围 */
.custom-time-row { display: flex; align-items: center; gap: 8px; margin-top: 8px; }
.time-input { width: 140px; flex: none; }
</style>
