<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">域名</span>
      <div v-if="tab === 'records'">
        <el-button v-if="canRecords && selected.length && statusView !== 'ignored'" type="warning" :icon="EditPen" @click="openBulk">批量设置（{{ selected.length }}）</el-button>
        <el-button v-if="canRecords && selected.length && statusView !== 'ignored'" :icon="Hide" @click="openIgnore(selected)">批量忽略（{{ selected.length }}）</el-button>
        <el-button v-if="canRecords && selected.length && statusView === 'ignored'" type="success" :icon="View" @click="doUnignore(selected)">取消忽略（{{ selected.length }}）</el-button>
        <el-button v-if="selected.length" :icon="Close" @click="clearSel">清空选择（{{ selected.length }}）</el-button>
        <el-button :icon="Download" @click="exportCsv">导出Excel</el-button>
        <el-button :icon="Connection" @click="openRules">源站映射</el-button>
        <el-button v-if="canRecords" type="warning" :icon="Operation" @click="openAssign">批量分配</el-button>
        <el-tooltip v-if="canManage" content="按域名匹配 K8s 入口(Istio VirtualService/Ingress/HTTPRoute)自动填模块(VS名=后端去-svc)，只补空的，不覆盖手填" placement="top">
          <el-button :icon="MagicStick" :loading="autoLinking" @click="autoLink">K8s自动关联模块</el-button>
        </el-tooltip>
        <el-tooltip v-if="canSync" content="只刷库里已有域名的 DNS 解析；发现新域名请去「DNS 记录」页点右上「从数据源同步」" placement="top">
          <el-button :icon="Refresh" :loading="syncingAll" @click="syncAll">刷新已有域名的解析</el-button>
        </el-tooltip>
        <el-button v-if="canRecords" type="primary" :icon="Plus" @click="openAdd">录入解析</el-button>
      </div>
      <div v-else>
        <el-dropdown v-if="canManage && domSelected.length" style="margin-right:8px" @command="(v) => setDomStatus(domSelected, v === '__clear__' ? '' : v)">
          <el-button type="primary" :icon="EditPen">批量设使用状态（{{ domSelected.length }}）<el-icon class="el-icon--right"><ArrowDown /></el-icon></el-button>
          <template #dropdown><el-dropdown-menu>
            <el-dropdown-item v-for="s in app.domainStatuses" :key="s.id" :command="s.label"><span :style="{ display:'inline-block', width:'8px', height:'8px', borderRadius:'50%', background: s.color, marginRight:'6px' }" />{{ s.label }}</el-dropdown-item>
            <el-dropdown-item command="__clear__" divided>清除状态</el-dropdown-item>
          </el-dropdown-menu></template>
        </el-dropdown>
        <el-button v-if="canManage && expiredDomains.length" type="warning" :icon="Hide" @click="ignoreExpired">一键忽略已过期（{{ expiredDomains.length }}）</el-button>
        <el-tooltip v-if="canSync" content="只刷注册到期：数据源域名由同步维护(跳过)，仅查手动录入域名(RDAP→WHOIS+重试)；证书到期见「到期巡检」" placement="top">
          <el-button :icon="Refresh" :loading="refreshingAll" @click="refreshAllDom">刷新到期</el-button>
        </el-tooltip>
        <el-button v-if="canManage" type="warning" :icon="RefreshRight" @click="openBatchRenew">批量续费</el-button>
        <el-button :icon="Tickets" @click="openRenewLog">续费记录</el-button>
        <el-button v-if="canManage" type="primary" :icon="Plus" @click="openAddDomain">录入域名</el-button>
      </div>
    </div>

    <el-tabs v-model="tab" class="lvl-tabs">
      <el-tab-pane label="业务域名" name="records" />
      <el-tab-pane label="主域名" name="domains" />
    </el-tabs>

    <!-- ================= 业务域名（解析层，一行一个 FQDN） ================= -->
    <template v-if="tab === 'records'">
    <el-card shadow="never" style="margin-bottom:12px">
      <div class="filter">
        <el-input v-model="f.keyword" placeholder="搜索 域名/模块/回源/IP" clearable :prefix-icon="Search" style="width:210px" @keyup.enter="doSearch" />
        <el-select v-model="f.domain" clearable filterable placeholder="域名" style="width:200px">
          <el-option v-for="d in domainOptions" :key="d" :label="d" :value="d" />
        </el-select>
        <el-select v-model="f.project" clearable placeholder="项目" style="width:150px">
          <el-option label="（未分配项目）" value="__none__" />
          <el-option v-for="p in projectOptions" :key="p" :label="p" :value="p" />
        </el-select>
        <el-select v-model="f.env" clearable placeholder="环境" style="width:120px">
          <el-option label="（未分配环境）" value="__none__" />
          <el-option v-for="e in envOptions" :key="e" :label="e" :value="e" />
        </el-select>
        <el-select v-model="f.module" clearable placeholder="模块" style="width:130px">
          <el-option label="（未分配模块）" value="__none__" />
          <el-option v-for="m in moduleOptions" :key="m" :label="m" :value="m" />
        </el-select>
        <el-select v-model="f.source" clearable placeholder="数据源" style="width:130px">
          <el-option v-for="r in registrars" :key="r.id" :label="r.name" :value="r.name" />
        </el-select>
        <el-select v-model="f.pstatus" clearable placeholder="项目状态" style="width:140px">
          <el-option v-for="s in app.projectStatuses" :key="s.id" :label="s.label" :value="s.label">
            <span :style="{ display:'inline-block', width:'8px', height:'8px', borderRadius:'50%', background: s.color, marginRight:'6px' }" />{{ s.label }}
          </el-option>
        </el-select>
        <el-select v-model="statusView" style="width:130px" @change="onStatusChange">
          <el-option label="正常" value="normal" />
          <el-option label="已忽略" value="ignored" />
          <el-option label="已移出/过户" value="stale" />
          <el-option label="全部" value="all" />
        </el-select>
        <el-button type="primary" :icon="Search" @click="doSearch">搜索</el-button>
        <el-button @click="resetFilter">重置</el-button>
        <el-radio-group v-model="recordView" size="small" style="margin-left:8px" @change="onRecordViewChange">
          <el-radio-button value="detail">明细</el-radio-button>
          <el-radio-button value="module">按模块</el-radio-button>
        </el-radio-group>
        <span class="muted" style="margin-left:auto">
          <template v-if="recordView==='detail'">共 {{ filteredRows.length }} / {{ rows.length }} 条业务域名</template>
          <template v-else>共 {{ moduleGroups.length }} 个模块 / {{ filteredRows.length }} 域名</template>
        </span>
      </div>
    </el-card>

    <el-card shadow="never" v-if="recordView==='detail'">
      <el-table ref="tableRef" :data="pagedRows" size="small" row-key="id" v-loading="loading"
        :default-sort="{ prop: 'project', order: 'ascending' }" @sort-change="onSort"
        @selection-change="(v) => selected = v">
        <el-table-column type="selection" width="42" reserve-selection />
        <el-table-column prop="project" label="项目" width="170" sortable="custom"><template #default="{ row }">
          <template v-if="projList(row.project).length">
            <span v-for="p in projList(row.project)" :key="p" style="display:inline-flex;align-items:center;margin-right:4px">
              <el-tag size="small" effect="plain" :style="tagStyle(projectColor(p))">{{ p }}</el-tag>
              <el-tooltip v-if="projStatus(p)" :content="p + ' 项目状态：' + projStatus(p)"><span :style="dotStyle(app.statusColor(projStatus(p)))" /></el-tooltip>
            </span>
          </template>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column prop="env" label="环境" width="100" sortable="custom"><template #default="{ row }">
          <el-tag v-if="row.env" size="small" effect="plain" :style="tagStyle(envColor(row.env))">{{ row.env }}</el-tag>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column prop="module" label="模块" width="150" sortable="custom"><template #default="{ row }">
          <span v-if="row.module">{{ row.module }}
            <el-tag v-if="row.module_source==='auto'" size="small" type="success" effect="plain" title="按 K8s 入口(Istio/Ingress)自动关联">自动</el-tag>
            <el-tag v-else-if="row.module_source==='manual'" size="small" type="info" effect="plain" title="手动设置">手动</el-tag>
          </span><span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column label="使用状态" width="130"><template #default="{ row }">
          <template v-if="row.life_status">
            <el-tag size="small" :style="tagStyle(app.statusColor(row.life_status))">{{ row.life_status }}</el-tag>
            <el-tag v-if="row.status_source==='auto'" size="small" type="success" effect="plain" title="关联K8s入口自动置为使用中">自动</el-tag>
            <el-tag v-else-if="row.status_source==='manual'" size="small" type="info" effect="plain" title="手动设置">手动</el-tag>
          </template><span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column prop="fqdn" label="域名" min-width="230" sortable="custom" show-overflow-tooltip><template #default="{ row }">
          <span class="mono" :class="{ stale: row.stale || row.domain_stale, ignored: row.ignored }">{{ row.fqdn }}</span>
          <el-tag v-if="row.stale" type="warning" size="small" style="margin-left:6px">解析已移出</el-tag>
          <el-tag v-if="row.domain_stale" type="danger" size="small" style="margin-left:6px">主域名{{ row.domain_gone }}</el-tag>
          <el-tag v-if="row.domain_status" size="small" effect="plain" :style="tagStyle(app.statusColor(row.domain_status))" style="margin-left:6px">{{ row.domain_status }}</el-tag>
          <el-tooltip v-if="row.ignored" :disabled="!row.ignore_reason" :content="row.ignore_reason">
            <el-tag type="info" size="small" style="margin-left:6px">已忽略</el-tag>
          </el-tooltip>
        </template></el-table-column>
        <el-table-column prop="cdn_name" label="CDN厂商" width="120" show-overflow-tooltip><template #default="{ row }">
          <el-tag v-if="row.cdn_name" size="small" effect="plain" :style="tagStyle(hashColor(row.cdn_name))">{{ row.cdn_name }}</el-tag>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column label="回源CNAME" min-width="180" show-overflow-tooltip><template #default="{ row }">
          <span class="mono">{{ row.cname || '—' }}</span>
        </template></el-table-column>
        <el-table-column label="源站IP" min-width="180" show-overflow-tooltip><template #default="{ row }">
          <span v-if="row.origin_ip" class="mono">{{ row.origin_ip }}</span>
          <span v-else-if="row.auto_origin_ip" class="mono">
            {{ row.auto_origin_ip }}
            <el-tooltip :content="row.auto_origin_src === '规则' ? '来自源站映射规则（DNS 查不到时兜底）' : '由回源 CNAME 在已同步 DNS 里解析 A 记录得到'" placement="top">
              <el-tag size="small" :type="row.auto_origin_src === '规则' ? 'warning' : 'info'" effect="plain" style="margin-left:4px">自动·{{ row.auto_origin_src }}</el-tag>
            </el-tooltip>
          </span>
          <span v-else class="mono">
            —
            <el-button v-if="row.cname" link type="primary" size="small" style="margin-left:4px" @click="openRuleFor(row)">+ 配源站</el-button>
          </span>
        </template></el-table-column>
        <el-table-column prop="cert_expiry_at" label="证书到期" width="140" sortable="custom"><template #default="{ row }">
          <el-tooltip :disabled="!row.cert_check_msg" :content="row.cert_check_msg">
            <el-tag :type="certState(row).type" size="small" effect="light">{{ certState(row).text }}</el-tag>
          </el-tooltip>
        </template></el-table-column>
        <el-table-column label="操作" width="172" fixed="right"><template #default="{ row }">
          <div style="display:flex;gap:8px;align-items:center">
            <el-tooltip content="查看详情"><el-button link type="primary" :icon="View" @click="openDetail(row)" /></el-tooltip>
            <el-tooltip v-if="canRecords" content="编辑"><el-button link type="primary" :icon="Edit" @click="openEdit(row)" /></el-tooltip>
            <el-tooltip v-if="canCert" content="检测证书"><el-button link type="primary" :loading="checking[row.id]" :icon="CircleCheck" @click="checkCert(row)" /></el-tooltip>
            <el-tooltip v-if="canRecords && !row.ignored" content="忽略此解析"><el-button link type="warning" :icon="Hide" @click="openIgnore([row])" /></el-tooltip>
            <el-tooltip v-else-if="canRecords" content="取消忽略"><el-button link type="success" :icon="RefreshLeft" @click="doUnignore([row])" /></el-tooltip>
            <el-tooltip v-if="row.origin === 'manual' || row.stale" :content="row.stale ? '移除（已移出账号）' : '删除'">
              <el-button link type="danger" :icon="Delete" @click="del(row)" />
            </el-tooltip>
          </div>
        </template></el-table-column>
      </el-table>
      <el-pagination v-model:current-page="currentPage" v-model:page-size="pageSize" :page-sizes="[10,20,50,100]"
        :total="filteredRows.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>

    <!-- 按模块聚合视图（一个模块一组，组内每域名独立） -->
    <el-card shadow="never" v-else v-loading="loading">
      <div style="margin-bottom:8px">
        <el-button size="small" text @click="expandAllGroups">展开全部</el-button>
        <el-button size="small" text @click="collapseAllGroups">收起全部</el-button>
        <span class="muted" style="margin-left:8px;font-size:12px">点模块行展开看域名明细</span>
      </div>
      <el-collapse v-model="openGroups">
        <el-collapse-item v-for="g in pagedGroups" :key="g.key" :name="g.key">
          <template #title>
            <div style="display:flex;align-items:center;gap:8px;flex:1;flex-wrap:wrap">
              <span v-for="p in projList(g.project)" :key="p" style="display:inline-flex;align-items:center">
                <el-tag size="small" effect="plain" :style="tagStyle(projectColor(p))">{{ p }}</el-tag>
                <span v-if="projStatus(p)" :style="dotStyle(app.statusColor(projStatus(p)))" :title="p+' 项目状态：'+projStatus(p)" />
              </span>
              <el-tag v-if="g.env" size="small" effect="plain" :style="tagStyle(envColor(g.env))">{{ g.env }}</el-tag>
              <b style="font-size:13px">{{ g.module || '（无模块）' }}</b>
              <span class="muted">{{ g.count }} 域名</span>
              <el-tag v-if="g.ipCount>1" size="small" type="warning" effect="plain">{{ g.ipCount }} 个源站</el-tag>
              <el-tag v-if="g.certFail" size="small" type="info" effect="plain">⚠{{ g.certFail }} 检测失败</el-tag>
              <el-tag v-if="g.certExpiring" size="small" type="warning" effect="plain">{{ g.certExpiring }} 快到期</el-tag>
            </div>
          </template>
          <el-table v-if="openGroups.includes(g.key)" :data="groupRows(g)" size="small" row-key="id">
            <el-table-column prop="fqdn" label="域名" min-width="230" show-overflow-tooltip><template #default="{ row }">
              <span class="mono" :class="{ stale: row.stale, ignored: row.ignored }">{{ row.fqdn }}</span>
              <el-tag v-if="row.domain_status" size="small" effect="plain" :style="tagStyle(app.statusColor(row.domain_status))" style="margin-left:6px">{{ row.domain_status }}</el-tag>
              <el-tag v-if="row.ignored" type="info" size="small" style="margin-left:6px">已忽略</el-tag>
            </template></el-table-column>
            <el-table-column prop="cdn_name" label="CDN厂商" width="120" show-overflow-tooltip><template #default="{ row }">
              <el-tag v-if="row.cdn_name" size="small" effect="plain" :style="tagStyle(hashColor(row.cdn_name))">{{ row.cdn_name }}</el-tag>
              <span v-else class="muted">—</span>
            </template></el-table-column>
            <el-table-column label="回源CNAME" min-width="180" show-overflow-tooltip><template #default="{ row }"><span class="mono">{{ row.cname || '—' }}</span></template></el-table-column>
            <el-table-column label="源站IP" min-width="180" show-overflow-tooltip><template #default="{ row }">
              <span v-if="row.origin_ip" class="mono">{{ row.origin_ip }}</span>
              <span v-else-if="row.auto_origin_ip" class="mono">{{ row.auto_origin_ip }}
                <el-tag size="small" :type="row.auto_origin_src === '规则' ? 'warning' : 'info'" effect="plain" style="margin-left:4px">自动·{{ row.auto_origin_src }}</el-tag>
              </span>
              <span v-else class="mono">— <el-button v-if="row.cname" link type="primary" size="small" @click="openRuleFor(row)">+ 配源站</el-button></span>
            </template></el-table-column>
            <el-table-column label="证书到期" width="140"><template #default="{ row }">
              <el-tooltip :disabled="!row.cert_check_msg" :content="row.cert_check_msg"><el-tag :type="certState(row).type" size="small" effect="light">{{ certState(row).text }}</el-tag></el-tooltip>
            </template></el-table-column>
            <el-table-column label="操作" width="150"><template #default="{ row }">
              <div style="display:flex;gap:8px;align-items:center">
                <el-tooltip content="查看详情"><el-button link type="primary" :icon="View" @click="openDetail(row)" /></el-tooltip>
                <el-tooltip v-if="canRecords" content="编辑"><el-button link type="primary" :icon="Edit" @click="openEdit(row)" /></el-tooltip>
                <el-tooltip v-if="canCert" content="检测证书"><el-button link type="primary" :loading="checking[row.id]" :icon="CircleCheck" @click="checkCert(row)" /></el-tooltip>
                <el-tooltip v-if="canRecords && !row.ignored" content="忽略此解析"><el-button link type="warning" :icon="Hide" @click="openIgnore([row])" /></el-tooltip>
                <el-tooltip v-else-if="canRecords" content="取消忽略"><el-button link type="success" :icon="RefreshLeft" @click="doUnignore([row])" /></el-tooltip>
              </div>
            </template></el-table-column>
          </el-table>
          <el-pagination v-if="openGroups.includes(g.key) && g.count > innerSize" size="small" background
            :current-page="innerPage[g.key] || 1" :page-size="innerSize" :total="g.count"
            layout="total, prev, pager, next" style="margin-top:8px; justify-content:flex-end"
            @current-change="(p) => setInnerPage(g.key, p)" />
        </el-collapse-item>
      </el-collapse>
      <el-empty v-if="!loadErr && !moduleGroups.length" description="没有匹配的业务域名" :image-size="60" />
      <div style="display:flex;align-items:center;margin-top:12px;justify-content:flex-end;gap:10px">
        <span class="muted" style="font-size:13px">共 {{ moduleGroups.length }} 个模块</span>
        <el-pagination v-model:current-page="groupPage" v-model:page-size="groupSize" :page-sizes="[10,20,50]"
          :total="moduleGroups.length" layout="sizes, prev, pager, next" />
      </div>
    </el-card>
    </template>

    <!-- ================= 主域名（唯一，一行一个域名） ================= -->
    <template v-else>
    <el-card shadow="never" style="margin-bottom:12px">
      <div class="filter">
        <el-input v-model="df.kw.value" placeholder="搜索主域名" clearable :prefix-icon="Search" style="width:180px" @keyup.enter="doDomSearch" />
        <el-select v-model="domNameFilter" clearable filterable placeholder="域名" style="width:200px" @change="domPage=1">
          <el-option v-for="d in domNameOptions" :key="d" :label="d" :value="d" />
        </el-select>
        <el-select v-model="df.view.value" style="width:150px" @change="domPage=1">
          <el-option v-for="o in df.viewOptions.value" :key="o.value" :label="`${o.label}（${o.count}）`" :value="o.value" />
        </el-select>
        <el-select v-model="df.src.value" clearable placeholder="来源" style="width:140px" @change="domPage=1">
          <el-option v-for="s in df.sourceOptions.value" :key="s" :label="s" :value="s">
            <span :style="{ display:'inline-block', width:'8px', height:'8px', borderRadius:'50%', background: registrarColor(s), marginRight:'6px' }" />{{ s }}
          </el-option>
        </el-select>
        <el-select v-model="df.expiry.value" clearable placeholder="到期" style="width:130px" @change="domPage=1">
          <el-option label="🔴 已过期" value="expired" />
          <el-option label="🟠 30天内" value="soon" />
          <el-option label="🟢 正常" value="normal" />
        </el-select>
        <el-select v-model="lifeStatusFilter" clearable placeholder="使用状态" style="width:140px" @change="domPage=1">
          <el-option v-for="s in app.domainStatuses" :key="s.id" :label="s.label" :value="s.label">
            <span :style="{ display:'inline-block', width:'8px', height:'8px', borderRadius:'50%', background: s.color, marginRight:'6px' }" />{{ s.label }}
          </el-option>
          <el-option label="（未设置）" value="__none__" />
        </el-select>
        <el-button type="primary" :icon="Search" @click="doDomSearch">搜索</el-button>
        <el-button @click="resetDomFilter">重置</el-button>
        <span class="muted" style="margin-left:auto">共 {{ domFiltered.length }} 个主域名</span>
      </div>
    </el-card>
    <el-card shadow="never">
      <LoadError :error="loadErr" @retry="load" />
      <el-table :data="domPaged" size="small" row-key="ci_id" v-loading="loading" @sort-change="onDomSort" @selection-change="(v) => domSelected = v">
        <el-table-column type="selection" width="40" />
        <el-table-column prop="name" label="主域名" min-width="220" sortable="custom"><template #default="{ row }">
          <span :class="{ stale: row.category !== 'active' && row.category !== 'pending' }">{{ row.name }}</span>
          <el-tooltip v-if="row.ignored && row.ignore_reason" :content="row.ignore_reason"><el-icon style="margin-left:4px;color:#909399;vertical-align:middle"><InfoFilled /></el-icon></el-tooltip>
        </template></el-table-column>
        <el-table-column label="状态" width="120"><template #default="{ row }">
          <el-tooltip :disabled="!row.source_status" :content="'GoDaddy 状态：' + row.source_status">
            <el-tag size="small" :style="domainCatStyle(row.category)">{{ domainCatLabel(row.category) }}</el-tag>
          </el-tooltip>
        </template></el-table-column>
        <el-table-column label="来源" width="130"><template #default="{ row }">
          <el-tag v-if="row.origin === 'manual'" type="info" size="small">手动录入</el-tag>
          <el-tag v-else size="small" :style="registrarStyle(row.registrar_name)">{{ row.registrar_name || '同步' }}</el-tag>
        </template></el-table-column>
        <el-table-column prop="expiry_at" label="域名到期" width="160" sortable="custom"><template #default="{ row }">
          <template v-if="row.expiry_at">
            <span :class="expiryClass(row.expiry_at)">{{ row.expiry_at }}</span>
            <el-tag v-if="isExpired(row.expiry_at)" type="danger" size="small" style="margin-left:6px">已过期</el-tag>
          </template>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column label="解析数" width="90"><template #default="{ row }">
          <el-button link type="primary" @click="jumpToRecords(row)">{{ resoCountMap[row.ci_id] || 0 }}</el-button>
        </template></el-table-column>
        <el-table-column label="使用状态" width="180"><template #default="{ row }">
          <el-select :model-value="row.life_status" size="small" placeholder="未设置" clearable style="width:110px"
                     @change="(v) => setDomStatus([row], v)">
            <el-option v-for="s in app.domainStatuses" :key="s.id" :label="s.label" :value="s.label">
              <span :style="{ display:'inline-block', width:'8px', height:'8px', borderRadius:'50%', background: s.color, marginRight:'6px' }" />{{ s.label }}
            </el-option>
          </el-select>
          <el-tooltip v-if="!row.life_status && row.suggest" content="0 业务解析且非回源目标，建议标此状态">
            <el-tag size="small" type="warning" effect="plain" style="margin-left:4px; cursor:pointer" @click="setDomStatus([row], row.suggest)">建议{{ row.suggest }}</el-tag>
          </el-tooltip>
        </template></el-table-column>
        <el-table-column label="操作" width="180" fixed="right"><template #default="{ row }">
          <div style="display:flex;gap:8px;align-items:center">
            <el-tooltip v-if="canSync" content="刷到期（WHOIS+443）"><el-button link type="primary" :loading="refreshingDom[row.ci_id]" :icon="Refresh" @click="refreshOneDom(row)" /></el-tooltip>
            <el-tooltip v-if="canManage && canRenew(row)" content="续费 / 自动续费（写回 GoDaddy）"><el-button link type="warning" :icon="RefreshRight" @click="openRenew(row)" /></el-tooltip>
            <template v-if="canManage && row.origin === 'manual'">
              <el-tooltip content="编辑"><el-button link type="primary" :icon="Edit" @click="openEditDomain(row)" /></el-tooltip>
              <el-tooltip content="删除"><el-button link type="danger" :icon="Delete" @click="delDomain(row)" /></el-tooltip>
            </template>
            <el-tooltip v-else-if="canManage && row.stale" content="移除（已移出账号）"><el-button link type="danger" :icon="Delete" @click="delDomain(row)" /></el-tooltip>
            <el-tooltip v-if="canManage && !row.ignored" content="忽略域名"><el-button link type="warning" :icon="Hide" @click="ignoreDomains([row], true)" /></el-tooltip>
            <el-tooltip v-else-if="canManage" content="取消忽略"><el-button link type="success" :icon="RefreshLeft" @click="ignoreDomains([row], false)" /></el-tooltip>
          </div>
        </template></el-table-column>
      </el-table>
      <el-pagination v-model:current-page="domPage" v-model:page-size="domPageSize" :page-sizes="[10,20,50,100]"
        :total="domFiltered.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>
    </template>

    <!-- 主域名表单（录入/编辑手动主域名） -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="domDlg" :title="domEditing ? '编辑主域名' : '录入主域名'" width="480px">
      <el-form :model="domForm" label-width="110px">
        <el-form-item label="主域名">
          <el-input v-model="domForm.name" :disabled="domEditing" placeholder="example.com（自动归一到注册域名）" style="width:240px" />
          <span v-if="domEditing" class="muted" style="margin-left:8px">不可改</span>
        </el-form-item>
        <el-form-item label="数据源/注册商">
          <el-select v-model="domForm.registrar_id" clearable placeholder="（可选，签证书/同步解析用）" style="width:240px">
            <el-option v-for="r in registrars" :key="r.id" :label="r.name" :value="r.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="DNS provider"><el-input v-model="domForm.dns_provider" placeholder="godaddy/aliyun/cloudflare（可选）" /></el-form-item>
        <el-form-item label="域名到期"><el-date-picker v-model="domForm.expiry_at" type="date" value-format="YYYY-MM-DD" placeholder="域名注册到期日" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="domDlg=false">取消</el-button><el-button type="primary" @click="saveDomain">保存</el-button></template>
    </el-dialog>

    <!-- 域名续费 / 自动续费（写回 GoDaddy；续费会真实扣费） -->
    <el-dialog v-model="renewDlg.show" title="域名续费 / 自动续费" width="520px"
      :close-on-click-modal="false" :close-on-press-escape="false">
      <div v-loading="renewDlg.loading">
        <el-form label-width="100px">
          <el-form-item label="域名"><span class="mono">{{ renewDlg.domain }}</span></el-form-item>
          <el-form-item label="当前到期">
            <span :class="{ 'exp-red': renewDlg.expired }">{{ renewDlg.expires || '—' }}</span>
            <el-tag v-if="renewDlg.expired" type="danger" size="small" style="margin-left:6px">已过期</el-tag>
            <el-tag v-if="renewDlg.env && renewDlg.env!=='生产'" type="warning" size="small" style="margin-left:6px">{{ renewDlg.env }}</el-tag>
          </el-form-item>
          <el-form-item label="隐私保护">
            <template v-if="!renewDlg.detailOk"><el-tag type="info" size="small">未知（详情读取失败）</el-tag></template>
            <el-tag v-else-if="renewDlg.privacy" type="success" size="small">✅ 已开启</el-tag>
            <el-tag v-else type="info" size="small">未开启</el-tag>
            <span class="muted" style="margin-left:8px">只读；续费不改隐私，购买隐私是单独付费项</span>
          </el-form-item>
          <el-form-item label="自动续费">
            <el-switch v-model="renewDlg.renewAuto" :loading="renewDlg.autoBusy" @change="onToggleAuto" />
            <span class="muted" style="margin-left:8px">开启后到期自动续（GoDaddy 账户扣费），不立即扣费</span>
          </el-form-item>
          <el-divider content-position="left">立即续费</el-divider>
          <el-form-item label="续费年数">
            <el-input-number v-model="renewDlg.period" :min="1" :max="10" controls-position="right" style="width:130px" />
          </el-form-item>
          <el-form-item label="域名费(估算)">
            <template v-if="renewDlg.pricePerYear > 0">
              <b>{{ renewDlg.currency }} {{ (renewDlg.pricePerYear * renewDlg.period).toFixed(2) }}</b>
              <span class="muted" style="margin-left:8px">{{ renewDlg.currency }} {{ renewDlg.pricePerYear.toFixed(2) }}/年 × {{ renewDlg.period }} 年</span>
              <div class="muted" style="font-size:12px">⚠ 仅域名费，另加税费/ICANN规费，实扣总额以 GoDaddy 结算为准</div>
            </template>
            <span v-else class="muted">价格以 GoDaddy 结算为准</span>
          </el-form-item>
          <el-alert type="error" :closable="false" show-icon
            :title="renewDlg.dryRun
              ? '当前数据源为「预演」模式：点续费不会真扣费，只打日志核对报文'
              : '⚠ 点「确认续费」会立即向 GoDaddy 账户真实扣费，不可撤销'" style="margin-left:0" />
        </el-form>
      </div>
      <template #footer>
        <el-button @click="renewDlg.show=false">关闭</el-button>
        <el-button type="danger" :loading="renewDlg.renewBusy" @click="doRenew">确认续费</el-button>
      </template>
    </el-dialog>

    <!-- 续费记录台账（防超付可查：报价/订单号/到期前后/操作人） -->
    <!-- 批量续费：两步（先预览确认，再执行），真金白银不做一步到位 -->
    <el-dialog v-model="batchRenew.show" title="批量续费" width="880px" :close-on-click-modal="false">
      <el-alert type="warning" :closable="false" show-icon style="margin-bottom:12px"
        title="续费会真实扣费。请先「检查」确认清单无误，再执行；结果以订单号为准。" />

      <div v-if="batchRenew.step === 'input'">
        <el-form label-width="90px">
          <el-form-item label="域名列表">
            <el-input v-model="batchRenew.text" type="textarea" :rows="8"
                      placeholder="一行一个，也支持逗号/空格分隔。大小写、http(s):// 前缀会自动规整。" />
            <div class="muted" style="margin-top:4px">一次最多 {{ 50 }} 个；重复的只会处理一次</div>
          </el-form-item>
          <el-form-item label="续费年数">
            <el-input-number v-model="batchRenew.period" :min="1" :max="10" controls-position="right" style="width:130px" />
          </el-form-item>
        </el-form>
      </div>

      <div v-else>
        <div class="brsum">
          <el-tag type="success" effect="plain">可续 {{ batchRenew.renewable }} 个</el-tag>
          <el-tag v-if="cntBy('not_found')" type="danger" effect="plain">未找到 {{ cntBy('not_found') }}</el-tag>
          <el-tag v-if="cntBy('unsupported')" type="warning" effect="plain">不支持写回 {{ cntBy('unsupported') }}</el-tag>
          <el-tag v-if="cntBy('duplicated')" type="info" effect="plain">重复 {{ cntBy('duplicated') }}</el-tag>
          <el-tag v-if="cntBy('renewed')" type="success">已续 {{ cntBy('renewed') }}</el-tag>
          <el-tag v-if="cntBy('failed')" type="danger">失败 {{ cntBy('failed') }}</el-tag>
          <span class="muted" style="margin-left:auto">续费年数：{{ batchRenew.period }} 年</span>
        </div>
        <el-alert v-if="batchRenew.warning" type="warning" :closable="false" show-icon
                  style="margin-bottom:10px" :title="batchRenew.warning" />
        <el-table :data="batchRenew.items" size="small" max-height="380" border>
          <el-table-column prop="domain" label="域名" min-width="180" show-overflow-tooltip />
          <el-table-column label="状态" width="110">
            <template #default="{ row }">
              <el-tag size="small" :type="brType(row.status)" effect="plain">{{ brLabel(row.status) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="到期日" min-width="180">
            <template #default="{ row }">
              <span v-if="row.expiry_before">
                {{ row.expiry_before }}
                <span v-if="row.expiry_after"> → <b class="ok">{{ row.expiry_after }}</b></span>
                <span v-else-if="row.expiry_expect" class="muted"> → {{ row.expiry_expect }}（预计）</span>
              </span>
              <span v-else class="muted">—</span>
            </template>
          </el-table-column>
          <el-table-column label="订单号 / 说明" min-width="230" show-overflow-tooltip>
            <template #default="{ row }">
              <code v-if="row.order_id">{{ row.order_id }}</code>
              <span v-else class="muted">{{ row.reason || '—' }}</span>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <template #footer>
        <el-button @click="batchRenew.show = false">关闭</el-button>
        <template v-if="batchRenew.step === 'input'">
          <el-button type="primary" :loading="batchRenew.checking" @click="doPreviewBatch">检查</el-button>
        </template>
        <template v-else-if="batchRenew.step === 'preview'">
          <el-button @click="batchRenew.step = 'input'">返回修改</el-button>
          <el-button type="warning" :disabled="!batchRenew.renewable" :loading="batchRenew.running"
                     @click="doBatchRenew">确认续费 {{ batchRenew.renewable }} 个（{{ batchRenew.period }} 年）</el-button>
        </template>
      </template>
    </el-dialog>

    <el-dialog v-model="renewLog.show" title="续费记录" width="920px" :close-on-click-modal="false">
      <el-alert type="info" :closable="false" show-icon style="margin-bottom:10px"
        title="平台报价为下单前挂牌估算；精确扣费以 GoDaddy 账单为准，凭订单号核对。到期「前→后」可核对是否只续了所选年数（防超付）。" />
      <el-table :data="renewLog.items" size="small" v-loading="renewLog.loading" max-height="440">
        <el-table-column prop="created_at" label="时间" width="150" />
        <el-table-column prop="domain" label="域名" min-width="160" show-overflow-tooltip />
        <el-table-column label="年数" width="60"><template #default="{ row }">{{ row.period }}</template></el-table-column>
        <el-table-column label="域名费(估算)" width="120"><template #default="{ row }">
          <span v-if="row.quoted_amount > 0">{{ row.quoted_currency }} {{ row.quoted_amount.toFixed(2) }}</span>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column label="实扣(厂商)" width="120"><template #default="{ row }">
          <span v-if="row.actual_amount > 0">{{ row.actual_currency }} {{ row.actual_amount.toFixed(2) }}</span>
          <el-tooltip v-else content="GoDaddy 续费接口未返回金额，凭订单号去 GoDaddy 账单查实扣总额"><span class="muted">凭订单查</span></el-tooltip>
        </template></el-table-column>
        <el-table-column label="订单号" width="150" show-overflow-tooltip><template #default="{ row }">
          <span v-if="row.order_id" class="mono">{{ row.order_id }}</span><span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column label="到期 前→后" width="200"><template #default="{ row }">
          <span v-if="row.expiry_before || row.expiry_after"><span class="mono">{{ row.expiry_before || '—' }}</span> → <span class="mono">{{ row.expiry_after || '—' }}</span></span>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column prop="operator" label="操作人" width="100" />
        <el-table-column label="环境" width="90"><template #default="{ row }">
          <el-tag v-if="row.dry_run" type="warning" size="small">预演</el-tag>
          <el-tag v-else-if="row.env && row.env!=='生产'" type="info" size="small">{{ row.env }}</el-tag>
          <el-tag v-else type="success" size="small">生产</el-tag>
        </template></el-table-column>
      </el-table>
      <el-pagination v-model:current-page="renewLog.page" :page-size="renewLog.size" :total="renewLog.total"
        layout="total, prev, pager, next" size="small" background style="margin-top:10px; justify-content:flex-end"
        @current-change="loadRenewLog" />
    </el-dialog>

    <!-- 编辑解析（业务字段 + 回源/源站；域名只读） -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="dlg" :title="editing ? '编辑解析' : '录入解析'" width="520px">
      <el-form :model="form" label-width="96px">
        <el-form-item v-if="!editing" label="主域名">
          <el-select v-model="form.domain_ci_id" filterable placeholder="选择主域名" style="width:260px">
            <el-option v-for="d in allDomains" :key="d.ci_id" :label="d.name" :value="d.ci_id" />
          </el-select>
        </el-form-item>
        <el-form-item label="主机头" v-if="!editing">
          <el-input v-model="form.host" placeholder="www / @ / api" style="width:180px" />
          <el-select v-model="form.record_type" style="width:100px; margin-left:8px">
            <el-option label="A" value="A" />
            <el-option label="CNAME" value="CNAME" />
          </el-select>
        </el-form-item>
        <el-form-item v-else label="域名">
          <span class="mono">{{ form.fqdn }}</span>
          <span v-if="synced" class="muted" style="margin-left:8px">同步来的，域名/回源CNAME 只读，可改业务字段</span>
        </el-form-item>
        <el-form-item label="项目">
          <el-select v-model="formProjectArr" multiple filterable clearable collapse-tags collapse-tags-tooltip placeholder="可选多个项目（共享域名）" style="width:260px">
            <el-option v-for="p in app.projects" :key="p.id" :label="p.name" :value="p.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="环境">
          <el-select v-model="form.env" clearable placeholder="选择环境" style="width:260px">
            <el-option v-for="e in app.environments" :key="e.id" :label="e.code" :value="e.code" />
          </el-select>
        </el-form-item>
        <el-form-item label="模块"><el-input v-model="form.module" style="width:260px" placeholder="留空可用「K8s自动关联模块」自动填" /></el-form-item>
        <el-form-item label="使用状态">
          <el-select v-model="form.life_status" clearable placeholder="未设置（关联K8s入口会自动置使用中）" style="width:260px">
            <el-option v-for="s in ['使用中','备用','未使用','待下线','已下线']" :key="s" :label="s" :value="s" />
          </el-select>
        </el-form-item>
        <el-form-item label="CDN">
          <el-select v-model="form.cdn_id" clearable placeholder="（可选）内网/直连可留空" style="width:260px">
            <el-option v-for="c in app.cdns" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="回源CNAME"><el-input v-model="form.cname" :disabled="synced" placeholder="走 CDN 时的回源地址 / CNAME" /></el-form-item>
        <el-form-item label="源站IP">
          <el-input v-model="form.origin_ip" :placeholder="form.auto_origin_ip ? `自动推测：${form.auto_origin_ip}（留空即用此值）` : '源站 IP（多个逗号分隔）'" />
          <span v-if="form.auto_origin_ip" class="muted" style="font-size:12px">留空则展示自动推测值 {{ form.auto_origin_ip }}；手填后以手填为准</span>
        </el-form-item>
        <el-form-item label="证书到期">
          <el-date-picker v-model="form.cert_expiry_at" type="date" value-format="YYYY-MM-DD" placeholder="可手填，或保存后点「检测证书」自动读" style="width:260px" />
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="dlg=false">取消</el-button><el-button type="primary" @click="save">保存</el-button></template>
    </el-dialog>

    <!-- 批量设置项目/环境/模块（只改勾选的字段，留空的不动） -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="bDlg" title="批量设置" width="480px">
      <div class="muted" style="margin-bottom:12px">对选中的 {{ selected.length }} 条解析统一设置。只改打开开关的字段，关闭的保持原值不动。</div>
      <el-form :model="bForm" label-width="72px">
        <el-form-item label="项目">
          <el-switch v-model="bForm.setProject" style="margin-right:10px" />
          <el-select v-model="bForm.project" :disabled="!bForm.setProject" multiple filterable clearable collapse-tags collapse-tags-tooltip placeholder="可选多个（共享域名）；留空=清除" style="width:240px">
            <el-option v-for="p in app.projects" :key="p.id" :label="p.name" :value="p.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="环境">
          <el-switch v-model="bForm.setEnv" style="margin-right:10px" />
          <el-select v-model="bForm.env" :disabled="!bForm.setEnv" clearable placeholder="选择环境" style="width:240px">
            <el-option v-for="e in app.environments" :key="e.id" :label="e.code" :value="e.code" />
          </el-select>
        </el-form-item>
        <el-form-item label="模块">
          <el-switch v-model="bForm.setModule" style="margin-right:10px" />
          <el-input v-model="bForm.module" :disabled="!bForm.setModule" placeholder="模块" style="width:240px" />
        </el-form-item>
        <el-form-item label="CDN">
          <el-switch v-model="bForm.setCdn" style="margin-right:10px" />
          <el-select v-model="bForm.cdn_id" :disabled="!bForm.setCdn" clearable placeholder="选择 CDN（清空=设为无）" style="width:240px">
            <el-option v-for="c in app.cdns" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="回源CNAME">
          <el-switch v-model="bForm.setCname" style="margin-right:10px" />
          <el-input v-model="bForm.cname" :disabled="!bForm.setCname" placeholder="回源 CNAME（留空=清掉，通常同步自动获取）" style="width:240px" />
        </el-form-item>
        <el-form-item label="源站IP">
          <el-switch v-model="bForm.setOriginIP" style="margin-right:10px" />
          <el-input v-model="bForm.origin_ip" :disabled="!bForm.setOriginIP" placeholder="源站IP（多个逗号分隔；留空=清掉手填、回到自动推算）" style="width:240px" />
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="bDlg=false">取消</el-button><el-button type="primary" @click="saveBulk">应用到 {{ selected.length }} 条</el-button></template>
    </el-dialog>

    <!-- 源站映射规则管理（回源CNAME → 源站IP，全局） -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="rulesDlg" title="源站映射（回源CNAME → 源站IP）" width="640px">
      <div class="muted" style="margin-bottom:10px">优先级：手填 &gt; DNS 解析 &gt; 本规则。规则只兜底 DNS 查不到的（如外部真 CDN）。一个回源 CNAME 一条。</div>
      <div style="display:flex;gap:8px;margin-bottom:10px">
        <el-input v-model="ruleForm.cname" placeholder="回源 CNAME" style="flex:1" />
        <el-input v-model="ruleForm.origin_ip" placeholder="源站IP（多个逗号分隔）" style="width:220px" />
        <el-button type="primary" :icon="Plus" @click="saveRule">保存</el-button>
      </div>
      <el-table :data="rules" size="small" max-height="360" v-loading="rulesLoading">
        <el-table-column label="回源 CNAME" prop="cname" min-width="220" show-overflow-tooltip />
        <el-table-column label="源站IP" prop="origin_ip" min-width="150" show-overflow-tooltip />
        <el-table-column label="用到" width="80"><template #default="{ row }"><el-tag size="small" type="info" effect="plain">{{ row.used }} 条</el-tag></template></el-table-column>
        <el-table-column label="操作" width="100"><template #default="{ row }">
          <el-button link type="primary" :icon="Edit" @click="editRule(row)" />
          <el-button link type="danger" :icon="Delete" @click="removeRule(row)" />
        </template></el-table-column>
      </el-table>
      <template #footer><el-button @click="rulesDlg=false">关闭</el-button></template>
    </el-dialog>

    <!-- 行内「+配源站」：为某条记录的回源 CNAME 快速配规则 -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="ruleOneDlg" title="配置源站映射" width="480px">
      <el-form label-width="90px">
        <el-form-item label="回源 CNAME"><span class="mono">{{ ruleForm.cname }}</span></el-form-item>
        <el-form-item label="源站IP"><el-input v-model="ruleForm.origin_ip" placeholder="源站IP（多个逗号分隔）" /></el-form-item>
      </el-form>
      <div class="muted" style="font-size:12px">保存后：所有回源到「{{ ruleForm.cname }}」的记录，源站IP 空的都会自动显示此值（标「自动·规则」）；DNS 能查到 A 记录的仍以解析为准。</div>
      <template #footer><el-button @click="ruleOneDlg=false">取消</el-button><el-button type="primary" @click="saveRuleOne">保存规则</el-button></template>
    </el-dialog>

    <!-- 批量分配：项目/环境选一次，多个模块各带一批域名（完整 FQDN 精确匹配，找不到跳过） -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="aDlg" title="批量分配" width="660px">
      <template v-if="!aResult">
        <el-radio-group v-model="aMode" size="small" style="margin-bottom:12px">
          <el-radio-button value="paste">两列粘贴（服务名 + 域名）</el-radio-button>
          <el-radio-button value="blocks">分模块填</el-radio-button>
        </el-radio-group>
        <div style="display:flex;gap:20px;align-items:center;margin-bottom:10px">
          <div>项目
            <el-select v-model="aForm.project" filterable clearable placeholder="选择项目" style="width:190px">
              <el-option v-for="p in app.projects" :key="p.id" :label="p.name" :value="p.name" />
            </el-select>
          </div>
          <div>环境
            <el-select v-model="aForm.env" clearable placeholder="选择环境" style="width:150px">
              <el-option v-for="e in app.environments" :key="e.id" :label="e.code" :value="e.code" />
            </el-select>
          </div>
          <span class="muted">整批统一</span>
        </div>

        <!-- 两列粘贴 -->
        <template v-if="aMode === 'paste'">
          <div class="muted" style="margin-bottom:6px">粘贴两列（逗号 或 空格/Tab 分隔均可，一行一个）：<b>左列 = 模块</b>（原样用），<b>右列 = 域名</b>（完整 FQDN 精确匹配）</div>
          <el-input v-model="pasteText" type="textarea" :rows="10"
            placeholder="dragon-tiger-game-frontend,dragon-tiger.k8s-g32-uat.com&#10;lobby-game-frontend,lobby-game.k8s-g32-uat.com&#10;（kubectl 那种空格对齐的也能直接贴）" />
          <span class="muted">识别 {{ parsedPaste.length }} 行有效（{{ new Set(parsedPaste.map(x => x.module)).size }} 个模块）</span>
        </template>

        <!-- 分模块填 -->
        <template v-else>
        <div class="muted" style="margin-bottom:8px">每个模块各配一批域名（完整 FQDN，一行一个 或 逗号分隔）；项目/环境留空则不改</div>
        <div v-for="(b, i) in aForm.blocks" :key="i" class="assign-block">
          <div style="display:flex;align-items:center;gap:8px;margin-bottom:6px">
            <span>模块</span>
            <el-input v-model="b.module" placeholder="如 门户 / 交易（留空则不改模块）" style="width:220px" />
            <span class="muted">已识别 {{ countDomains(b.domains) }} 个</span>
            <el-button v-if="aForm.blocks.length > 1" link type="danger" :icon="Delete" style="margin-left:auto" @click="aForm.blocks.splice(i, 1)">删除</el-button>
          </div>
          <el-input v-model="b.domains" type="textarea" :rows="3" placeholder="www.sync-shop.com&#10;m.sync-shop.com" />
        </div>
        <div style="margin-top:10px">
          <el-button :icon="Plus" @click="aForm.blocks.push({ module: '', domains: '' })">添加模块</el-button>
          <span class="muted" style="margin-left:12px">合计 {{ aForm.blocks.length }} 模块 / {{ totalAssignDomains }} 域名</span>
        </div>
        </template>
      </template>
      <template v-else>
        <el-result icon="success" :title="`成功更新 ${aResult.updated} 条`"
          :sub-title="`项目=${aForm.project || '（不改）'}　环境=${aForm.env || '（不改）'}`">
          <template #extra>
            <div v-if="aResult.groups.length" class="muted">{{ aResult.groups.map(g => `${g.module} ${g.count} 条`).join('　·　') }}</div>
          </template>
        </el-result>
        <el-alert v-if="aResult.notFound.length" type="warning" :closable="false"
          :title="`${aResult.notFound.length} 个未找到，已跳过（台账里没有这些 FQDN）`" style="margin-top:8px">
          <div style="display:flex;justify-content:flex-end;margin-bottom:4px">
            <el-button size="small" :icon="CopyDocument" @click="copyNotFound">复制未找到（{{ aResult.notFound.length }}）</el-button>
          </div>
          <div class="mono" style="white-space:pre-line;max-height:160px;overflow:auto">{{ aResult.notFound.join('\n') }}</div>
        </el-alert>
      </template>
      <template #footer>
        <template v-if="!aResult">
          <el-button @click="aDlg = false">取消</el-button>
          <el-button type="primary" :loading="assigning" :disabled="!totalAssignDomains" @click="applyAssign">应用（{{ totalAssignDomains }} 条）</el-button>
        </template>
        <template v-else>
          <el-button @click="resetAssign">继续分配</el-button>
          <el-button type="primary" @click="aDlg = false; load()">完成关闭</el-button>
        </template>
      </template>
    </el-dialog>

    <!-- 忽略业务域名（单条/批量共用，原因可选） -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="igDlg" title="忽略此解析" width="480px">
      <div class="muted" style="margin-bottom:12px">将 {{ igRows.length }} 条业务域名（解析）标为「忽略」：<b>仅隐藏、不删数据</b>——整条解析默认不再展示，也不计入证书巡检 / 总览统计，可随时在「已忽略」里恢复。<br/>（注：若只是「这条解析不需要证书」但仍想保留展示，请到「到期巡检」页标「无需证书」）</div>
      <el-form label-width="60px">
        <el-form-item label="原因"><el-input v-model="igReason" placeholder="（可选）如 已下线 / 项目未使用" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="igDlg=false">取消</el-button><el-button type="primary" @click="confirmIgnore">确认忽略</el-button></template>
    </el-dialog>

    <!-- 查看详情（只读，长字段全展开） -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="dDlg" title="解析详情" width="560px">
      <el-descriptions v-if="detail" :column="1" border size="small">
        <el-descriptions-item label="域名"><span class="mono detail-val">{{ detail.fqdn }}</span></el-descriptions-item>
        <el-descriptions-item label="主机头 / 类型"><span class="mono">{{ detail.host || '@' }}</span> / {{ detail.record_type }}</el-descriptions-item>
        <el-descriptions-item label="项目">
          <el-tag v-for="p in projList(detail.project)" :key="p" size="small" effect="plain" :style="tagStyle(projectColor(p))" style="margin-right:4px">{{ p }}</el-tag>
          <span v-if="!projList(detail.project).length" class="muted">—</span>
          <span v-else class="muted">—</span>
        </el-descriptions-item>
        <el-descriptions-item label="环境">
          <el-tag v-if="detail.env" size="small" effect="plain" :style="tagStyle(envColor(detail.env))">{{ detail.env }}</el-tag>
          <span v-else class="muted">—</span>
        </el-descriptions-item>
        <el-descriptions-item label="模块">{{ detail.module || '—' }}</el-descriptions-item>
        <el-descriptions-item label="CDN厂商">{{ detail.cdn_name || '—' }}</el-descriptions-item>
        <el-descriptions-item label="回源CNAME"><span class="mono detail-val">{{ detail.cname || '—' }}</span></el-descriptions-item>
        <el-descriptions-item label="源站IP"><span class="mono detail-val">{{ detail.origin_ip || (detail.auto_origin_ip ? detail.auto_origin_ip + '（自动）' : '—') }}</span></el-descriptions-item>
        <el-descriptions-item label="证书到期">
          <el-tag :type="certState(detail).type" size="small" effect="light">{{ certState(detail).text }}</el-tag>
          <div v-if="detail.cert_check_msg" class="muted" style="margin-top:4px">{{ detail.cert_check_msg }}</div>
        </el-descriptions-item>
        <el-descriptions-item label="来源">
          <el-tag v-if="detail.origin === 'manual'" type="info" size="small">手动录入</el-tag>
          <span v-else>{{ detail.source_name || '同步' }}</span>
          <el-tag v-if="detail.stale" type="warning" size="small" style="margin-left:6px">已移出账号</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="操作人">{{ detail.operator || '—' }}</el-descriptions-item>
        <el-descriptions-item label="更新时间">{{ detail.updated_at || '—' }}</el-descriptions-item>
      </el-descriptions>
      <template #footer><el-button @click="dDlg=false">关闭</el-button><el-button type="primary" :icon="Edit" @click="editFromDetail">编辑</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus, Refresh, Search, CircleCheck, Edit, Delete, EditPen, View, Download, Operation, CopyDocument, Hide, RefreshLeft, RefreshRight, Tickets, InfoFilled, Connection, ArrowDown, Close, MagicStick } from '@element-plus/icons-vue'
import { registrarStyle, registrarColor, domainCatLabel, domainCatStyle } from '../utils/cloud'
import { useDomainFilter } from '../composables/useDomainFilter'
import { listAllRecords, createRecord, updateRecord, bulkUpdateRecords, bulkIgnoreRecords, deleteRecord, checkRecordCert,
  syncDomainRecords, listDomains, listRegistrars, createDomain, updateDomain, deleteDomain, refreshDomain, refreshAllDomains, bulkIgnoreDomains,
  listOriginRules, upsertOriginRule, deleteOriginRule, bulkDomainStatus,
  godaddyDetail, renewDomain, setAutoRenew, listRenewals, autoLinkDomainModules,
  previewBatchRenew, batchRenewDomains } from '../api/cmdb'
import { normalizeError } from '../api/http'
import LoadError from '../components/LoadError.vue'
import { useAppStore } from '../stores/app'
import { useAuthStore } from '../stores/auth'

const app = useAppStore()
// 按钮级权限。前端只管显隐，真正的拦截在后端 perm.go——
// 少一个 v-if 是"看得见点了报错"的体验问题，不是安全问题。
const auth = useAuthStore()
const canManage = computed(() => auth.hasButton('manage_domains'))
const canSync = computed(() => auth.hasButton('sync_domains'))
const canCert = computed(() => auth.hasButton('manage_certs'))
const canRecords = computed(() => auth.hasButton('manage_records'))

// ── 批量续费 ──
// 两步走：input（粘域名）→ preview（看清单）→ 执行。
// 中间那一步不是多余的仪式：打错一个字母就会静默跳过某个该续的域名，
// 而"跳过"在一屏结果里最容易被眼睛滑过去。
const batchRenew = reactive({
  show: false, step: 'input', text: '', period: 1,
  items: [], renewable: 0, warning: '', checking: false, running: false,
})

function openBatchRenew() {
  Object.assign(batchRenew, { show: true, step: 'input', text: '', period: 1,
    items: [], renewable: 0, warning: '', checking: false, running: false })
}

function cntBy(st) { return batchRenew.items.filter((x) => x.status === st).length }
function brLabel(st) {
  return { ok: '可续费', renewed: '已续费', failed: '失败', not_found: '未找到',
    unsupported: '不支持', duplicated: '重复' }[st] || st
}
function brType(st) {
  return { ok: 'success', renewed: 'success', failed: 'danger', not_found: 'danger',
    unsupported: 'warning', duplicated: 'info' }[st] || 'info'
}

async function doPreviewBatch() {
  if (!batchRenew.text.trim()) { ElMessage.warning('请先粘贴域名'); return }
  batchRenew.checking = true
  try {
    const r = await previewBatchRenew({ domains: batchRenew.text, period: batchRenew.period })
    batchRenew.items = r.items || []
    batchRenew.renewable = r.renewable || 0
    batchRenew.warning = r.warning || ''
    batchRenew.step = 'preview'
    if (!batchRenew.renewable) ElMessage.warning('清单里没有可续费的域名，请检查')
  } catch (e) {
    ElMessage.error(e?.message || '检查失败')
  } finally { batchRenew.checking = false }
}

async function doBatchRenew() {
  // 二次确认写明数量和年数——这是花钱的操作，不能让人点得太顺手
  try {
    await app.showConfirm(
      `将为 ${batchRenew.renewable} 个域名各续费 ${batchRenew.period} 年，这会真实扣费且无法回滚。确定继续？`,
      '确认批量续费')
  } catch (_) { return }
  batchRenew.running = true
  try {
    const r = await batchRenewDomains({
      domains: batchRenew.text, period: batchRenew.period,
      confirm_count: batchRenew.renewable,   // 服务端会核对，对不上说明台账变过
    })
    batchRenew.items = r.items || []
    batchRenew.renewable = 0
    if (r.failed) ElMessage.warning(r.msg)
    else ElMessage.success(r.msg)
    load()
  } catch (e) {
    // 409 = 预览之后台账变了，必须重新看一遍
    const d = e?.raw?.response?.data
    if (e?.status === 409) {
      batchRenew.items = d?.items || batchRenew.items
      batchRenew.step = 'preview'
    }
    ElMessage.error(d?.error || e?.message || '批量续费失败')
  } finally { batchRenew.running = false }
}

// 从 K8s 入口自动填模块(只补空的)
const autoLinking = ref(false)
async function autoLink() {
  autoLinking.value = true
  try {
    const r = await autoLinkDomainModules()
    ElMessage.success(`扫描 ${r.scanned} 个空模块域名，自动填了 ${r.filled} 个`)
    load()
  } catch (e) { ElMessage.error('自动关联失败：' + (e.response?.data?.error || e.message)) } finally { autoLinking.value = false }
}

// ---- 域名续费 / 自动续费（写回 GoDaddy）----
// 能续费：绑了数据源(非手动、有注册商)、未忽略、未移出账号。后端还会校验 provider 是否支持。
function canRenew(row) { return !!(row && row.registrar_name && row.origin !== 'manual' && !row.ignored && !row.stale) }
const renewDlg = reactive({ show: false, loading: false, ciid: null, domain: '', expires: '', expired: false,
  renewAuto: false, privacy: false, detailOk: true, pricePerYear: 0, currency: '', period: 1, env: '生产', dryRun: false, autoBusy: false, renewBusy: false })
async function openRenew(row) {
  Object.assign(renewDlg, { show: true, loading: true, ciid: row.ci_id, domain: row.name,
    expires: row.expiry_at || '', expired: isExpired(row.expiry_at), renewAuto: false, privacy: false, detailOk: true,
    pricePerYear: 0, currency: '', period: 1, env: '生产', dryRun: false })
  try {
    const d = await godaddyDetail(row.ci_id)
    renewDlg.detailOk = d.detail_ok !== false
    renewDlg.expires = d.expires || renewDlg.expires
    renewDlg.expired = d.expires ? isExpired(d.expires) : renewDlg.expired
    renewDlg.renewAuto = !!d.renew_auto
    renewDlg.privacy = !!d.privacy
    renewDlg.pricePerYear = d.price_per_year || 0
    renewDlg.currency = d.currency || ''
    renewDlg.env = d.env || '生产'
    renewDlg.dryRun = !!d.dry_run
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '读取域名详情失败（仍可尝试续费）')
  } finally { renewDlg.loading = false }
}
// 续费记录台账
const renewLog = reactive({ show: false, loading: false, items: [], total: 0, page: 1, size: 20 })
function openRenewLog() { renewLog.show = true; renewLog.page = 1; loadRenewLog() }
async function loadRenewLog() {
  renewLog.loading = true
  try {
    const r = await listRenewals({ limit: renewLog.size, offset: (renewLog.page - 1) * renewLog.size })
    renewLog.items = r.items || []
    renewLog.total = r.total || 0
  } catch (e) { renewLog.items = [] } finally { renewLog.loading = false }
}
async function onToggleAuto(val) {
  renewDlg.autoBusy = true
  try {
    const r = await setAutoRenew(renewDlg.ciid, val)
    ElMessage.success(r.msg || '已设置')
  } catch (e) {
    renewDlg.renewAuto = !val // 回滚开关
    ElMessage.error(e.response?.data?.error || '设置失败')
  } finally { renewDlg.autoBusy = false }
}
async function doRenew() {
  const cost = renewDlg.pricePerYear > 0
    ? `预估域名费 ${renewDlg.currency} ${(renewDlg.pricePerYear * renewDlg.period).toFixed(2)}（另加税费/规费，实扣总额以 GoDaddy 结算为准）`
    : '（价格以 GoDaddy 结算为准）'
  const warn = renewDlg.dryRun
    ? `【预演模式】对 ${renewDlg.domain} 续费 ${renewDlg.period} 年，${cost}（不会真扣费，只核对报文）？`
    : `确认对 ${renewDlg.domain} 续费 ${renewDlg.period} 年？\n${cost}\n⚠ 会立即向 GoDaddy 账户真实扣费，不可撤销！`
  try {
    await app.showConfirm(warn, '确认续费（写回 GoDaddy）')
  } catch (e) { return }
  renewDlg.renewBusy = true
  try {
    const r = await renewDomain(renewDlg.ciid, {
      period: renewDlg.period,
      quoted_amount: renewDlg.pricePerYear > 0 ? +(renewDlg.pricePerYear * renewDlg.period).toFixed(2) : 0,
      quoted_currency: renewDlg.currency,
    })
    // 成功提示写清订单号 + 到期前后，防超付一眼可核
    const parts = [r.msg || '已续费']
    if (r.order_id) parts.push(`订单号 ${r.order_id}`)
    if (r.expiry_before && r.expiry_after) parts.push(`到期 ${r.expiry_before} → ${r.expiry_after}`)
    if (!r.dry_run) parts.push('精确扣费以 GoDaddy 账单为准，凭订单号核对')
    app.showConfirm(parts.join('\n'), '续费结果').catch(() => {}) // 用确认框展示，方便看清/复制订单号
    renewDlg.show = false
    await load()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '续费失败')
  } finally { renewDlg.renewBusy = false }
}
const rows = ref([]), allDomains = ref([]), registrars = ref([]), loading = ref(false)
const loadErr = ref('')
const checking = ref({}), syncingAll = ref(false)
const dlg = ref(false), editing = ref(false), form = ref({})
// 编辑表单：项目多选(数组) ↔ form.project(逗号字符串)
const formProjectArr = computed({ get: () => projList(form.value.project), set: (v) => { form.value.project = (v || []).join(',') } })
const selected = ref([]), tableRef = ref()
const statusView = ref('normal') // normal=未忽略 / ignored / all
const tab = ref('records') // records=主机头台账 / domains=主域名
// 主域名 tab
const domPage = ref(1), domPageSize = ref(10)
const df = useDomainFilter(allDomains) // 状态/来源/到期/关键词 统一筛选
const domDlg = ref(false), domEditing = ref(false), domForm = ref({})
const refreshingAll = ref(false), refreshingDom = ref({})
const igDlg = ref(false), igRows = ref([]), igReason = ref('')
const bDlg = ref(false), bForm = ref({})
const dDlg = ref(false), detail = ref(null)
const aDlg = ref(false), aForm = ref({ project: '', env: '', blocks: [{ module: '', domains: '' }] }), aResult = ref(null), assigning = ref(false)
const aMode = ref('paste'), pasteText = ref('')
const synced = computed(() => editing.value && form.value.origin && form.value.origin !== 'manual')
const f = ref({ keyword: '', domain: null, project: null, env: null, module: null, source: null, pstatus: null })
const query = ref({ ...f.value })
const currentPage = ref(1), pageSize = ref(10)

