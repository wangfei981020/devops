<template>
  <div v-if="!hasAnyManagePerm" class="no-perm">
    <el-icon class="np-icon"><Lock /></el-icon>
    <div class="np-title">需要管理类权限</div>
    <div class="np-desc">系统设置需要 admin 或任一管理类按钮权限（global / projects / argocd / lark / contacts）。请联系管理员分配。</div>
  </div>
  <div v-else class="ss">
    <div class="rail">
      <div class="rail-title">配置分区</div>
      <div v-for="t in visibleTabs" :key="t.v" :class="['rail-item', { active: tab === t.v }]" @click="tab = t.v">
        <span>{{ t.label }}</span>
        <span v-if="statusBadge(t.v)" :class="['rail-badge', statusBadge(t.v).kind]">{{ statusBadge(t.v).text }}</span>
      </div>
    </div>

    <div class="pane">
      <!-- ✦ 配置总览 -->
      <div v-if="tab === 'overview'" class="section">
        <div class="sec-head">
          <div class="sec-title">配置总览</div>
          <div class="sec-desc">点卡片进入对应配置 · 新部署建议按「全局凭证 → GitLab 仓库 → ArgoCD 实例 → Lark 机器人」顺序配</div>
        </div>
        <div class="sec-body">
          <div class="ov-group">
            <div class="ov-group-title">🔐 认证与用户</div>
            <div class="ov-grid">
              <div class="ov-card" @click="tab='cred'">
                <div class="ov-icon" style="background:#eff6ff;color:#1d4ed8"><el-icon><Key /></el-icon></div>
                <div class="ov-main">
                  <div class="ov-title">全局凭证</div>
                  <div class="ov-desc">GitLab URL/User/Token + 测试仓库路径</div>
                </div>
                <div class="ov-right">
                  <span :class="['ov-badge', ovStatus.cred.kind]">{{ ovStatus.cred.text }}</span>
                  <el-icon class="ov-arrow"><ArrowRight /></el-icon>
                </div>
              </div>
              <div class="ov-card" @click="tab='accounts'">
                <div class="ov-icon" style="background:#f3e8ff;color:#7e22ce"><el-icon><User /></el-icon></div>
                <div class="ov-main">
                  <div class="ov-title">用户管理</div>
                  <div class="ov-desc">平台登录账号（admin / portal SSO）</div>
                </div>
                <div class="ov-right">
                  <span :class="['ov-badge', ovStatus.accounts.kind]">{{ ovStatus.accounts.text }}</span>
                  <el-icon class="ov-arrow"><ArrowRight /></el-icon>
                </div>
              </div>
              <div class="ov-card" @click="tab='contacts'">
                <div class="ov-icon" style="background:#fef9c3;color:#a16207"><el-icon><UserFilled /></el-icon></div>
                <div class="ov-main">
                  <div class="ov-title">通知人</div>
                  <div class="ov-desc">Lark @ 艾特专用</div>
                </div>
                <div class="ov-right">
                  <span :class="['ov-badge', ovStatus.contacts.kind]">{{ ovStatus.contacts.text }}</span>
                  <el-icon class="ov-arrow"><ArrowRight /></el-icon>
                </div>
              </div>
            </div>
          </div>

          <div class="ov-group">
            <div class="ov-group-title">🔔 通知</div>
            <div class="ov-grid">
              <div class="ov-card" @click="tab='larkbots'">
                <div class="ov-icon" style="background:#ecfdf5;color:#059669"><el-icon><ChatLineRound /></el-icon></div>
                <div class="ov-main">
                  <div class="ov-title">Lark 机器人</div>
                  <div class="ov-desc">多 webhook 管理 + 测试</div>
                </div>
                <div class="ov-right">
                  <span :class="['ov-badge', ovStatus.larkbots.kind]">{{ ovStatus.larkbots.text }}</span>
                  <el-icon class="ov-arrow"><ArrowRight /></el-icon>
                </div>
              </div>
            </div>
          </div>

          <div class="ov-group">
            <div class="ov-group-title">🔌 集成</div>
            <div class="ov-grid">
              <div class="ov-card" @click="tab='gitlabrepos'">
                <div class="ov-icon" style="background:#fff7ed;color:#c2410c"><el-icon><Folder /></el-icon></div>
                <div class="ov-main">
                  <div class="ov-title">GitLab 仓库</div>
                  <div class="ov-desc">可复用的仓库登记表（项目环境下拉选）</div>
                </div>
                <div class="ov-right">
                  <span :class="['ov-badge', ovStatus.gitlabrepos.kind]">{{ ovStatus.gitlabrepos.text }}</span>
                  <el-icon class="ov-arrow"><ArrowRight /></el-icon>
                </div>
              </div>
              <div class="ov-card" @click="tab='argocd'">
                <div class="ov-icon" style="background:#e0f2fe;color:#0369a1"><el-icon><Connection /></el-icon></div>
                <div class="ov-main">
                  <div class="ov-title">ArgoCD 实例</div>
                  <div class="ov-desc">ArgoCD Server URL + Token</div>
                </div>
                <div class="ov-right">
                  <span :class="['ov-badge', ovStatus.argocd.kind]">{{ ovStatus.argocd.text }}</span>
                  <el-icon class="ov-arrow"><ArrowRight /></el-icon>
                </div>
              </div>
              <div class="ov-card" @click="tab='harbor'">
                <div class="ov-icon" style="background:#e0e7ff;color:#3730a3"><el-icon><Box /></el-icon></div>
                <div class="ov-main">
                  <div class="ov-title">Harbor 镜像仓库</div>
                  <div class="ov-desc">Robot 账号 · 镜像 tag 下拉 + 提交前校验</div>
                </div>
                <div class="ov-right">
                  <span :class="['ov-badge', ovStatus.harbor.kind]">{{ ovStatus.harbor.text }}</span>
                  <el-icon class="ov-arrow"><ArrowRight /></el-icon>
                </div>
              </div>
              <div class="ov-card" @click="tab='agents'">
                <div class="ov-icon" style="background:#faf5ff;color:#7c3aed"><el-icon><Monitor /></el-icon></div>
                <div class="ov-main">
                  <div class="ov-title">VM Agent · 版本接口</div>
                  <div class="ov-desc">VM 部署所需的 agent + list-version API（所有项目共用）</div>
                </div>
                <div class="ov-right">
                  <span :class="['ov-badge', ovStatus.agents.kind]">{{ ovStatus.agents.text }}</span>
                  <el-icon class="ov-arrow"><ArrowRight /></el-icon>
                </div>
              </div>
            </div>
          </div>

          <div class="ov-group">
            <div class="ov-group-title">⚙ 系统</div>
            <div class="ov-grid">
              <div class="ov-card" @click="tab='poll'">
                <div class="ov-icon" style="background:#f1f5f9;color:#475569"><el-icon><Timer /></el-icon></div>
                <div class="ov-main">
                  <div class="ov-title">同步策略</div>
                  <div class="ov-desc">ArgoCD 轮询 · Git push 重试</div>
                </div>
                <div class="ov-right">
                  <span :class="['ov-badge', ovStatus.poll.kind]">{{ ovStatus.poll.text }}</span>
                  <el-icon class="ov-arrow"><ArrowRight /></el-icon>
                </div>
              </div>
              <div class="ov-card" @click="tab='minio'">
                <div class="ov-icon" style="background:#fef3c7;color:#b45309"><el-icon><Box /></el-icon></div>
                <div class="ov-main">
                  <div class="ov-title">日志归档</div>
                  <div class="ov-desc">失败 pod 日志存 MinIO · 保留 {{ gc.minio_retention_days || 90 }} 天</div>
                </div>
                <div class="ov-right">
                  <span :class="['ov-badge', ovStatus.minio.kind]">{{ ovStatus.minio.text }}</span>
                  <el-icon class="ov-arrow"><ArrowRight /></el-icon>
                </div>
              </div>
              <div class="ov-card" @click="tab='history'">
                <div class="ov-icon" style="background:#fee2e2;color:#b91c1c"><el-icon><Delete /></el-icon></div>
                <div class="ov-main">
                  <div class="ov-title">发布历史</div>
                  <div class="ov-desc">每天凌晨自动清理 · 保留 {{ gc.history_retention_days || 180 }} 天</div>
                </div>
                <div class="ov-right">
                  <span :class="['ov-badge', ovStatus.history.kind]">{{ ovStatus.history.text }}</span>
                  <el-icon class="ov-arrow"><ArrowRight /></el-icon>
                </div>
              </div>
              <div class="ov-card" @click="tab='about'">
                <div class="ov-icon" style="background:#f5f5f4;color:#57534e"><el-icon><InfoFilled /></el-icon></div>
                <div class="ov-main">
                  <div class="ov-title">关于</div>
                  <div class="ov-desc">版本号 · 技术栈</div>
                </div>
                <div class="ov-right">
                  <el-icon class="ov-arrow"><ArrowRight /></el-icon>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- 全局凭证 -->
      <div v-if="tab === 'cred'" class="section">
        <div class="sec-head">
          <div class="sec-title">GitLab 全局凭证</div>
          <div class="sec-desc">用于 clone/commit/push · token AES 加密</div>
        </div>
        <div class="sec-body" v-loading="loading.cred">
          <el-form :model="gc" label-width="140px" label-position="left" size="default">
            <el-form-item label="GitLab URL"><el-input v-model="gc.gitlab_url" class="mono" placeholder="https://gitlab.your-company.com" /></el-form-item>
            <el-form-item label="User"><el-input v-model="gc.gitlab_user" placeholder="token 绑定的用户名（PAT）或 oauth2" /></el-form-item>
            <el-form-item label="Email"><el-input v-model="gc.gitlab_email" placeholder="commit 使用" /></el-form-item>
            <el-form-item label="Token">
              <el-input v-model="gc.gitlab_token" type="password" show-password placeholder="已设置，留空则不覆盖" />
            </el-form-item>
            <el-form-item label="测试仓库路径">
              <el-input v-model="gc.test_repo_path" class="mono" placeholder="如 argocd/uat-k8s-platform（可选，填了点「测试连接」用这个仓库精确验证）" />
            </el-form-item>
            <el-form-item label="发布中心 URL">
              <el-input v-model="gc.deploy_center_base_url" class="mono"
                placeholder="如 http://opsplatform-deploy.your-company.com（Lark 通知里「查看发布详情」按钮跳这个地址）" />
            </el-form-item>
          </el-form>
          <div class="actions" v-if="authStore.isAdmin || authStore.hasButton('manage_global')">
            <el-button @click="onTestGit" :loading="testing.git">测试连接</el-button>
            <el-button type="primary" @click="saveGlobal" :loading="saving.cred">保存</el-button>
          </div>
        </div>
      </div>

      <!-- GitLab 仓库 -->
      <div v-if="tab === 'gitlabrepos'" class="section">
        <div class="sec-head">
          <div class="sec-title-row">
            <div>
              <div class="sec-title">GitLab 仓库</div>
              <div class="sec-desc">登记一次，项目环境直接下拉选，避免重复敲长 URL</div>
            </div>
            <button v-if="authStore.isAdmin || authStore.hasButton('manage_global')" class="add-btn" @click="openRepoCreate">
              <el-icon><Plus /></el-icon>新增仓库
            </button>
          </div>
        </div>
        <div class="sec-body">
          <table class="tbl">
            <thead>
              <tr>
                <th style="width:160px">名称</th>
                <th>Repo URL</th>
                <th style="width:100px">默认分支</th>
                <th>描述</th>
                <th style="width:140px">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="g in gitlabRepos" :key="g.id">
                <td class="mono"><b>{{ g.name }}</b></td>
                <td class="mono webhook-cell">{{ g.repo_url }}</td>
                <td class="mono">{{ g.default_branch }}</td>
                <td>{{ g.description || '—' }}</td>
                <td>
                  <button v-if="authStore.isAdmin || authStore.hasButton('manage_global')" class="act" @click="openRepoEdit(g)">编辑</button>
                  <button v-if="authStore.isAdmin || authStore.hasButton('manage_global')" class="act danger" @click="onDeleteRepo(g)">删除</button>
                </td>
              </tr>
              <tr v-if="!gitlabRepos.length">
                <td colspan="5" class="empty-row">还没有登记 GitLab 仓库，点右上「+ 新增仓库」添加</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- ArgoCD 实例 -->
      <div v-if="tab === 'argocd'" class="section">
        <div class="sec-head">
          <div class="sec-title-row">
            <div>
              <div class="sec-title">ArgoCD 实例</div>
              <div class="sec-desc">全局可管理多个 ArgoCD · 项目环境只从这里选一个</div>
            </div>
            <button v-if="authStore.isAdmin || authStore.hasButton('manage_argocd')" class="add-btn" @click="openArgoCreate">
              <el-icon><Plus /></el-icon>新增实例
            </button>
          </div>
        </div>
        <div class="sec-body">
          <table class="tbl">
            <thead>
              <tr>
                <th>名称</th>
                <th>URL</th>
                <th>描述</th>
                <th style="width:160px">创建时间</th>
                <th style="width:140px">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="a in argoInstances" :key="a.id">
                <td class="mono"><b>{{ a.name }}</b></td>
                <td class="mono">{{ a.url }}</td>
                <td>{{ a.description || '—' }}</td>
                <td class="mono">{{ fmt(a.created_at) }}</td>
                <td>
                  <button class="act" @click="onTestArgo(a)">测试</button>
                  <button v-if="authStore.isAdmin || authStore.hasButton('manage_argocd')" class="act" @click="openArgoEdit(a)">编辑</button>
                  <button v-if="authStore.isAdmin || authStore.hasButton('manage_argocd')" class="act danger" @click="onDeleteArgo(a)">删除</button>
                </td>
              </tr>
              <tr v-if="!argoInstances.length">
                <td colspan="5" class="empty-row">还没有 ArgoCD 实例，点右上「+ 新增实例」添加</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- 用户管理：平台登录账号（admin 可用）-->
      <div v-if="tab === 'accounts'" class="section">
        <div class="sec-head">
          <div class="sec-title-row">
            <div>
              <div class="sec-title">用户管理</div>
              <div class="sec-desc">登录此平台的账号 · portal 用户由运维平台 SSO 自动创建 · Lark 艾特请到「通知人」配置</div>
            </div>
            <button v-if="authStore.isAdmin" class="add-btn" @click="openUserCreate">
              <el-icon><Plus /></el-icon>新增本地用户
            </button>
          </div>
        </div>
        <div class="sec-body">
          <div v-if="!authStore.isAdmin" class="empty-row" style="padding:40px 20px;text-align:center;color:var(--text-3)">
            仅管理员可查看 · 需要 admin 角色
          </div>
          <table v-else class="tbl">
            <thead>
              <tr>
                <th style="width:140px">用户名</th>
                <th style="width:140px">显示名</th>
                <th style="width:90px">角色</th>
                <th style="width:90px">来源</th>
                <th style="width:80px">状态</th>
                <th style="width:240px">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="u in users" :key="u.id">
                <td class="mono"><b>{{ u.username }}</b></td>
                <td>{{ u.display_name || '—' }}</td>
                <td>
                  <span :class="['role-tag', u.role]">{{ u.role }}</span>
                </td>
                <td>
                  <span :class="['src-tag', u.auth_source]">{{ u.auth_source }}</span>
                </td>
                <td>
                  <span v-if="u.status === 1" class="status-on">启用</span>
                  <span v-else class="status-off">禁用</span>
                </td>
                <td>
                  <button class="act" @click="openUserEdit(u)">编辑</button>
                  <button v-if="u.auth_source === 'local'" class="act" @click="onResetPwd(u)">重置密码</button>
                  <button class="act" @click="onToggleUser(u)">{{ u.status === 1 ? '禁用' : '启用' }}</button>
                  <button class="act danger" @click="onDeleteUser(u)">删除</button>
                </td>
              </tr>
              <tr v-if="!users.length">
                <td colspan="6" class="empty-row">还没有用户</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- 通知人：Lark 艾特专用 -->
      <div v-if="tab === 'contacts'" class="section">
        <div class="sec-head">
          <div class="sec-title-row">
            <div>
              <div class="sec-title">通知人</div>
              <div class="sec-desc">发布后根据操作人名字匹配 Lark ID · 艾特用</div>
            </div>
            <button v-if="authStore.isAdmin || authStore.hasButton('manage_contacts')" class="add-btn" @click="openContactCreate">
              <el-icon><Plus /></el-icon>新增通知人
            </button>
          </div>
        </div>
        <div class="sec-body">
          <table class="tbl">
            <thead>
              <tr>
                <th style="width:200px">名称</th>
                <th>Lark ID</th>
                <th>备注</th>
                <th style="width:120px">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="c in contacts" :key="c.id">
                <td class="mono"><b>{{ c.name }}</b></td>
                <td class="mono">{{ c.lark_id || '—' }}</td>
                <td>{{ c.remark || '—' }}</td>
                <td>
                  <button v-if="authStore.isAdmin || authStore.hasButton('manage_contacts')" class="act" @click="openContactEdit(c)">编辑</button>
                  <button v-if="authStore.isAdmin || authStore.hasButton('manage_contacts')" class="act danger" @click="onDeleteContact(c)">删除</button>
                </td>
              </tr>
              <tr v-if="!contacts.length">
                <td colspan="4" class="empty-row">还没有通知人，点右上「+ 新增通知人」添加</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- Lark 机器人：多 webhook -->
      <div v-if="tab === 'larkbots'" class="section">
        <div class="sec-head">
          <div class="sec-title-row">
            <div>
              <div class="sec-title">Lark 机器人</div>
              <div class="sec-desc">全局可配置多个 webhook · 项目环境只需选一个</div>
            </div>
            <button v-if="authStore.isAdmin || authStore.hasButton('manage_lark_bots')" class="add-btn" @click="openBotCreate">
              <el-icon><Plus /></el-icon>新增机器人
            </button>
          </div>
        </div>
        <div class="sec-body">
          <table class="tbl">
            <thead>
              <tr>
                <th style="width:160px">名称</th>
                <th>Webhook</th>
                <th>描述</th>
                <th style="width:180px">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="b in larkBots" :key="b.id">
                <td class="mono"><b>{{ b.name }}</b></td>
                <td class="mono webhook-cell">{{ b.webhook }}</td>
                <td>{{ b.description || '—' }}</td>
                <td>
                  <button class="act" @click="onTestBot(b)">测试</button>
                  <button v-if="authStore.isAdmin || authStore.hasButton('manage_lark_bots')" class="act" @click="openBotEdit(b)">编辑</button>
                  <button v-if="authStore.isAdmin || authStore.hasButton('manage_lark_bots')" class="act danger" @click="onDeleteBot(b)">删除</button>
                </td>
              </tr>
              <tr v-if="!larkBots.length">
                <td colspan="4" class="empty-row">还没有 Lark 机器人，点右上「+ 新增机器人」添加</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- 同步策略 -->
      <div v-if="tab === 'poll'" class="section">
        <div class="sec-head">
          <div class="sec-title">ArgoCD 同步轮询策略</div>
          <div class="sec-desc">触发 sync 后，后端每隔 N 秒查状态，直到 Synced+Healthy 或超时</div>
        </div>
        <div class="sec-body" v-loading="loading.cred">
          <div class="slider-row">
            <div class="lbl"><b>轮询间隔</b><div class="desc">过短给 ArgoCD 压力大；过长反馈慢</div></div>
            <el-slider v-model="gc.poll_interval_sec" :min="5" :max="60" :step="1" style="flex:1;margin:0 16px" />
            <div class="val mono">{{ gc.poll_interval_sec }}s</div>
          </div>
          <div class="slider-row">
            <div class="lbl"><b>最长等待</b><div class="desc">超时则标 partial/timeout 并发 Lark</div></div>
            <el-slider v-model="gc.poll_timeout_min" :min="1" :max="10" :step="1" style="flex:1;margin:0 16px" />
            <div class="val mono">{{ gc.poll_timeout_min }}min</div>
          </div>
          <div class="slider-row">
            <div class="lbl"><b>Git Push 重试</b><div class="desc">冲突 pull rebase 再推，最多 N 次</div></div>
            <el-slider v-model="gc.git_retry_count" :min="1" :max="10" :step="1" style="flex:1;margin:0 16px" />
            <div class="val mono">{{ gc.git_retry_count }} 次</div>
          </div>
          <div class="actions" v-if="authStore.isAdmin || authStore.hasButton('manage_global')">
            <el-button type="primary" @click="saveGlobal" :loading="saving.cred">保存</el-button>
          </div>
        </div>
      </div>

      <!-- MinIO 日志归档配置 -->
      <div v-if="tab === 'minio'" class="section">
        <div class="sec-head">
          <div class="sec-title">失败日志归档（MinIO）</div>
          <div class="sec-desc">
            发布失败时把「上一次崩溃前」pod 日志（最多 2000 行）异步上传到 MinIO，
            避免 pod 更新后日志丢失。<b>未配置时归档功能跳过，不影响发布主流程</b>。
          </div>
        </div>
        <div class="sec-body" v-loading="loading.cred">
          <div class="form-row">
            <div class="form-group full-width">
              <label>MinIO 端点 URL</label>
              <el-input v-model="gc.minio_endpoint" class="mono" />
            </div>
          </div>
          <div class="form-row">
            <div class="form-group">
              <label>Bucket</label>
              <el-input v-model="gc.minio_bucket" class="mono" />
            </div>
            <div class="form-group">
              <label>Region</label>
              <el-input v-model="gc.minio_region" class="mono" />
            </div>
          </div>
          <div class="form-row">
            <div class="form-group">
              <label>Access Key</label>
              <el-input v-model="gc.minio_access_key" class="mono" />
            </div>
            <div class="form-group">
              <label>Secret Key <span class="hint">已保存的留空表示不修改</span></label>
              <el-input v-model="gc.minio_secret_key" type="password" show-password
                placeholder="保存后此处不再回显，留空则保留原值" />
            </div>
          </div>
          <div class="minio-retention">
            <div class="retention-label">保留天数 <span class="hint">改后立即更新 bucket lifecycle 规则</span></div>
            <el-radio-group v-model="gc.minio_retention_days" class="retention-radio">
              <el-radio-button :value="7">7 天</el-radio-button>
              <el-radio-button :value="30">30 天</el-radio-button>
              <el-radio-button :value="90">90 天（推荐）</el-radio-button>
              <el-radio-button :value="180">180 天</el-radio-button>
            </el-radio-group>
          </div>
          <div class="actions" v-if="authStore.isAdmin || authStore.hasButton('manage_global')">
            <el-button @click="onTestMinIO" :loading="testing.minio">测试连接</el-button>
            <el-button type="primary" @click="saveGlobal" :loading="saving.cred">保存</el-button>
          </div>
        </div>
      </div>

      <!-- VM Agent · 版本接口 -->
      <div v-if="tab === 'agents'" class="section">
        <div class="sec-head">
          <div class="sec-title">VM 版本接口（list-version API）</div>
          <div class="sec-desc">
            所有 VM 项目共用一个接口拉版本号。token 可空。配 URL 后保存即可。
          </div>
        </div>
        <div class="sec-body" v-loading="loading.cred">
          <div class="form-row">
            <div class="form-group full-width">
              <label>API URL</label>
              <el-input v-model="gc.list_version_api" class="mono"
                placeholder="https://list-version.slileisure.com/list-version" />
            </div>
          </div>
          <div class="form-row">
            <div class="form-group full-width">
              <label>Token <span class="hint">已保存的留空表示不修改</span></label>
              <el-input v-model="gc.list_version_token" type="password" show-password
                placeholder="保存后此处不再回显" />
            </div>
          </div>
          <div class="actions" v-if="authStore.isAdmin || authStore.hasButton('manage_global')">
            <el-button type="primary" @click="saveGlobal" :loading="saving.cred">保存</el-button>
          </div>
        </div>

        <div class="sec-head" style="margin-top:24px;">
          <div class="sec-title-row">
            <div>
              <div class="sec-title">Deploy Agent（VM ansible 控制机）</div>
              <div class="sec-desc">
                VM 项目环境必须绑定一个 agent · agent 跑在 ansible 控制机上 · token 加密存 DB
              </div>
            </div>
            <button v-if="authStore.isAdmin || authStore.hasButton('manage_argocd')" class="add-btn" @click="openAgentCreate">
              <el-icon><Plus /></el-icon>新增 Agent
            </button>
          </div>
        </div>
        <div class="sec-body">
          <table class="tbl">
            <thead>
              <tr>
                <th style="width:160px">名称</th>
                <th>URL</th>
                <th>描述</th>
                <th style="width:160px">创建时间</th>
                <th style="width:200px">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="a in deployAgents" :key="a.id">
                <td class="mono"><b>{{ a.name }}</b></td>
                <td class="mono webhook-cell">{{ a.url }}</td>
                <td>{{ a.description || '—' }}</td>
                <td class="mono">{{ fmt(a.created_at) }}</td>
                <td>
                  <button class="act" @click="onTestAgent(a)">测试</button>
                  <button v-if="authStore.isAdmin || authStore.hasButton('manage_argocd')" class="act" @click="openAgentEdit(a)">编辑</button>
                  <button v-if="authStore.isAdmin || authStore.hasButton('manage_argocd')" class="act danger" @click="onDeleteAgent(a)">删除</button>
                </td>
              </tr>
              <tr v-if="!deployAgents.length">
                <td colspan="5" class="empty-row">还没有 agent，点右上「+ 新增 Agent」添加</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- Harbor 镜像仓库 -->
      <div v-if="tab === 'harbor'" class="section">
        <div class="sec-head">
          <div class="sec-title">Harbor 镜像仓库</div>
          <div class="sec-desc">
            发布时从 Harbor 拉取镜像 tag 列表给用户下拉选择，并在提交前实时校验所选 tag 是否存在。
            <b>使用 Robot 账号</b>（含 <code>$</code> 字符），跟你 Jenkins 流水线同款认证。
          </div>
        </div>
        <div class="sec-body" v-loading="loading.cred">
          <div class="form-row">
            <div class="form-group full-width">
              <label>Harbor URL</label>
              <el-input v-model="gc.harbor_url" class="mono" placeholder="https://harbor.slileisure.com" />
            </div>
          </div>
          <div class="form-row">
            <div class="form-group">
              <label>Robot 账号 <span class="hint">含 $ 字符，如 robot$public-pull</span></label>
              <el-input v-model="gc.harbor_user" class="mono" placeholder="robot$public-pull" />
            </div>
            <div class="form-group">
              <label>Robot 密码 <span class="hint">已保存的留空表示不修改</span></label>
              <el-input v-model="gc.harbor_token" type="password" show-password
                placeholder="保存后此处不再回显，留空则保留原值" />
            </div>
          </div>
          <div class="form-row">
            <div class="form-group full-width">
              <label>提交前校验 tag 是否存在
                <span class="hint">关闭后只用于下拉，不在提交时拦截"不存在的 tag"</span></label>
              <el-switch v-model="gc.harbor_verify_on_submit"
                active-text="开（推荐）" inactive-text="关" inline-prompt />
            </div>
          </div>
          <div class="actions" v-if="authStore.isAdmin || authStore.hasButton('manage_global')">
            <el-button @click="onTestHarbor" :loading="testing.harbor">测试连接</el-button>
            <el-button type="primary" @click="saveGlobal" :loading="saving.cred">保存</el-button>
          </div>
        </div>
      </div>

      <!-- 发布历史清理 -->
      <div v-if="tab === 'history'" class="section">
        <div class="sec-head">
          <div class="sec-title">发布历史自动清理</div>
          <div class="sec-desc">
            后端每天凌晨 2:00 自动删除 N 天前的发布记录（含归档日志的 DB 索引）。
            实际 MinIO 文件靠 lifecycle 自动过期。
          </div>
        </div>
        <div class="sec-body" v-loading="loading.cred">
          <div class="minio-retention">
            <div class="retention-label">保留时长 <span class="hint">改后立即生效，下次凌晨清理使用新值</span></div>
            <el-radio-group v-model="gc.history_retention_days" class="retention-radio">
              <el-radio-button :value="30">30 天</el-radio-button>
              <el-radio-button :value="90">90 天</el-radio-button>
              <el-radio-button :value="180">180 天（推荐）</el-radio-button>
              <el-radio-button :value="365">365 天</el-radio-button>
              <el-radio-button :value="0">永久（不清理）</el-radio-button>
            </el-radio-group>
          </div>
          <div class="form-row" style="margin-top:14px;">
            <div class="form-group full-width">
              <label>最后清理时间</label>
              <div class="info-text">
                {{ gc.last_history_cleanup_at ? formatDate(gc.last_history_cleanup_at) : '从未清理' }}
              </div>
            </div>
          </div>
          <div class="actions" v-if="authStore.isAdmin || authStore.hasButton('manage_global')">
            <el-button @click="openHistoryCleanupDialog">立即清理…</el-button>
            <el-button type="primary" @click="saveGlobal" :loading="saving.cred">保存</el-button>
          </div>
        </div>
      </div>

      <!-- Audit Logs (admin only) -->
      <div v-if="tab === 'audit'" class="section" style="padding:0;">
        <AuditLogPanel />
      </div>

      <!-- About -->
      <div v-if="tab === 'about'" class="section">
        <div class="sec-head">
          <div class="sec-title">Deploy Center</div>
          <div class="sec-desc">GitOps 发布控制台 V1</div>
        </div>
        <div class="sec-body">
          <div class="info-grid">
            <div class="info"><div class="l">后端版本</div><div class="v mono">v105</div></div>
            <div class="info"><div class="l">前端版本</div><div class="v mono">v114</div></div>
            <div class="info"><div class="l">数据库</div><div class="v">MySQL 8.0 · deploy_center</div></div>
          </div>
        </div>
      </div>
    </div>

    <!-- Deploy Agent 弹窗 -->
    <el-dialog v-model="agentDlg.vis" :title="agentDlg.isEdit ? '编辑 Deploy Agent' : '新增 Deploy Agent'" width="520px" :close-on-click-modal="false" :close-on-press-escape="false">
      <el-form :model="agentDlg.form" label-width="100px" label-position="top" size="default">
        <el-form-item label="名称 *">
          <el-input v-model="agentDlg.form.name" :disabled="agentDlg.isEdit" class="mono" placeholder="如: ansible-uat" />
        </el-form-item>
        <el-form-item label="URL *">
          <el-input v-model="agentDlg.form.url" class="mono" placeholder="https://10.x.x.x:8443" />
        </el-form-item>
        <el-form-item :label="agentDlg.isEdit ? 'Token（留空不更新）' : 'Token *'">
          <el-input v-model="agentDlg.form.token" type="password" show-password placeholder="agent 端配置的 bearer token" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="agentDlg.form.description" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="onTestAgentInDialog" :loading="testing.agentDlg">测试连接</el-button>
        <el-button @click="agentDlg.vis = false">取消</el-button>
        <el-button type="primary" @click="onSaveAgent">保存</el-button>
      </template>
    </el-dialog>

    <!-- ArgoCD 实例弹窗 -->
    <el-dialog v-model="argoDlg.vis" :title="argoDlg.isEdit ? '编辑 ArgoCD 实例' : '新增 ArgoCD 实例'" width="520px" :close-on-click-modal="false" :close-on-press-escape="false">
      <el-form :model="argoDlg.form" label-width="100px" label-position="top" size="default">
        <el-form-item label="名称 *">
          <el-input v-model="argoDlg.form.name" :disabled="argoDlg.isEdit" class="mono" placeholder="如: uat-cluster" />
        </el-form-item>
        <el-form-item label="URL *">
          <el-input v-model="argoDlg.form.url" class="mono" placeholder="http://argocd.xx" />
        </el-form-item>
        <el-form-item :label="argoDlg.isEdit ? 'Token（留空不更新）' : 'Token *'">
          <el-input v-model="argoDlg.form.token" type="password" show-password />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="argoDlg.form.description" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="onTestArgoInDialog" :loading="testing.argoDlg">测试连接</el-button>
        <el-button @click="argoDlg.vis = false">取消</el-button>
        <el-button type="primary" @click="onSaveArgo">保存</el-button>
      </template>
    </el-dialog>

    <!-- 用户弹窗 -->
    <el-dialog v-model="userDlg.vis" :title="userDlg.isEdit ? '编辑用户' : '新增本地用户'" width="520px" :close-on-click-modal="false" :close-on-press-escape="false">
      <el-form :model="userDlg.form" label-width="100px" label-position="top" size="default">
        <el-form-item label="用户名 *">
          <el-input v-model="userDlg.form.username" :disabled="userDlg.isEdit" class="mono" placeholder="如: zhangsan" />
        </el-form-item>
        <el-form-item v-if="!userDlg.isEdit" label="初始密码 *">
          <el-input v-model="userDlg.form.password" type="password" show-password placeholder="用户首次登录后请修改" />
        </el-form-item>
        <el-form-item label="显示名">
          <el-input v-model="userDlg.form.display_name" placeholder="如: 张三" />
        </el-form-item>
        <el-form-item label="角色">
          <el-select v-model="userDlg.form.role" style="width:100%">
            <el-option value="user" label="user - 普通用户" />
            <el-option value="admin" label="admin - 管理员" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="userDlg.vis = false">取消</el-button>
        <el-button type="primary" @click="onSaveUser">保存</el-button>
      </template>
    </el-dialog>

    <!-- 通知人弹窗 -->
    <el-dialog v-model="contactDlg.vis" :title="contactDlg.isEdit ? '编辑通知人' : '新增通知人'" width="560px" :close-on-click-modal="false" :close-on-press-escape="false">
      <!-- 新增时支持单个 / 批量切换；编辑时只能单个 -->
      <el-radio-group v-if="!contactDlg.isEdit" v-model="contactDlg.mode" size="small" style="margin-bottom:14px;">
        <el-radio-button value="single">单个</el-radio-button>
        <el-radio-button value="batch">批量</el-radio-button>
      </el-radio-group>

      <el-form v-if="contactDlg.isEdit || contactDlg.mode === 'single'"
               :model="contactDlg.form" label-width="100px" label-position="top" size="default">
        <el-form-item label="名称 *">
          <el-input v-model="contactDlg.form.name" :disabled="contactDlg.isEdit" placeholder="如: 张三 或 zhangsan（和操作人名字一致时自动匹配）" />
        </el-form-item>
        <el-form-item label="Lark ID">
          <el-input v-model="contactDlg.form.lark_id" class="mono" placeholder="ou_xxxxxxxxxxxxxxxxxxxx" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="contactDlg.form.remark" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>

      <el-form v-else label-width="100px" label-position="top" size="default">
        <el-form-item label="批量文本">
          <el-input v-model="contactDlg.batchText" type="textarea" :rows="10" class="mono"
            placeholder="每行一条，格式：名称,Lark ID&#10;&#10;示例：&#10;张三,ou_abc123...&#10;李四,ou_def456...&#10;cesar,ou_931577..."
          />
          <div class="batch-hint">
            格式：<code>名称,Lark ID</code> 每行一条 ·
            空行/以 <code>#</code> 开头的行忽略 ·
            已解析：<b>{{ batchParsed.valid.length }}</b> 条有效
            <span v-if="batchParsed.errors.length" style="color:var(--danger);margin-left:6px;">
              · <b>{{ batchParsed.errors.length }}</b> 条格式错误
            </span>
          </div>
          <div v-if="batchParsed.errors.length" class="batch-errors">
            格式错误的行：
            <div v-for="(e, i) in batchParsed.errors" :key="i">
              行 {{ e.line }}：<code>{{ e.raw }}</code>
            </div>
          </div>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="contactDlg.vis = false">取消</el-button>
        <el-button type="primary" @click="onSaveContact">
          {{ !contactDlg.isEdit && contactDlg.mode === 'batch'
              ? `批量新增 ${batchParsed.valid.length} 条`
              : '保存' }}
        </el-button>
      </template>
    </el-dialog>

    <!-- Lark 机器人弹窗 -->
    <el-dialog v-model="botDlg.vis" :title="botDlg.isEdit ? '编辑 Lark 机器人' : '新增 Lark 机器人'" width="560px" :close-on-click-modal="false" :close-on-press-escape="false">
      <el-form :model="botDlg.form" label-width="100px" label-position="top" size="default">
        <el-form-item label="名称 *">
          <el-input v-model="botDlg.form.name" :disabled="botDlg.isEdit" class="mono" placeholder="如: uat-deploy" />
        </el-form-item>
        <el-form-item label="Webhook *">
          <el-input v-model="botDlg.form.webhook" class="mono" placeholder="https://open.larksuite.com/open-apis/bot/v2/hook/..." />
        </el-form-item>
        <el-form-item :label="botDlg.isEdit ? 'Secret（留空不更新）' : 'Secret'">
          <el-input v-model="botDlg.form.secret" type="password" show-password placeholder="机器人开启签名校验才需要" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="botDlg.form.description" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="onTestBotInDialog" :loading="testing.botDlg">测试发送</el-button>
        <el-button @click="botDlg.vis = false">取消</el-button>
        <el-button type="primary" @click="onSaveBot">保存</el-button>
      </template>
    </el-dialog>

    <!-- GitLab 仓库弹窗 -->
    <el-dialog v-model="repoDlg.vis" :title="repoDlg.isEdit ? '编辑 GitLab 仓库' : '新增 GitLab 仓库'" width="580px" :close-on-click-modal="false" :close-on-press-escape="false">
      <el-form :model="repoDlg.form" label-width="100px" label-position="top" size="default">
        <el-form-item label="名称 *">
          <el-input v-model="repoDlg.form.name" :disabled="repoDlg.isEdit" class="mono" placeholder="如: uat-k8s-platform" />
        </el-form-item>
        <el-form-item label="仓库 URL *">
          <el-input v-model="repoDlg.form.repo_url" class="mono" placeholder="https://gitlab.xx/group/project.git" />
        </el-form-item>
        <el-form-item label="默认分支">
          <el-input v-model="repoDlg.form.default_branch" class="mono" placeholder="main" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="repoDlg.form.description" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="repoDlg.vis = false">取消</el-button>
        <el-button type="primary" @click="onSaveRepo">保存</el-button>
      </template>
    </el-dialog>

    <!-- 立即清理发布历史弹窗 -->
    <el-dialog v-model="historyCleanupDlg.vis" title="立即清理发布历史" width="500px"
      :close-on-click-modal="false" :close-on-press-escape="false">
      <el-form :model="historyCleanupDlg" label-width="100px" label-position="left" size="default">
        <el-form-item label="删除多少天前">
          <el-radio-group v-model="historyCleanupDlg.days" @change="onPreviewCleanup">
            <el-radio-button :value="30">30 天</el-radio-button>
            <el-radio-button :value="90">90 天</el-radio-button>
            <el-radio-button :value="180">180 天</el-radio-button>
            <el-radio-button :value="365">365 天</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="环境范围">
          <el-select v-model="historyCleanupDlg.envNames" style="width:100%" multiple
            collapse-tags collapse-tags-tooltip
            @change="onPreviewCleanup"
            placeholder="所有环境（留空）">
            <el-option v-for="e in historyCleanupDlg.envs" :key="e" :value="e" :label="e" />
          </el-select>
        </el-form-item>
      </el-form>
      <div class="cleanup-preview" :class="{ ok: historyCleanupDlg.preview === 0 }">
        <template v-if="historyCleanupDlg.previewing">正在统计…</template>
        <template v-else-if="historyCleanupDlg.preview === 0">没有符合条件的发布记录</template>
        <template v-else>预览将删除 <b>{{ historyCleanupDlg.preview }}</b> 条发布记录</template>
      </div>
      <template #footer>
        <el-button @click="historyCleanupDlg.vis = false">取消</el-button>
        <el-button type="danger" :disabled="!historyCleanupDlg.preview" :loading="historyCleanupDlg.running"
          @click="onRunCleanup">
          确认删除 {{ historyCleanupDlg.preview }} 条
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import dayjs from 'dayjs'
import {
  getGlobalConfig, updateGlobalConfig, testGitlab, testMinIO, testHarbor,
  previewHistoryCleanup, runHistoryCleanup, listProjectEnvs,
  listUsers, createUser, updateUser, toggleUser, resetUserPassword, deleteUser,
  listContacts, createContact, updateContact, deleteContact,
  listLarkBots, createLarkBot, updateLarkBot, deleteLarkBot, testLarkBot,
  listArgocdInstances, createArgocdInstance, updateArgocdInstance, deleteArgocdInstance, testArgocdInstance,
  listDeployAgents, createDeployAgent, updateDeployAgent, deleteDeployAgent, testDeployAgent,
  listGitlabRepos, createGitlabRepo, updateGitlabRepo, deleteGitlabRepo,
} from '../api'
import {
  Key, User, UserFilled, ChatLineRound, Bell, Folder, Connection, Timer, InfoFilled, ArrowRight, Lock, Box, Delete, Monitor
} from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'
import AuditLogPanel from '../components/AuditLogPanel.vue'
const authStore = useAuthStore()

