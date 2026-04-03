<template>
  <div>
    <div class="card">
      <div class="card-header">
        <div class="card-title">{{ isEdit ? '编辑告警规则' : '新建告警规则' }}</div>
        <router-link to="/alert-rules" class="btn btn-outline">返回列表</router-link>
      </div>

      <form @submit.prevent="handleSubmit">
        <!-- 基本信息 -->
        <h3 style="margin-bottom: 12px; font-size: 15px; color: var(--text-secondary);">基本信息</h3>
        <div class="form-row">
          <div class="form-group">
            <label class="form-label">规则名称 *</label>
            <input v-model="form.name" class="form-input" placeholder="如: G32 resource alarm" required />
          </div>
          <div class="form-group">
            <label class="form-label">所属项目</label>
            <select v-model.number="form.project_id" class="form-select">
              <option :value="0">未分类</option>
              <option v-for="p in allProjects" :key="p.id" :value="p.id">
                {{ p.parent_id > 0 ? '  └ ' : '' }}{{ p.name }}
              </option>
            </select>
          </div>
          <div class="form-group">
            <label class="form-label">告警级别</label>
            <select v-model="form.severity" class="form-select">
              <option value="S1">S1 灾难</option>
              <option value="S2">S2 严重</option>
              <option value="S3">S3 警告</option>
            </select>
          </div>
        </div>

        <div class="form-row">
          <div class="form-group">
            <label class="form-label">告警模式</label>
            <select v-model="form.alert_mode" class="form-select">
              <option value="found">搜到关键词 → 告警</option>
              <option value="not_found">搜不到关键词 → 告警</option>
            </select>
            <div class="form-hint">
              <template v-if="form.alert_mode === 'found'">ES 搜到匹配日志时触发告警（默认）</template>
              <template v-else>指定时间内搜不到匹配日志时触发告警，搜到后发送恢复通知</template>
            </div>
          </div>
          <div class="form-group" v-if="form.alert_mode === 'not_found'">
            <label class="form-label">
              <input type="checkbox" v-model="recoveryChecked" style="margin-right: 6px;" />
              启用恢复通知
            </label>
            <div class="form-hint">恢复后发送绿色通知卡片</div>
          </div>
        </div>

        <!-- 数据源 + 连接 -->
        <div class="form-row">
          <div class="form-group">
            <label class="form-label">数据源类型</label>
            <select v-model="form.data_source_type" class="form-select">
              <option value="es">Elasticsearch</option>
              <option value="loki">Loki</option>
            </select>
          </div>
          <div class="form-group" v-if="form.data_source_type === 'es'">
            <label class="form-label">ES 连接 *</label>
            <select v-model="form.es_connection_id" class="form-select" required>
              <option :value="0" disabled>请选择 ES 连接</option>
              <option v-for="c in esConnections" :key="c.id" :value="c.id">
                {{ c.name }} ({{ c.version }}.x)
              </option>
            </select>
          </div>
          <div class="form-group" v-else>
            <label class="form-label">Loki 连接 *</label>
            <select v-model="form.loki_connection_id" class="form-select" required>
              <option :value="0" disabled>请选择 Loki 连接</option>
              <option v-for="c in lokiConnections" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select>
          </div>
        </div>

        <div class="form-row">
          <div class="form-group">
            <label class="form-label">Lark 配置 *</label>
            <select v-model="form.lark_config_id" class="form-select" required>
              <option :value="0" disabled>请选择 Lark 配置</option>
              <option v-for="c in larkConfigs" :key="c.id" :value="c.id">
                {{ c.name }} ({{ c.lark_type }})
              </option>
            </select>
          </div>
        </div>

        <!-- 搜索配置 -->
        <h3 style="margin: 20px 0 12px; font-size: 15px; color: var(--text-secondary);">搜索配置</h3>

        <!-- ES 搜索配置 -->
        <template v-if="form.data_source_type === 'es'">
          <div class="form-row">
            <div class="form-group">
              <label class="form-label">ES 索引</label>
              <input v-model="form.es_index" class="form-input" placeholder="如: app-logs-* 或 filebeat-*" />
              <div class="form-hint">支持通配符，多个用逗号分隔</div>
            </div>
            <div class="form-group">
              <label class="form-label">搜索关键词</label>
              <input v-model="form.keyword" class="form-input" placeholder='如: "搜不到指定格式日志" OR "跳局"' />
              <div class="form-hint">支持 Lucene 语法，AND/OR/NOT</div>
            </div>
          </div>
        </template>

        <!-- Loki 搜索配置 -->
        <template v-else>
          <div class="form-group">
            <label class="form-label">LogQL 查询 *</label>
            <input v-model="form.logql" class="form-input" :placeholder="nsPlaceholder" />
            <div class="form-hint">{{ namespacesArray.length ? '多命名空间模式：只填写管道部分（|= ... |~ ...），系统自动拼接 {namespace="X"}' : 'Loki LogQL 查询语句，可在日志查询页调试后复制过来' }}</div>
          </div>
          <div class="form-group">
            <label class="form-label">多命名空间（可选）</label>
            <div class="namespace-tags">
              <span v-for="(ns, i) in namespacesArray" :key="i" class="ns-tag">
                {{ ns }}
                <button class="ns-tag-remove" @click="removeNamespace(i)">&times;</button>
              </span>
              <div class="ns-input-wrap">
                <input v-model="nsInput" class="form-input ns-input" placeholder="输入命名空间回车添加"
                  @keydown.enter.prevent="addNamespace" />
                <button v-if="nsInput" class="btn btn-sm btn-primary" @click="addNamespace" style="margin-left:6px">添加</button>
              </div>
            </div>
            <div class="form-hint">配置后系统会逐个命名空间查询，按容器聚合告警，降低 Loki 压力</div>
          </div>
          <div class="form-row" v-if="namespacesArray.length">
            <div class="form-group">
              <label class="form-label">命名空间并发数</label>
              <input v-model.number="form.namespace_concurrency" type="number" class="form-input" min="1" max="10" />
              <div class="form-hint">同时查询的命名空间数量（默认3，越大越快但 Loki 压力越大）</div>
            </div>
          </div>
        </template>

        <div class="form-row">
          <div class="form-group">
            <label class="form-label">执行周期 (Cron)</label>
            <input v-model="form.schedule" class="form-input" placeholder="*/5 * * * *" />
            <div class="form-hint">{{ cronHint || 'Cron 表达式，如 */5 * * * * 每5分钟' }}</div>
          </div>
          <div class="form-group">
            <label class="form-label">搜索时间范围</label>
            <input v-model="form.time_range" class="form-input" placeholder="5m" />
            <div class="form-hint">如 5m(分钟)、1h(小时)、30s(秒)</div>
          </div>
        </div>

        <!-- 过滤字段 -->
        <div class="form-group">
          <label class="form-label">过滤字段 (JSON)</label>
          <textarea v-model="form.filter_fields" class="form-textarea" rows="3"
            placeholder='[{"field":"kubernetes.namespace","value":"g32-uat","op":"match"}]'></textarea>
          <div class="form-hint">op 支持: match(默认), term, wildcard, exists</div>
        </div>

        <!-- 自定义 DSL -->
        <div class="form-group">
          <label class="form-label">自定义查询 DSL (JSON, 可选)</label>
          <textarea v-model="form.query_dsl" class="form-textarea" rows="4"
            placeholder="留空则自动根据关键词和过滤字段构建查询"></textarea>
          <div class="form-hint">填写后将覆盖关键词和过滤字段的自动构建</div>
        </div>

        <!-- 字段提取 -->
        <h3 style="margin: 20px 0 12px; font-size: 15px; color: var(--text-secondary);">字段提取</h3>
        <div class="form-group">
          <label class="form-label">提取规则 (JSON)</label>
          <textarea v-model="form.extract_fields" class="form-textarea" rows="4"
            :placeholder="extractFieldsPlaceholder"></textarea>
          <div class="form-hint">name=变量名, path=ES字段路径(支持嵌套如 kubernetes.namespace), pattern=正则(捕获组1)</div>
        </div>

        <!-- 消息模板 -->
        <h3 style="margin: 20px 0 12px; font-size: 15px; color: var(--text-secondary);">消息模板</h3>
        <div class="form-group">
          <label class="form-label">告警标题</label>
          <input v-model="form.message_title" class="form-input" placeholder="如: G32 resource alarm" />
        </div>
        <div class="form-group">
          <label class="form-label">消息模板</label>
          <textarea v-model="form.message_template" class="form-textarea" rows="6"
            :placeholder="templatePlaceholder"></textarea>
          <div class="form-hint">支持 Go template 语法，如 &#123;&#123;.field&#125;&#125;，变量来自字段提取或 ES _source 原始字段</div>
        </div>

        <!-- 恢复通知模板 (仅 not_found 模式) -->
        <template v-if="form.alert_mode === 'not_found' && recoveryChecked">
          <h3 style="margin: 20px 0 12px; font-size: 15px; color: var(--text-secondary);">恢复通知配置</h3>
          <div class="form-group">
            <label class="form-label">恢复通知标题</label>
            <input v-model="form.recovery_title" class="form-input" placeholder="留空则自动用: 告警标题 - 已恢复" />
          </div>
          <div class="form-group">
            <label class="form-label">恢复通知模板</label>
            <textarea v-model="form.recovery_template" class="form-textarea" rows="4"
              placeholder="留空则使用告警消息模板，恢复时变量来自第一条搜到的日志"></textarea>
          </div>
        </template>

        <!-- @用户 -->
        <h3 style="margin: 20px 0 12px; font-size: 15px; color: var(--text-secondary);">通知配置</h3>
        <div class="form-group">
          <label class="form-label">@通知人</label>
          <textarea v-model="form.at_users" class="form-textarea" rows="2"
            placeholder='["Bruce","Cesar"]'></textarea>
          <div class="form-hint">填写姓名数组，如 ["Bruce","Cesar"]。姓名需在"通知人管理"页面先添加对应的 Lark ID</div>
        </div>
        <div class="form-group">
          <label class="form-label">
            <input type="checkbox" v-model="atAllChecked" style="margin-right: 6px;" />
            @所有人
          </label>
        </div>

        <!-- 分组配置 -->
        <h3 style="margin: 20px 0 12px; font-size: 15px; color: var(--text-secondary);">分组配置</h3>
        <div class="form-group">
          <label class="form-label">分组字段</label>
          <div class="flex gap-2">
            <input v-model="form.group_by" class="form-input" placeholder="如: container" style="flex: 1;" />
            <select class="form-select" style="width: 220px;" @change="selectGroupBy($event)">
              <option value="">快捷选择...</option>
              <optgroup label="Loki Labels" v-if="(form.data_source_type || 'es') === 'loki'">
                <option value="container">container</option>
                <option value="namespace">namespace</option>
                <option value="pod">pod</option>
                <option value="job">job</option>
                <option value="instance">instance</option>
                <option value="service_name">service_name</option>
                <option value="node_name">node_name</option>
                <option value="stream">stream</option>
              </optgroup>
              <optgroup label="ES 常用字段" v-else>
                <option value="kubernetes.container_name">kubernetes.container_name</option>
                <option value="kubernetes.namespace">kubernetes.namespace</option>
                <option value="kubernetes.pod_name">kubernetes.pod_name</option>
                <option value="kubernetes.node_name">kubernetes.node_name</option>
                <option value="kubernetes.labels.app">kubernetes.labels.app</option>
                <option value="host.name">host.name</option>
                <option value="service.name">service.name</option>
              </optgroup>
            </select>
          </div>
          <div class="form-hint">按此字段分组，每组独立告警/恢复。留空则不分组</div>
        </div>

        <div v-if="form.group_by" class="form-group">
          <label class="form-label">期望容器列表 (可选)</label>
          <textarea v-model="form.expected_groups" class="form-textarea" rows="3"
            placeholder='["roulette-resource-backend","baccarat-resource-backend","dragon-tiger-resource-backend"]'></textarea>
          <div class="form-hint">JSON 数组，指定需要监控的容器。填写后按此列表检查，容器挂了也能发现。留空则自动从 24h 日志发现</div>
        </div>

        <div v-if="form.group_by" class="form-group">
          <label class="form-label">查询并发数</label>
          <select v-model.number="form.query_concurrency" class="form-select" style="width: 200px;">
            <option :value="1">1 (串行)</option>
            <option :value="3">3</option>
            <option :value="5">5 (默认)</option>
            <option :value="10">10</option>
            <option :value="20">20</option>
          </select>
          <div class="form-hint">单规则检查分组时的并发数。规则多时建议降低，避免给 Loki/ES 过大压力</div>
        </div>

        <div v-if="form.alert_mode === 'not_found'" class="form-group">
          <label class="form-label">告警间隔</label>
          <select v-model="form.alert_interval" class="form-select" style="width: 250px;">
            <option value="">每次执行都发（默认）</option>
            <option value="5m">每 5 分钟</option>
            <option value="10m">每 10 分钟</option>
            <option value="30m">每 30 分钟</option>
            <option value="1h">每 1 小时</option>
            <option value="2h">每 2 小时</option>
            <option value="6h">每 6 小时</option>
            <option value="12h">每 12 小时</option>
            <option value="24h">每 24 小时</option>
          </select>
          <div class="form-hint">持续搜不到日志时，多久发送一次告警。留空则跟随执行周期每次都发</div>
        </div>

        <!-- 屏蔽管理 -->
        <div v-if="isEdit && form.group_by" class="form-group">
          <label class="form-label">屏蔽管理</label>
          <div v-if="mutes.length === 0" class="text-sm text-secondary" style="padding: 8px; background: #f8fafc; border-radius: 6px;">
            暂无屏蔽的容器
          </div>
          <div v-else>
            <div v-for="mute in mutes" :key="mute.id" class="flex justify-between items-center" style="padding: 6px 10px; background: #fffbeb; border-radius: 6px; margin-bottom: 4px; border: 1px solid #fde68a;">
              <div>
                <span class="badge badge-warning" style="margin-right: 8px;">{{ mute.group_key }}</span>
                <span class="text-sm text-secondary">截止: {{ mute.mute_until }}</span>
                <span v-if="mute.reason" class="text-sm text-secondary"> | {{ mute.reason }}</span>
              </div>
              <button type="button" class="btn btn-sm btn-outline" style="color: var(--danger);" @click="removeMute(mute.id)">取消屏蔽</button>
            </div>
          </div>
          <div class="flex gap-2 items-center" style="margin-top: 8px;">
            <input v-model="newMuteGroup" class="form-input" style="width: 200px;" placeholder="容器名" />
            <select v-model="newMuteDuration" class="form-select" style="width: 120px;">
              <option value="10m">10 分钟</option>
              <option value="30m">30 分钟</option>
              <option value="1h">1 小时</option>
              <option value="6h">6 小时</option>
              <option value="1d">1 天</option>
              <option value="7d">7 天</option>
              <option value="30d">30 天</option>
              <option value="forever">1 年</option>
            </select>
            <button type="button" class="btn btn-sm btn-outline" @click="addMute">添加屏蔽</button>
          </div>
        </div>

        <!-- 去重配置 -->
        <h3 style="margin: 20px 0 12px; font-size: 15px; color: var(--text-secondary);">去重配置</h3>
        <div class="form-row">
          <div class="form-group">
            <label class="form-label">去重字段</label>
            <input v-model="form.dedup_field" class="form-input" placeholder="如: round,timestamp" />
            <div class="form-hint">多个字段用逗号分隔，相同组合不重复告警</div>
          </div>
          <div class="form-group">
            <label class="form-label">去重有效期 (秒)</label>
            <input v-model.number="form.dedup_ttl" type="number" class="form-input" placeholder="3600" />
          </div>
        </div>

        <div class="form-group">
          <label class="form-label">单次最大告警条数</label>
          <input v-model.number="form.max_alerts" type="number" class="form-input" style="width: 200px;" placeholder="10" />
        </div>

        <!-- 字段值路由 (found 模式) -->
        <template v-if="form.alert_mode === 'found'">
          <h3 style="margin: 20px 0 12px; font-size: 15px; color: var(--text-secondary);">字段值路由</h3>
          <div class="form-group">
            <label class="form-label">路由字段</label>
            <input v-model="routeConfig.route_field" class="form-input" style="width: 200px;" placeholder="如: code, level, service" />
            <div class="form-hint">从日志提取的字段名，根据该字段值决定发到哪个群或忽略</div>
          </div>
          <template v-if="routeConfig.route_field">
            <div class="form-group">
              <label class="form-label">忽略的值（不告警）</label>
              <input v-model="routeIgnoreStr" class="form-input" placeholder="如: 9010, 9011, 9015（逗号分隔）" />
              <div class="form-hint">这些值的日志不发送告警</div>
            </div>
            <div class="form-group">
              <label class="form-label">路由规则</label>
              <div v-for="(route, idx) in routeConfig.routes" :key="idx" class="flex gap-2 items-center" style="margin-bottom: 8px;">
                <input v-model="route.valuesStr" class="form-input" style="width: 200px;" placeholder="值（逗号分隔）" />
                <span>→</span>
                <select v-model.number="route.lark_id" class="form-select" style="width: 200px;">
                  <option value="0">-- 选择 Lark 群 --</option>
                  <option v-for="lc in larkConfigs" :key="lc.id" :value="lc.id">{{ lc.name }}</option>
                </select>
                <input v-model="route.name" class="form-input" style="width: 120px;" placeholder="备注" />
                <button type="button" class="btn btn-sm btn-outline" style="color: var(--danger);" @click="routeConfig.routes.splice(idx, 1)">&times;</button>
              </div>
              <button type="button" class="btn btn-sm btn-outline" @click="routeConfig.routes.push({valuesStr: '', lark_id: 0, name: ''})">+ 添加路由</button>
            </div>
            <div class="form-group">
              <label class="form-label">其他值发送到</label>
              <select v-model.number="routeConfig.default_lark_id" class="form-select" style="width: 300px;">
                <option value="0">使用规则默认 Lark 配置</option>
                <option v-for="lc in larkConfigs" :key="lc.id" :value="lc.id">{{ lc.name }}</option>
              </select>
            </div>
          </template>
        </template>

        <!-- Prometheus 配置 -->
        <h3 style="margin: 20px 0 12px; font-size: 15px; color: var(--text-secondary);">Prometheus 指标配置</h3>
        <div class="form-group">
          <label class="form-label">
            <input type="checkbox" v-model="promEnabled" style="margin-right: 6px;" />
            启用自定义 Prometheus 指标
          </label>
          <div class="form-hint">启用后，每次规则执行会输出自定义指标到 /metrics 端点（内置指标始终输出）</div>
        </div>
        <template v-if="promEnabled">
          <div class="form-group">
            <label class="form-label">自定义 Labels (JSON)</label>
            <input v-model="promLabelsStr" class="form-input" placeholder='{"project":"g32","env":"uat","team":"backend"}' />
            <div class="form-hint">附加到 alert_container_status 指标上的标签，用于 Grafana 筛选分组</div>
          </div>
          <div class="card" style="padding: 12px; background: #f8fafc; font-size: 12px; margin-top: 8px;">
            <div><strong>指标输出示例：</strong></div>
            <div style="margin-top: 6px; font-family: monospace; color: #6366f1; line-height: 1.8;">
              alert_container_status{rule_id="2", rule_name="...", namespace="g32-uat", container="max-24d-resource-backend"{{ promPreviewLabels }}} <strong>1</strong><br>
              alert_container_status{rule_id="2", rule_name="...", namespace="g32-uat", container="baccarat-resource-backend"{{ promPreviewLabels }}} <strong>0</strong>
            </div>
            <div style="margin-top: 6px; color: #64748b;">1=正常（搜到日志） 0=告警（搜不到日志）</div>
          </div>
        </template>

        <!-- Submit -->
        <div class="modal-footer" style="border-top: none; padding-top: 24px;">
          <button type="button" class="btn btn-outline" @click="handlePreview" :disabled="previewing">
            {{ previewing ? '查询中...' : '预览告警结果' }}
          </button>
          <button type="button" class="btn btn-warning" @click="handleTestSend" :disabled="testSending">
            {{ testSending ? '发送中...' : '测试发送到 Lark' }}
          </button>
          <router-link to="/alert-rules" class="btn btn-outline">取消</router-link>
          <button type="submit" class="btn btn-primary" :disabled="submitting">
            {{ submitting ? '保存中...' : (isEdit ? '更新规则' : '创建规则') }}
          </button>
        </div>
      </form>
    </div>

    <!-- Preview Modal -->
    <div v-if="previewData" class="modal-overlay" @click.self="previewData = null">
      <div class="modal" style="min-width: 800px; max-width: 95vw; max-height: 90vh; display: flex; flex-direction: column;">
        <div class="modal-header" style="position: sticky; top: 0; background: var(--bg-card, #fff); z-index: 10; flex-shrink: 0;">
          <div class="modal-title">告警预览结果</div>
          <button class="btn-icon" @click="previewData = null"><X :size="18" /></button>
        </div>

        <div style="overflow-y: auto; flex: 1; padding: 0 4px;">

        <!-- Stats -->
        <div style="display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 12px; margin-bottom: 16px;">
          <div class="stat-card" style="padding: 12px;">
            <div class="label">数据源</div>
            <div style="font-weight: 600;">{{ previewData.source_name }}</div>
          </div>
          <div class="stat-card" style="padding: 12px;">
            <div class="label">详情</div>
            <div style="font-weight: 600;">{{ previewData.source_detail }}</div>
            <div v-if="previewData.time_range" class="text-sm text-secondary" style="margin-top: 2px;">搜索范围: {{ previewData.time_range }}</div>
          </div>
          <div v-if="previewData.total_groups" class="stat-card" style="padding: 12px;">
            <div class="label">容器总数</div>
            <div style="font-weight: 600; font-size: 20px;">
              <span style="color: var(--success);">{{ previewData.ok_count || 0 }}</span>
              <span class="text-secondary" style="font-size: 14px;"> 正常 / </span>
              <span style="color: var(--danger);">{{ previewData.alert_count || 0 }}</span>
              <span class="text-secondary" style="font-size: 14px;"> 告警</span>
            </div>
          </div>
          <div v-else class="stat-card" style="padding: 12px;">
            <div class="label">{{ previewData.group_by ? '分组数 / 总日志' : '命中数' }}</div>
            <div style="font-weight: 600; font-size: 20px;" :style="{ color: previewData.hit_count > 0 ? 'var(--warning)' : 'var(--success)' }">
              {{ previewData.group_by ? previewData.group_count + ' 组' : previewData.hit_count }} / {{ previewData.total }}
            </div>
          </div>
        </div>

        <!-- @人信息 -->
        <div v-if="form.at_users" class="card" style="padding: 8px 12px; margin-bottom: 12px; background: #eff6ff; border-color: #bfdbfe;">
          <span class="text-sm" style="color: #1e40af;">
            通知: {{ parseAtNames(form.at_users) }}
            <span v-if="form.at_all === 1" style="margin-left: 8px;">+ @所有人</span>
          </span>
        </div>

        <!-- ========== not_found grouped preview ========== -->
        <template v-if="previewData.ok_list || previewData.alert_list">

          <!-- Alert containers -->
          <div v-if="previewData.alert_list && previewData.alert_list.length > 0">
            <h4 style="margin-bottom: 8px; color: var(--danger);">告警容器 ({{ previewData.alert_list.length }})</h4>
            <div v-for="item in previewData.alert_list" :key="item.name" class="card" style="margin-bottom: 8px; padding: 12px; border-left: 4px solid var(--danger);">
              <div class="flex justify-between items-center" style="margin-bottom: 6px;">
                <span class="badge badge-danger">{{ item.name }}</span>
                <div class="flex items-center gap-2">
                  <span class="text-sm text-secondary">
                    来源: {{ {current: '当前范围', redis: 'Redis缓存', '24h': '24h查询', '3d': '3天查询', no_history: '无历史日志', '30m': '30m查询'}[item.source] || item.source }}
                  </span>
                  <select class="form-select" style="width: 120px; padding: 2px 6px; font-size: 12px;"
                    @change="muteContainer(item.name, $event)">
                    <option value="">屏蔽...</option>
                    <option value="1h">1 小时</option>
                    <option value="3h">3 小时</option>
                    <option value="6h">6 小时</option>
                    <option value="12h">12 小时</option>
                    <option value="24h">24 小时</option>
                    <option value="7d">7 天</option>
                    <option value="30d">30 天</option>
                    <option value="forever">1 年</option>
                  </select>
                </div>
              </div>
              <pre v-if="item.rendered" style="background: #fef2f2; padding: 10px; border-radius: 6px; font-size: 13px; white-space: pre-wrap; max-height: 150px; overflow-y: auto;">{{ formatRendered(item.rendered) }}</pre>
              <div v-else class="text-sm text-secondary" style="padding: 8px; background: #f8fafc; border-radius: 6px;">无历史日志记录</div>
            </div>
          </div>

          <!-- OK containers -->
          <div v-if="previewData.ok_list && previewData.ok_list.length > 0" style="margin-top: 12px;">
            <h4 style="margin-bottom: 8px; color: var(--success);">正常容器 ({{ previewData.ok_list.length }})</h4>
            <div style="display: flex; flex-wrap: wrap; gap: 6px;">
              <span v-for="item in previewData.ok_list" :key="item.name" class="badge badge-success" style="padding: 4px 10px;">
                {{ item.name }}
              </span>
            </div>
          </div>
        </template>

        <!-- ========== Default preview (found mode / no grouping) ========== -->
        <template v-else>
          <!-- Alert mode hint -->
          <div v-if="form.alert_mode === 'not_found'" class="card" style="padding: 12px; margin-bottom: 12px;"
            :style="{ background: previewData.hit_count === 0 ? '#fef2f2' : '#ecfdf5', borderColor: previewData.hit_count === 0 ? '#fecaca' : '#a7f3d0' }">
            <strong>{{ previewData.hit_count === 0 ? '将触发告警' : '正常（不会告警）' }}</strong>
            — 反向模式: {{ previewData.hit_count === 0 ? '在指定时间范围内未搜到匹配日志' : '搜到匹配日志，不会触发告警' }}
          </div>
          <div v-else-if="previewData.hit_count > 0" class="card" style="padding: 12px; margin-bottom: 12px; background: #fffbeb; border-color: #fde68a;">
            <strong>将触发 {{ previewData.hit_count }} 条告警</strong>
          </div>
          <div v-else class="card" style="padding: 12px; margin-bottom: 12px; background: #ecfdf5; border-color: #a7f3d0;">
            <strong>正常（无命中，不会告警）</strong>
          </div>

          <!-- Rendered messages -->
          <div v-if="previewData.hits && previewData.hits.length > 0">
            <h4 style="margin-bottom: 8px;">渲染后的告警消息</h4>
            <div v-for="(hit, idx) in previewData.hits" :key="idx" class="card" style="margin-bottom: 8px; padding: 12px;">
              <div class="flex justify-between items-center" style="margin-bottom: 8px;">
                <span class="badge badge-info">{{ hit.vars?._group_key ? hit.vars._group_key : '命中 #' + (idx + 1) }}</span>
                <button class="btn btn-sm btn-outline" @click="hit._showRaw = !hit._showRaw">
                  {{ hit._showRaw ? '隐藏原始数据' : '查看原始数据' }}
                </button>
              </div>
              <pre style="background: #f1f5f9; padding: 12px; border-radius: 6px; font-size: 13px; white-space: pre-wrap; max-height: 200px; overflow-y: auto;">{{ formatRendered(hit.rendered) }}</pre>
              <div v-if="hit._showRaw" style="margin-top: 8px;">
                <pre style="background: #f8fafc; padding: 8px; border-radius: 4px; font-size: 12px; max-height: 200px; overflow-y: auto;">{{ formatJSON(hit.raw) }}</pre>
              </div>
            </div>
          </div>
        </template>

        <!-- Query -->
        <details v-if="previewData.query" style="margin-top: 12px;">
          <summary class="text-sm text-secondary" style="cursor: pointer;">查看查询语句</summary>
          <pre style="background: #f1f5f9; padding: 12px; border-radius: 6px; font-size: 12px; margin-top: 8px; max-height: 200px; overflow-y: auto;">{{ previewData.query }}</pre>
        </details>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import api from '../api'