// —— 配色：项目/环境优先用「基础配置」里配的颜色，没配则按名字 hash 出冷色（红橙只留给证书告警）——
const COOL = ['#3b7dd8', '#5b8ff9', '#269a99', '#5ad8a6', '#6dc8ec', '#9270ca', '#5d7092', '#0e7a6e', '#7d5fd6', '#2f9e8f', '#3d76c9', '#417ec0']
function hashColor(s) { if (!s) return '#909399'; let h = 0; for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0; return COOL[h % COOL.length] }
const ENV_MAP = { PROD: '#3b7dd8', PRD: '#3b7dd8', 生产: '#3b7dd8', UAT: '#269a99', SIT: '#5d7092', PRE: '#9270ca', DEV: '#5ad8a6', 开发: '#5ad8a6', TEST: '#6dc8ec', 测试: '#6dc8ec' }
function projectColor(name) { const p = app.projects.find((x) => x.name === name); return (p && p.color) || hashColor(name) }
// 多项目：project 字段逗号分隔存多个项目
function projList(s) { return (s || '').split(/[,，]/).map((x) => x.trim()).filter(Boolean) }
function projStatus(name) { const p = app.projects.find((x) => x.name === name); return (p && p.status) || '' }
function dotStyle(color) { return { display: 'inline-block', width: '8px', height: '8px', borderRadius: '50%', background: color, marginLeft: '4px', verticalAlign: 'middle' } }
function envColor(e) { if (!e) return '#909399'; const env = app.environments.find((x) => x.code === e); if (env && env.color) return env.color; return ENV_MAP[e.toUpperCase()] || ENV_MAP[e] || hashColor(e) }
function tagStyle(color) { return { color, borderColor: color + '66', background: color + '14' } }
// 证书到期：语义告警色（过期红 / 快到期橙 / 正常绿 / 未检测·失败灰）
function certState(r) {
  if (r.cert_expiry_at) {
    const days = (new Date(r.cert_expiry_at) - Date.now()) / 86400000
    if (days < 0) return { type: 'danger', text: '已过期 ' + r.cert_expiry_at }
    if (days < 30) return { type: 'warning', text: r.cert_expiry_at }
    return { type: 'success', text: r.cert_expiry_at }
  }
  if (r.cert_check_msg) return { type: 'info', text: '检测失败' }
  return { type: 'info', text: '未检测' }
}