// 有任一管理类按钮 或 admin → 能进 SystemSettings
const hasAnyManagePerm = computed(() =>
  authStore.isAdmin ||
  authStore.hasButton('manage_global') ||
  authStore.hasButton('manage_projects') ||
  authStore.hasButton('manage_argocd') ||
  authStore.hasButton('manage_lark_bots') ||
  authStore.hasButton('manage_contacts')
)

const tab = ref('overview')
const tabs = [
  { v: 'overview', label: '✦ 配置总览' },
  { v: 'cred', label: '全局凭证' },
  { v: 'gitlabrepos', label: 'GitLab 仓库' },
  { v: 'argocd', label: 'ArgoCD 实例' },
  { v: 'agents', label: 'VM Agent · 版本接口' },
  { v: 'harbor', label: 'Harbor 镜像仓库' },
  { v: 'larkbots', label: 'Lark 机器人' },
  { v: 'accounts', label: '账号管理' },
  { v: 'contacts', label: '通知人' },
  { v: 'poll', label: '同步策略' },
  { v: 'minio', label: '日志归档' },
  { v: 'history', label: '发布历史' },
  { v: 'audit', label: '📋 审计日志', adminOnly: true },
  { v: 'about', label: '关于' }
]
const visibleTabs = computed(() => tabs.filter(t => !t.adminOnly || authStore.isAdmin))