import { useToast, useConfirm } from '../stores/ui'
import { X } from 'lucide-vue-next'

const toast = useToast()
const dialog = useConfirm()
const previewing = ref(false)
const previewData = ref(null)
const testSending = ref(false)

// Mutes
const mutes = ref([])
const newMuteGroup = ref('')
const newMuteDuration = ref('1h')

const route = useRoute()
const router = useRouter()
const isEdit = computed(() => !!route.params.id)
const submitting = ref(false)

const esConnections = ref([])
const lokiConnections = ref([])
const larkConfigs = ref([])
const allProjects = ref([])

const form = ref({
  data_source_type: 'es',
  loki_connection_id: 0,
  logql: '',
  name: '',
  es_connection_id: 0,
  lark_config_id: 0,
  es_index: '*',
  schedule: '*/5 * * * *',
  time_range: '5m',
  query_dsl: '',
  keyword: '',
  filter_fields: '',
  extract_fields: '',
  message_title: '',
  message_template: '',
  at_users: '',
  at_all: 0,
  alert_mode: 'found',
  recovery_enabled: 0,
  recovery_title: '',
  recovery_template: '',
  severity: 'S2',
  group_by: '',
  expected_groups: '',
  query_concurrency: 5,
  alert_interval: '',
  dedup_field: '',
  dedup_ttl: 3600,
  max_alerts: 10,
  prometheus_config: '',
  route_config: '',
  namespaces: '',
  namespace_concurrency: 3,
  project_id: 0
})