const distinct = (key) => [...new Set(rows.value.map((r) => r[key]).filter(Boolean))].sort()
const domainOptions = computed(() => distinct('domain'))
const projectOptions = computed(() => [...new Set(rows.value.flatMap((r) => projList(r.project)))].sort()) // 多项目：拆分成单个项目
const envOptions = computed(() => distinct('env'))
const moduleOptions = computed(() => distinct('module'))

const filteredRows = computed(() => rows.value.filter((r) => {
  const q = query.value
  const kw = q.keyword?.toLowerCase()
  return (!kw || r.fqdn.toLowerCase().includes(kw) || (r.project || '').toLowerCase().includes(kw) ||
      (r.module || '').toLowerCase().includes(kw) || (r.cname || '').toLowerCase().includes(kw) ||
      (r.origin_ip || '').toLowerCase().includes(kw) || (r.auto_origin_ip || '').toLowerCase().includes(kw)) &&
    (!q.domain || r.domain === q.domain) &&
    (!q.project || (q.project === '__none__' ? !r.project : projList(r.project).includes(q.project))) &&
    (!q.env || (q.env === '__none__' ? !r.env : r.env === q.env)) &&
    (!q.module || (q.module === '__none__' ? !r.module : r.module === q.module)) &&
    (!q.source || r.source_name === q.source) &&
    (!q.pstatus || projList(r.project).some((p) => projStatus(p) === q.pstatus))
}))
// 外部排序：对全量 filteredRows 先排序，再分页（避免 el-table 只排当前页）
const sortState = ref({ prop: 'project', order: 'ascending' })
function onSort({ prop, order }) { sortState.value = { prop, order } }
function sortList(list, prop, order) {
  if (!prop || !order) return list
  const dir = order === 'ascending' ? 1 : -1
  const isDate = prop === 'cert_expiry_at' || prop === 'expiry_at'
  return [...list].sort((a, b) => {
    const av = a[prop], bv = b[prop]
    if (isDate) { // 空到期日永远排最后，不受升降序影响
      if (!av && !bv) return 0
      if (!av) return 1
      if (!bv) return -1
      return dir * (new Date(av) - new Date(bv))
    }
    return dir * String(av || '').localeCompare(String(bv || ''), 'zh')
  })
}
const sortedRows = computed(() => sortList(filteredRows.value, sortState.value.prop, sortState.value.order))
const pagedRows = computed(() => {
  const s = (currentPage.value - 1) * pageSize.value
  return sortedRows.value.slice(s, s + pageSize.value)
})
// —— 按模块聚合视图（项目+环境+模块 分组，组内每域名独立）——
const recordView = ref('detail')
const openGroups = ref([])
const groupPage = ref(1), groupSize = ref(10) // 模块视图独立分页（每页模块数），不与明细共用 pageSize
const innerSize = 20                            // 组内域名每页条数（大组如"无模块"不再一坨）
const innerPage = ref({})                       // key -> 组内页码
function onRecordViewChange() { currentPage.value = 1; groupPage.value = 1 }
function groupRows(g) { const p = innerPage.value[g.key] || 1; return g.rows.slice((p - 1) * innerSize, p * innerSize) }
function setInnerPage(key, p) { innerPage.value = { ...innerPage.value, [key]: p } }
const moduleGroups = computed(() => {
  const map = new Map()
  for (const r of sortedRows.value) {
    const key = `${r.project || ''}|${r.env || ''}|${r.module || ''}`
    let g = map.get(key)
    if (!g) { g = { key, project: r.project, env: r.env, module: r.module, rows: [] }; map.set(key, g) }
    g.rows.push(r)
  }
  return [...map.values()].map((g) => {
    const ips = new Set(g.rows.map((r) => r.origin_ip || r.auto_origin_ip).filter(Boolean))
    const certFail = g.rows.filter((r) => !r.cert_expiry_at && r.cert_check_msg).length
    const certExpiring = g.rows.filter((r) => r.cert_expiry_at && (new Date(r.cert_expiry_at) - Date.now()) / 86400000 < 30).length
    return { ...g, count: g.rows.length, ipCount: ips.size, certFail, certExpiring }
  })
})
const pagedGroups = computed(() => {
  const s = (groupPage.value - 1) * groupSize.value
  return moduleGroups.value.slice(s, s + groupSize.value)
})
// 默认折叠、展开才渲染组内表格（懒渲染）：切换视图/翻页瞬间只渲染组头，不再一次性渲染上百个表格
watch([groupPage, recordView], () => { openGroups.value = [] })
function expandAllGroups() { openGroups.value = pagedGroups.value.map((g) => g.key) }
function collapseAllGroups() { openGroups.value = [] }
function doSearch() { query.value = { ...f.value }; currentPage.value = 1 }
function resetFilter() { f.value = { keyword: '', domain: null, project: null, env: null, module: null, source: null, pstatus: null }; query.value = { ...f.value }; currentPage.value = 1 }