const gc = reactive({
  gitlab_url: '', gitlab_user: '', gitlab_email: '', gitlab_token: '',
  test_repo_path: '',
  deploy_center_base_url: '',
  lark_default_webhook: '', lark_default_secret: '',
  poll_interval_sec: 10, poll_timeout_min: 3, git_retry_count: 3,
  minio_endpoint: '', minio_bucket: '', minio_region: '',
  minio_access_key: '', minio_secret_key: '', minio_retention_days: 90,
  history_retention_days: 180, last_history_cleanup_at: '',
  harbor_url: '', harbor_user: '', harbor_token: '', harbor_verify_on_submit: true,
  // VM list-version API（拉版本号）：所有 VM 项目共用一个
  list_version_api: '', list_version_token: '',
})
const users = ref([])
const contacts = ref([])
const larkBots = ref([])
const argoInstances = ref([])
const gitlabRepos = ref([])
const deployAgents = ref([])
const loading = reactive({ cred: false })
const saving = reactive({ cred: false })
const testing = reactive({ git: false, argoDlg: false, botDlg: false, minio: false, harbor: false, agentDlg: false })

function fmt(s) { return s ? dayjs(s).format('YYYY-MM-DD HH:mm') : '' }

async function loadGlobal() {
  loading.cred = true
  try {
    const r = await getGlobalConfig()
    // 所有"密码/token"类字段在加载后必须重置成空，避免后端返的掩码 "••••••••"
    // 被用户无意中保存覆盖到 DB（曾经的真密码就被掩码顶掉了，导致后续认证全 401）
    Object.assign(gc, r, {
      gitlab_token: '',
      lark_default_secret: '',
      minio_secret_key: '',
      harbor_token: '',
      list_version_token: '',
    })
  } finally { loading.cred = false }
}
async function saveGlobal() {
  const payload = { ...gc }
  if (!payload.gitlab_token) delete payload.gitlab_token
  if (!payload.lark_default_secret) delete payload.lark_default_secret
  if (!payload.minio_secret_key) delete payload.minio_secret_key
  if (!payload.harbor_token) delete payload.harbor_token // 留空 = 不修改原值
  if (!payload.list_version_token) delete payload.list_version_token
  delete payload.last_history_cleanup_at // 只读字段，不允许更新
  saving.cred = true
  try {
    await updateGlobalConfig(payload)
    ElMessage.success('已保存')
    await loadGlobal()
  } finally { saving.cred = false }
}
async function onTestMinIO() {
  testing.minio = true
  try {
    await testMinIO({
      minio_endpoint: gc.minio_endpoint,
      minio_bucket: gc.minio_bucket,
      minio_access_key: gc.minio_access_key,
      minio_secret_key: gc.minio_secret_key || '',
      minio_region: gc.minio_region,
    })
    ElMessage.success('MinIO 连接 OK · bucket 已就绪 + lifecycle 已设置')
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || e.message || '测试失败')
  } finally { testing.minio = false }
}