// Namespace 多选相关
const nsInput = ref('')
const namespacesArray = computed(() => {
  if (!form.value.namespaces) return []
  try { return JSON.parse(form.value.namespaces) } catch { return [] }
})
const nsPlaceholder = computed(() =>
  namespacesArray.value.length
    ? '|= "ERROR" |~ `"code":"[0-9]+"`'
    : '{namespace="default"} |= "ERROR"'
)
function setNamespaces(arr) {
  form.value.namespaces = arr.length ? JSON.stringify(arr) : ''
}
function addNamespace() {
  const ns = nsInput.value.trim()
  if (!ns) return
  const arr = [...namespacesArray.value]
  if (!arr.includes(ns)) { arr.push(ns); setNamespaces(arr) }
  nsInput.value = ''
}
function removeNamespace(i) {
  const arr = [...namespacesArray.value]
  arr.splice(i, 1)
  setNamespaces(arr)
}

// Route config helpers
const routeConfig = ref({ route_field: '', ignore_values: [], routes: [], default_lark_id: 0 })
const routeIgnoreStr = ref('')

function syncRouteConfig() {
  if (!routeConfig.value.route_field) {
    form.value.route_config = ''
    return
  }
  const cfg = {
    route_field: routeConfig.value.route_field,
    ignore_values: routeIgnoreStr.value ? routeIgnoreStr.value.split(',').map(s => s.trim()).filter(Boolean) : [],
    routes: routeConfig.value.routes.map(r => ({
      values: r.valuesStr ? r.valuesStr.split(',').map(s => s.trim()).filter(Boolean) : [],
      lark_id: r.lark_id,
      name: r.name
    })).filter(r => r.values.length > 0 && r.lark_id > 0),
    default_lark_id: routeConfig.value.default_lark_id || 0
  }
  form.value.route_config = JSON.stringify(cfg)
}

