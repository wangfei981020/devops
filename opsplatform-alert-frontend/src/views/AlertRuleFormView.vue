<template>
  <div>
    <div class="card">
      <div class="card-header">
        <div class="card-title">{{ isEdit ? '编辑告警规则' : '新建告警规则' }}</div>
        <router-link to="/alert-rules" class="btn btn-outline">返回列表</router-link>
      </div>

      <form @submit.prevent="handleSubmit">
        <!-- 基本信息 -->
        <h3 class="form-section form-section-flush">基本信息</h3>
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

        <div class="form-group">
          <label class="form-label">通知渠道 *</label>
          <TransitionGroup tag="div" name="tag" class="namespace-tags">
            <span v-for="cid in form.channel_ids" :key="cid" class="ns-tag">
              {{ channelLabel(cid) }}
              <button type="button" class="ns-tag-remove" @click="removeChannel(cid)">&times;</button>
            </span>
            <div class="ns-input-wrap" key="channel-input-wrap">
              <select v-model.number="channelPicker" class="form-select channel-picker" @change="addChannel">
                <option :value="0">+ 添加渠道</option>
                <option v-for="c in availableChannels" :key="c.id" :value="c.id">
                  {{ c.name }} ({{ c.channel_type === 'telegram' ? 'Telegram' : 'Lark' }})
                </option>
              </select>
            </div>
          </TransitionGroup>
          <div class="form-hint" :class="{ 'form-hint-attn': submitAttempted && !form.channel_ids.length }">可同时选择多个渠道，告警会逐个发送；某个渠道失败不影响其他渠道</div>
        </div>

        <!-- 搜索配置 -->
        <h3 class="form-section">搜索配置</h3>

        <!-- ES 搜索配置 -->
        <template v-if="form.data_source_type === 'es'">
          <div class="form-notice">
            <strong>查询构建规则：</strong>关键词 + 过滤字段会自动拼接生成查询；一旦填写「自定义查询 DSL」，将完全替换关键词和过滤字段的自动构建结果（两者均被忽略）。
          </div>
          <!-- ES 索引 keeps its group label, so it takes a full-width row of
               its own and spreads its three fields across it. Sharing a row
               with a single-field neighbour left that neighbour's half empty. -->
          <div class="form-group">
            <label class="form-label">ES 索引</label>
            <IndexSelector v-model="form.es_index" :es-connection-id="form.es_connection_id" />
            <div class="form-hint">支持通配符（高级模式），多个用逗号分隔</div>
          </div>
          <div class="form-group">
            <label class="form-label">搜索关键词</label>
            <input v-model="form.keyword" class="form-input" placeholder='如: "搜不到指定格式日志" OR "跳局"' />
            <div class="form-hint">支持 Lucene 语法，AND/OR/NOT</div>
          </div>

          <!-- 过滤字段：仅 ES 使用，queryLoki 不读取此字段 -->
          <div class="form-group">
            <label class="form-label">过滤字段 (JSON)</label>
            <textarea v-model="form.filter_fields" class="form-textarea" rows="3"
              :class="{ 'has-error': filterFieldsError }"
              placeholder='[{"field":"kubernetes.namespace","value":"g32-uat","op":"match"}]'></textarea>
            <div class="form-hint">op 支持: match(默认), term, wildcard, exists</div>
            <div v-if="filterFieldsError" class="form-error">{{ filterFieldsError }}</div>
          </div>

          <!-- 自定义 DSL：仅 ES 使用，queryLoki 不读取此字段 -->
          <div class="form-group">
            <label class="form-label">自定义查询 DSL (JSON, 可选)</label>
            <textarea v-model="form.query_dsl" class="form-textarea" rows="4"
              :class="{ 'has-error': queryDslError }"
              placeholder="留空则自动根据关键词和过滤字段构建查询"></textarea>
            <div class="form-hint">填写后将覆盖关键词和过滤字段的自动构建</div>
            <div v-if="queryDslError" class="form-error">{{ queryDslError }}</div>
          </div>
        </template>

        <!-- Loki 搜索配置 -->
        <template v-else>
          <div class="form-group">
            <label class="form-label">LogQL 查询 *</label>
            <input v-model="form.logql" class="form-input" :placeholder="nsPlaceholder" />
            <div class="form-hint">{{ namespacesArray.length ? '多命名空间模式：只填写管道部分（|= ... |~ ...），系统自动拼接 {namespace="X"}' : 'Loki LogQL 查询语句，可在日志查询页调试后复制过来' }}</div>
            <div class="chip-row">
              <button v-for="s in logqlSnippets" :key="s" type="button" class="chip" @click="appendLogqlSnippet(s)">{{ s }}</button>
            </div>
            <div class="form-hint">点击片段追加到上方 LogQL 查询末尾</div>
          </div>
          <div class="form-group">
            <label class="form-label">多命名空间（可选）</label>
            <TransitionGroup tag="div" name="tag" class="namespace-tags">
              <span v-for="ns in namespacesArray" :key="ns" class="ns-tag">
                {{ ns }}
                <button type="button" class="ns-tag-remove" @click="removeNamespace(ns)">&times;</button>
              </span>
              <div class="ns-input-wrap" key="ns-input-wrap">
                <input v-model="nsInput" class="form-input ns-input" placeholder="输入命名空间回车添加"
                  @keydown.enter.prevent="addNamespace" />
                <button v-if="nsInput" type="button" class="btn btn-sm btn-primary" @click="addNamespace" style="margin-left:6px">添加</button>
              </div>
            </TransitionGroup>
            <div class="form-hint">配置后系统会逐个命名空间查询，按容器聚合告警，降低 Loki 压力</div>
          </div>
          <div class="form-row" v-if="namespacesArray.length">
            <div class="form-group">
              <label class="form-label">命名空间并发数</label>
              <input v-model.number="form.namespace_concurrency" type="number" class="form-input" min="1" max="10" />
              <div class="form-hint">同时查询的命名空间数量（默认3，越大越快但 Loki 压力越大）</div>
            </div>
          </div>
          <div class="form-group" v-if="form.alert_mode === 'found' && namespacesArray.length">
            <label class="form-label">标签过滤器（可选）</label>
            <input v-model="form.label_filters" class="form-input"
              placeholder='container!~"api-gateway|frontend", app="order"' />
            <div class="form-hint">
              注入到 selector：<code>{namespace="X", <b>此处</b>}</code>。支持任意 Loki label（container / app / pod ...）和操作符（= != =~ !~）。不要写外层大括号。
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

        <!-- 字段提取 -->
        <button type="button" class="form-section form-section-toggle" :class="{ 'is-open': !collapsed.extract }" @click="collapsed.extract = !collapsed.extract">
          <span class="section-chevron" aria-hidden="true"></span>
          <span class="section-name">字段提取</span>
          <span v-if="collapsed.extract && sectionFilled.extract" class="section-filled">已配置</span>
        </button>
        <div v-show="!collapsed.extract">
        <div class="form-group">
          <label class="form-label">提取规则 (JSON)</label>
          <textarea v-model="form.extract_fields" class="form-textarea" rows="4"
            :class="{ 'has-error': extractFieldsError }"
            :placeholder="extractFieldsPlaceholder"></textarea>
          <div class="form-hint">name=变量名, path=ES字段路径(支持嵌套如 kubernetes.namespace), pattern=正则(捕获组1)</div>
          <div v-if="extractFieldsError" class="form-error">{{ extractFieldsError }}</div>
        </div>
        </div>

        <!-- 消息模板 -->
        <h3 class="form-section">消息模板</h3>
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
          <h3 class="form-section">恢复通知配置</h3>
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
        <button type="button" class="form-section form-section-toggle" :class="{ 'is-open': !collapsed.notify }" @click="collapsed.notify = !collapsed.notify">
          <span class="section-chevron" aria-hidden="true"></span>
          <span class="section-name">通知配置</span>
          <span v-if="collapsed.notify && sectionFilled.notify" class="section-filled">已配置</span>
        </button>
        <div v-show="!collapsed.notify">
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
        </div>

        <!-- 分组配置 -->
        <button type="button" class="form-section form-section-toggle" :class="{ 'is-open': !collapsed.group }" @click="collapsed.group = !collapsed.group">
          <span class="section-chevron" aria-hidden="true"></span>
          <span class="section-name">分组配置</span>
          <span v-if="collapsed.group && sectionFilled.group" class="section-filled">已配置</span>
        </button>
        <div v-show="!collapsed.group">
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
            <option value="once">只告警一次（恢复后再通知）</option>
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
          <div class="form-hint">
            持续搜不到日志时，多久发送一次告警。留空则跟随执行周期每次都发<br>
            选「只告警一次」时：搜不到发一条告警，之后不再重复；恢复时发一条恢复通知，一告一恢复算一个闭环。再次搜不到会重新告警（分组时每个容器各自独立闭环）
          </div>
          <div v-if="form.alert_interval === 'once' && form.recovery_enabled !== 1" class="form-hint" style="color: #b45309;">
            ⚠️ 当前未启用恢复通知：只告警一次又不发恢复，故障恢复后不会有任何消息，建议勾选上方「启用恢复通知」
          </div>
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
        </div>

        <!-- 去重配置 -->
        <button type="button" class="form-section form-section-toggle" :class="{ 'is-open': !collapsed.dedup }" @click="collapsed.dedup = !collapsed.dedup">
          <span class="section-chevron" aria-hidden="true"></span>
          <span class="section-name">去重配置</span>
          <span v-if="collapsed.dedup && sectionFilled.dedup" class="section-filled">已配置</span>
        </button>
        <div v-show="!collapsed.dedup">
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
        </div>

        <!-- 字段值路由 (found 模式) -->
        <template v-if="form.alert_mode === 'found'">
          <h3 class="form-section">字段值路由</h3>
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
                  <option value="0">-- 选择通知渠道 --</option>
                  <option v-for="lc in channels" :key="lc.id" :value="lc.id">
                    {{ lc.name }} ({{ lc.channel_type === 'telegram' ? 'Telegram' : 'Lark' }})
                  </option>
                </select>
                <input v-model="route.name" class="form-input" style="width: 120px;" placeholder="备注" />
                <button type="button" class="btn btn-sm btn-outline" style="color: var(--danger);" @click="routeConfig.routes.splice(idx, 1)">&times;</button>
              </div>
              <button type="button" class="btn btn-sm btn-outline" @click="routeConfig.routes.push({valuesStr: '', lark_id: 0, name: ''})">+ 添加路由</button>
            </div>
            <div class="form-group">
              <label class="form-label">其他值发送到</label>
              <select v-model.number="routeConfig.default_lark_id" class="form-select" style="width: 300px;">
                <option value="0">使用规则默认渠道</option>
                <option v-for="lc in channels" :key="lc.id" :value="lc.id">
                  {{ lc.name }} ({{ lc.channel_type === 'telegram' ? 'Telegram' : 'Lark' }})
                </option>
              </select>
            </div>
          </template>
        </template>

        <!-- 性能告警 (Loki 性能监控：实时阈值 + 每日报告) -->
        <template v-if="form.data_source_type === 'loki'">
          <h3 class="form-section">性能告警 (Loki)</h3>
          <div class="form-hint" style="margin-bottom: 10px;">
            适用于性能类告警：按关键词查询 Loki，从日志行正则提取 <code>tid/domain/cost_ms</code> 等字段（配置在"字段提取"中），按阈值实时告警并按域名累计日报。
            <br>⚠️ 一条日志只允许有 1 个 <code>http(s)://</code> URL，多/少都会记录错误日志跳过。
          </div>
          <div class="form-group">
            <label class="form-label">
              <input type="checkbox" :checked="form.realtime_enabled === 1" @change="form.realtime_enabled = $event.target.checked ? 1 : 0" style="margin-right: 6px;" />
              启用实时告警
            </label>
            <div class="form-hint">开启后，当日志耗时超过下方阈值才推送 Lark 卡片；同 tid 一天只告警一次</div>
          </div>
          <div class="form-row" v-if="form.realtime_enabled === 1">
            <div class="form-group">
              <label class="form-label">告警阈值 (毫秒)</label>
              <input v-model.number="form.threshold_ms" type="number" class="form-input" placeholder="如: 3000 / 5000 / 6000" style="width: 200px;" />
              <div class="form-hint">cost_ms &gt; 此值才推送</div>
            </div>
          </div>

          <div class="form-group" style="margin-top: 16px;">
            <label class="form-label">
              <input type="checkbox" :checked="form.report_enabled === 1" @change="form.report_enabled = $event.target.checked ? 1 : 0" style="margin-right: 6px;" />
              启用每日报告
            </label>
            <div class="form-hint">每次查询都会静默累计每个域名的 tid→cost 到 Redis，到点发送统计报告（最快/平均/最慢）</div>
          </div>
          <template v-if="form.report_enabled === 1">
            <div class="form-row">
              <div class="form-group">
                <label class="form-label">日报 cron (6 段: 秒 分 时 日 月 周)</label>
                <input v-model="form.report_schedule" class="form-input" placeholder="0 1 0 * * *" style="width: 220px;" />
                <div class="form-hint">默认 <code>0 1 0 * * *</code> = 每天 00:01:00 触发</div>
              </div>
              <div class="form-group">
                <label class="form-label">日报模式</label>
                <select v-model="form.report_mode" class="form-select" style="width: 220px;">
                  <option value="separate">分开发送 (每域名一张卡片)</option>
                  <option value="merged">合并发送 (所有域名一张卡片)</option>
                </select>
              </div>
            </div>
            <div class="form-group">
              <label class="form-label">日报标题 (可选)</label>
              <input v-model="form.report_title" class="form-input" placeholder="如: 📊 每日性能报告" />
            </div>
            <div class="form-group">
              <label class="form-label">日报模板 (可选, Go template)</label>
              <textarea v-model="form.report_template" class="form-textarea" rows="5"
                placeholder="留空则使用默认模板。可用变量 (separate): {{.domain}} {{.count}} {{.min_ms}} {{.avg_ms}} {{.max_ms}} {{.date}} {{.send_time}}&#10;可用变量 (merged): {{.date}} {{.send_time}} {{range .stats}} .domain .count .min_ms .avg_ms .max_ms {{end}}"></textarea>
            </div>
          </template>

          <!-- 错误栈上下文：日志报错时只有一行栈顶，向后补查同 tid/pid 的后续行 -->
          <h3 class="form-section">错误栈上下文</h3>
          <div class="form-group">
            <label class="form-label">
              <input type="checkbox" :checked="form.stack_context_enabled === 1" @change="form.stack_context_enabled = $event.target.checked ? 1 : 0" style="margin-right: 6px;" />
              启用错误栈上下文
            </label>
            <div class="form-hint">
              开启后，命中告警的日志行会向后补查同来源的后续行，拼成 <code>&#123;&#123;.stack&#125;&#125;</code> 注入到消息模板。
              采集端已经把多行栈合并成一条日志时，不会发额外查询（这是常见情况，也是免费的）。
            </div>
          </div>
          <template v-if="form.stack_context_enabled === 1">
            <div class="form-row">
              <div class="form-group">
                <label class="form-label">最多行数</label>
                <input v-model.number="form.stack_max_lines" type="number" class="form-input" placeholder="30" style="width: 160px;" />
                <div class="form-hint">硬上限，防止边界正则失效时把整个日志流拖进来</div>
              </div>
              <div class="form-group">
                <label class="form-label">头部保留行数</label>
                <input v-model.number="form.stack_head_lines" type="number" class="form-input" placeholder="12" style="width: 160px;" />
                <div class="form-hint">超过最多行数时，保留栈的前几行</div>
              </div>
            </div>
            <div class="form-row">
              <div class="form-group">
                <label class="form-label">尾部保留行数</label>
                <input v-model.number="form.stack_tail_lines" type="number" class="form-input" placeholder="8" style="width: 160px;" />
                <div class="form-hint">超过最多行数时，保留栈的后几行</div>
              </div>
              <div class="form-group">
                <label class="form-label">时间窗口 (秒)</label>
                <input v-model.number="form.stack_window_sec" type="number" class="form-input" placeholder="5" style="width: 160px;" />
                <div class="form-hint">向后取多久范围内的日志作为候选</div>
              </div>
            </div>
            <div class="form-group">
              <label class="form-label">新日志行边界正则 (可选)</label>
              <input v-model="form.stack_boundary_pattern" class="form-input" placeholder="留空则用内置默认" />
              <div class="form-hint">边界正则留空则用内置默认（识别行首的日期/时间/日志级别）</div>
            </div>
          </template>

          <!-- 日志上下文：与错误栈上下文相互独立。栈上下文顺着同一条记录的续行往后走，
               这里取的是命中行前后的完整日志记录，不看边界正则 -->
          <h3 class="form-section">日志上下文</h3>
          <div class="form-group">
            <label class="form-label">
              <input type="checkbox" :checked="form.log_context_enabled === 1" @change="form.log_context_enabled = $event.target.checked ? 1 : 0" style="margin-right: 6px;" />
              启用日志上下文
            </label>
            <div class="form-hint">
              开启后，命中行的前后若干条日志会拼成 <code>&#123;&#123;.logcontext&#125;&#125;</code> 注入到消息模板；
              模板没引用该变量时自动追加到消息末尾。命中行会用 <code>&gt;&gt;&gt;</code> 和分隔带标出。
              与上面的「错误栈上下文」互不影响，可同时开启。
            </div>
          </div>
          <template v-if="form.log_context_enabled === 1">
            <div class="form-row">
              <div class="form-group">
                <label class="form-label">向前行数</label>
                <input v-model.number="form.log_context_before" type="number" class="form-input" placeholder="25" style="width: 160px;" />
                <div class="form-hint">命中行之前取多少条日志</div>
              </div>
              <div class="form-group">
                <label class="form-label">向后行数</label>
                <input v-model.number="form.log_context_after" type="number" class="form-input" placeholder="50" style="width: 160px;" />
                <div class="form-hint">命中行之后取多少条日志</div>
              </div>
            </div>
            <div class="form-row">
              <div class="form-group">
                <label class="form-label">时间窗上限 (秒)</label>
                <input v-model.number="form.log_context_max_window_sec" type="number" class="form-input" placeholder="1800" style="width: 160px;" />
                <div class="form-hint">
                  阶梯查询 30秒 → 2分钟 → 10分钟 → 此上限，取够行数立刻停止。
                  日志量大的服务通常第一档就取满；服务很闲才会往上退。调大只影响闲服务的兜底范围。
                </div>
              </div>
              <div class="form-group">
                <label class="form-label">消息内最多展示行数</label>
                <input v-model.number="form.log_context_display_lines" type="number" class="form-input" placeholder="20" style="width: 160px;" />
                <div class="form-hint">超出时保留命中行附近的行，并在消息里注明前后各省略了多少行</div>
              </div>
            </div>
          </template>
        </template>

        <!-- Prometheus 配置 -->
        <button type="button" class="form-section form-section-toggle" :class="{ 'is-open': !collapsed.prom }" @click="collapsed.prom = !collapsed.prom">
          <span class="section-chevron" aria-hidden="true"></span>
          <span class="section-name">Prometheus 指标配置</span>
          <span v-if="collapsed.prom && sectionFilled.prom" class="section-filled">已配置</span>
        </button>
        <div v-show="!collapsed.prom">
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
            <input v-model="promLabelsStr" class="form-input" :class="{ 'has-error': promLabelsError }" placeholder='{"project":"g32","env":"uat","team":"backend"}' />
            <div class="form-hint">附加到 alert_container_status 指标上的标签，用于 Grafana 筛选分组</div>
            <div v-if="promLabelsError" class="form-error">{{ promLabelsError }}</div>
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
            {{ testSending ? '发送中...' : '测试发送' }}
          </button>
          <button v-if="isEdit && form.report_enabled === 1" type="button" class="btn btn-outline" @click="handlePreviewReport" :disabled="reportPreviewing">
            {{ reportPreviewing ? '生成中...' : '预览日报' }}
          </button>
          <button v-if="isEdit && form.report_enabled === 1" type="button" class="btn btn-warning" @click="handleSendReport" :disabled="reportSending">
            {{ reportSending ? '发送中...' : '立即发送日报' }}
          </button>
          <router-link to="/alert-rules" class="btn btn-outline">取消</router-link>
          <button type="submit" class="btn btn-primary" :disabled="submitting">
            {{ submitting ? '保存中...' : (isEdit ? '更新规则' : '创建规则') }}
          </button>
        </div>
        </div>
      </form>
    </div>

    <!-- 日报预览 Modal -->
    <Transition name="modal">
    <div v-if="reportPreviewData" class="modal-overlay" @click.self="reportPreviewData = null">
      <div class="modal" style="min-width: 600px; max-width: 90vw; max-height: 90vh; display: flex; flex-direction: column;">
        <div class="modal-header" style="position: sticky; top: 0; background: var(--bg-card, #fff); z-index: 10; flex-shrink: 0;">
          <div class="modal-title">日报预览（{{ reportPreviewData.day }} · {{ reportPreviewData.domain_count }} 个域名）</div>
          <button class="btn-icon" @click="reportPreviewData = null"><X :size="18" /></button>
        </div>
        <div style="overflow-y: auto; padding: 4px;">
          <div v-if="!reportPreviewData.cards || reportPreviewData.cards.length === 0" class="card" style="padding: 16px; color: var(--text-secondary);">
            该日暂无累计数据。日报需规则先运行并累计当日交易（cost_ms / domain 提取成功）后才有内容。
          </div>
          <div v-for="(c, idx) in reportPreviewData.cards" :key="idx" class="card" style="margin-bottom: 8px; padding: 12px;">
            <div style="font-weight: 600; margin-bottom: 8px;">{{ c.title }}<span v-if="c.domain" class="text-secondary" style="font-weight: 400;"> — {{ c.domain }}</span></div>
            <pre style="white-space: pre-wrap; font-family: inherit; margin: 0; font-size: 13px;">{{ c.content }}</pre>
          </div>
        </div>
      </div>
    </div>
    </Transition>

    <!-- Preview Modal -->
    <Transition name="modal">
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
    </Transition>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, reactive, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import api from '../api'