async function onTestHarbor() {
  if (!gc.harbor_url || !gc.harbor_user) {
    ElMessage.warning('Harbor URL 和 User 必填'); return
  }
  testing.harbor = true
  try {
    await testHarbor({
      harbor_url: gc.harbor_url,
      harbor_user: gc.harbor_user,
      harbor_token: gc.harbor_token || '', // 空表示用 DB 已存的
    })
    ElMessage.success('Harbor 连接 OK · 凭证有效')
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || e.message || '测试失败')
  } finally { testing.harbor = false }
}

// ---- 发布历史立即清理 ----
const historyCleanupDlg = reactive({
  vis: false, days: 90, envNames: [],
  envs: [], preview: 0, previewing: false, running: false,
})
async function openHistoryCleanupDialog() {
  historyCleanupDlg.vis = true
  historyCleanupDlg.days = 90
  historyCleanupDlg.envNames = []
  // 拉环境列表
  try {
    const r = await listProjectEnvs()
    historyCleanupDlg.envs = (Array.isArray(r) ? r : []).map(e => e.name)
  } catch { historyCleanupDlg.envs = [] }
  await onPreviewCleanup()
}
async function onPreviewCleanup() {
  historyCleanupDlg.previewing = true
  try {
    const r = await previewHistoryCleanup({
      days: historyCleanupDlg.days,
      envs: (historyCleanupDlg.envNames || []).join(','),
    })
    historyCleanupDlg.preview = r.count || 0
  } catch (e) {
    ElMessage.error('预览失败：' + (e?.response?.data?.message || e.message))
    historyCleanupDlg.preview = 0
  } finally { historyCleanupDlg.previewing = false }
}
async function onRunCleanup() {
  historyCleanupDlg.running = true
  try {
    const r = await runHistoryCleanup({
      days: historyCleanupDlg.days,
      env_names: historyCleanupDlg.envNames || [],
    })
    ElMessage.success(`已删除 ${r.deleted} 条发布记录`)
    historyCleanupDlg.vis = false
    await loadGlobal()
  } catch (e) {
    ElMessage.error('清理失败：' + (e?.response?.data?.message || e.message))
  } finally { historyCleanupDlg.running = false }
}
function formatDate(s) {
  if (!s) return '—'
  return dayjs(s).format('YYYY-MM-DD HH:mm:ss')
}
async function onTestGit() {
  testing.git = true
  try {
    const r = await testGitlab({
      gitlab_url: gc.gitlab_url,
      gitlab_user: gc.gitlab_user,
      gitlab_token: gc.gitlab_token || '',
      test_repo_path: gc.test_repo_path || '',
    })
    if (r?.method === 'git') {
      ElMessage.success(`GitLab 连通 OK · HEAD: ${r.head}`)
    } else {
      ElMessage.success('GitLab API 可访问，token 有效')
    }
  } finally { testing.git = false }
}