function loadRouteConfig(configStr) {
  if (!configStr) return
  try {
    const cfg = JSON.parse(configStr)
    routeConfig.value = {
      route_field: cfg.route_field || '',
      ignore_values: cfg.ignore_values || [],
      routes: (cfg.routes || []).map(r => ({ valuesStr: (r.values || []).join(', '), lark_id: r.lark_id, name: r.name || '' })),
      default_lark_id: cfg.default_lark_id || 0
    }
    routeIgnoreStr.value = (cfg.ignore_values || []).join(', ')
  } catch (e) {}
}

const recoveryChecked = computed({
  get: () => form.value.recovery_enabled === 1,
  set: (v) => { form.value.recovery_enabled = v ? 1 : 0 }
})

// Prometheus config helpers
const promEnabled = ref(false)
const promConfig = ref({ static_labels: {} })
const promLabelsStr = ref('')

const promPreviewLabels = computed(() => {
  try {
    const labels = JSON.parse(promLabelsStr.value || '{}')
    const parts = Object.entries(labels).map(([k, v]) => `${k}="${v}"`)
    return parts.length > 0 ? ', ' + parts.join(', ') : ''
  } catch { return '' }
})

// Sync prometheus config to form.prometheus_config before submit
function syncPromConfig() {
  if (!promEnabled.value) {
    form.value.prometheus_config = ''
    return
  }
  try { promConfig.value.static_labels = JSON.parse(promLabelsStr.value || '{}') } catch { promConfig.value.static_labels = {} }
  promConfig.value.enabled = true
  form.value.prometheus_config = JSON.stringify(promConfig.value)
}