import { formatTime } from '../utils/datetime'
import { useToast, useConfirm } from '../stores/ui'
import { X } from 'lucide-vue-next'
import IndexSelector from '../components/IndexSelector.vue'

const toast = useToast()
const dialog = useConfirm()
const previewing = ref(false)
const previewData = ref(null)
const testSending = ref(false)
const reportPreviewing = ref(false)
const reportSending = ref(false)
const reportPreviewData = ref(null)

// Mutes
const mutes = ref([])
const newMuteGroup = ref('')
const newMuteDuration = ref('1h')

const route = useRoute()
const router = useRouter()
const isEdit = computed(() => !!route.params.id)
const submitting = ref(false)
// 通知渠道的说明文字只有在用户真正尝试保存过之后才转为警示态，
// 否则新建页一打开就是满屏红字，把"说明"演成"报错"。
const submitAttempted = ref(false)

const esConnections = ref([])
const lokiConnections = ref([])
const channels = ref([])
const channelPicker = ref(0)

// Only enabled channels can be picked: the backend rejects a disabled id with
// a 400, and a rule bound to one dies at run time with "all N bound
// notification channels are disabled". A channel bound earlier and disabled
// since still renders as a tag (see channelLabel) so the operator can see it
// and remove it — it is never dropped from form.channel_ids behind their back.
const availableChannels = computed(() =>
  channels.value.filter(c => c.status === 1 && !form.value.channel_ids.includes(c.id))
)