// === ArgoCD 实例 ===
const argoDlg = reactive({ vis: false, isEdit: false, editingID: null, form: { name: '', url: '', token: '', description: '' } })
async function loadArgo() { argoInstances.value = (await listArgocdInstances()) || [] }
function openArgoCreate() {
  argoDlg.isEdit = false; argoDlg.editingID = null
  Object.assign(argoDlg.form, { name: '', url: '', token: '', description: '' })
  argoDlg.vis = true
}
function openArgoEdit(a) {
  argoDlg.isEdit = true; argoDlg.editingID = a.id
  Object.assign(argoDlg.form, { name: a.name, url: a.url, token: '', description: a.description || '' })
  argoDlg.vis = true
}
async function onSaveArgo() {
  if (!argoDlg.isEdit && !argoDlg.form.name.trim()) { ElMessage.warning('名称必填'); return }
  if (!argoDlg.form.url.trim()) { ElMessage.warning('URL 必填'); return }
  if (!argoDlg.isEdit && !argoDlg.form.token) { ElMessage.warning('Token 必填'); return }
  if (argoDlg.isEdit) await updateArgocdInstance(argoDlg.editingID, argoDlg.form)
  else await createArgocdInstance(argoDlg.form)
  ElMessage.success('已保存')
  argoDlg.vis = false
  await loadArgo()
}
async function onTestArgo(a) {
  try {
    // 列表里点测试：用 ID，token 为空后端 fallback DB
    const r = await testArgocdInstance({ id: a.id })
    ElMessage.success(`连通 OK · version=${r.version}`)
  } catch (_) {}
}
// 弹窗内「测试连接」：用当前 form 值（未保存也能测）
async function onTestArgoInDialog() {
  if (!argoDlg.form.url) { ElMessage.warning('URL 必填'); return }
  testing.argoDlg = true
  try {
    const body = {
      url: argoDlg.form.url,
      token: argoDlg.form.token || '',
    }
    // 编辑模式：token 留空时让后端 fallback DB
    if (argoDlg.isEdit && argoDlg.editingID) body.id = argoDlg.editingID
    const r = await testArgocdInstance(body)
    ElMessage.success(`连通 OK · version=${r.version}`)
  } catch (_) {} finally { testing.argoDlg = false }
}
async function onDeleteArgo(a) {
  try { await ElMessageBox.confirm(`确认删除 ArgoCD 实例「${a.name}」？`, '删除确认', { type: 'warning', closeOnClickModal: false, closeOnPressEscape: false }) }
  catch { return }
  try { await deleteArgocdInstance(a.id); ElMessage.success('已删除'); await loadArgo() } catch (_) {}
}