// Load prometheus config from form data
function loadPromConfig(configStr) {
  if (!configStr) return
  try {
    const cfg = JSON.parse(configStr)
    promEnabled.value = cfg.enabled || false
    const staticLabels = cfg.static_labels || cfg.labels || {}
    promConfig.value = { static_labels: staticLabels }
    promLabelsStr.value = Object.keys(staticLabels).length > 0 ? JSON.stringify(staticLabels) : ''
  } catch { /* ignore */ }
}

function selectGroupBy(e) {
  if (e.target.value) {
    form.value.group_by = e.target.value
    e.target.value = ''
  }
}

const atAllChecked = computed({
  get: () => form.value.at_all === 1,
  set: (v) => { form.value.at_all = v ? 1 : 0 }
})

const extractFieldsPlaceholder = `[
  {"name":"namespace","path":"kubernetes.namespace","pattern":""},
  {"name":"round","path":"message","pattern":"Round:\\\\s*(\\\\S+)"},
  {"name":"link","path":"message","pattern":"Link-(\\\\S+)"}
]`

const templatePlaceholder = `**Namespace:** {{.namespace}}
**Container:** {{.container}}
**Round:** {{.round}}
**Message:** {{.message}}
**Time:** {{.time}}`

// Cron 表达式可读化
function cronToHuman(cron) {
  if (!cron) return ''
  const parts = cron.trim().split(/\s+/)
  if (parts.length !== 5) return cron
  const [min, hour, dom, mon, dow] = parts
  if (min.startsWith('*/') && hour === '*') return `每 ${min.slice(2)} 分钟执行`
  if (min !== '*' && hour.startsWith('*/')) return `每 ${hour.slice(2)} 小时的第 ${min} 分钟执行`
  if (min !== '*' && hour !== '*' && dom === '*') return `每天 ${hour}:${min.padStart(2,'0')} 执行`
  return cron
}