function channelLabel(id) {
  const c = channels.value.find(x => x.id === id)
  if (!c) return `#${id}（已删除）`
  const type = c.channel_type === 'telegram' ? 'Telegram' : 'Lark'
  return c.status === 1 ? `${c.name} (${type})` : `${c.name} (${type}·已禁用)`
}

function addChannel() {
  const id = channelPicker.value
  if (id > 0 && !form.value.channel_ids.includes(id)) {
    form.value.channel_ids.push(id)
  }
  channelPicker.value = 0
}

function removeChannel(id) {
  form.value.channel_ids = form.value.channel_ids.filter(x => x !== id)
}

const allProjects = ref([])

const form = ref({
  data_source_type: 'es',
  loki_connection_id: 0,
  logql: '',
  name: '',
  es_connection_id: 0,
  channel_ids: [],
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
  label_filters: '',
  project_id: 0,
  realtime_enabled: 0,
  threshold_ms: 0,
  report_enabled: 0,
  report_schedule: '0 1 0 * * *',
  report_mode: 'separate',
  report_title: '',
  report_template: '',
  stack_context_enabled: 0,
  stack_max_lines: 30,
  stack_head_lines: 12,
  stack_tail_lines: 8,
  stack_boundary_pattern: '',
  stack_window_sec: 5,
  log_context_enabled: 0,
  log_context_before: 25,
  log_context_after: 50,
  log_context_max_window_sec: 1800,
  log_context_display_lines: 20
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
function removeNamespace(ns) {
  setNamespaces(namespacesArray.value.filter(x => x !== ns))
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

// The advanced sections start closed on a new rule and open themselves when
// they hold something, so editing never hides configuration that is set. A
// "已配置" marker on a closed section says there is something inside without
// making the reader open it to find out.
//
// This sits below the refs it reads rather than beside the other UI state: the
// watch evaluates immediately, so declaring it earlier would touch form before
// it exists.
function hasJson(v) {
  const s = (v || '').trim()
  return !!s && s !== '[]' && s !== '{}'
}

const sectionFilled = computed(() => ({
  extract: hasJson(form.value.extract_fields),
  notify: atAllChecked.value || hasJson(form.value.at_users),
  group: !!form.value.group_by || hasJson(form.value.expected_groups),
  dedup: !!form.value.dedup_field,
  prom: promEnabled.value,
}))

const collapsed = reactive({ extract: true, notify: true, group: true, dedup: true, prom: true })

// A rule's own values arrive after the form mounts. Opening happens once, so a
// section the reader then closes stays closed.
watch(sectionFilled, filled => {
  for (const key of Object.keys(collapsed)) {
    if (filled[key]) collapsed[key] = false
  }
}, { once: true })


// JSON 校验：解析失败时返回带字符位置的提示，方便定位手写 JSON 里的错误
function jsonPositionError(str) {
  if (!str) return null
  try {
    JSON.parse(str)
    return null
  } catch (e) {
    const m = /position (\d+)/.exec(e.message)
    if (m) return `JSON 格式错误：第 ${parseInt(m[1], 10) + 1} 个字符附近`
    return `JSON 格式错误：${e.message}`
  }
}

const filterFieldsError = computed(() => jsonPositionError(form.value.filter_fields))
const queryDslError = computed(() => jsonPositionError(form.value.query_dsl))
const extractFieldsError = computed(() => jsonPositionError(form.value.extract_fields))
const promLabelsError = computed(() => jsonPositionError(promLabelsStr.value))

// Loki LogQL 行过滤片段：点击后追加到 LogQL 输入框末尾（不隐藏改写，所见即所得）
const logqlSnippets = ['|= "ERROR"', '!= "health"', '|~ "regex"']
function appendLogqlSnippet(snippet) {
  form.value.logql = form.value.logql ? `${form.value.logql} ${snippet}` : snippet
}

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
      api.get('/notify-channels'),
      api.get('/projects')
    ])
    if (esRes.code === 0) esConnections.value = esRes.data
    if (lokiRes.code === 0) lokiConnections.value = lokiRes.data
    if (larkRes.code === 0) channels.value = larkRes.data
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
        channel_ids: (d.channel_ids && d.channel_ids.length) ? d.channel_ids
                     : (d.lark_config_id ? [d.lark_config_id] : []),
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
        label_filters: d.label_filters || '',
        project_id: d.project_id || 0,
        realtime_enabled: d.realtime_enabled || 0,
        threshold_ms: d.threshold_ms || 0,
        report_enabled: d.report_enabled || 0,
        report_schedule: d.report_schedule || '0 1 0 * * *',
        report_mode: d.report_mode || 'separate',
        report_title: d.report_title || '',
        report_template: d.report_template || '',
        stack_context_enabled: d.stack_context_enabled || 0,
        stack_max_lines: d.stack_max_lines || 30,
        stack_head_lines: d.stack_head_lines || 12,
        stack_tail_lines: d.stack_tail_lines || 8,
        stack_boundary_pattern: d.stack_boundary_pattern || '',
        stack_window_sec: d.stack_window_sec || 5,
        log_context_enabled: d.log_context_enabled || 0,
        log_context_before: d.log_context_before || 25,
        log_context_after: d.log_context_after || 50,
        log_context_max_window_sec: d.log_context_max_window_sec || 1800,
        log_context_display_lines: d.log_context_display_lines || 20
      }
      loadPromConfig(d.prometheus_config)
      loadRouteConfig(d.route_config)
    }
  } catch (e) { /* ignore */ }
}