// === 用户（登录）===
const userDlg = reactive({ vis: false, isEdit: false, editingID: null, form: { username: '', password: '', display_name: '', role: 'user' } })
async function loadUsers() {
  if (!authStore.isAdmin) return
  users.value = (await listUsers()) || []
}
function openUserCreate() {
  userDlg.isEdit = false; userDlg.editingID = null
  Object.assign(userDlg.form, { username: '', password: '', display_name: '', role: 'user' })
  userDlg.vis = true
}
function openUserEdit(u) {
  userDlg.isEdit = true; userDlg.editingID = u.id
  Object.assign(userDlg.form, { username: u.username, password: '', display_name: u.display_name, role: u.role })
  userDlg.vis = true
}
async function onSaveUser() {
  if (!userDlg.isEdit) {
    if (!userDlg.form.username.trim()) { ElMessage.warning('用户名必填'); return }
    if (!userDlg.form.password) { ElMessage.warning('初始密码必填'); return }
    await createUser(userDlg.form)
  } else {
    await updateUser(userDlg.editingID, { display_name: userDlg.form.display_name, role: userDlg.form.role })
  }
  ElMessage.success('已保存')
  userDlg.vis = false
  await loadUsers()
}
async function onDeleteUser(u) {
  try { await ElMessageBox.confirm(`确认删除用户「${u.username}」？`, '删除确认', { type: 'warning', closeOnClickModal: false, closeOnPressEscape: false }) }
  catch { return }
  await deleteUser(u.id)
  ElMessage.success('已删除')
  await loadUsers()
}
async function onToggleUser(u) {
  await toggleUser(u.id)
  ElMessage.success(u.status === 1 ? '已禁用' : '已启用')
  await loadUsers()
}
async function onResetPwd(u) {
  try {
    const { value } = await ElMessageBox.prompt(`为「${u.username}」设置新密码`, '重置密码', {
      inputType: 'password', inputPlaceholder: '至少 6 位',
      inputValidator: v => !!v && v.length >= 6 || '密码至少 6 位',
    })
    await resetUserPassword(u.id, value)
    ElMessage.success('密码已重置')
  } catch (_) { /* 取消 */ }
}

// === 通知人（Lark 艾特） ===
const contactDlg = reactive({
  vis: false, isEdit: false, editingID: null,
  mode: 'single', // 'single' | 'batch'，仅新增时可切换
  form: { name: '', lark_id: '', remark: '' },
  batchText: '',
})
async function loadContacts() { contacts.value = (await listContacts()) || [] }
function openContactCreate() {
  contactDlg.isEdit = false; contactDlg.editingID = null
  contactDlg.mode = 'single'
  Object.assign(contactDlg.form, { name: '', lark_id: '', remark: '' })
  contactDlg.batchText = ''
  contactDlg.vis = true
}
function openContactEdit(c) {
  contactDlg.isEdit = true; contactDlg.editingID = c.id
  contactDlg.mode = 'single'
  Object.assign(contactDlg.form, { name: c.name, lark_id: c.lark_id, remark: c.remark })
  contactDlg.vis = true
}

// 解析批量文本。每行格式：name,lark_id[,remark]
// 逗号可以是中文逗号；空行/以 # 开头忽略。
const batchParsed = computed(() => {
  const valid = []
  const errors = []
  const lines = (contactDlg.batchText || '').split('\n')
  lines.forEach((raw, idx) => {
    const line = raw.trim()
    if (!line || line.startsWith('#')) return
    const parts = line.split(/[,，]/).map(s => s.trim())
    if (parts.length < 2 || !parts[0] || !parts[1]) {
      errors.push({ line: idx + 1, raw })
      return
    }
    valid.push({
      name: parts[0],
      lark_id: parts[1],
      remark: parts[2] || '',
    })
  })
  return { valid, errors }
})

async function onSaveContact() {
  // 编辑：单条更新
  if (contactDlg.isEdit) {
    await updateContact(contactDlg.editingID, contactDlg.form)
    ElMessage.success('已保存')
    contactDlg.vis = false
    await loadContacts()
    return
  }
  // 新增 · 单个
  if (contactDlg.mode === 'single') {
    if (!contactDlg.form.name.trim()) { ElMessage.warning('名称必填'); return }
    await createContact(contactDlg.form)
    ElMessage.success('已保存')
    contactDlg.vis = false
    await loadContacts()
    return
  }
  // 新增 · 批量：按行解析 + 逐条创建，统计成功/失败
  const { valid, errors } = batchParsed.value
  if (!valid.length) {
    ElMessage.warning('没有可创建的行，请检查格式：名称,Lark ID 每行一条')
    return
  }
  if (errors.length) {
    try {
      await ElMessageBox.confirm(
        `有 ${errors.length} 条格式错误（将跳过）；${valid.length} 条会被创建，是否继续？`,
        '批量创建确认',
        { type: 'warning', closeOnClickModal: false, closeOnPressEscape: false }
      )
    } catch { return }
  }
  let ok = 0, fail = 0
  const failMsgs = []
  for (const item of valid) {
    try {
      await createContact(item)
      ok++
    } catch (e) {
      fail++
      failMsgs.push(`${item.name}: ${e?.message || '失败'}`)
    }
  }
  if (fail === 0) {
    ElMessage.success(`已批量新增 ${ok} 条`)
  } else {
    ElMessage.warning(`成功 ${ok} 条 · 失败 ${fail} 条（可能重名已存在）`)
    console.warn('batch contact failures:', failMsgs)
  }
  contactDlg.vis = false
  await loadContacts()
}
async function onDeleteContact(c) {
  try { await ElMessageBox.confirm(`确认删除通知人「${c.name}」？`, '删除确认', { type: 'warning', closeOnClickModal: false, closeOnPressEscape: false }) }
  catch { return }
  await deleteContact(c.id)
  ElMessage.success('已删除')
  await loadContacts()
}