const cronHint = computed(() => cronToHuman(form.value.schedule))

async function loadOptions() {
  try {
    const [esRes, lokiRes, larkRes, projRes] = await Promise.all([
      api.get('/es-connections'),
      api.get('/loki-connections'),
      api.get('/lark-configs'),
      api.get('/projects')
    ])
    if (esRes.code === 0) esConnections.value = esRes.data
    if (lokiRes.code === 0) lokiConnections.value = lokiRes.data
    if (larkRes.code === 0) larkConfigs.value = larkRes.data
    if (projRes.code === 0) allProjects.value = projRes.data
  } catch (e) { /* ignore */ }
}

async function loadRule() {
  if (!route.params.id) return
  try {
    const res = await api.get(`/alert-rules/${route.params.id}`)
    if (res.code === 0) {
      const d = res.data
      form.value = {
        data_source_type: d.data_source_type || 'es',
        name: d.name,
        es_connection_id: d.es_connection_id,
        loki_connection_id: d.loki_connection_id || 0,
        lark_config_id: d.lark_config_id,
        es_index: d.es_index,
        schedule: d.schedule,
        time_range: d.time_range,
        query_dsl: d.query_dsl || '',
        keyword: d.keyword,
        logql: d.logql || '',
        filter_fields: d.filter_fields || '',
        extract_fields: d.extract_fields || '',
        message_title: d.message_title,
        message_template: d.message_template || '',
        at_users: d.at_users || '',
        at_all: d.at_all,
        alert_mode: d.alert_mode || 'found',
        recovery_enabled: d.recovery_enabled || 0,
        recovery_title: d.recovery_title || '',
        recovery_template: d.recovery_template || '',
        severity: d.severity,
        group_by: d.group_by || '',
        expected_groups: d.expected_groups || '',
        query_concurrency: d.query_concurrency || 5,
        alert_interval: d.alert_interval || '',
        dedup_field: d.dedup_field,
        dedup_ttl: d.dedup_ttl,
        max_alerts: d.max_alerts,
        prometheus_config: d.prometheus_config || '',
        route_config: d.route_config || '',
        namespaces: d.namespaces || '',
        namespace_concurrency: d.namespace_concurrency || 3,
        project_id: d.project_id || 0
      }
      loadPromConfig(d.prometheus_config)
      loadRouteConfig(d.route_config)
    }
  } catch (e) { /* ignore */ }
}

