<template>
  <div class="orch-page">
    <div class="page-head">
      <div class="head-left">
        <h2>服务编排</h2>
        <el-select v-model="envId" placeholder="选择项目环境" filterable style="width: 260px" @change="loadModules">
          <el-option v-for="e in envs" :key="e.id" :label="`${e.name}（${e.env_type}）`" :value="e.id" />
        </el-select>
      </div>
      <div class="head-btns">
        <el-button :disabled="!envId" @click="openBatch">批量新增</el-button>
        <el-button type="primary" :disabled="!envId" @click="openAdd">+ 新增模块</el-button>
      </div>
    </div>

    <div v-if="envId" class="mod-filter">
      <el-input v-model="modQuery" placeholder="搜索模块名 / 镜像 / namespace" clearable style="width:300px" @input="modPage = 1">
        <template #prefix><el-icon><Search /></el-icon></template>
      </el-input>
      <span class="mod-count">共 {{ filteredModules.length }} 个模块</span>
    </div>

    <el-table :data="pagedModules" border stripe v-loading="loadingMods" empty-text="选择环境后显示其模块（新增的模块提交后会被扫描进来）">
      <el-table-column label="模块" prop="name" min-width="260" />
      <el-table-column label="镜像仓库" prop="image_repository" min-width="240" show-overflow-tooltip />
      <el-table-column label="当前 tag" prop="current_tag" min-width="180" show-overflow-tooltip />
      <el-table-column label="namespace" prop="namespace" width="140" />
      <el-table-column label="操作" width="150">
        <template #default>
          <el-tooltip content="下一期：直接改 values.yaml / configmap / secret，不用再拉 GitLab 到本地" placement="top">
            <el-button link disabled>编辑（即将）</el-button>
          </el-tooltip>
        </template>
      </el-table-column>
    </el-table>

    <div v-if="filteredModules.length" class="pager-bar">
      <el-pagination background layout="total, sizes, prev, pager, next"
        :total="filteredModules.length" :page-size="modPageSize" :current-page="modPage" :page-sizes="[10, 20, 50, 100]"
        @size-change="s => { modPageSize = s; modPage = 1 }" @current-change="p => modPage = p" />
    </div>

    <!-- 新增模块弹窗 -->
    <el-dialog v-model="addDialog" title="新增模块" width="820px" :close-on-click-modal="false" top="5vh">
      <el-form :model="form" label-width="110px">
        <el-form-item label="目标环境">
          <span>{{ curEnvLabel }}</span>
        </el-form-item>
        <el-form-item label="参照模板" required>
          <el-select v-model="form.templateId" placeholder="选样板服务当模子" filterable style="width: 420px" @change="resetPreview">
            <el-option v-for="t in templates" :key="t.id"
              :label="`${t.project || '全局'} · ${t.src_service} · ${t.module_type === 'frontend' ? '前端' : '后端'}`"
              :value="t.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="模块名" required>
          <el-input v-model="form.moduleName" placeholder="完整模块名，如 g32-baccarat-settle-backend" style="width: 420px" @input="resetPreview" @change="autoPrefill" />
          <el-button style="margin-left: 10px" :disabled="!canPrefill" :loading="prefilling" @click="doPrefill">预填 values.yaml（刷新）</el-button>
          <span class="ns-hint">填完模块名失焦即自动预填</span>
        </el-form-item>
        <el-form-item label="namespace" required>
          <el-select v-model="form.namespace" filterable allow-create default-first-option placeholder="选/输 namespace" style="width: 420px" @change="resetPreview">
            <el-option v-for="n in nsOptionsSingle" :key="n" :label="n" :value="n" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="imgPreview" label="镜像/域名">
          <table class="kv-table">
            <tbody>
              <tr>
                <td class="kv-k">镜像</td>
                <td>
                  <code v-if="imgPreview.full">{{ imgPreview.full }}</code>
                  <span v-else class="ip-none">—</span>
                  <el-button v-if="imgPreview.full" link type="primary" size="small" style="margin-left:8px" @click="copyText(imgPreview.full)">复制</el-button>
                </td>
              </tr>
              <tr>
                <td class="kv-k">tag</td>
                <td><code v-if="imgPreview.tag">{{ imgPreview.tag }}</code><span v-else class="miss">缺镜像</span></td>
              </tr>
            </tbody>
          </table>
        </el-form-item>
        <!-- 域名：所有环境可配多个；生产按数量生成占位域名(平均分到主域名)再手改 -->
        <el-form-item v-if="ingressEnabled" label="域名">
          <div class="dom-box">
            <div v-if="isProdEnv" class="dom-gen">
              生成 <el-input-number v-model="domCount" :min="1" :max="20" size="small" style="width:110px" /> 个
              <span class="dom-primary">主域名：{{ primaryDomains.length ? primaryDomains.join('、') : '未配置(去项目参数配主域名)' }}</span>
              <el-button size="small" :disabled="!primaryDomains.length" @click="genProdDomains">生成</el-button>
              <div class="dom-tip">⚠ 主机头 xxx 是占位，请手动改成实际主机头（随机自动生成后期做）</div>
            </div>
            <div v-for="(d, i) in domains" :key="i" class="dom-row">
              <el-input v-model="domains[i]" size="small" style="width:360px" placeholder="如 xxx.dragontiger-game.com" />
              <el-button link type="danger" size="small" @click="domains.splice(i, 1)">×</el-button>
            </div>
            <el-button link type="primary" size="small" @click="domains.push('')">+ 加域名</el-button>
          </div>
        </el-form-item>
        <el-form-item v-if="secretRefs && (secretRefs.existing.length || secretRefs.pending.length)" label="密钥">
          <div class="sec-box">
            <div class="sec-head">
              <span class="sec-src">依赖的密钥 · 内容存 z-kv-secrets
                <span v-if="!secretRefs.zkv_found" class="miss">· z-kv-secrets 还不存在
                  <el-button link type="primary" size="small" @click="$router.push('/env-params')">去初始化</el-button>
                </span>
              </span>
              <el-radio-group v-model="secMode" size="small" @change="onSecModeChange">
                <el-radio-button label="form">表单</el-radio-button>
                <el-radio-button label="yaml">YAML</el-radio-button>
              </el-radio-group>
            </div>

            <!-- 表单模式 -->
            <template v-if="secMode === 'form'">
              <!-- 已存在（复用） -->
              <div v-if="secretRefs.existing.length" class="sec-grp">
                <el-tag v-for="n in secretRefs.existing" :key="n" size="small" type="success" style="margin:2px 4px 2px 0">✓ {{ n }} · 复用</el-tag>
              </div>
              <!-- 缺失 / 手动新增：可填 -->
              <div v-for="(p, pi) in secretRefs.pending" :key="pi" class="sec-pending">
                <div class="sec-ph">
                  <b>{{ p.name || '（未命名）' }}</b>
                  <el-tag size="small" :type="p.manual ? 'info' : 'warning'">{{ p.manual ? '手动新增' : '缺失 · 待新建' }}</el-tag>
                  <el-select v-model="p.type" size="small" style="width:120px" @change="onTypeChange(p)">
                    <el-option label="TiDB" value="tidb" />
                    <el-option label="普通 Opaque" value="opaque" />
                  </el-select>
                  <span class="sec-ns">namespace: {{ p.namespace }}</span>
                  <el-button v-if="p.manual" link type="danger" size="small" @click="secretRefs.pending.splice(pi, 1)">移除</el-button>
                </div>
                <div v-if="p.manual" class="sec-row"><span class="sec-k">名称 <b class="req">*</b></span>
                  <el-input v-model="p.name" size="small" placeholder="如 g32-xxx-config-secret" style="width:280px" /></div>
                <template v-if="p.type === 'tidb'">
                  <div class="sec-row"><span class="sec-k">database <b class="req">*</b></span>
                    <el-input v-model="p.database" size="small" placeholder="如 g66_xxx_game" style="width:280px" /></div>
                  <div v-for="(kv, i) in p.extra" :key="i" class="sec-row">
                    <el-input v-model="kv.key" size="small" placeholder="键" style="width:180px" />
                    <el-input v-model="kv.value" size="small" placeholder="值" style="width:280px;margin:0 6px" />
                    <el-button link type="danger" size="small" @click="p.extra.splice(i, 1)">×</el-button>
                  </div>
                  <el-button link type="primary" size="small" @click="p.extra.push({ key: '', value: '' })">+ 额外字段</el-button>
                </template>
                <template v-else>
                  <div v-for="(kv, i) in p.extra" :key="i" class="sec-row">
                    <el-input v-model="kv.key" size="small" placeholder="键" style="width:180px" />
                    <el-input v-model="kv.value" size="small" placeholder="值" style="width:280px;margin:0 6px" />
                    <el-button link type="danger" size="small" @click="p.extra.splice(i, 1)">×</el-button>
                  </div>
                  <el-button link type="primary" size="small" @click="p.extra.push({ key: '', value: '' })">+ 键值对</el-button>
                </template>
              </div>
              <el-button link type="primary" size="small" style="margin-top:6px" @click="addManualSecret">+ 新增密钥</el-button>
            </template>

            <!-- YAML 模式 -->
            <template v-else>
              <div class="sec-src" style="margin-bottom:6px">直接编辑将追加到 z-kv-secrets 的片段（只含新增项；提交时按段并入）</div>
              <CodeEditor v-model="secYaml" />
            </template>
          </div>
        </el-form-item>
        <el-form-item label="配置">
          <div v-if="form.valuesYaml" style="width:100%">
            <ValuesEditor ref="editorRef" :modelValue="form.valuesYaml" :moduleType="selectedTemplateType" />
          </div>
          <span v-else class="hint">先填模块名并点「预填 values.yaml」，再在此用 表单/YAML 配置</span>
        </el-form-item>
        <el-form-item v-if="configmaps.length" label="ConfigMap">
          <el-tabs v-model="cmTab" type="border-card" style="width:100%">
            <el-tab-pane v-for="cm in configmaps" :key="cm.path" :label="cmName(cm.path)" :name="cm.path">
              <CodeEditor v-model="cm.content" />
            </el-tab-pane>
          </el-tabs>
          <div class="hint">自动从 templates/ 扫出的 configmap（多个按文件名分 tab），改里面的配置值；helm 变量 {{ }} 别动</div>
        </el-form-item>
        <el-form-item label="额外艾特">
          <el-select v-model="extraAt" multiple filterable placeholder="选通知人（可留空）" style="width:100%">
            <el-option v-for="c in atContactsWithLark" :key="c.lark_id" :label="c.name" :value="c.lark_id" />
          </el-select>
          <div class="hint" v-if="envFixedAt.length">已固定艾特：{{ envFixedAt.map(atName).join('、') }}（项目参数配的，自动带）</div>
          <div class="hint">发布后 Lark 一并艾特这些人（你自己会自动艾特，不用选）</div>
        </el-form-item>
        <el-form-item label="ArgoCD">
          <el-switch v-model="form.disable" :active-value="true" :inactive-value="false"
            active-text="disable:true 安全预演（先不生成 Application）"
            inactive-text="关闭=直接部署（生成 Application，默认）" />
        </el-form-item>

        <div v-if="preview" class="preview-box">
          <el-alert v-if="preview.helm_skipped" type="warning" :closable="false" title="helm 未安装，跳过渲染校验" />
          <el-alert v-else :type="preview.helm_ok ? 'success' : 'error'" :closable="false"
            :title="preview.helm_ok ? 'helm 渲染校验通过 ✓' : 'helm 渲染校验失败 ✗（已阻止提交）'" />
          <div v-if="!preview.helm_ok && !preview.helm_skipped" class="err-card">
            <div class="err-head">
              <el-icon><CircleCloseFilled /></el-icon>
              <span>helm 渲染校验失败</span>
              <el-button link type="primary" class="err-copy" @click="copyErr(preview.helm_output)">复制报错</el-button>
            </div>
            <pre class="err-body">{{ preview.helm_output }}</pre>
          </div>
          <div class="changed-title">将提交的改动（{{ (preview.changed_files || []).length }}）：</div>
          <ul class="changed"><li v-for="f in preview.changed_files" :key="f"><code>{{ f }}</code></li></ul>
          <div class="hint">🔒 提交抢环境写锁 + 硬同步远端，不覆盖别人</div>
        </div>
        <el-alert v-if="imageMissing" type="error" :closable="false" class="submitted" show-icon
          title="⛔ Harbor 缺少镜像" :description="imageMissingMsg" />
        <el-alert v-if="submitted" type="success" :closable="false" class="submitted"
          :title="`已提交 commit ${submitted.commit_sha}`" show-icon />
      </el-form>

      <template #footer>
        <el-button @click="addDialog = false">关闭</el-button>
        <el-button type="primary" :disabled="!canPreview" :loading="previewing" @click="doPreview">helm 校验并预览</el-button>
        <el-button type="success" :disabled="!canSubmit" :loading="submitting" @click="doSubmit">确认提交</el-button>
      </template>
    </el-dialog>

    <!-- 批量新增弹窗 -->
    <el-dialog v-model="batchDialog" title="批量新增模块" width="1060px" :close-on-click-modal="false" top="5vh">
      <el-form label-width="110px">
        <el-form-item label="目标环境"><span>{{ curEnvLabel }}</span></el-form-item>
        <el-form-item label="参照模板" required>
          <el-select v-model="batch.templateId" placeholder="选样板服务当模子" filterable style="width: 420px" @change="resetBatch">
            <el-option v-for="t in templates" :key="t.id"
              :label="`${t.project || '全局'} · ${t.src_service} · ${t.module_type === 'frontend' ? '前端' : '后端'}`" :value="t.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="粘贴模块名">
          <el-input v-model="batch.paste" type="textarea" :rows="3" placeholder="一行一个完整模块名，粘贴后点『解析成行』" style="width: 520px" />
          <el-button style="margin-left: 10px" @click="parsePaste">解析成行</el-button>
        </el-form-item>
        <el-form-item label="模块清单">
          <div v-if="batch.rows.length" class="ns-batch-bar">
            namespace 批量设
            <el-select v-model="nsBatchSet" size="small" filterable allow-create default-first-option placeholder="选/输" style="width:180px;margin:0 8px">
              <el-option v-for="n in nsOptions" :key="n" :label="n" :value="n" />
            </el-select>
            <el-button size="small" @click="applyNsToAll">应用到全部</el-button>
            <span class="ns-hint">Harbor 域名 harbor 全局配置，不在每行重复</span>
          </div>
          <el-table :data="batch.rows" border size="small" style="width: 100%">
            <el-table-column label="模块名" min-width="180">
              <template #default="{ row }"><el-input v-model="row.module_name" size="small" @input="resetBatch" @change="deriveRow(row)" /></template>
            </el-table-column>
            <el-table-column label="namespace" width="150">
              <template #default="{ row }">
                <el-select v-model="row.namespace" size="small" filterable allow-create default-first-option placeholder="选/输" style="width:100%" @change="resetBatch">
                  <el-option v-for="n in nsOptions" :key="n" :label="n" :value="n" />
                </el-select>
              </template>
            </el-table-column>
            <el-table-column label="Harbor 项目" min-width="180">
              <template #default="{ row }"><span class="mono ellip" :title="row.image_short">{{ row.image_short || '—' }}</span></template>
            </el-table-column>
            <el-table-column label="tag" width="92">
              <template #default="{ row }">
                <span v-if="row.image_missing" class="miss" title="Harbor 缺该镜像，提交会被拦">缺镜像</span>
                <span v-else class="mono">{{ row.latest_tag || '—' }}</span>
              </template>
            </el-table-column>
            <el-table-column label="域名" min-width="170">
              <template #default="{ row }"><span class="mono ellip" :title="row.domain">{{ row.domain || '—' }}</span></template>
            </el-table-column>
            <el-table-column label="配置" width="80">
              <template #default="{ row }">
                <el-button link type="primary" :disabled="!batch.templateId || !row.module_name.trim()" @click="openRowConfig(row)">
                  {{ row.values_yaml ? '已改' : '配置' }}
                </el-button>
              </template>
            </el-table-column>
            <el-table-column label="校验" width="62">
              <template #default="{ row }">
                <el-tag v-if="rowStatus(row.module_name)" :type="rowStatus(row.module_name).ok ? 'success' : 'danger'" size="small">
                  {{ rowStatus(row.module_name).ok ? '通过' : '失败' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="" width="38">
              <template #default="{ $index }"><el-button link type="danger" @click="batch.rows.splice($index, 1)">×</el-button></template>
            </el-table-column>
          </el-table>
          <el-button link type="primary" @click="batch.rows.push({ module_name: '', namespace: nsBatchSet || nsOptions[0] || '' })">+ 加一行</el-button>
          <span class="ns-hint">Harbor项目/tag/域名 自动派生只读；缺镜像会被拦；「配置」可选（不配也自动派生）</span>
        </el-form-item>
        <!-- 密钥：复用 + 待新建（按模块分组，tidb/opaque 都可填，字段自动识别） -->
        <el-form-item v-if="batchPendingList.length || batchExisting.length" label="依赖密钥">
          <div class="batch-sec-box">
            <!-- 复用（已存在，不用填） -->
            <div v-if="batchExisting.length" class="batch-sec-reuse">
              <el-tag v-for="n in batchExisting" :key="n" size="small" type="success" style="margin:2px 4px 2px 0">✓ {{ n }} · 复用</el-tag>
            </div>
            <!-- 待新建 · 表单/YAML 双模式 -->
            <template v-if="batchPendingList.length">
              <div class="batch-sec-head">
                待新建（提交时建进 z-kv-secrets）<span class="batch-sec-auto">前缀已换；类型/字段按现有 tidb 密钥自动识别，可手改</span>
                <el-radio-group v-model="batchSecMode" size="small" style="margin-left:12px" @change="onBatchSecModeChange">
                  <el-radio-button label="form">表单</el-radio-button>
                  <el-radio-button label="yaml">YAML</el-radio-button>
                </el-radio-group>
              </div>
              <!-- 表单模式：按模块分组卡片 -->
              <template v-if="batchSecMode === 'form'">
                <div v-for="sec in batchSecretSections" :key="sec.key" class="batch-sec-group">
                  <div class="batch-sec-grp">{{ sec.title }}</div>
                  <div v-for="p in sec.secrets" :key="p.name" class="batch-sec-card">
                    <div class="batch-sec-cardhead" v-if="batchSecretFills[p.name]">
                      <b>{{ p.name }}</b>
                      <el-select v-model="batchSecretFills[p.name].type" size="small" style="width:104px;margin:0 8px" @change="onBatchTypeChange(p.name)">
                        <el-option label="TiDB" value="tidb" />
                        <el-option label="普通 Opaque" value="opaque" />
                      </el-select>
                      <span class="batch-sec-ns">ns: {{ p.namespace }}</span>
                      <span v-if="sec.shared" class="batch-sec-ns">· 被 {{ p.modules.join('、') }} 共用</span>
                      <template v-if="batchSecretFills[p.name].type === 'tidb'">
                        <span class="batch-sec-k">database <b class="req">*</b></span>
                        <el-input v-model="batchSecretFills[p.name].database" size="small" placeholder="如 g50_xxx_game" style="width:180px" />
                      </template>
                      <el-button link size="small" :style="batchSecretFills[p.name].type === 'tidb' ? '' : 'margin-left:auto'" @click="batchSecretFills[p.name].open = !batchSecretFills[p.name].open">{{ batchSecretFills[p.name].open ? '收起 ▴' : `${batchSecretFills[p.name].type === 'tidb' ? '字段' : '键值对'} (${batchSecretFills[p.name].fields.length}) ▾` }}</el-button>
                    </div>
                    <div v-if="batchSecretFills[p.name] && batchSecretFills[p.name].open" class="batch-sec-fields">
                      <div v-for="(kv, i) in batchSecretFills[p.name].fields" :key="i" class="batch-sec-frow">
                        <el-input v-model="kv.key" size="small" placeholder="key" style="width:180px" />
                        <el-input v-model="kv.value" size="small" placeholder="value" style="width:320px;margin:0 6px" />
                        <el-button link type="danger" size="small" @click="batchSecretFills[p.name].fields.splice(i, 1)">×</el-button>
                      </div>
                      <el-button link type="primary" size="small" @click="addFillField(p.name)">+ {{ batchSecretFills[p.name].type === 'tidb' ? '额外字段' : '键值对' }}</el-button>
                    </div>
                  </div>
                </div>
                <!-- + 新增密钥：手动加模板没引用的 -->
                <div class="batch-sec-add">
                  <span class="batch-sec-grp" style="margin:0">+ 新增密钥</span>
                  <el-input v-model="addSecName" size="small" placeholder="密钥名（如 g50-xxx-config-secret）" style="width:260px" />
                  <el-select v-model="addSecType" size="small" style="width:104px">
                    <el-option label="TiDB" value="tidb" />
                    <el-option label="普通 Opaque" value="opaque" />
                  </el-select>
                  <el-select v-model="addSecOwner" size="small" style="width:190px" placeholder="归属">
                    <el-option label="环境共享" value="" />
                    <el-option v-for="r in validRows" :key="r.module_name" :label="'模块 ' + r.module_name.trim()" :value="r.module_name.trim()" />
                  </el-select>
                  <el-button size="small" type="primary" @click="addManualBatchSecret">加</el-button>
                </div>
              </template>
              <!-- YAML 模式：直接粘 tidbSecrets/secrets 片段 -->
              <template v-else>
                <div class="batch-sec-auto" style="margin:6px 0">直接编辑将整段并入 z-kv-secrets（含 tidbSecrets:/secrets: 列表；生产每模块差异值可从别处拷）</div>
                <CodeEditor v-model="batchSecretsYaml" />
              </template>
            </template>
          </div>
        </el-form-item>
        <el-form-item label="ArgoCD">
          <el-switch v-model="batch.disable" :active-value="true" :inactive-value="false" active-text="disable:true 安全预演" inactive-text="关闭=直接部署（默认）" />
        </el-form-item>

        <div v-if="batchResult" class="preview-box">
          <el-alert :type="batchResult.all_ok ? 'success' : 'error'" :closable="false"
            :title="batchResult.all_ok ? `全部通过 ✓（共 ${batchResult.rows.length} 个模块，${batchResult.changed_files} 个文件）` : '有行未通过，已阻止提交（见清单红标）'" />
          <div v-for="row in batchResult.rows.filter(r => r.error)" :key="row.module_name" class="err-card">
            <div class="err-head">
              <el-icon><CircleCloseFilled /></el-icon>
              <span>{{ row.module_name }} 校验失败</span>
              <el-button link type="primary" class="err-copy" @click="copyErr(row.error)">复制报错</el-button>
            </div>
            <pre class="err-body">{{ row.error }}</pre>
          </div>
        </div>
        <el-alert v-if="batchSubmitted" type="success" :closable="false" class="submitted"
          :title="`已提交 commit ${batchSubmitted.commit_sha}（${batchSubmitted.rows.length} 个模块）`" show-icon />
      </el-form>

      <template #footer>
        <el-button @click="batchDialog = false">关闭</el-button>
        <el-button type="primary" :disabled="!canBatchPreview" :loading="batchPreviewing" @click="doBatchPreview">批量校验预览</el-button>
        <el-button type="success" :disabled="!canBatchSubmit" :loading="batchSubmitting" @click="doBatchSubmit">确认批量提交</el-button>
      </template>
    </el-dialog>

    <!-- 批量·单行配置弹窗 -->
    <el-dialog v-model="rowDialog" :title="`配置模块 ${rowEditing?.module_name || ''}`" width="820px" :close-on-click-modal="false" top="5vh" append-to-body>
      <div v-if="rowYaml" v-loading="rowLoading">
        <ValuesEditor ref="rowEditorRef" :modelValue="rowYaml" :moduleType="selectedBatchTemplateType" />
        <div v-if="rowConfigmaps.length" class="cm-block">
          <div class="cm-title">ConfigMap</div>
          <el-tabs v-model="rowCmTab" type="border-card">
            <el-tab-pane v-for="cm in rowConfigmaps" :key="cm.path" :label="cmName(cm.path)" :name="cm.path">
              <CodeEditor v-model="cm.content" />
            </el-tab-pane>
          </el-tabs>
        </div>
      </div>
      <div v-else v-loading="rowLoading" style="min-height:80px">加载模板中…</div>
      <template #footer>
        <el-button @click="rowDialog = false">取消</el-button>
        <el-button type="primary" @click="saveRowConfig">保存该模块配置</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import * as api from '../api'
import ValuesEditor from '../components/ValuesEditor.vue'
import CodeEditor from '../components/CodeEditor.vue'
import { CircleCloseFilled, Search } from '@element-plus/icons-vue'
import { stringify as yamlStringify } from 'yaml'

const router = useRouter()

function cmName(p) { return (p || '').split('/').pop() }

function copyErr(text) {
  navigator.clipboard?.writeText(text || '').then(() => ElMessage.success('已复制报错')).catch(() => {})
}
function copyText(text) {
  navigator.clipboard?.writeText(text || '').then(() => ElMessage.success('已复制')).catch(() => {})
}

const envs = ref([])
const templates = ref([])
const envId = ref(null)
const modules = ref([])
const loadingMods = ref(false)

// 模块列表：本地搜索 + 分页（modules 一次性拿全，前端过滤/切页；默认 10 条/页）
const modQuery = ref('')
const modPage = ref(1)
const modPageSize = ref(10)
const filteredModules = computed(() => {
  const q = modQuery.value.trim().toLowerCase()
  if (!q) return modules.value
  return modules.value.filter(m =>
    (m.name || '').toLowerCase().includes(q) ||
    (m.image_repository || '').toLowerCase().includes(q) ||
    (m.namespace || '').toLowerCase().includes(q))
})
const pagedModules = computed(() => {
  const s = (modPage.value - 1) * modPageSize.value
  return filteredModules.value.slice(s, s + modPageSize.value)
})

const curEnvLabel = computed(() => {
  const e = envs.value.find(x => x.id === envId.value)
  return e ? `${e.name}（${e.env_type}）` : ''
})

async function loadModules() {
  modQuery.value = ''; modPage.value = 1 // 切环境重置搜索/翻页
  if (!envId.value) { modules.value = []; return }
  loadingMods.value = true
  try { modules.value = (await api.listModules(envId.value)) || [] }
  catch { modules.value = [] }
  finally { loadingMods.value = false }
}

// ---- 新增模块弹窗 ----
const addDialog = ref(false)
const editorRef = ref(null)
const form = ref({ templateId: null, moduleName: '', namespace: '', valuesYaml: '', disable: false })
const configmaps = ref([]) // [{path, content}]，前端服务才有；prefill 带出
const cmTab = ref('')
const selectedTemplateType = computed(() => templates.value.find(t => t.id === form.value.templateId)?.module_type || 'backend')
const prefilling = ref(false)
const previewing = ref(false)
const submitting = ref(false)
const preview = ref(null)
const submitted = ref(null)

const canPrefill = computed(() => envId.value && form.value.templateId && form.value.moduleName.trim())
const imageMissing = ref(false)
const imageMissingMsg = ref('')
// 额外艾特（临时）+ 环境固定艾特（只读展示）
const extraAt = ref([])       // 本次临时选的 lark_id 列表
const envFixedAt = ref([])    // 该环境固定艾特人 lark_id（prefill 带出，只读）
const atContacts = ref([])    // 通知人列表
const atContactsWithLark = computed(() => atContacts.value.filter(c => c.lark_id))
function atName(larkId) { return atContacts.value.find(c => c.lark_id === larkId)?.name || larkId }
// 预填后的镜像/域名短显示（Harbor项目/tag/域名），只读展示，避免看长串完整地址
const imgPreview = ref(null) // { short, tag, domain }
// 访问域名（所有环境可配多个；生产按数量生成占位）
const domains = ref([])          // 当前域名列表（可加删改）
const primaryDomains = ref([])   // 项目参数配的主域名列表（生产生成用）
const isProdEnv = ref(false)     // 目标环境是否 prod
const ingressEnabled = ref(false)// 模板是否开 ingress（开了才显示域名区）
const domCount = ref(1)          // 生产：要生成几个域名
// 后端专属密钥分类（已存在复用 / 待新建填内容），来自 prefill 的 secret_refs
const secretRefs = ref(null) // { zkv_path, zkv_found, existing[], pending[{name,type,namespace,database,extra[],use_data,manual}] }

// 生产：把 domCount 个域名平均分配到多个主域名（轮流），主机头 xxx1..N 占位
function genProdDomains() {
  const pd = primaryDomains.value
  if (!pd.length) return
  const out = []
  for (let i = 0; i < domCount.value; i++) {
    const dom = String(pd[i % pd.length]).replace(/^\.+/, '')
    out.push(`xxx${i + 1}.${dom}`)
  }
  domains.value = out
}
const secMode = ref('form')  // 表单 / YAML 双模式
const secYaml = ref('')      // YAML 模式：将追加到 z-kv-secrets 的片段
// 缺镜像 / 专属密钥 database 没填全时：helm 预览 + 确认提交都禁用
const canPreview = computed(() => canPrefill.value && form.value.namespace.trim() && form.value.valuesYaml.trim() && !imageMissing.value && pendingTidbFilled.value)
const canSubmit = computed(() => preview.value && (preview.value.helm_ok || preview.value.helm_skipped) && !imageMissing.value && pendingTidbFilled.value)

function resetPreview() { preview.value = null; submitted.value = null; imageMissing.value = false; imageMissingMsg.value = '' }

function openAdd() {
  form.value = { templateId: null, moduleName: '', namespace: '', valuesYaml: '', disable: false }
  configmaps.value = []
  cmTab.value = ''
  lastPrefilledName.value = ''
  nsOptionsSingle.value = []
  imgPreview.value = null
  secretRefs.value = null
  domains.value = []; primaryDomains.value = []; isProdEnv.value = false; ingressEnabled.value = false; domCount.value = 1
  extraAt.value = []; envFixedAt.value = []
  resetPreview()
  addDialog.value = true
}

function reqBody() {
  // 从 ValuesEditor 取最终 YAML（表单模式会把字段写回、保留原顺序；YAML 模式取原文）
  const yaml = editorRef.value?.getYaml?.() || form.value.valuesYaml
  return {
    template_id: form.value.templateId,
    target_env_id: envId.value,
    module_name: form.value.moduleName.trim(),
    namespace: form.value.namespace.trim(),
    values_yaml: yaml,
    configmaps: configmaps.value.map(c => ({ path: c.path, content: c.content })),
    ...(secMode.value === 'yaml'
      ? { new_secrets_yaml: secYaml.value }
      : {
        new_secrets: (secretRefs.value?.pending || []).map(p => ({
          name: (p.name || '').trim(), type: p.type, namespace: p.namespace,
          database: (p.database || '').trim(),
          extra: (p.extra || []).filter(kv => (kv.key || '').trim()),
        })),
      }),
    domains: domains.value.map(d => (d || '').trim()).filter(Boolean), // 访问域名(覆盖 values host)
    at_lark_ids: extraAt.value, // 临时额外艾特人
    disable: form.value.disable,
  }
}

// 密钥填写合法性：表单模式下每条要有 name，tidb 要有 database；YAML 模式不卡（helm 校验兜底）
const pendingTidbFilled = computed(() => {
  if (secMode.value === 'yaml') return true
  return (secretRefs.value?.pending || []).every(p =>
    (p.name || '').trim() && (p.type !== 'tidb' || (p.database || '').trim()))
})

function onTypeChange(p) {
  // tidb ↔ opaque 切换时重置字段
  if (p.type === 'tidb') { p.extra = [{ key: 'TIDB_PWDSALT', value: '' }, { key: 'TIDB_PWDCRYPT', value: '' }] }
  else { p.database = ''; p.extra = [] }
}
function addManualSecret() {
  secretRefs.value.pending.push({
    name: '', type: 'opaque', namespace: form.value.namespace || '', database: '',
    extra: [], manual: true,
  })
}
// 表单 → YAML 片段（切到 YAML 模式时生成一次，供用户直接改）
function genSecYaml(pending) {
  const tidbs = pending.filter(p => p.type === 'tidb')
  const plains = pending.filter(p => p.type === 'opaque')
  let out = ''
  if (tidbs.length) {
    out += 'tidbSecrets:\n'
    for (const p of tidbs) {
      out += `  - name: ${p.name}\n    namespace: ${p.namespace}\n    database: ${p.database || ''}\n    extraStringData:\n`
      for (const kv of (p.extra || [])) if ((kv.key || '').trim()) out += `      ${kv.key}: ${kv.value}\n`
    }
  }
  if (plains.length) {
    out += 'secrets:\n'
    for (const p of plains) {
      out += `  - name: ${p.name}\n    namespace: ${p.namespace}\n    type: Opaque\n    stringData:\n`
      for (const kv of (p.extra || [])) if ((kv.key || '').trim()) out += `      ${kv.key}: ${kv.value}\n`
    }
  }
  return out
}
function onSecModeChange(m) {
  if (m === 'yaml') secYaml.value = genSecYaml(secretRefs.value?.pending || [])
}

async function doPrefill() {
  prefilling.value = true
  try {
    const r = await api.prefillModule({ template_id: form.value.templateId, target_env_id: envId.value, module_name: form.value.moduleName.trim() })
    form.value.valuesYaml = r.values_yaml || ''
    configmaps.value = (r.configmaps || []).map(c => ({ ...c }))
    cmTab.value = configmaps.value[0]?.path || ''
    nsOptionsSingle.value = r.namespaces || []
    if (!form.value.namespace) form.value.namespace = r.suggest_namespace || ''
    lastPrefilledName.value = form.value.moduleName.trim()
    resetPreview()
    imgPreview.value = { full: r.image_repository || '', short: r.image_short || '', tag: r.latest_tag || '' }
    // 域名：非生产带出自动单个；生产为空(点生成)。主域名列表 + 是否生产 + 是否开 ingress
    domains.value = (r.domains || []).slice()
    primaryDomains.value = r.primary_domains || []
    isProdEnv.value = !!r.is_prod
    ingressEnabled.value = !!r.ingress_enabled
    domCount.value = 1
    envFixedAt.value = r.env_at_lark_ids || [] // 该环境固定艾特人（只读展示）
    secMode.value = 'form'; secYaml.value = ''
    const sr = r.secret_refs || null
    if (sr) (sr.pending || []).forEach(p => { p.manual = false; p.extra = p.extra || [] })
    secretRefs.value = sr
    imageMissing.value = !!r.image_missing
    imageMissingMsg.value = r.image_missing_msg || ''
    if (imageMissing.value) ElMessage.warning('Harbor 缺少该镜像，请先同步后再新增')
    else ElMessage.success('已带出样板 values.yaml，请复核后编辑')
  } finally { prefilling.value = false }
}

// 填完模块名失焦 → 自动预填（模板已选、名字变了才触发；改了模块名本就该重新预填）
const lastPrefilledName = ref('')
const nsOptionsSingle = ref([])
async function autoPrefill() {
  const name = form.value.moduleName.trim()
  if (!canPrefill.value || !name || name === lastPrefilledName.value) return
  await doPrefill()
}

async function doPreview() {
  previewing.value = true
  submitted.value = null
  try { preview.value = await api.previewModule(reqBody()) } finally { previewing.value = false }
}

async function doSubmit() {
  try {
    await ElMessageBox.confirm(`确认提交新增模块 ${form.value.moduleName.trim()}？（disable:${form.value.disable}）`, '确认提交', { type: 'warning' })
  } catch { return }
  submitting.value = true
  try {
    await api.submitModule(reqBody())
    addDialog.value = false
    ElMessage.success('已提交，后台执行中——去「新增历史」看结果')
    router.push('/orchestration-history')
  } finally { submitting.value = false }
}

// ---- 批量新增 ----
const batchDialog = ref(false)
const batch = ref({ templateId: null, paste: '', disable: false, rows: [] })
const batchResult = ref(null)
const batchSubmitted = ref(null)
const batchPreviewing = ref(false)
const batchSubmitting = ref(false)

const validRows = computed(() => batch.value.rows.filter(r => r.module_name.trim() && r.namespace.trim()))
const canBatchPreview = computed(() => batch.value.templateId && validRows.value.length > 0)

// 批量·密钥分类（解析成行时算好）：existing=复用；pending=待新建[{name,type,namespace,modules,fields}]
// batchSecretFills：name → { type, database, open, fields:[{key,value}] }，逐条独立填。
const batchSecretFills = ref({})
const batchExisting = ref([])    // 复用（已存在，不用填）
const batchPendingList = ref([]) // 待新建（解析成行/改行名时刷新）
const batchSecMode = ref('form') // 密钥区模式：form 逐条卡片 / yaml 直接粘片段
const batchSecretsYaml = ref('') // YAML 模式内容（提交时整段并入 z-kv）
// 按归属分组：模块专属(只被1个模块引用)挂到该模块框；多模块同名引用=共享单列
const batchModuleGroups = computed(() => {
  const order = validRows.value.map(r => r.module_name.trim())
  return order
    .map(m => ({ module: m, secrets: batchPendingList.value.filter(p => (p.modules?.length || 0) === 1 && p.modules[0] === m) }))
    .filter(g => g.secrets.length)
})
const batchSharedPending = computed(() => batchPendingList.value.filter(p => (p.modules?.length || 0) > 1))
// 分区：每模块一组 + 共享一组（卡片渲染共用）
const batchSecretSections = computed(() => {
  const secs = batchModuleGroups.value.map(g => ({ key: 'm:' + g.module, title: '模块 ' + g.module, shared: false, secrets: g.secrets }))
  if (batchSharedPending.value.length) secs.push({ key: 'shared', title: '环境共享（被多个模块引用 · 只建一次）', shared: true, secrets: batchSharedPending.value })
  return secs
})
const batchPendingTidb = computed(() => batchPendingList.value.filter(p => p.type === 'tidb'))
// tidb 判定以下拉(fill.type)为准
const batchTidbFilled = computed(() => batchPendingList.value.every(p => {
  const f = batchSecretFills.value[p.name]
  return (f?.type || p.type) !== 'tidb' || (f?.database || '').trim()
}))
// 预览是纯 helm 门禁；提交要 helm 全绿 +（表单模式）tidb 都填了 database
const canBatchSubmit = computed(() => batchResult.value && batchResult.value.all_ok && (batchSecMode.value === 'yaml' || batchTidbFilled.value))

// 按 pending 初始化/合并每条待新建密钥的填写状态（保留已填的值，去掉不再需要的）
function syncBatchFills() {
  const next = {}
  for (const p of batchPendingList.value) {
    const old = batchSecretFills.value[p.name]
    next[p.name] = old || { type: p.type, namespace: p.namespace, database: '', open: p.type === 'tidb', fields: (p.fields || []).map(f => ({ key: f.key, value: f.value || '' })) }
  }
  batchSecretFills.value = next
}
function addFillField(name) {
  const f = batchSecretFills.value[name]
  if (f) f.fields.push({ key: '', value: '' })
}
// 类型下拉切到 tidb 且没字段时，用识别出的模板/默认 salt-crypt 兜底铺上
function onBatchTypeChange(name) {
  const f = batchSecretFills.value[name]
  if (!f || f.type !== 'tidb') return
  f.open = true
  if (!f.fields.length) {
    const schema = batchPendingList.value.find(x => x.name === name)?.fields || []
    f.fields = schema.length ? schema.map(s => ({ key: s.key, value: s.value || '' }))
      : [{ key: 'TIDB_PWDSALT', value: '' }, { key: 'TIDB_PWDCRYPT', value: '' }]
  }
}

// 由当前「待新建 + 已填字段」生成 z-kv 片段脚手架（YAML 模式起点，带预填值）
function scaffoldFromForm() {
  const tidb = [], plain = []
  for (const p of batchPendingList.value) {
    const f = batchSecretFills.value[p.name] || {}
    const kvs = (f.fields || []).filter(x => x.key && x.key.trim())
    if ((f.type || p.type) === 'tidb') {
      const esd = {}; kvs.forEach(x => { esd[x.key.trim()] = x.value || '' })
      tidb.push({ name: p.name, namespace: f.namespace || p.namespace, database: f.database || '', extraStringData: esd })
    } else {
      const sd = {}; kvs.forEach(x => { sd[x.key.trim()] = x.value || '' })
      plain.push({ name: p.name, namespace: f.namespace || p.namespace, type: 'Opaque', stringData: sd })
    }
  }
  const obj = {}
  if (tidb.length) obj.tidbSecrets = tidb
  if (plain.length) obj.secrets = plain
  return Object.keys(obj).length ? yamlStringify(obj) : ''
}
// 切到 YAML：仅当编辑器还空时铺一次脚手架（bug: 不覆盖你已改/已加的内容）
function onBatchSecModeChange(mode) {
  if (mode === 'yaml' && !batchSecretsYaml.value.trim()) batchSecretsYaml.value = scaffoldFromForm()
}

// 「+新增密钥」：手动加一条模板没引用的待新建密钥
const addSecName = ref('')
const addSecType = ref('tidb')
const addSecOwner = ref('') // 模块名 / '' 表示环境共享
function addManualBatchSecret() {
  const name = addSecName.value.trim()
  if (!name) { ElMessage.warning('填密钥名'); return }
  if (batchPendingList.value.some(p => p.name === name) || batchExisting.value.includes(name)) {
    ElMessage.warning('该密钥名已在列表里'); return
  }
  const modules = addSecOwner.value ? [addSecOwner.value] : validRows.value.map(r => r.module_name.trim())
  const ns = addSecOwner.value
    ? (batch.value.rows.find(r => r.module_name.trim() === addSecOwner.value)?.namespace || nsBatchSet.value)
    : nsBatchSet.value
  batchPendingList.value.push({ name, type: addSecType.value, namespace: ns, modules, fields: [], manual: true })
  syncBatchFills()
  if (addSecType.value === 'tidb') onBatchTypeChange(name)
  addSecName.value = ''; resetBatch()
}
const selectedBatchTemplateType = computed(() => templates.value.find(t => t.id === batch.value.templateId)?.module_type || 'backend')

// 批量·单行配置弹窗
const rowDialog = ref(false)
const rowEditorRef = ref(null)
const rowEditing = ref(null)
const rowYaml = ref('')
const rowConfigmaps = ref([])
const rowCmTab = ref('')
const rowLoading = ref(false)

async function openRowConfig(row) {
  rowEditing.value = row
  rowDialog.value = true
  rowYaml.value = ''
  rowConfigmaps.value = []
  // 已改过就用已存的；否则拉该模块的派生预填
  if (row.values_yaml) {
    rowYaml.value = row.values_yaml
    rowConfigmaps.value = (row.configmaps || []).map(c => ({ ...c }))
    rowCmTab.value = rowConfigmaps.value[0]?.path || ''
    return
  }
  rowLoading.value = true
  try {
    const r = await api.prefillModule({ template_id: batch.value.templateId, target_env_id: envId.value, module_name: row.module_name.trim() })
    rowYaml.value = r.values_yaml || ''
    rowConfigmaps.value = (r.configmaps || []).map(c => ({ ...c }))
    rowCmTab.value = rowConfigmaps.value[0]?.path || ''
  } finally { rowLoading.value = false }
}

function saveRowConfig() {
  if (rowEditing.value && rowEditorRef.value) {
    rowEditing.value.values_yaml = rowEditorRef.value.getYaml()
    rowEditing.value.configmaps = rowConfigmaps.value.map(c => ({ path: c.path, content: c.content }))
    resetBatch()
    ElMessage.success('已保存该模块配置')
  }
  rowDialog.value = false
}

function resetBatch() { batchResult.value = null; batchSubmitted.value = null }
function rowStatus(name) {
  if (!batchResult.value) return null
  const r = batchResult.value.rows.find(x => x.module_name === name.trim())
  return r ? { ok: r.helm_ok || r.helm_skipped } : null
}

function openBatch() {
  batch.value = { templateId: null, paste: '', disable: false, rows: [] }
  batchSecretFills.value = {}
  batchPendingList.value = []
  batchExisting.value = []
  batchSecMode.value = 'form'
  batchSecretsYaml.value = ''
  addSecName.value = ''; addSecType.value = 'tidb'; addSecOwner.value = ''
  resetBatch()
  batchDialog.value = true
}

const nsOptions = ref([])

const nsBatchSet = ref('')

// namespace 批量设：一键把上面选的 namespace 应用到所有行
function applyNsToAll() {
  if (!nsBatchSet.value) return
  batch.value.rows.forEach(r => { r.namespace = nsBatchSet.value })
}

async function parsePaste() {
  const names = batch.value.paste.split('\n').map(s => s.trim()).filter(Boolean)
  if (!names.length) { batch.value.rows = []; return }
  let derived = null
  try { derived = await api.deriveModules({ target_env_id: envId.value, template_id: batch.value.templateId, module_names: names }) } catch { /* 派生失败不阻断 */ }
  nsOptions.value = derived?.namespaces || []
  const defNs = derived?.default_namespace || (envs.value.find(e => e.id === envId.value)?.name || '')
  nsBatchSet.value = defNs
  const dmap = {}
  ;(derived?.modules || []).forEach(m => { dmap[m.module_name] = m })
  batch.value.rows = names.map(n => {
    const d = dmap[n] || {}
    return { module_name: n, namespace: defNs, image_short: d.image_short || '', latest_tag: d.latest_tag || '', image_missing: !!d.image_missing, domain: d.domain || '' }
  })
  batchExisting.value = derived?.secret_refs?.existing || [] // 复用 + 待新建，解析成行就带出
  batchPendingList.value = derived?.secret_refs?.pending || []
  syncBatchFills()
  resetBatch()
}

// 编辑某行模块名后重新派生该行的镜像/tag/域名 + 全表刷新待新建密钥（名字变→密钥名变）
async function deriveRow(row) {
  const name = (row.module_name || '').trim()
  if (!name || !envId.value) return
  try {
    const names = validRows.value.map(r => r.module_name.trim())
    const res = await api.deriveModules({ target_env_id: envId.value, template_id: batch.value.templateId, module_names: names })
    const d = (res?.modules || []).find(m => m.module_name === name) || {}
    row.image_short = d.image_short || ''
    row.latest_tag = d.latest_tag || ''
    row.image_missing = !!d.image_missing
    row.domain = d.domain || ''
    batchExisting.value = res?.secret_refs?.existing || []
    batchPendingList.value = res?.secret_refs?.pending || []
    syncBatchFills()
    resetBatch()
  } catch { /* 忽略 */ }
}

function batchBody() {
  return {
    template_id: batch.value.templateId,
    target_env_id: envId.value,
    disable: batch.value.disable,
    rows: validRows.value.map(r => ({ module_name: r.module_name.trim(), namespace: r.namespace.trim(), values_yaml: r.values_yaml || '', configmaps: r.configmaps || [] })),
    secret_fills: batchPendingList.value.map(p => {
      const f = batchSecretFills.value[p.name] || { type: p.type, namespace: p.namespace, database: '', fields: [] }
      return { name: p.name, type: f.type || p.type, namespace: f.namespace || p.namespace, database: (f.database || '').trim(), fields: (f.fields || []).filter(x => x.key.trim()).map(x => ({ key: x.key.trim(), value: x.value })) }
    }),
    secrets_yaml: batchSecMode.value === 'yaml' ? batchSecretsYaml.value : '',
  }
}

async function doBatchPreview() {
  batchPreviewing.value = true
  batchSubmitted.value = null
  try { batchResult.value = await api.batchPreviewModules(batchBody()) } finally { batchPreviewing.value = false }
}

async function doBatchSubmit() {
  try {
    await ElMessageBox.confirm(`确认批量提交 ${validRows.value.length} 个模块？（disable:${batch.value.disable}）`, '确认批量提交', { type: 'warning' })
  } catch { return }
  batchSubmitting.value = true
  try {
    await api.batchSubmitModules(batchBody())
    batchDialog.value = false
    ElMessage.success('已提交，后台执行中——去「新增历史」看结果')
    router.push('/orchestration-history')
  } finally { batchSubmitting.value = false }
}

onMounted(async () => {
  envs.value = (await api.listProjectEnvs()) || []
  templates.value = (await api.listTemplates()) || []
  atContacts.value = (await api.listContacts()) || []
})
</script>

<style scoped>
.orch-page { padding: 16px 20px; }
.page-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.head-left { display: flex; align-items: center; gap: 14px; }
.head-left h2 { margin: 0; font-size: 18px; }
.yaml-editor :deep(textarea) { font-family: 'Menlo', 'Consolas', monospace; font-size: 13px; line-height: 1.5; }
.preview-box { margin: 8px 0 0 110px; padding: 12px 14px; background: var(--el-fill-color-light); border-radius: 8px; }
.batch-sec-box { width: 100%; padding: 12px 14px; background: #fffdf5; border: 1px solid #f0e0b0; border-radius: 8px; box-sizing: border-box; }
.batch-sec-head { font-weight: 600; margin-bottom: 8px; }
.batch-sec-auto { margin-left: 10px; color: #16a34a; font-size: 12px; font-weight: normal; }
.batch-sec-grp { margin: 10px 0 4px; color: #6b7280; font-size: 12px; border-top: 1px dashed #e5d9a8; padding-top: 8px; }
.batch-sec-row { display: flex; align-items: center; gap: 6px; padding: 3px 0; font-size: 13px; }
.batch-sec-reuse { margin-bottom: 8px; }
.batch-sec-group { margin-top: 6px; }
.batch-sec-ns { color: #9ca3af; font-size: 12px; margin-left: 8px; }
.batch-sec-card { padding: 6px 0 6px 10px; border-bottom: 1px dashed #eee; border-left: 2px solid #eef; margin-left: 4px; }
.batch-sec-add { display: flex; align-items: center; gap: 8px; margin-top: 10px; padding-top: 8px; border-top: 1px dashed #e5d9a8; }
.batch-sec-cardhead { display: flex; align-items: center; gap: 8px; font-size: 13px; }
.batch-sec-fields { margin: 6px 0 6px 18px; padding-left: 10px; border-left: 2px solid #f0e0b0; }
.batch-sec-frow { display: flex; align-items: center; padding: 2px 0; }
.batch-sec-mod { color: #2563eb; }
.batch-sec-k { margin-left: auto; color: #6b7280; }
.batch-sec-k .req { color: #ef4444; }
.batch-sec-warn { margin-top: 10px; color: #d97706; font-size: 12px; }
.err-card { margin: 8px 0; border: 1px solid #fbc4c4; border-radius: 8px; overflow: hidden; background: #fff5f5; }
.err-head { display: flex; align-items: center; gap: 6px; padding: 8px 12px; background: #fde2e2; color: #c0392b; font-weight: 600; font-size: 13px; }
.err-head .err-copy { margin-left: auto; }
.err-body { margin: 0; padding: 10px 12px; background: #1e1e1e; color: #ff9b9b; overflow-x: auto; font-size: 12px; line-height: 1.5; white-space: pre-wrap; font-family: 'Menlo', 'Consolas', monospace; max-height: 260px; }
.changed-title { font-weight: 600; margin: 8px 0 4px; }
.changed { margin: 0; padding-left: 18px; }
.changed code { font-size: 12px; }
.mod-filter { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; }
.mod-count { color: #909399; font-size: 13px; }
.pager-bar { display: flex; justify-content: flex-end; margin-top: 14px; }
.hint { color: #909399; font-size: 12px; margin-top: 6px; }
.ns-hint { color: #909399; font-size: 12px; margin-left: 10px; }
.mono { font-family: var(--mono, monospace); font-size: 12px; }
.ellip { display: inline-block; max-width: 240px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; vertical-align: bottom; }
.miss { color: #dc2626; font-weight: 600; }
.kv-table { border-collapse: collapse; background: var(--el-fill-color-light); border-radius: 6px; overflow: hidden; }
.kv-table td { padding: 6px 12px; border-bottom: 1px solid var(--el-border-color-lighter); font-size: 13px; vertical-align: middle; }
.kv-table tr:last-child td { border-bottom: none; }
.kv-table .kv-k { color: #909399; font-size: 12px; white-space: nowrap; width: 90px; background: var(--el-fill-color); }
.kv-table code { background: var(--el-fill-color); padding: 1px 6px; border-radius: 4px; font-size: 12px; word-break: break-all; }
.ip-none { color: #c0c4cc; font-size: 12px; }
.dom-box { width: 100%; }
.dom-gen { padding: 8px 10px; background: var(--el-fill-color-light); border-radius: 6px; margin-bottom: 8px; }
.dom-primary { margin: 0 10px; color: #606266; font-size: 12px; }
.dom-tip { color: #d97706; font-size: 12px; margin-top: 4px; }
.dom-row { display: flex; align-items: center; gap: 6px; margin: 4px 0; }
.sec-box { width: 100%; padding: 10px 12px; background: var(--el-fill-color-light); border-radius: 6px; }
.sec-src { font-size: 12px; color: #909399; margin-bottom: 8px; }
.sec-grp { margin-top: 6px; }
.sec-lbl { font-size: 12px; font-weight: 600; margin-bottom: 4px; }
.sec-lbl.ok { color: #16a34a; }
.sec-lbl.warn { color: #d97706; }
.sec-pending { border: 1px dashed var(--el-border-color); border-radius: 6px; padding: 8px 10px; margin-bottom: 8px; }
.sec-ph { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; }
.sec-ns { color: #909399; font-size: 12px; }
.sec-row { display: flex; align-items: center; margin: 4px 0; }
.sec-k { width: 100px; color: #606266; font-size: 12px; }
.sec-k .req { color: #dc2626; }
.sec-warn { color: #d97706; font-size: 12px; }
.submitted { margin: 10px 0 0 110px; }
.cm-block { margin-top: 12px; }
.cm-title { font-weight: 600; font-size: 13px; margin-bottom: 6px; color: var(--el-color-primary); }
</style>