// === Lark 机器人 ===
const botDlg = reactive({ vis: false, isEdit: false, editingID: null, form: { name: '', webhook: '', secret: '', description: '' } })
async function loadBots() { larkBots.value = (await listLarkBots()) || [] }
function openBotCreate() {
  botDlg.isEdit = false; botDlg.editingID = null
  Object.assign(botDlg.form, { name: '', webhook: '', secret: '', description: '' })
  botDlg.vis = true
}
function openBotEdit(b) {
  botDlg.isEdit = true; botDlg.editingID = b.id
  Object.assign(botDlg.form, { name: b.name, webhook: b.webhook, secret: '', description: b.description || '' })
  botDlg.vis = true
}
async function onSaveBot() {
  if (!botDlg.isEdit && !botDlg.form.name.trim()) { ElMessage.warning('名称必填'); return }
  if (!botDlg.form.webhook.trim()) { ElMessage.warning('Webhook 必填'); return }
  const payload = { ...botDlg.form }
  if (botDlg.isEdit && !payload.secret) delete payload.secret
  if (botDlg.isEdit) await updateLarkBot(botDlg.editingID, payload)
  else await createLarkBot(payload)
  ElMessage.success('已保存')
  botDlg.vis = false
  await loadBots()
}
async function onDeleteBot(b) {
  try { await ElMessageBox.confirm(`确认删除 Lark 机器人「${b.name}」？被项目环境引用时会失败。`, '删除确认', { type: 'warning', closeOnClickModal: false, closeOnPressEscape: false }) }
  catch { return }
  await deleteLarkBot(b.id)
  ElMessage.success('已删除')
  await loadBots()
}
async function onTestBot(b) {
  try {
    await testLarkBot({ id: b.id })
    ElMessage.success(`已发送测试消息到「${b.name}」`)
  } catch (_) {}
}
// 弹窗里「测试发送」：用当前 form 值，未保存也能测
async function onTestBotInDialog() {
  if (!botDlg.form.webhook) { ElMessage.warning('Webhook 必填'); return }
  testing.botDlg = true
  try {
    const body = {
      webhook: botDlg.form.webhook,
      secret: botDlg.form.secret || '',
    }
    if (botDlg.isEdit && botDlg.editingID) body.id = botDlg.editingID
    await testLarkBot(body)
    ElMessage.success('测试消息已发送，请查看 Lark')
  } catch (_) {} finally { testing.botDlg = false }
}

// === GitLab 仓库 ===
const repoDlg = reactive({ vis: false, isEdit: false, editingID: null, form: { name: '', repo_url: '', default_branch: 'main', description: '' } })
async function loadRepos() { gitlabRepos.value = (await listGitlabRepos()) || [] }
function openRepoCreate() {
  repoDlg.isEdit = false; repoDlg.editingID = null
  Object.assign(repoDlg.form, { name: '', repo_url: '', default_branch: 'main', description: '' })
  repoDlg.vis = true
}
function openRepoEdit(g) {
  repoDlg.isEdit = true; repoDlg.editingID = g.id
  Object.assign(repoDlg.form, { name: g.name, repo_url: g.repo_url, default_branch: g.default_branch, description: g.description || '' })
  repoDlg.vis = true
}
async function onSaveRepo() {
  if (!repoDlg.form.name.trim()) { ElMessage.warning('名称必填'); return }
  if (!repoDlg.form.repo_url.trim()) { ElMessage.warning('仓库 URL 必填'); return }
  const payload = { ...repoDlg.form }
  if (repoDlg.isEdit) await updateGitlabRepo(repoDlg.editingID, payload)
  else await createGitlabRepo(payload)
  ElMessage.success('已保存')
  repoDlg.vis = false
  await loadRepos()
}
async function onDeleteRepo(g) {
  try { await ElMessageBox.confirm(`确认删除 GitLab 仓库「${g.name}」？被项目环境引用时会失败。`, '删除确认', { type: 'warning', closeOnClickModal: false, closeOnPressEscape: false }) }
  catch { return }
  await deleteGitlabRepo(g.id)
  ElMessage.success('已删除')
  await loadRepos()
}

// === Deploy Agent (VM) ===
const agentDlg = reactive({ vis: false, isEdit: false, editingID: null, form: { name: '', url: '', token: '', description: '' } })
async function loadAgents() { deployAgents.value = (await listDeployAgents()) || [] }
function openAgentCreate() {
  agentDlg.isEdit = false; agentDlg.editingID = null
  Object.assign(agentDlg.form, { name: '', url: '', token: '', description: '' })
  agentDlg.vis = true
}
function openAgentEdit(a) {
  agentDlg.isEdit = true; agentDlg.editingID = a.id
  Object.assign(agentDlg.form, { name: a.name, url: a.url, token: '', description: a.description || '' })
  agentDlg.vis = true
}
async function onSaveAgent() {
  if (!agentDlg.isEdit && !agentDlg.form.name.trim()) { ElMessage.warning('名称必填'); return }
  if (!agentDlg.form.url.trim()) { ElMessage.warning('URL 必填'); return }
  if (!agentDlg.isEdit && !agentDlg.form.token) { ElMessage.warning('Token 必填'); return }
  const payload = { ...agentDlg.form }
  if (agentDlg.isEdit && !payload.token) delete payload.token
  if (agentDlg.isEdit) await updateDeployAgent(agentDlg.editingID, payload)
  else await createDeployAgent(payload)
  ElMessage.success('已保存')
  agentDlg.vis = false
  await loadAgents()
}
async function onDeleteAgent(a) {
  try { await ElMessageBox.confirm(`确认删除 Agent「${a.name}」？被 VM 项目环境引用时会失败。`, '删除确认', { type: 'warning', closeOnClickModal: false, closeOnPressEscape: false }) }
  catch { return }
  await deleteDeployAgent(a.id)
  ElMessage.success('已删除')
  await loadAgents()
}
async function onTestAgent(a) {
  try {
    const r = await testDeployAgent({ id: a.id })
    ElMessage.success(`Agent OK · v${r.version} · max_concurrent=${r.max_concurrent}`)
  } catch (_) {}
}
async function onTestAgentInDialog() {
  if (!agentDlg.form.url) { ElMessage.warning('URL 必填'); return }
  testing.agentDlg = true
  try {
    const body = { url: agentDlg.form.url, token: agentDlg.form.token || '' }
    if (agentDlg.isEdit && agentDlg.editingID) body.id = agentDlg.editingID
    const r = await testDeployAgent(body)
    ElMessage.success(`Agent OK · v${r.version}`)
  } catch (_) {} finally { testing.agentDlg = false }
}

// === Overview 状态徽章 ===
const ovStatus = computed(() => ({
  cred: gc.gitlab_url && gc.gitlab_user ? { kind: 'ok', text: '已配置' } : { kind: 'miss', text: '未配置' },
  accounts: users.value.length ? { kind: 'count', text: `${users.value.length} 个用户` } : { kind: 'miss', text: '未配置' },
  contacts: contacts.value.length ? { kind: 'count', text: `${contacts.value.length} 个通知人` } : { kind: 'miss', text: '未配置' },
  larkbots: larkBots.value.length ? { kind: 'count', text: `${larkBots.value.length} 个机器人` } : { kind: 'miss', text: '未配置' },
  gitlabrepos: gitlabRepos.value.length ? { kind: 'count', text: `${gitlabRepos.value.length} 个仓库` } : { kind: 'miss', text: '未配置' },
  argocd: argoInstances.value.length ? { kind: 'count', text: `${argoInstances.value.length} 个实例` } : { kind: 'miss', text: '未配置' },
  agents: deployAgents.value.length
    ? { kind: 'count', text: `${deployAgents.value.length} 个 agent${gc.list_version_api ? '' : ' · 缺版本 API'}` }
    : { kind: 'miss', text: '未配置' },
  poll: gc.poll_interval_sec ? { kind: 'ok', text: '已配置' } : { kind: 'miss', text: '未配置' },
  minio: gc.minio_endpoint && gc.minio_access_key
    ? { kind: 'ok', text: '已配置' } : { kind: 'miss', text: '未配置' },
  history: gc.history_retention_days === 0
    ? { kind: 'count', text: '永久' }
    : { kind: 'ok', text: `${gc.history_retention_days || 180} 天` },
  harbor: gc.harbor_url && gc.harbor_user
    ? { kind: 'ok', text: '已配置' } : { kind: 'miss', text: '未配置' },
}))