async function handleSubmit() {
  syncPromConfig()
  syncRouteConfig()
  submitting.value = true
  try {
    const data = { ...form.value }
    let res
    if (isEdit.value) {
      res = await api.put(`/alert-rules/${route.params.id}`, data)
    } else {
      res = await api.post('/alert-rules', data)
    }
    if (res.code === 0) {
      router.push('/alert-rules')
    } else {
      toast.error(res.message || '保存失败')
    }
  } catch (e) {
    toast.error('保存失败: ' + (e.response?.data?.message || e.message))
  }
  submitting.value = false
}

async function loadMutes() {
  if (!route.params.id) return
  try {
    const res = await api.get('/alert-mutes', { params: { rule_id: route.params.id } })
    if (res.code === 0) mutes.value = res.data
  } catch (e) { /* ignore */ }
}

async function addMute() {
  if (!newMuteGroup.value) { toast.error('请输入容器名'); return }
  if (!route.params.id) { toast.warning('请先保存规则'); return }
  try {
    const res = await api.post('/alert-mutes', {
      rule_id: parseInt(route.params.id),
      group_key: newMuteGroup.value,
      duration: newMuteDuration.value,
      reason: '规则编辑页手动屏蔽'
    })
    if (res.code === 0) {
      toast.success(`${newMuteGroup.value} 已屏蔽 ${newMuteDuration.value}`)
      newMuteGroup.value = ''
      loadMutes()
    } else {
      toast.error(res.message)
    }
  } catch (e) { toast.error('屏蔽失败') }
}