async function handleSubmit() {
  submitAttempted.value = true
  // JSON 校验：只检查当前数据源下实际可见/生效的字段，命名出错字段而非笼统报错
  const jsonFieldErrors = []
  if (form.value.data_source_type === 'es') {
    if (filterFieldsError.value) jsonFieldErrors.push('过滤字段')
    if (queryDslError.value) jsonFieldErrors.push('自定义查询 DSL')
  }
  if (extractFieldsError.value) jsonFieldErrors.push('提取规则')
  if (promEnabled.value && promLabelsError.value) jsonFieldErrors.push('自定义 Labels')
  if (jsonFieldErrors.length) {
    toast.error(`「${jsonFieldErrors.join('、')}」不是合法的 JSON，请修正后再保存`)
    return
  }

  syncPromConfig()
  syncRouteConfig()
  // 简单校验：label_filters 不允许写大括号（避免被拼成双括号）
  if (form.value.label_filters && /[{}]/.test(form.value.label_filters)) {
    toast.error('标签过滤器不要写大括号 {}，只写 label="value" 的部分')
    return
  }
  if (!form.value.channel_ids.length) { toast.error('请先选择通知渠道'); return }
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
  syncPromConfig()
  syncRouteConfig()
  const ds = form.value.data_source_type || 'es'
  if (ds === 'es' && !form.value.es_connection_id) { toast.error('请先选择 ES 连接'); return }
  if (ds === 'loki' && !form.value.loki_connection_id) { toast.error('请先选择 Loki 连接'); return }
  if (!form.value.channel_ids.length) { toast.error('请先选择通知渠道'); return }

  const ok = await dialog.confirm({ title: '测试发送', message: '将查询数据源并真实发送一条告警到所选渠道，确认？' })
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
  syncPromConfig()
  syncRouteConfig()
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

async function handlePreviewReport() {
  reportPreviewing.value = true
  try {
    const res = await api.post(`/alert-rules/${route.params.id}/preview-report`)
    if (res.code === 0) {
      reportPreviewData.value = res.data
      if (!res.data.cards || res.data.cards.length === 0) {
        toast.info(`${res.data.day} 暂无累计数据（需规则已运行并累计当日交易）`)
      }
    } else {
      toast.error(res.message || '预览日报失败')
    }
  } catch (e) {
    toast.error('预览日报失败: ' + (e.response?.data?.message || e.message))
  }
  reportPreviewing.value = false
}

async function handleSendReport() {
  const ok = await dialog.confirm({ title: '立即发送日报', message: '将按当日累计数据立即发送性能日报到 Lark，确认？' })
  if (!ok) return
  reportSending.value = true
  try {
    const res = await api.post(`/alert-rules/${route.params.id}/send-report`)
    if (res.code === 0) {
      if (res.data.sent > 0) {
        toast.success(`已发送 ${res.data.sent} 条日报（${res.data.domain_count} 个域名，${res.data.day}）`)
      } else {
        toast.info(`${res.data.day} 暂无累计数据，未发送`)
      }
    } else {
      toast.error(res.message || '发送日报失败')
    }
  } catch (e) {
    toast.error('发送日报失败: ' + (e.response?.data?.message || e.message))
  }
  reportSending.value = false
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

// Convert nanosecond timestamps in the preview to the platform's display zone.
// This runs on the rendered message, which is ours to format — never on raw log
// text, whose timestamps come from another system and stay verbatim.
function formatRendered(text) {
  if (!text) return text
  return text.replace(/(?<!\d)(\d{16,19})(?!\d)/g, (match) => {
    // Nanosecond timestamp: take first 13 digits as milliseconds
    if (match.length >= 16) {
      const ms = Number(match.substring(0, 13))
      if (ms > 1700000000000 && ms < 1900000000000) {
        return formatTime(new Date(ms))
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