// 左侧 rail 每个 tab 右侧的徽章（精简版：只显示数量或未配置）
function statusBadge(v) {
  const s = ovStatus.value[v]
  if (!s) return null
  return s
}

// Overview 打开时拉所有 resource
async function loadOverview() {
  await Promise.all([
    loadGlobal(),
    loadRepos(),
    loadArgo(),
    loadBots(),
    loadContacts(),
    loadAgents(),
    authStore.isAdmin ? loadUsers() : Promise.resolve(),
  ])
}

watch(tab, (t) => {
  if (t === 'overview') loadOverview()
  else if (t === 'argocd') loadArgo()
  else if (t === 'gitlabrepos') loadRepos()
  else if (t === 'accounts') loadUsers()
  else if (t === 'contacts') loadContacts()
  else if (t === 'larkbots') loadBots()
  else if (t === 'agents') { loadAgents(); loadGlobal() }
  else if (['cred', 'lark', 'poll'].includes(t)) loadGlobal()
})

onMounted(loadOverview)
</script>

<style scoped>
.ss { display: grid; grid-template-columns: 200px 1fr; gap: 0; height: calc(100vh - 120px); }
.rail { background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius) 0 0 var(--radius); padding: 16px 0; border-right: none; }
.rail-title { padding: 0 16px; font-size: 10px; text-transform: uppercase; letter-spacing: 1px; color: var(--text-3); font-weight: 600; margin-bottom: 8px; }
.rail-item { padding: 9px 16px; cursor: pointer; font-size: 13px; color: var(--text-2); border-left: 3px solid transparent; display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.rail-item:hover { background: var(--bg-hover); color: var(--text); }
.rail-item.active { background: var(--primary-bg); border-left-color: var(--primary); color: var(--primary); font-weight: 600; }

.rail-badge {
  font: 500 10px var(--mono);
  padding: 1px 6px; border-radius: 99px;
  flex-shrink: 0;
}
.rail-badge.ok { background: #ecfdf5; color: #059669; }
.rail-badge.count { background: #eff6ff; color: #1d4ed8; }
.rail-badge.miss { background: #fef2f2; color: #dc2626; }

.pane { flex: 1; overflow: auto; padding: 18px 24px; background: var(--bg-card); border: 1px solid var(--border); border-radius: 0 var(--radius) var(--radius) 0; }
.section { margin-bottom: 14px; }
.sec-head { padding-bottom: 14px; border-bottom: 1px solid var(--border-soft); margin-bottom: 16px; }
.sec-title-row { display: flex; justify-content: space-between; align-items: flex-start; }
.sec-title { font-size: 14px; font-weight: 600; color: var(--text); }
.sec-desc { font-size: 11.5px; color: var(--text-3); margin-top: 3px; }
.actions { margin-top: 14px; text-align: right; }
.slider-row { display: flex; align-items: center; padding: 12px 0; border-bottom: 1px dashed var(--border-soft); }
.lbl { width: 180px; font-size: 12.5px; }
.lbl b { display: block; }
.lbl .desc { color: var(--text-3); font-size: 11px; margin-top: 2px; }
.val { width: 80px; text-align: right; color: var(--primary); font-weight: 600; }
.info-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; }
.info { background: var(--bg-input); border: 1px solid var(--border-soft); border-radius: 6px; padding: 12px; }
.info .l { font-size: 11px; color: var(--text-3); text-transform: uppercase; letter-spacing: .5px; }
.info .v { font-size: 13px; margin-top: 4px; font-weight: 500; }

/* 通用表格 */
.tbl { width: 100%; border-collapse: collapse; font-size: 13px; }
.tbl th { background: var(--bg-input); color: var(--text-2); text-align: left; padding: 10px 12px; font-size: 11px; text-transform: uppercase; letter-spacing: .5px; font-weight: 600; border-bottom: 1px solid var(--border); }
.tbl td { padding: 10px 12px; border-bottom: 1px solid var(--border-soft); }
.tbl tr:hover td { background: var(--bg-hover); }
.tbl .mono { font-family: var(--mono); font-size: 12px; }
.tbl .empty-row { text-align: center; color: var(--text-3); padding: 40px 20px; font-size: 12.5px; }

.add-btn { display: flex; align-items: center; gap: 4px; background: var(--primary); color: #fff; border: none; padding: 7px 14px; border-radius: 5px; font: 500 12.5px var(--body); cursor: pointer; }
.add-btn:hover { background: var(--primary-dark); }
.add-btn .el-icon { font-size: 14px; }

.act { background: transparent; border: 1px solid var(--border); color: var(--text-2); padding: 4px 10px; border-radius: 4px; cursor: pointer; font-size: 11.5px; font-family: var(--body); margin-right: 4px; }
.act:hover { border-color: var(--primary); color: var(--primary); }
.act.danger:hover { border-color: var(--danger); color: var(--danger); }

.webhook-cell { max-width: 360px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.role-tag { display: inline-block; padding: 2px 8px; border-radius: 99px; font: 500 11px var(--mono); }
.role-tag.admin { background: #fef2f2; color: #dc2626; }
.role-tag.user { background: #eff6ff; color: #1d4ed8; }

.src-tag { display: inline-block; padding: 2px 8px; border-radius: 99px; font: 500 11px var(--mono); }
.src-tag.local { background: #f3f4f6; color: #4b5563; }
.src-tag.portal { background: #ecfdf5; color: #059669; }

.status-on { color: var(--success); font-size: 12px; font-weight: 500; }
.status-off { color: var(--text-3); font-size: 12px; }

/* ===== Overview 卡片 ===== */
.ov-group { margin-bottom: 24px; }
.ov-group-title { font: 600 13px var(--body); color: var(--text-2); margin-bottom: 10px; padding-left: 2px; }
.ov-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 10px; }
.ov-card {
  display: flex; align-items: center; gap: 12px;
  background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius);
  padding: 14px; cursor: pointer; transition: all .15s;
}
.ov-card:hover { border-color: var(--primary); background: #fbfdff; transform: translateY(-1px); box-shadow: 0 4px 12px rgba(24,144,255,.08); }
.ov-icon { width: 40px; height: 40px; border-radius: 8px; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.ov-icon .el-icon { font-size: 20px; }
.ov-main { flex: 1; min-width: 0; }
.ov-title { font: 600 14px var(--body); color: var(--text); margin-bottom: 2px; }
.ov-desc { font: 400 11.5px var(--body); color: var(--text-3); line-height: 1.5; }
.ov-right { display: flex; align-items: center; gap: 10px; flex-shrink: 0; }
.ov-arrow { color: var(--text-3); font-size: 14px; }
.ov-card:hover .ov-arrow { color: var(--primary); }

.ov-badge {
  font: 500 11px var(--mono);
  padding: 2px 8px; border-radius: 99px;
}
.ov-badge.ok { background: #ecfdf5; color: #059669; }
.ov-badge.count { background: #eff6ff; color: #1d4ed8; }
.ov-badge.miss { background: #fef2f2; color: #dc2626; }

/* 批量新增通知人提示 */
.batch-hint {
  margin-top: 8px;
  font-size: 12px; color: var(--text-2);
  line-height: 1.7;
}
.batch-hint code {
  font-family: var(--mono);
  background: var(--bg-hover);
  padding: 1px 6px; border-radius: 3px;
  color: var(--text);
  font-size: 11.5px;
}
.batch-hint b { color: var(--text); font-family: var(--mono); font-weight: 700; }
.batch-errors {
  margin-top: 6px; padding: 8px 12px;
  background: #fef2f2; border: 1px solid #fecaca; border-radius: 4px;
  font-size: 11.5px; color: #991b1b; line-height: 1.7;
  max-height: 120px; overflow-y: auto;
}
.batch-errors code { font-family: var(--mono); background: rgba(0,0,0,.05); padding: 1px 4px; border-radius: 3px; }

/* 非 admin 访问提示 */
.no-perm {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  min-height: 60vh; color: var(--text-2);
}
.no-perm .np-icon { font-size: 48px; color: #f59e0b; margin-bottom: 16px; }
.no-perm .np-title { font: 600 18px var(--body); color: var(--text); margin-bottom: 8px; }
.no-perm .np-desc { font-size: 13.5px; color: var(--text-3); }

/* MinIO 保留天数：label 一行 / 按钮组一行 */
.minio-retention { margin: 16px 0 8px; }
.minio-retention .retention-label {
  display: block; margin-bottom: 8px;
  font-size: 13px; color: var(--text-2); font-weight: 500;
}
.minio-retention .retention-label .hint {
  color: var(--text-3); font-weight: 400; margin-left: 6px; font-size: 12px;
}
.minio-retention .retention-radio { display: block; }

.cleanup-preview {
  margin-top: 16px; padding: 10px 14px;
  background: #fef3c7; border: 1px solid #fde68a; border-radius: 6px;
  color: #92400e; font-size: 13px;
}
.cleanup-preview b { font-family: 'Fira Code', monospace; color: #b91c1c; }
.cleanup-preview.ok { background: #ecfdf5; border-color: #a7f3d0; color: #047857; }

.info-text {
  font-size: 13px; color: var(--text-2); padding: 6px 10px;
  background: var(--bg-input); border-radius: 4px; font-family: 'Fira Code', monospace;
}
</style>