// —— 主域名 tab ——
const resoCountMap = computed(() => {
  const m = {}
  for (const r of rows.value) m[r.domain_ci_id] = (m[r.domain_ci_id] || 0) + 1
  return m
})
const domNameFilter = ref(null)
const lifeStatusFilter = ref(null)
const domSelected = ref([])
const domNameOptions = computed(() => [...new Set(allDomains.value.map((d) => d.name).filter(Boolean))].sort())
const domFiltered = computed(() => {
  let base = df.filtered.value
  if (domNameFilter.value) base = base.filter((d) => d.name === domNameFilter.value)
  if (lifeStatusFilter.value === '__none__') base = base.filter((d) => !d.life_status)
  else if (lifeStatusFilter.value) base = base.filter((d) => d.life_status === lifeStatusFilter.value)
  return base
})
async function setDomStatus(rows, status) {
  try { await bulkDomainStatus(rows.map((r) => r.ci_id), status || ''); ElMessage.success('已更新使用状态'); load() }
  catch (e) { ElMessage.error(e.response?.data?.error || '失败') }
}
const domSortState = ref({ prop: '', order: null })
function onDomSort({ prop, order }) { domSortState.value = { prop, order } }
const domSorted = computed(() => sortList(domFiltered.value, domSortState.value.prop, domSortState.value.order))
const domPaged = computed(() => {
  const s = (domPage.value - 1) * domPageSize.value
  return domSorted.value.slice(s, s + domPageSize.value)
})
function doDomSearch() { df.doSearch(); domPage.value = 1 }
function resetDomFilter() { df.reset(); domNameFilter.value = null; domPage.value = 1 }
function sortByDate(a, b) { if (!a && !b) return 0; if (!a) return 1; if (!b) return -1; return new Date(a) - new Date(b) }
function expiryClass(d) { if (!d) return ''; const days = (new Date(d) - Date.now()) / 86400000; return days < 0 ? 'exp-red' : (days < 30 ? 'exp-orange' : '') }
function isExpired(d) { return d && new Date(d) < new Date() }
// 已过期且未忽略、未移出账号的主域名——用于「一键忽略已过期」
const expiredDomains = computed(() => allDomains.value.filter((d) => !d.ignored && !d.stale && isExpired(d.expiry_at)))
async function ignoreExpired() {
  const list = expiredDomains.value
  if (!list.length) return
  try {
    await app.showConfirm(`将 ${list.length} 个已过期主域名标为「忽略」？仅隐藏、不删数据——不再展示、不计入到期巡检和总览统计，可随时恢复`)
    const r = await bulkIgnoreDomains(list.map((d) => d.ci_id), 1, '已过期')
    ElMessage.success(`已忽略 ${r.count ?? list.length} 个`); load()
  } catch (e) { if (e !== 'cancel') ElMessage.error(e.response?.data?.error || '忽略失败') }
}
async function ignoreDomains(list, ignored) {
  try {
    if (ignored) await app.showConfirm(`忽略主域名 ${list.map((d) => d.name).join('、')}？仅隐藏、不删数据——不再展示、不计入到期巡检和总览统计，可随时恢复`)
    const r = await bulkIgnoreDomains(list.map((d) => d.ci_id), ignored ? 1 : 0, ignored ? '手动忽略' : '')
    ElMessage.success(ignored ? `已忽略 ${r.count ?? list.length} 个` : `已取消忽略 ${r.count ?? list.length} 个`); load()
  } catch (e) { if (e !== 'cancel') ElMessage.error(e.response?.data?.error || '操作失败') }
}
function jumpToRecords(row) { tab.value = 'records'; f.value = { keyword: '', domain: row.name, project: null, env: null, module: null, source: null, pstatus: null }; query.value = { ...f.value }; currentPage.value = 1 }
function openAddDomain() { domEditing.value = false; domForm.value = { name: '', registrar_id: null, dns_provider: '', expiry_at: '' }; domDlg.value = true }
function openEditDomain(row) { domEditing.value = true; domForm.value = { ...row, expiry_at: row.expiry_at || '' }; domDlg.value = true }
async function saveDomain() {
  try {
    if (domEditing.value) await updateDomain(domForm.value.ci_id, domForm.value)
    else await createDomain(domForm.value)
    ElMessage.success('已保存'); domDlg.value = false; load()
  } catch (e) { ElMessage.error(e.response?.data?.error || '保存失败') }
}
async function delDomain(row) {
  try {
    await app.showConfirm(row.stale ? `该域名 GoDaddy 已无，确认从台账移除 ${row.name}？其下解析一并删除`
      : `确认删除手动录入的域名 ${row.name}？其下解析一并删除`)
    await deleteDomain(row.ci_id); ElMessage.success('已删除'); load()
  } catch (e) { if (e !== 'cancel') ElMessage.error('删除失败') }
}
async function refreshOneDom(row) {
  refreshingDom.value = { ...refreshingDom.value, [row.ci_id]: true }
  try { const r = await refreshDomain(row.ci_id); ElMessage.success(r.msg || '已刷新'); await load() }
  catch (e) { ElMessage.error('刷新失败') } finally { refreshingDom.value = { ...refreshingDom.value, [row.ci_id]: false } }
}
async function refreshAllDom() {
  refreshingAll.value = true
  try {
    const r = await refreshAllDomains()
    if (r.failures && r.failures.length) ElMessage.warning(`${r.msg}（${r.failures.length} 项失败，详见执行记录）`)
    else ElMessage.success(r.msg || '已刷新')
    await load()
  } catch (e) { ElMessage.error('刷新失败') } finally { refreshingAll.value = false }
}