async function removeMute(muteId) {
  try {
    await api.delete(`/alert-mutes/${muteId}`)
    toast.success('已取消屏蔽')
    loadMutes()
  } catch (e) { toast.error('取消失败') }
}

async function muteContainer(containerName, event) {
  const duration = event.target.value
  event.target.value = '' // reset select
  if (!duration) return

  const ruleId = route.params.id
  if (!ruleId) {
    toast.warning('请先保存规则后再屏蔽')
    return
  }

  try {
    const res = await api.post('/alert-mutes', {
      rule_id: parseInt(ruleId),
      group_key: containerName,
      duration: duration,
      reason: '预览页手动屏蔽'
    })
    if (res.code === 0) {
      toast.success(`${containerName} 已屏蔽 ${duration}`)
    } else {
      toast.error(res.message)
    }
  } catch (e) {
    toast.error('屏蔽失败: ' + (e.response?.data?.message || e.message))
  }
}

async function handleTestSend() {
  const ds = form.value.data_source_type || 'es'
  if (ds === 'es' && !form.value.es_connection_id) { toast.error('请先选择 ES 连接'); return }
  if (ds === 'loki' && !form.value.loki_connection_id) { toast.error('请先选择 Loki 连接'); return }
  if (!form.value.lark_config_id) { toast.error('请先选择 Lark 配置'); return }

  const ok = await dialog.confirm({ title: '测试发送', message: '将查询数据源并真实发送一条告警到 Lark，确认？' })
  if (!ok) return

  testSending.value = true
  try {
    const res = await api.post('/alert-rules/test-send', form.value)
    if (res.code === 0) {
      if (res.data.would_alert === false) {
        let msg = res.data.message
        if (res.data.found_groups) {
          msg += '\n\n正常容器: ' + res.data.found_groups.join(', ')
        }
        toast.info(msg)
      } else {
        toast.success(res.data.response ? `测试发送成功！命中 ${res.data.hit_count} 条` : res.data.message)
      }
    } else {
      toast.error(res.message)
    }
  } catch (e) {
    toast.error('测试发送失败: ' + (e.response?.data?.message || e.message))
  }
  testSending.value = false
}

async function handlePreview() {
  const ds = form.value.data_source_type || 'es'
  if (ds === 'es' && !form.value.es_connection_id) {
    toast.error('请先选择 ES 连接')
    return
  }
  previewing.value = true
  try {
    const res = await api.post('/alert-rules/preview', form.value)
    if (res.code === 0) {
      // Add _showRaw toggle to each hit
      if (res.data.hits) {
        res.data.hits.forEach(h => { h._showRaw = false })
      }
      previewData.value = res.data
    } else {
      toast.error(res.message || '预览失败')
    }
  } catch (e) {
    toast.error('预览失败: ' + (e.response?.data?.message || e.message))
  }
  previewing.value = false
}

function parseAtNames(atUsersStr) {
  if (!atUsersStr) return ''
  try {
    const parsed = JSON.parse(atUsersStr)
    if (Array.isArray(parsed)) {
      return parsed.map(item => typeof item === 'string' ? '@' + item : '@' + (item.name || '')).join(' ')
    }
  } catch { /* ignore */ }
  return atUsersStr
}

// Convert nanosecond timestamps to readable time (UTC+8)
function formatRendered(text) {
  if (!text) return text
  return text.replace(/(?<!\d)(\d{16,19})(?!\d)/g, (match) => {
    // Nanosecond timestamp: take first 13 digits as milliseconds
    if (match.length >= 16) {
      const ms = Number(match.substring(0, 13))
      if (ms > 1700000000000 && ms < 1900000000000) {
        return new Date(ms).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })
      }
    }
    return match
  })
}

function formatVars(vars) {
  if (!vars) return ''
  const filtered = {}
  for (const [k, v] of Object.entries(vars)) {
    if (k !== '_id' && k !== '_index' && typeof v !== 'object') filtered[k] = v
  }
  return JSON.stringify(filtered, null, 2)
}

function formatJSON(obj) {
  try { return JSON.stringify(obj, null, 2) } catch { return String(obj) }
}

onMounted(async () => {
  await loadOptions()
  await loadRule()
  loadMutes()

  // Pre-fill from query params
  const q = route.query
  if (!isEdit.value) {
    if (q.project_id) form.value.project_id = parseInt(q.project_id) || 0
    if (q.es_connection_id) {
      form.value.es_connection_id = parseInt(q.es_connection_id) || 0
      if (q.es_index) form.value.es_index = q.es_index
      if (q.keyword) form.value.keyword = q.keyword
      if (q.filter_fields) form.value.filter_fields = q.filter_fields
      if (q.time_range) form.value.time_range = q.time_range
    }
  }
})
</script>
