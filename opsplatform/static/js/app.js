// Vue 应用
const { createApp } = Vue;

createApp({
    data() {
        return {
            // 主题状态
            isDarkMode: localStorage.getItem('theme') !== 'light',

            // 用户状态
            currentUser: JSON.parse(localStorage.getItem('currentUser')),
            loginForm: { username: '', password: '' },
            loginLoading: false,

            // 页面状态
            currentTab: 'records',
            expandedGroups: { data: true, ops: true, system: true },
            inspectionSubTab: 'templates',
            sidebarCollapsed: false,

            // 数据
            records: [],
            users: [],
            auditLogs: [],
            datasources: [],
            selectedRecords: [],

            // 搜索和过滤
            searchQuery: '',
            auditSearchQuery: '',
            projectFilter: '',
            statusFilter: '',
            envFilter: '',
            actionFilter: '',
            sortField: '',
            sortOrder: 'asc',

            // 分页
            currentPage: 1,
            pageSize: 10,
            jumpPage: 1,

            // 模态框状态
            showRecordModal: false,
            showBatchModal: false,
            showUserModal: false,
            showDeleteModal: false,
            showAuditDetailModal: false,
            showDataSourceModal: false,
            showProfileModal: false,

            // 面包屑导航
            breadcrumbVisible: true,
            breadcrumbs: [],

            // 用户菜单
            showUserMenu: false,
            
            // 操作菜单
            activeActionMenu: null,
            
            // 批量状态菜单
            showBatchStatusMenu: false,

            // 表单模式
            recordModalMode: 'add',
            userModalMode: 'add',
            dataSourceModalMode: 'add',

            // 表单数据
            recordForm: { id: '', connection_id: '', project: '', env: 'uat', vid: '', src_ip: '', dest_ip: '', port: '', status: 'active' },
            userForm: { id: '', username: '', password: '', display_name: '', role: 'user', status: 'active', permissions: [] },
            dataSourceForm: { id: '', name: '', type: 'prometheus', url: '', username: '', password: '', token: '', description: '', status: 'active' },
            profileForm: { id: '', username: '', password: '', display_name: '', role: '' },

            // 批量添加
            batchText: '',
            batchRecords: [],
            batchError: '',
            batchEnv: 'uat',
            batchStatus: 'active',

            // 删除
            deleteTarget: null,
            deleteType: '',
            deleteMessage: '',

            // 审计详情
            auditDetail: null,

            // Toast
            toast: { show: false, message: '', type: 'success' },

            // 巡检相关
            showInspectionModal: false,
            inspectionForm: {
                selectedDataSources: [],
                reportType: 'daily',
                includeMetrics: ['cpu', 'memory', 'disk', 'network']
            },
            inspectionRunning: false,
            inspectionResults: [],
            availableMetrics: [
                { key: 'cpu', label: 'CPU 使用率', promql: '100 - (avg by(instance) (irate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)' },
                { key: 'memory', label: '内存使用率', promql: '(1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100' },
                { key: 'disk', label: '磁盘使用率', promql: '(1 - node_filesystem_avail_bytes / node_filesystem_size_bytes) * 100' },
                { key: 'network', label: '网络流量', promql: 'irate(node_network_receive_bytes_total[5m])' },
                { key: 'uptime', label: '运行时间', promql: 'node_time_seconds - node_boot_time_seconds' },
                { key: 'load', label: '系统负载', promql: 'node_load1' }
            ],

            // 权限配置
            allPermissions: [
                { key: 'records', label: '数据源ID', group: '数据管理' },
                { key: 'domains', label: '域名管理', group: '数据管理' },
                { key: 'inspection', label: '一键巡检', group: '运维工具' },
                { key: 'datasources', label: '数据源管理', group: '系统管理' },
                { key: 'audit', label: '审计日志', group: '系统管理' },
                { key: 'users', label: '用户管理', group: '系统管理' }
            ]
        };
    },

    computed: {
        projectList() {
            const projects = [...new Set((this.records || []).map(r => r.project).filter(Boolean))];
            return projects.sort((a, b) => a.localeCompare(b, 'zh-CN'));
        },
        filteredRecords() {
            let r = this.records || [];
            if (this.projectFilter) r = r.filter(x => x.project === this.projectFilter);
            if (this.envFilter) r = r.filter(x => x.env === this.envFilter);
            if (this.statusFilter) r = r.filter(x => x.status === this.statusFilter);
            if (this.searchQuery) {
                const q = this.searchQuery.toLowerCase();
                r = r.filter(x => [x.connection_id, x.vid, x.src_ip, x.dest_ip, x.port].some(v => v && v.toLowerCase().includes(q)));
            }
            if (this.sortField) {
                r = [...r].sort((a, b) => {
                    let va = a[this.sortField] || '';
                    let vb = b[this.sortField] || '';
                    const cmp = va.localeCompare(vb, 'zh-CN', { numeric: true });
                    return this.sortOrder === 'asc' ? cmp : -cmp;
                });
            }
            return r;
        },

        // 分页后的记录
        paginatedRecords() {
            const start = (this.currentPage - 1) * this.pageSize;
            const end = start + this.pageSize;
            return this.filteredRecords.slice(start, end);
        },

        // 总页数
        totalPages() {
            return Math.max(1, Math.ceil(this.filteredRecords.length / this.pageSize));
        },

        // 显示的页码
        displayedPages() {
            const total = this.totalPages;
            const current = this.currentPage;
            const pages = [];
            
            if (total <= 7) {
                for (let i = 1; i <= total; i++) pages.push(i);
            } else {
                if (current <= 4) {
                    for (let i = 1; i <= 5; i++) pages.push(i);
                    pages.push('...');
                    pages.push(total);
                } else if (current >= total - 3) {
                    pages.push(1);
                    pages.push('...');
                    for (let i = total - 4; i <= total; i++) pages.push(i);
                } else {
                    pages.push(1);
                    pages.push('...');
                    for (let i = current - 1; i <= current + 1; i++) pages.push(i);
                    pages.push('...');
                    pages.push(total);
                }
            }
            return pages;
        },

        filteredAuditLogs() {
            let r = this.auditLogs || [];
            if (this.actionFilter) r = r.filter(x => x.action === this.actionFilter);
            if (this.auditSearchQuery) {
                const q = this.auditSearchQuery.toLowerCase();
                r = r.filter(x => [x.operator, x.changes].some(v => v && v.toLowerCase().includes(q)));
            }
            return r;
        },

        isAllSelected() {
            const filtered = this.filteredRecords || [];
            const selected = this.selectedRecords || [];
            return filtered.length > 0 && selected.length === filtered.length;
        },

        isPartialSelected() {
            const filtered = this.filteredRecords || [];
            const selected = this.selectedRecords || [];
            return selected.length > 0 && selected.length < filtered.length;
        },

        permissionGroups() {
            const groups = {};
            (this.allPermissions || []).forEach(p => {
                if (!groups[p.group]) groups[p.group] = { name: p.group, items: [] };
                groups[p.group].items.push(p);
            });
            return Object.values(groups);
        },

        prometheusSources() {
            return (this.datasources || []).filter(ds => ds.type === 'prometheus' && ds.status === 'active');
        }
    },

    watch: {
        searchQuery() { this.currentPage = 1; },
        projectFilter() { this.currentPage = 1; },
        envFilter() { this.currentPage = 1; },
        statusFilter() { this.currentPage = 1; },
        currentPage(val) { this.jumpPage = val; },
        currentTab() { this.updateBreadcrumbs(); }
    },

    mounted() {
        // 初始化主题
        this.initTheme();

        if (this.currentUser) {
            this.loadRecords();
            this.refreshCurrentUser();
            this.updateBreadcrumbs();
        }

        // 点击外部关闭用户菜单和操作菜单
        document.addEventListener('click', (e) => {
            if (!e.target.closest('.user-menu-container')) {
                this.showUserMenu = false;
            }
            if (!e.target.closest('.action-menu-container')) {
                this.activeActionMenu = null;
            }
            if (!e.target.closest('.batch-status-dropdown')) {
                this.showBatchStatusMenu = false;
            }
        });
    },

    methods: {
        // ========== 主题相关 ==========
        initTheme() {
            const savedTheme = localStorage.getItem('theme');
            if (savedTheme === 'light') {
                document.documentElement.setAttribute('data-theme', 'light');
                this.isDarkMode = false;
            } else {
                document.documentElement.removeAttribute('data-theme');
                this.isDarkMode = true;
            }
        },

        toggleTheme() {
            this.isDarkMode = !this.isDarkMode;
            if (this.isDarkMode) {
                document.documentElement.removeAttribute('data-theme');
                localStorage.setItem('theme', 'dark');
            } else {
                document.documentElement.setAttribute('data-theme', 'light');
                localStorage.setItem('theme', 'light');
            }
        },

        // ========== 面包屑导航 ==========
        updateBreadcrumbs() {
            const tabMap = {
                'records': '数据源ID',
                'domains': '域名管理',
                'inspection': '一键巡检',
                'datasources': '数据源管理',
                'audit': '审计日志',
                'users': '用户管理'
            };
            const groupMap = {
                'records': '数据管理',
                'domains': '数据管理',
                'inspection': '运维工具',
                'datasources': '系统管理',
                'audit': '系统管理',
                'users': '系统管理'
            };
            const current = tabMap[this.currentTab] || '';
            const group = groupMap[this.currentTab] || '';
            if (current) {
                this.breadcrumbs = ['运维管理平台', group, current];
            } else {
                this.breadcrumbs = ['运维管理平台'];
            }
            // 切换页面时自动显示面包屑
            this.breadcrumbVisible = true;
        },

        goToBreadcrumb(index) {
            if (index === 0) {
                // 回到首页，可以设置默认tab
                this.currentTab = 'records';
            } else if (index === 1) {
                // 跳转到分组的第一项
                const group = this.breadcrumbs[1];
                if (group === '数据管理') {
                    this.currentTab = 'records';
                } else if (group === '运维工具') {
                    this.currentTab = 'inspection';
                } else if (group === '系统管理') {
                    this.currentTab = 'datasources';
                }
            }
        },

        closeBreadcrumb() {
            this.breadcrumbVisible = false;
        },

        toggleUserMenu() {
            this.showUserMenu = !this.showUserMenu;
        },

        toggleActionMenu(id) {
            if (this.activeActionMenu === id) {
                this.activeActionMenu = null;
            } else {
                this.activeActionMenu = id;
            }
        },

        // ========== 用户信息编辑 ==========
        openProfileModal() {
            this.profileForm = {
                id: this.currentUser.id,
                username: this.currentUser.username,
                password: '',
                display_name: this.currentUser.display_name,
                role: this.currentUser.role
            };
            this.showProfileModal = true;
        },

        async saveProfile() {
            try {
                const userData = { ...this.profileForm };
                if (!userData.password) {
                    delete userData.password;
                }
                const res = await API.updateUser(this.currentUser.id, userData);
                if (res.ok) {
                    const updated = await res.json();
                    this.currentUser = { ...this.currentUser, ...updated };
                    localStorage.setItem('currentUser', JSON.stringify(this.currentUser));
                    this.showToast('个人信息更新成功', 'success');
                    this.showProfileModal = false;
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('更新失败', 'error');
            }
        },

        // ========== 分页相关 ==========
        goToPage() {
            const page = parseInt(this.jumpPage);
            if (page >= 1 && page <= this.totalPages) {
                this.currentPage = page;
            } else {
                this.jumpPage = this.currentPage;
            }
        },

        resetPagination() {
            this.currentPage = 1;
            this.jumpPage = 1;
        },

        // ========== 认证相关 ==========
        async login() {
            this.loginLoading = true;
            try {
                const res = await API.login(this.loginForm.username, this.loginForm.password);
                if (res.ok) {
                    this.currentUser = await res.json();
                    localStorage.setItem('currentUser', JSON.stringify(this.currentUser));
                    this.showToast('登录成功', 'success');
                    this.loadRecords();
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('登录失败', 'error');
            }
            this.loginLoading = false;
        },

        logout() {
            this.currentUser = null;
            localStorage.removeItem('currentUser');
            this.loginForm = { username: '', password: '' };
        },

        async refreshCurrentUser() {
            try {
                const res = await API.getUsers();
                if (res.ok) {
                    const users = await res.json() || [];
                    const me = users.find(u => u.id === this.currentUser.id || u.username === this.currentUser.username);
                    if (me) {
                        this.currentUser = { ...this.currentUser, ...me };
                        localStorage.setItem('currentUser', JSON.stringify(this.currentUser));
                    }
                }
            } catch (e) { /* 静默失败 */ }
        },

        // ========== 权限相关 ==========
        hasPermission(perm) {
            if (!this.currentUser) return false;
            if (this.currentUser.role === 'admin') return true;
            const perms = (this.currentUser.permissions || '').split(',').map(p => p.trim());
            return perms.includes(perm);
        },

        formatPermissions(perms) {
            if (!perms) return '-';
            const permMap = { records: '数据源ID', domains: '域名', inspection: '巡检', datasources: '数据源', audit: '日志', users: '用户' };
            return perms.split(',').map(p => permMap[p.trim()] || p).join(', ') || '-';
        },

        // ========== 数据加载 ==========
        async loadRecords() {
            try {
                const res = await API.getRecords();
                this.records = await res.json() || [];
            } catch (e) {
                this.showToast('加载失败', 'error');
            }
        },

        async loadUsers() {
            try {
                const res = await API.getUsers();
                this.users = await res.json() || [];
            } catch (e) {
                this.showToast('加载失败', 'error');
            }
        },

        async loadAuditLogs() {
            try {
                const res = await API.getAuditLogs();
                this.auditLogs = await res.json() || [];
            } catch (e) {
                this.showToast('加载失败', 'error');
            }
        },

        async loadDataSources() {
            try {
                const res = await API.getDataSources();
                this.datasources = await res.json() || [];
            } catch (e) {
                this.showToast('加载失败', 'error');
            }
        },

        // ========== 工具方法 ==========
        getStatusText(s) {
            return { active: '启用', inactive: '停用', pending: '待定' }[s] || s;
        },

        getActionText(a) {
            return { create: '创建', update: '修改', delete: '删除' }[a] || a;
        },

        getTypeLabel(type) {
            return { prometheus: 'Prometheus', jira: 'Jira', domain: '域名' }[type] || type;
        },

        toggleSort(field) {
            if (this.sortField === field) {
                this.sortOrder = this.sortOrder === 'asc' ? 'desc' : 'asc';
            } else {
                this.sortField = field;
                this.sortOrder = 'asc';
            }
        },

        getSortIcon(field) {
            if (this.sortField !== field) return '↕';
            return this.sortOrder === 'asc' ? '↑' : '↓';
        },

        toggleSelectAll(e) {
            if (e.target.checked) {
                this.selectedRecords = this.filteredRecords.map(r => r.id);
            } else {
                this.selectedRecords = [];
            }
        },

        formatJSON(str) {
            try {
                return JSON.stringify(JSON.parse(str), null, 2);
            } catch {
                return str;
            }
        },

        showToast(message, type = 'success') {
            this.toast = { show: true, message, type };
            setTimeout(() => { this.toast.show = false; }, 3000);
        },

        // ========== 记录操作 ==========
        openRecordModal(mode, record = null) {
            this.recordModalMode = mode;
            this.recordForm = record ? { ...record } : { id: '', connection_id: '', project: '', env: 'uat', vid: '', src_ip: '', dest_ip: '', port: '', status: 'active' };
            this.showRecordModal = true;
        },

        async saveRecord() {
            try {
                const url = this.recordModalMode === 'edit' ? `/records/${this.recordForm.id}` : '/records';
                const method = this.recordModalMode === 'edit' ? 'PUT' : 'POST';
                const res = await API.request(method, url, { record: this.recordForm, operator: this.currentUser.username });
                if (res.ok) {
                    this.showToast(this.recordModalMode === 'edit' ? '更新成功' : '添加成功', 'success');
                    this.showRecordModal = false;
                    this.loadRecords();
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('保存失败', 'error');
            }
        },

        confirmDeleteRecord(record) {
            this.deleteTarget = record;
            this.deleteType = 'record';
            this.deleteMessage = `确定删除记录 "${record.project} - ${record.vid}" 吗？`;
            this.showDeleteModal = true;
        },

        confirmBatchDelete() {
            this.deleteTarget = null;
            this.deleteType = 'batch';
            this.deleteMessage = `确定删除选中的 ${this.selectedRecords.length} 条记录吗？`;
            this.showDeleteModal = true;
        },

        async batchUpdateStatus(status) {
            this.showBatchStatusMenu = false;
            if (this.selectedRecords.length === 0) return;
            
            const statusText = { active: '启用', pending: '待定', inactive: '停用' }[status];
            try {
                const res = await API.request('POST', '/records/batch-status', {
                    ids: this.selectedRecords,
                    status: status,
                    operator: this.currentUser.username
                });
                if (res.ok) {
                    const r = await res.json();
                    this.showToast(`成功将 ${r.count} 条记录设为${statusText}`, 'success');
                    this.selectedRecords = [];
                    this.loadRecords();
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('批量修改状态失败', 'error');
            }
        },

        async executeBatchDelete() {
            try {
                const res = await API.batchDeleteRecords(this.selectedRecords, this.currentUser.username);
                if (res.ok) {
                    const r = await res.json();
                    this.showToast(r.message, 'success');
                    this.selectedRecords = [];
                    this.loadRecords();
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('批量删除失败', 'error');
            }
        },

        // ========== 批量添加 ==========
        openBatchModal() {
            this.batchText = '';
            this.batchRecords = [];
            this.batchError = '';
            this.showBatchModal = true;
        },

        parseBatchText() {
            this.batchError = '';
            this.batchRecords = [];
            if (!this.batchText.trim()) return;
            const lines = this.batchText.trim().split('\n');
            const seenConnIds = new Set();
            for (let i = 0; i < lines.length; i++) {
                const line = lines[i].trim();
                if (!line || line.startsWith('#')) continue;
                let parts;
                if (line.includes('\t')) parts = line.split('\t');
                else if (line.includes(',')) parts = line.split(',');
                else if (line.includes('|')) parts = line.split('|');
                else parts = line.split(/\s+/);
                parts = parts.map(p => p.trim()).filter(p => p);
                if (parts.length < 6) {
                    this.batchError = `第 ${i + 1} 行: 需要6个字段（项目、VID、源IP、目标IP、端口、连接ID）`;
                    return;
                }
                const connId = parts[5]; // 连接ID在最后
                if (seenConnIds.has(connId)) {
                    this.batchError = `第 ${i + 1} 行: 连接ID "${connId}" 重复`;
                    return;
                }
                seenConnIds.add(connId);
                this.batchRecords.push({
                    project: parts[0],
                    vid: parts[1],
                    src_ip: parts[2],
                    dest_ip: parts[3],
                    port: parts[4],
                    connection_id: parts[5], // 连接ID在最后
                    env: this.batchEnv,
                    status: this.batchStatus
                });
            }
        },

        async submitBatch() {
            try {
                const res = await API.batchAddRecords(this.batchRecords, this.currentUser.username);
                if (res.ok) {
                    const r = await res.json();
                    this.showToast(r.message, 'success');
                    this.showBatchModal = false;
                    this.loadRecords();
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('批量添加失败', 'error');
            }
        },

        // ========== 用户管理 ==========
        openUserModal(mode, user = null) {
            this.userModalMode = mode;
            if (user) {
                const perms = (user.permissions || '').split(',').map(p => p.trim()).filter(p => p);
                this.userForm = { ...user, password: '', permissions: perms };
            } else {
                this.userForm = { id: '', username: '', password: '', display_name: '', role: 'user', status: 'active', permissions: ['records', 'audit'] };
            }
            this.showUserModal = true;
        },

        async saveUser() {
            try {
                const userData = { ...this.userForm, permissions: this.userForm.permissions.join(',') };
                if (this.userModalMode === 'edit') {
                    const res = await API.updateUser(this.userForm.id, userData);
                    if (res.ok) {
                        this.showToast('更新成功', 'success');
                        this.showUserModal = false;
                        this.loadUsers();
                    } else {
                        this.showToast(await res.text(), 'error');
                    }
                } else {
                    const res = await API.createUser(userData, this.currentUser.id);
                    if (res.ok) {
                        this.showToast('创建成功', 'success');
                        this.showUserModal = false;
                        this.loadUsers();
                    } else {
                        this.showToast(await res.text(), 'error');
                    }
                }
            } catch (e) {
                this.showToast('保存失败', 'error');
            }
        },

        confirmDeleteUser(user) {
            this.deleteTarget = user;
            this.deleteType = 'user';
            this.deleteMessage = `确定删除用户 "${user.display_name}" 吗？`;
            this.showDeleteModal = true;
        },

        // ========== 数据源管理 ==========
        openDataSourceModal(mode, ds = null) {
            this.dataSourceModalMode = mode;
            this.dataSourceForm = ds ? { ...ds, password: '' } : { id: '', name: '', type: 'prometheus', url: '', username: '', password: '', token: '', description: '', status: 'active' };
            this.showDataSourceModal = true;
        },

        async saveDataSource() {
            try {
                if (this.dataSourceModalMode === 'edit') {
                    const res = await API.updateDataSource(this.dataSourceForm.id, this.dataSourceForm);
                    if (res.ok) {
                        this.showToast('更新成功', 'success');
                        this.showDataSourceModal = false;
                        this.loadDataSources();
                    } else {
                        this.showToast(await res.text(), 'error');
                    }
                } else {
                    const res = await API.createDataSource(this.dataSourceForm, this.currentUser.username);
                    if (res.ok) {
                        this.showToast('创建成功', 'success');
                        this.showDataSourceModal = false;
                        this.loadDataSources();
                    } else {
                        this.showToast(await res.text(), 'error');
                    }
                }
            } catch (e) {
                this.showToast('保存失败', 'error');
            }
        },

        async testDataSource(ds) {
            this.showToast('正在测试连接...', 'success');
            try {
                const res = await API.testDataSource(ds);
                const r = await res.json();
                this.showToast(r.message, r.success ? 'success' : 'error');
            } catch (e) {
                this.showToast('测试失败', 'error');
            }
        },

        confirmDeleteDataSource(ds) {
            this.deleteTarget = ds;
            this.deleteType = 'datasource';
            this.deleteMessage = `确定删除数据源 "${ds.name}" 吗？`;
            this.showDeleteModal = true;
        },

        // ========== 审计日志 ==========
        showAuditDetail(log) {
            this.auditDetail = log;
            this.showAuditDetailModal = true;
        },

        // ========== 导出 ==========
        exportRecords() {
            const params = {};
            if (this.envFilter) params.env = this.envFilter;
            if (this.statusFilter) params.status = this.statusFilter;
            if (this.searchQuery) params.search = this.searchQuery;
            API.exportRecords(params);
            this.showToast('正在导出...', 'success');
        },

        exportAuditLogs() {
            API.exportAuditLogs();
            this.showToast('正在导出...', 'success');
        },

        // ========== 巡检相关 ==========
        openInspectionModal() {
            this.loadDataSources();
            this.inspectionForm = {
                selectedDataSources: [],
                reportType: 'daily',
                includeMetrics: ['cpu', 'memory', 'disk', 'network']
            };
            this.showInspectionModal = true;
        },

        toggleDataSource(dsId) {
            const idx = this.inspectionForm.selectedDataSources.indexOf(dsId);
            if (idx > -1) {
                this.inspectionForm.selectedDataSources.splice(idx, 1);
            } else {
                this.inspectionForm.selectedDataSources.push(dsId);
            }
        },

        selectAllDataSources() {
            const sources = this.prometheusSources || [];
            const selected = this.inspectionForm.selectedDataSources || [];
            if (selected.length === sources.length) {
                this.inspectionForm.selectedDataSources = [];
            } else {
                this.inspectionForm.selectedDataSources = sources.map(ds => ds.id);
            }
        },

        toggleMetric(metricKey) {
            const idx = this.inspectionForm.includeMetrics.indexOf(metricKey);
            if (idx > -1) {
                this.inspectionForm.includeMetrics.splice(idx, 1);
            } else {
                this.inspectionForm.includeMetrics.push(metricKey);
            }
        },

        async executeInspection() {
            if (this.inspectionForm.selectedDataSources.length === 0) {
                this.showToast('请选择至少一个数据源', 'error');
                return;
            }
            if (this.inspectionForm.includeMetrics.length === 0) {
                this.showToast('请选择至少一个巡检指标', 'error');
                return;
            }

            this.inspectionRunning = true;
            this.showToast('正在执行巡检...', 'success');

            try {
                const res = await API.request('POST', '/inspection/execute', {
                    datasource_ids: this.inspectionForm.selectedDataSources,
                    report_type: this.inspectionForm.reportType,
                    metrics: this.inspectionForm.includeMetrics,
                    operator: this.currentUser.username
                });

                if (res.ok) {
                    const result = await res.json();
                    this.inspectionResults = result.results || [];
                    this.showToast(`巡检完成，共检查 ${result.total || 0} 项`, 'success');
                    this.showInspectionModal = false;
                    this.inspectionSubTab = 'reports';
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('巡检执行失败: ' + e.message, 'error');
            }

            this.inspectionRunning = false;
        },

        getMetricLabel(key) {
            const m = this.availableMetrics.find(x => x.key === key);
            return m ? m.label : key;
        },

        // ========== 删除确认 ==========
        async executeDelete() {
            try {
                if (this.deleteType === 'record') {
                    const res = await API.deleteRecord(this.deleteTarget.id, this.currentUser.username);
                    if (res.ok) {
                        this.showToast('删除成功', 'success');
                        this.loadRecords();
                    } else {
                        this.showToast(await res.text(), 'error');
                    }
                } else if (this.deleteType === 'user') {
                    const res = await API.deleteUser(this.deleteTarget.id);
                    if (res.ok) {
                        this.showToast('删除成功', 'success');
                        this.loadUsers();
                    } else {
                        this.showToast(await res.text(), 'error');
                    }
                } else if (this.deleteType === 'batch') {
                    await this.executeBatchDelete();
                } else if (this.deleteType === 'datasource') {
                    const res = await API.deleteDataSource(this.deleteTarget.id);
                    if (res.ok) {
                        this.showToast('删除成功', 'success');
                        this.loadDataSources();
                    } else {
                        this.showToast(await res.text(), 'error');
                    }
                }
            } catch (e) {
                this.showToast('删除失败', 'error');
            }
            this.showDeleteModal = false;
        }
    }
}).mount('#app');