async function load() {
  loading.value = true
  try {
    loadErr.value = ''
    rows.value = await listAllRecords(statusView.value)
    allDomains.value = await listDomains()
    registrars.value = await listRegistrars()
    app.loadBasics()
  } catch (e) {
    loadErr.value = normalizeError(e).message
    rows.value = []; allDomains.value = []
  } finally { loading.value = false }
}

function openAdd() {
  editing.value = false
  form.value = { domain_ci_id: null, host: '', record_type: 'A', project: '', env: '', module: '', life_status: '', cdn_id: null, cname: '', origin_ip: '', cert_expiry_at: '' }
  dlg.value = true
}
function openEdit(row) {
  editing.value = true
  form.value = { ...row, cert_expiry_at: row.cert_expiry_at || '' }
  dlg.value = true
}
async function save() {
  try {
    if (editing.value) {
      await updateRecord(form.value.id, form.value)
    } else {
      if (!form.value.domain_ci_id) { ElMessage.warning('请选择主域名'); return }
      if (!form.value.host) { ElMessage.warning('主机头必填'); return }
      await createRecord(form.value.domain_ci_id, form.value)
    }
    ElMessage.success('已保存'); dlg.value = false; load()
  } catch (e) { ElMessage.error(e.response?.data?.error || '保存失败') }
}
async function del(row) {
  try {
    await app.showConfirm(row.stale ? `该解析 GoDaddy 已删除，确认从台账移除 ${row.fqdn}？` : `删除解析 ${row.fqdn}？`)
    await deleteRecord(row.id); ElMessage.success('已删除'); load()
  } catch (e) { if (e !== 'cancel') ElMessage.error('删除失败') }
}
function onStatusChange() { currentPage.value = 1; tableRef.value?.clearSelection(); load() }
function openIgnore(list) { igRows.value = list; igReason.value = ''; igDlg.value = true }
async function confirmIgnore() {
  try {
    const r = await bulkIgnoreRecords({ ids: igRows.value.map((x) => x.id), ignored: true, reason: igReason.value })
    ElMessage.success(`已忽略 ${r.updated} 条`); igDlg.value = false; tableRef.value?.clearSelection(); load()
  } catch (e) { ElMessage.error(e.response?.data?.error || '忽略失败') }
}
async function doUnignore(list) {
  try {
    const r = await bulkIgnoreRecords({ ids: list.map((x) => x.id), ignored: false })
    ElMessage.success(`已取消忽略 ${r.updated} 条`); tableRef.value?.clearSelection(); load()
  } catch (e) { ElMessage.error(e.response?.data?.error || '操作失败') }
}
function openDetail(row) { detail.value = row; dDlg.value = true }
function editFromDetail() { dDlg.value = false; openEdit(detail.value) }
function openBulk() {
  bForm.value = { setProject: false, project: [], setEnv: false, env: '', setModule: false, module: '', setCdn: false, cdn_id: null, setCname: false, cname: '', setOriginIP: false, origin_ip: '' }
  bDlg.value = true
}

// —— 源站映射规则 ——
const rulesDlg = ref(false), ruleOneDlg = ref(false), rulesLoading = ref(false)
const rules = ref([])
const ruleForm = ref({ cname: '', origin_ip: '' })
async function loadRules() {
  rulesLoading.value = true
  try { rules.value = await listOriginRules() } catch (e) { rules.value = [] } finally { rulesLoading.value = false }
}
function openRules() { ruleForm.value = { cname: '', origin_ip: '' }; rulesDlg.value = true; loadRules() }
function editRule(row) { ruleForm.value = { cname: row.cname, origin_ip: row.origin_ip } }
async function saveRule() {
  if (!ruleForm.value.cname.trim()) { ElMessage.warning('回源 CNAME 必填'); return }
  try { await upsertOriginRule({ cname: ruleForm.value.cname.trim(), origin_ip: ruleForm.value.origin_ip.trim() })
    ElMessage.success('已保存'); ruleForm.value = { cname: '', origin_ip: '' }; loadRules(); load() }
  catch (e) { ElMessage.error(e.response?.data?.error || '保存失败') }
}
async function removeRule(row) {
  try { await app.showConfirm(`删除规则「${row.cname} → ${row.origin_ip}」？回源到它的记录源站IP 将回到空/DNS解析`)
    await deleteOriginRule(row.id); ElMessage.success('已删除'); loadRules(); load() }
  catch (e) { if (e !== 'cancel') ElMessage.error(e.response?.data?.error || '删除失败') }
}
function openRuleFor(row) { ruleForm.value = { cname: row.cname, origin_ip: '' }; ruleOneDlg.value = true }
async function saveRuleOne() {
  try { await upsertOriginRule({ cname: ruleForm.value.cname.trim(), origin_ip: ruleForm.value.origin_ip.trim() })
    ElMessage.success('已保存规则'); ruleOneDlg.value = false; load() }
  catch (e) { ElMessage.error(e.response?.data?.error || '保存失败') }
}
async function saveBulk() {
  const b = bForm.value
  const payload = { ids: selected.value.map((r) => r.id) }
  if (b.setProject) payload.project = (b.project || []).join(',')
  if (b.setEnv) payload.env = b.env || ''
  if (b.setModule) payload.module = b.module || ''
  if (b.setCdn) { payload.set_cdn = true; payload.cdn_id = b.cdn_id ?? null }
  if (b.setCname) { payload.set_cname = true; payload.cname = b.cname || '' }
  if (b.setOriginIP) { payload.set_origin_ip = true; payload.origin_ip = b.origin_ip || '' }
  if (payload.project === undefined && payload.env === undefined && payload.module === undefined && !payload.set_cdn && !payload.set_cname && !payload.set_origin_ip) {
    ElMessage.warning('请至少打开一个要设置的字段'); return
  }
  try {
    const r = await bulkUpdateRecords(payload)
    ElMessage.success(`已批量更新 ${r.updated} 条`); bDlg.value = false; tableRef.value?.clearSelection(); load()
  } catch (e) { ElMessage.error(e.response?.data?.error || '批量设置失败') }
}
// 无条件清空选择（含 reserve-selection 跨页/跨筛选记着的），解决"取消全选跨筛选点不动"
function clearSel() { tableRef.value?.clearSelection(); selected.value = [] }
// —— 批量分配：项目/环境统一，多个模块块各带一批 FQDN，完整精确匹配 ——
function parseDomains(s) { return (s || '').split(/[\n,;，；]+/).map((x) => x.trim().toLowerCase()).filter(Boolean) }
function countDomains(s) { return parseDomains(s).length }
// 两列粘贴：每行「服务名<空白>域名」→ 左列=模块(原样)，右列=FQDN
const parsedPaste = computed(() => (pasteText.value || '').split('\n').map((l) => l.trim()).filter(Boolean).map((l) => {
  const parts = l.split(/[,，\s]+/).filter(Boolean) // 逗号(中英)/Tab/空格 任意分隔
  if (parts.length < 2) return null
  return { module: parts[0], fqdn: parts[parts.length - 1].toLowerCase() }
}).filter(Boolean))
const totalAssignDomains = computed(() => aMode.value === 'paste'
  ? parsedPaste.value.length
  : aForm.value.blocks.reduce((n, b) => n + countDomains(b.domains), 0))
// 统一成 [{module, domains(换行拼接)}]，两种模式共用应用逻辑
function assignGroups() {
  if (aMode.value === 'paste') {
    const map = new Map()
    for (const { module, fqdn } of parsedPaste.value) {
      if (!map.has(module)) map.set(module, [])
      map.get(module).push(fqdn)
    }
    return [...map.entries()].map(([module, domains]) => ({ module, domains: domains.join('\n') }))
  }
  return aForm.value.blocks
}
function openAssign() { aForm.value = { project: '', env: '', blocks: [{ module: '', domains: '' }] }; pasteText.value = ''; aResult.value = null; aDlg.value = true }
function resetAssign() { aForm.value = { project: '', env: '', blocks: [{ module: '', domains: '' }] }; pasteText.value = ''; aResult.value = null }
async function copyNotFound() {
  const list = aResult.value?.notFound || []
  const text = list.join('\n')
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success(`已复制 ${list.length} 个未找到域名`)
  } catch (e) {
    const ta = document.createElement('textarea')
    ta.value = text; ta.style.position = 'fixed'; ta.style.opacity = '0'
    document.body.appendChild(ta); ta.select()
    try { document.execCommand('copy'); ElMessage.success(`已复制 ${list.length} 个未找到域名`) }
    catch (e2) { ElMessage.error('复制失败，请手动选中') }
    document.body.removeChild(ta)
  }
}
async function applyAssign() {
  const fqdnMap = new Map(rows.value.map((r) => [r.fqdn.toLowerCase(), r.id]))
  const groups = [], notFound = []
  let updated = 0
  assigning.value = true
  try {
    for (const b of assignGroups()) {
      const doms = parseDomains(b.domains)
      if (!doms.length) continue
      const ids = []
      for (const d of doms) { const id = fqdnMap.get(d); if (id) ids.push(id); else notFound.push(d) }
      const payload = { ids }
      if (aForm.value.project) payload.project = aForm.value.project
      if (aForm.value.env) payload.env = aForm.value.env
      if (b.module) payload.module = b.module
      if (!ids.length || (payload.project === undefined && payload.env === undefined && payload.module === undefined)) continue
      const r = await bulkUpdateRecords(payload)
      updated += r.updated || 0
      groups.push({ module: b.module || '(未设模块)', count: r.updated || 0 })
    }
    aResult.value = { updated, groups, notFound: [...new Set(notFound)] }
  } catch (e) { ElMessage.error(e.response?.data?.error || '批量分配失败') }
  finally { assigning.value = false }
}
// 导出当前筛选结果为 CSV（Excel 可直接打开，UTF-8 BOM 防中文乱码）
function exportCsv() {
  const headers = ['项目', '环境', '模块', '域名', 'CDN厂商', '回源CNAME', '源站IP', '证书到期', '来源', '操作人', '更新时间']
  const esc = (v) => { v = (v == null ? '' : String(v)); return /[",\n]/.test(v) ? '"' + v.replace(/"/g, '""') + '"' : v }
  const lines = [headers.join(',')]
  for (const r of filteredRows.value) {
    lines.push([r.project, r.env, r.module, r.fqdn, r.cdn_name, r.cname, r.origin_ip, r.cert_expiry_at,
      r.origin === 'manual' ? '手动录入' : (r.source_name || '同步'), r.operator, r.updated_at].map(esc).join(','))
  }
  const blob = new Blob(['\ufeff' + lines.join('\n')], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url; a.download = 'cmdb-业务域名.csv'; a.click()
  URL.revokeObjectURL(url)
}
async function checkCert(row) {
  checking.value = { ...checking.value, [row.id]: true }
  try {
    const res = await checkRecordCert(row.id)
    if (res.ok) ElMessage.success(`${res.fqdn} 证书到期 ${res.cert_expiry_at}`)
    else ElMessage.warning(`${res.fqdn} 检测失败：${res.msg}`)
    load()
  } catch (e) { ElMessage.error('检测失败') } finally { checking.value = { ...checking.value, [row.id]: false } }
}
async function syncAll() {
  const targets = allDomains.value.filter((d) => d.origin !== 'manual' && !d.stale)
  if (!targets.length) { ElMessage.info('没有可从数据源同步的域名'); return }
  syncingAll.value = true
  let ok = 0, imported = 0, failed = 0
  for (const d of targets) {
    try { const r = await syncDomainRecords(d.ci_id); ok++; imported += r.imported_records || 0 }
    catch (e) { failed++ }
  }
  ElMessage.success(`同步完成：${ok} 个域名，新导入 ${imported} 条解析${failed ? `，${failed} 个失败` : ''}`)
  syncingAll.value = false; load()
}
// 证书到期排序：无到期日（未检测/失败）排最后
function sortByCertExpiry(a, b) {
  if (!a.cert_expiry_at && !b.cert_expiry_at) return 0
  if (!a.cert_expiry_at) return 1
  if (!b.cert_expiry_at) return -1
  return new Date(a.cert_expiry_at) - new Date(b.cert_expiry_at)
}
onMounted(load)
</script>

<style scoped>
.brsum { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; }
.brsum .muted { font-size: 12px; }
.ok { color: #2f7d31; }
.filter { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.mono { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12px; }
.stale { text-decoration: line-through; color: #b0b3bb; }
.ignored { color: #b0b3bb; }
.detail-val { word-break: break-all; }
.exp-red { color: #f56c6c; font-weight: 600; }
.exp-orange { color: #e6a23c; font-weight: 600; }
.lvl-tabs { margin-top: -6px; margin-bottom: 4px; }
.lvl-tabs :deep(.el-tabs__header) { margin-bottom: 0; }
</style>
