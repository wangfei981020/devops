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

            // MFA 状态
            mfaPending: false,           // 是否等待 MFA 验证
            mfaPendingUserId: '',        // 待验证的用户 ID
            mfaCode: '',                 // MFA 验证码
            showMFASetupModal: false,    // MFA 设置弹窗
            showMFADisableModal: false,  // MFA 禁用弹窗
            mfaSetup: {},                // MFA 设置信息（二维码、密钥）
            mfaBindCode: '',             // MFA 绑定验证码
            mfaDisablePassword: '',      // MFA 禁用确认密码

            // 页面状态（从 localStorage 恢复）
            currentTab: localStorage.getItem('currentTab') || 'records',
            expandedGroups: { data: true, ops: true, system: true },
            inspectionSubTab: 'templates',
            metricsGroupFilter: 'all', // 指标分组筛选: all, k8s, container, node, custom
            metricsCurrentPage: 1,
            metricsPageSize: 10,
            sidebarCollapsed: false,

            // 数据
            records: [],
            users: [],
            auditLogs: [],
            datasources: [],
            domains: [],
            selectedRecords: [],
            selectedDomains: [],
            activeDomainMenu: null,
            activeMetricMenu: null,
            activeDsMenu: null,
            activeUserMenu: null,

            // 搜索和过滤
            searchQuery: '',
            auditSearchQuery: '',
            projectFilter: '',
            statusFilter: '',
            envFilter: '',
            actionFilter: '',
            sortField: '',
            sortOrder: 'asc',

            // 分页 - 数据源管理
            currentPage: 1,
            pageSize: 10,
            jumpPage: 1,
            
            // 分页 - 审计日志
            auditCurrentPage: 1,
            auditPageSize: 10,
            auditJumpPage: 1,
            
            // 分页 - 用户管理
            userCurrentPage: 1,
            userPageSize: 10,
            userJumpPage: 1,
            
            // 分页 - 巡检报告
            inspectionCurrentPage: 1,
            inspectionPageSize: 5,
            
            // 分页 - 数据源管理
            dsCurrentPage: 1,
            dsPageSize: 10,
            dsJumpPage: 1,

            // 分页 - 域名管理
            domainCurrentPage: 1,
            domainPageSize: 10,
            domainJumpPage: 1,

            // 模态框状态
            showRecordModal: false,
            showDomainModal: false,
            showBatchDomainModal: false,
            showBatchModal: false,
            showUserModal: false,
            showDeleteModal: false,
            showHistoryModal: false,
            showPreviewModal: false,
            
            // 历史记录
            historyRecord: null,
            recordHistories: [],
            previewData: null,
            previewHistory: null,
            previewVersion: 0,
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
            domainForm: { id: '', project: '', module: '', domain_name: '', origin: '', cdn_provider: '', expire_time: '', cert_expire_time: '', status: 'active', remark: '' },
            profileForm: { id: '', username: '', password: '', display_name: '', role: '' },

            // 域名筛选
            domainSearchQuery: '',
            domainProjectFilter: '',
            domainStatusFilter: '',
            domainCdnFilter: '',
            domainExpireFilter: '',

            // 批量添加域名
            batchDomainText: '',
            batchDomainRecords: [],
            batchDomainError: '',
            batchDomainProject: '',  // 批量添加时的默认项目

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
                includeMetrics: [] // 默认不勾选
            },
            inspectionRunning: false,
            inspectionResults: [],
            availableMetrics: [], // 从数据库加载
            
            // 指标管理
            showMetricModal: false,
            metricForm: { id: '', name: '', label: '', promql: '', unit: '', group: 'k8s', description: '', enabled: true, sort_order: 0 },
            editingMetric: false,
            selectedMetricIds: [], // 批量选择的指标

            // 权限配置
            allPermissions: [
                { key: 'records', label: '网络管理', group: '数据管理' },
                { key: 'domains', label: '域名管理', group: '数据管理' },
                { key: 'cmdb', label: 'CMDB', group: '数据管理' },
                { key: 'inspection', label: '一键巡检', group: '运维工具' },
                { key: 'datasources', label: '数据源配置', group: '系统管理' },
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
        
        // 审计日志分页
        pagedAuditLogs() {
            const start = (this.auditCurrentPage - 1) * this.auditPageSize;
            return this.filteredAuditLogs.slice(start, start + this.auditPageSize);
        },
        auditTotalPages() {
            return Math.max(1, Math.ceil(this.filteredAuditLogs.length / this.auditPageSize));
        },
        auditDisplayedPages() {
            return this.getDisplayedPages(this.auditTotalPages, this.auditCurrentPage);
        },
        
        // 巡检报告分页
        pagedInspectionResults() {
            const start = (this.inspectionCurrentPage - 1) * this.inspectionPageSize;
            return (this.inspectionResults || []).slice(start, start + this.inspectionPageSize);
        },
        inspectionTotalPages() {
            return Math.max(1, Math.ceil((this.inspectionResults || []).length / this.inspectionPageSize));
        },
        inspectionDisplayedPages() {
            return this.getDisplayedPages(this.inspectionTotalPages, this.inspectionCurrentPage);
        },

        // 指标分组
        metricGroups() {
            const groups = {};
            const groupLabels = { k8s: '☸️ K8s 集群', container: '📦 容器', node: '🖥️ 节点', custom: '🔧 自定义' };
            (this.availableMetrics || []).filter(m => m.enabled !== false).forEach(m => {
                const g = m.group || 'custom';
                if (!groups[g]) groups[g] = { key: g, label: groupLabels[g] || g, metrics: [] };
                groups[g].metrics.push(m);
            });
            return Object.values(groups);
        },

        // 过滤后的指标列表（用于指标管理页面）
        filteredMetrics() {
            if (this.metricsGroupFilter === 'all') {
                return this.availableMetrics || [];
            }
            return (this.availableMetrics || []).filter(m => (m.group || 'custom') === this.metricsGroupFilter);
        },

        // 获取分组统计
        metricGroupStats() {
            const stats = { all: 0, k8s: 0, container: 0, node: 0, custom: 0 };
            (this.availableMetrics || []).forEach(m => {
                stats.all++;
                const g = m.group || 'custom';
                if (stats[g] !== undefined) stats[g]++;
            });
            return stats;
        },

        // 分页后的指标列表
        pagedMetrics() {
            const start = (this.metricsCurrentPage - 1) * this.metricsPageSize;
            return this.filteredMetrics.slice(start, start + this.metricsPageSize);
        },
        metricsTotalPages() {
            return Math.max(1, Math.ceil(this.filteredMetrics.length / this.metricsPageSize));
        },
        metricsDisplayedPages() {
            return this.getDisplayedPages(this.metricsTotalPages, this.metricsCurrentPage);
        },
        
        // 用户管理分页
        pagedUsers() {
            const start = (this.userCurrentPage - 1) * this.userPageSize;
            return (this.users || []).slice(start, start + this.userPageSize);
        },
        userTotalPages() {
            return Math.max(1, Math.ceil((this.users || []).length / this.userPageSize));
        },
        userDisplayedPages() {
            return this.getDisplayedPages(this.userTotalPages, this.userCurrentPage);
        },
        
        // 数据源管理分页
        pagedDatasources() {
            const start = (this.dsCurrentPage - 1) * this.dsPageSize;
            return (this.datasources || []).slice(start, start + this.dsPageSize);
        },
        dsTotalPages() {
            return Math.max(1, Math.ceil((this.datasources || []).length / this.dsPageSize));
        },
        dsDisplayedPages() {
            return this.getDisplayedPages(this.dsTotalPages, this.dsCurrentPage);
        },

        // 域名项目列表
        domainProjectList() {
            const projects = [...new Set((this.domains || []).map(d => d.project).filter(Boolean))];
            return projects.sort((a, b) => a.localeCompare(b, 'zh-CN'));
        },
        // CDN厂商列表
        cdnProviderList() {
            const providers = [...new Set((this.domains || []).map(d => d.cdn_provider).filter(Boolean))];
            return providers.sort((a, b) => a.localeCompare(b, 'zh-CN'));
        },
        // 过滤后的域名
        filteredDomains() {
            let d = this.domains || [];
            if (this.domainProjectFilter) d = d.filter(x => x.project === this.domainProjectFilter);
            if (this.domainStatusFilter) d = d.filter(x => x.status === this.domainStatusFilter);
            if (this.domainCdnFilter) d = d.filter(x => x.cdn_provider === this.domainCdnFilter);
            // 到期时间筛选
            if (this.domainExpireFilter) {
                d = d.filter(x => {
                    if (!x.expire_time) return false;
                    const expireDate = new Date(x.expire_time);
                    const now = new Date();
                    const diffDays = Math.ceil((expireDate - now) / (1000 * 60 * 60 * 24));
                    if (this.domainExpireFilter === '90+') {
                        return diffDays > 90;
                    }
                    const days = parseInt(this.domainExpireFilter);
                    return diffDays >= 0 && diffDays <= days;
                });
            }
            if (this.domainSearchQuery) {
                const q = this.domainSearchQuery.toLowerCase();
                d = d.filter(x => 
                    (x.domain_name && x.domain_name.toLowerCase().includes(q)) ||
                    (x.project && x.project.toLowerCase().includes(q)) ||
                    (x.module && x.module.toLowerCase().includes(q)) ||
                    (x.origin && x.origin.toLowerCase().includes(q))
                );
            }
            return d;
        },
        // 分页后的域名
        pagedDomains() {
            const start = (this.domainCurrentPage - 1) * this.domainPageSize;
            return this.filteredDomains.slice(start, start + this.domainPageSize);
        },
        domainTotalPages() {
            return Math.max(1, Math.ceil(this.filteredDomains.length / this.domainPageSize));
        },
        domainDisplayedPages() {
            return this.getDisplayedPages(this.domainTotalPages, this.domainCurrentPage);
        },
        // 域名全选
        isAllDomainsSelected() {
            return this.filteredDomains.length > 0 && this.selectedDomains.length === this.filteredDomains.length;
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
        auditSearchQuery() { this.auditCurrentPage = 1; },
        actionFilter() { this.auditCurrentPage = 1; },
        auditCurrentPage(val) { this.auditJumpPage = val; },
        userCurrentPage(val) { this.userJumpPage = val; },
        dsCurrentPage(val) { this.dsJumpPage = val; },
        currentTab(val) { 
            localStorage.setItem('currentTab', val);
            this.updateBreadcrumbs();
            this.loadDataForCurrentTab();
        }
    },

    mounted() {
        // 初始化主题
        this.initTheme();

        if (this.currentUser) {
            // 根据当前页面加载对应数据
            this.loadDataForCurrentTab();
            this.refreshCurrentUser();
            this.updateBreadcrumbs();
            // 加载自定义指标
            this.loadMetrics();
        }

        // 点击外部关闭用户菜单和操作菜单
        document.addEventListener('click', (e) => {
            if (!e.target.closest('.user-menu-container')) {
                this.showUserMenu = false;
            }
            if (!e.target.closest('.action-inline')) {
                this.activeActionMenu = null;
                this.activeDomainMenu = null;
                this.activeMetricMenu = null;
                this.activeDsMenu = null;
                this.activeUserMenu = null;
            }
            if (!e.target.closest('.batch-status-dropdown')) {
                this.showBatchStatusMenu = false;
            }
        });
    },

    methods: {
        // ========== 通用方法 ==========
        getDisplayedPages(total, current) {
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
        
        // ========== 数据加载 ==========
        loadDataForCurrentTab() {
            switch(this.currentTab) {
                case 'records':
                    this.loadRecords();
                    break;
                case 'audit':
                    this.loadAuditLogs();
                    break;
                case 'users':
                    this.loadUsers();
                    break;
                case 'datasources':
                    this.loadDataSources();
                    break;
                case 'domains':
                    this.loadDomains();
                    break;
                default:
                    this.loadRecords();
            }
        },

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
                'records': '网络管理',
                'domains': '域名管理',
                'inspection': '一键巡检',
                'datasources': '数据源配置',
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
                // 保留原有的 status、permissions、mfa 设置
                const userData = { 
                    ...this.profileForm,
                    status: this.currentUser.status,
                    permissions: this.currentUser.permissions,
                    mfa_enabled: this.currentUser.mfa_enabled
                };
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
        
        auditGoToPage() {
            const page = parseInt(this.auditJumpPage);
            if (page >= 1 && page <= this.auditTotalPages) {
                this.auditCurrentPage = page;
            } else {
                this.auditJumpPage = this.auditCurrentPage;
            }
        },
        
        userGoToPage() {
            const page = parseInt(this.userJumpPage);
            if (page >= 1 && page <= this.userTotalPages) {
                this.userCurrentPage = page;
            } else {
                this.userJumpPage = this.userCurrentPage;
            }
        },
        
        dsGoToPage() {
            const page = parseInt(this.dsJumpPage);
            if (page >= 1 && page <= this.dsTotalPages) {
                this.dsCurrentPage = page;
            } else {
                this.dsJumpPage = this.dsCurrentPage;
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
                    const data = await res.json();
                    
                    // 检查是否需要 MFA 验证
                    if (data.require_mfa) {
                        this.mfaPending = true;
                        this.mfaPendingUserId = data.user_id;
                        this.mfaCode = '';
                        this.showToast('请输入 MFA 验证码', 'success');
                    } else {
                        // 登录成功
                        this.currentUser = data.user;
                        localStorage.setItem('currentUser', JSON.stringify(this.currentUser));
                        this.loadRecords();
                        
                        // 检查是否需要绑定 MFA
                        if (data.need_binding) {
                            this.showToast('管理员已为您启用 MFA，请先绑定认证器', 'success');
                            // 自动打开个人设置进行绑定
                            setTimeout(() => {
                                this.openProfileModal();
                            }, 500);
                        } else {
                            this.showToast('登录成功', 'success');
                        }
                    }
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('登录失败', 'error');
            }
            this.loginLoading = false;
        },

        // MFA 验证
        async verifyMFA() {
            if (!this.mfaCode || this.mfaCode.length !== 6) {
                this.showToast('请输入6位验证码', 'error');
                return;
            }
            this.loginLoading = true;
            try {
                const res = await API.mfaVerify(this.mfaPendingUserId, this.mfaCode);
                if (res.ok) {
                    this.currentUser = await res.json();
                    localStorage.setItem('currentUser', JSON.stringify(this.currentUser));
                    this.showToast('登录成功', 'success');
                    this.mfaPending = false;
                    this.mfaPendingUserId = '';
                    this.mfaCode = '';
                    this.loadRecords();
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('验证失败', 'error');
            }
            this.loginLoading = false;
        },

        // 取消 MFA 验证，返回登录
        cancelMFA() {
            this.mfaPending = false;
            this.mfaPendingUserId = '';
            this.mfaCode = '';
            this.loginForm.password = '';
        },

        logout() {
            this.currentUser = null;
            localStorage.removeItem('currentUser');
            this.loginForm = { username: '', password: '' };
            this.mfaPending = false;
            this.mfaPendingUserId = '';
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

        // ========== MFA 设置相关 ==========
        async setupMFA() {
            try {
                const res = await API.mfaSetup(this.currentUser.id);
                if (res.ok) {
                    this.mfaSetup = await res.json();
                    this.mfaBindCode = '';
                } else {
                    this.showToast(await res.text(), 'error');
                    this.showMFASetupModal = false;
                }
            } catch (e) {
                this.showToast('获取 MFA 信息失败', 'error');
                this.showMFASetupModal = false;
            }
        },

        async bindMFA() {
            if (!this.mfaBindCode || this.mfaBindCode.length !== 6) {
                this.showToast('请输入6位验证码', 'error');
                return;
            }
            try {
                const res = await API.mfaBind(this.currentUser.id, this.mfaBindCode);
                if (res.ok) {
                    this.showToast('MFA 绑定成功！', 'success');
                    this.currentUser.mfa_enabled = true;
                    this.currentUser.mfa_bound = true;
                    localStorage.setItem('currentUser', JSON.stringify(this.currentUser));
                    this.showMFASetupModal = false;
                    this.mfaSetup = {};
                    this.mfaBindCode = '';
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('绑定失败', 'error');
            }
        },

        async disableMFA() {
            if (!this.mfaDisablePassword) {
                this.showToast('请输入密码', 'error');
                return;
            }
            try {
                const res = await API.mfaDisable(this.currentUser.id, this.mfaDisablePassword);
                if (res.ok) {
                    this.showToast('MFA 已禁用', 'success');
                    this.currentUser.mfa_enabled = false;
                    this.currentUser.mfa_bound = false;
                    localStorage.setItem('currentUser', JSON.stringify(this.currentUser));
                    this.showMFADisableModal = false;
                    this.mfaDisablePassword = '';
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('禁用失败', 'error');
            }
        },

        // 管理员重置用户 MFA
        async resetUserMFA(userId) {
            if (!confirm('确定要重置该用户的 MFA 吗？用户需要重新绑定认证器。')) {
                return;
            }
            try {
                const res = await API.mfaReset(userId);
                if (res.ok) {
                    this.showToast('MFA 已重置，用户需重新绑定', 'success');
                    this.userForm.mfa_bound = false;
                    this.loadUsers();
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('重置失败', 'error');
            }
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
            const permMap = { records: '网络', domains: '域名', inspection: '巡检', datasources: '数据源', audit: '日志', users: '用户' };
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

        // ========== 域名管理 ==========
        async loadDomains() {
            try {
                const res = await API.getDomains();
                const data = await res.json();
                this.domains = data.domains || [];
            } catch (e) {
                this.showToast('加载域名失败', 'error');
            }
        },

        openDomainModal(domain = null) {
            if (domain) {
                this.domainForm = { ...domain };
            } else {
                this.domainForm = { id: '', project: '', module: '', domain_name: '', origin: '', cdn_provider: '', expire_time: '', cert_expire_time: '', status: 'active', remark: '' };
            }
            this.showDomainModal = true;
        },

        async saveDomain() {
            if (!this.domainForm.project) {
                this.showToast('请填写项目名称', 'error');
                return;
            }
            if (!this.domainForm.domain_name) {
                this.showToast('请填写域名', 'error');
                return;
            }

            try {
                let res;
                if (this.domainForm.id) {
                    res = await API.updateDomain(this.domainForm.id, {
                        ...this.domainForm,
                        updated_by: this.currentUser?.username
                    });
                } else {
                    res = await API.createDomain({
                        ...this.domainForm,
                        created_by: this.currentUser?.username
                    });
                }
                if (res.ok) {
                    this.showToast(this.domainForm.id ? '域名更新成功' : '域名添加成功', 'success');
                    this.showDomainModal = false;
                    await this.loadDomains();
                } else {
                    const err = await res.text();
                    this.showToast(err || '操作失败', 'error');
                }
            } catch (e) {
                this.showToast('操作失败: ' + e.message, 'error');
            }
        },

        async deleteDomain(domain) {
            if (!confirm(`确定删除域名 ${domain.domain_name}？`)) return;
            try {
                const res = await API.deleteDomain(domain.id);
                if (res.ok) {
                    this.showToast('删除成功', 'success');
                    await this.loadDomains();
                } else {
                    this.showToast('删除失败', 'error');
                }
            } catch (e) {
                this.showToast('删除失败', 'error');
            }
        },

        toggleDomainSelection(id) {
            const idx = this.selectedDomains.indexOf(id);
            if (idx === -1) {
                this.selectedDomains.push(id);
            } else {
                this.selectedDomains.splice(idx, 1);
            }
        },

        toggleAllDomains() {
            if (this.isAllDomainsSelected) {
                this.selectedDomains = [];
            } else {
                this.selectedDomains = this.filteredDomains.map(d => d.id);
            }
        },

        async batchDomainAction(action) {
            if (this.selectedDomains.length === 0) {
                this.showToast('请先选择域名', 'error');
                return;
            }
            const actionNames = { delete: '删除', enable: '启用', disable: '停用' };
            if (!confirm(`确定${actionNames[action]}选中的 ${this.selectedDomains.length} 个域名？`)) return;

            try {
                const res = await API.batchDomains(this.selectedDomains, action, this.currentUser?.username);
                if (res.ok) {
                    this.showToast(`批量${actionNames[action]}成功`, 'success');
                    this.selectedDomains = [];
                    await this.loadDomains();
                } else {
                    this.showToast('操作失败', 'error');
                }
            } catch (e) {
                this.showToast('操作失败', 'error');
            }
        },

        async checkDomainCert() {
            if (!this.domainForm.domain_name) {
                this.showToast('请先填写域名', 'error');
                return;
            }
            try {
                const res = await API.checkDomainCert(this.domainForm.domain_name);
                const data = await res.json();
                if (data.success) {
                    let messages = [];
                    if (data.cert_expire_time) {
                        this.domainForm.cert_expire_time = data.cert_expire_time;
                        messages.push('证书到期: ' + data.cert_expire_time);
                    }
                    if (data.expire_time) {
                        this.domainForm.expire_time = data.expire_time;
                        messages.push('域名到期: ' + data.expire_time);
                    }
                    if (messages.length > 0) {
                        this.showToast(messages.join(', '), 'success');
                    } else {
                        this.showToast('未能获取到期时间', 'error');
                    }
                } else {
                    this.showToast(data.message || '获取失败', 'error');
                }
            } catch (e) {
                this.showToast('获取域名信息失败', 'error');
            }
        },

        domainGoToPage() {
            const page = parseInt(this.domainJumpPage);
            if (page >= 1 && page <= this.domainTotalPages) {
                this.domainCurrentPage = page;
            } else {
                this.domainJumpPage = this.domainCurrentPage;
            }
        },

        toggleDomainActionMenu(id) {
            this.activeDomainMenu = this.activeDomainMenu === id ? null : id;
        },

        // 刷新单个域名的到期时间
        async refreshDomainExpire(domain) {
            this.showToast('正在获取到期时间...', 'info');
            try {
                const res = await API.checkDomainCert(domain.domain_name);
                const data = await res.json();
                if (data.success) {
                    // 更新域名信息
                    const updateData = { ...domain };
                    let updated = false;
                    if (data.expire_time) {
                        updateData.expire_time = data.expire_time;
                        updated = true;
                    }
                    if (data.cert_expire_time) {
                        updateData.cert_expire_time = data.cert_expire_time;
                        updated = true;
                    }
                    if (updated) {
                        updateData.updated_by = this.currentUser?.username;
                        const updateRes = await API.updateDomain(domain.id, updateData);
                        if (updateRes.ok) {
                            this.showToast('到期时间已更新', 'success');
                            await this.loadDomains();
                        } else {
                            this.showToast('保存失败', 'error');
                        }
                    } else {
                        this.showToast('未能获取到期时间', 'error');
                    }
                } else {
                    this.showToast(data.message || '获取失败', 'error');
                }
            } catch (e) {
                this.showToast('获取失败: ' + e.message, 'error');
            }
        },

        getDomainStatusClass(domain) {
            // 检查证书是否即将过期（30天内）
            if (domain.cert_expire_time) {
                const expireDate = new Date(domain.cert_expire_time);
                const now = new Date();
                const daysLeft = Math.ceil((expireDate - now) / (1000 * 60 * 60 * 24));
                if (daysLeft < 0) return 'status-expired';
                if (daysLeft <= 30) return 'status-warning';
            }
            return domain.status === 'active' ? 'status-active' : 'status-inactive';
        },

        getCertDaysLeft(certExpireTime) {
            if (!certExpireTime) return null;
            const expireDate = new Date(certExpireTime);
            const now = new Date();
            return Math.ceil((expireDate - now) / (1000 * 60 * 60 * 24));
        },

        // 获取域名到期时间的样式类
        getDomainExpireClass(expireTime) {
            if (!expireTime) return '';
            const days = this.getCertDaysLeft(expireTime);
            if (days === null) return '';
            if (days <= 0) return 'text-danger';
            if (days <= 15) return 'text-danger';
            return 'text-success';
        },

        // 格式化日期显示（处理ISO格式）
        formatDate(dateStr) {
            if (!dateStr) return '-';
            try {
                const date = new Date(dateStr);
                if (isNaN(date.getTime())) return dateStr;
                return date.toISOString().split('T')[0];
            } catch (e) {
                return dateStr;
            }
        },

        // 打开批量添加域名弹窗
        openBatchDomainModal() {
            this.batchDomainText = '';
            this.batchDomainRecords = [];
            this.batchDomainError = '';
            this.batchDomainProject = '';
            this.showBatchDomainModal = true;
        },

        // 解析批量域名文本
        parseBatchDomains() {
            this.batchDomainError = '';
            this.batchDomainRecords = [];
            
            if (!this.batchDomainText.trim()) return;
            if (!this.batchDomainProject.trim()) {
                this.batchDomainError = '请先填写项目名称';
                return;
            }

            const lines = this.batchDomainText.trim().split('\n');
            const records = [];

            for (let i = 0; i < lines.length; i++) {
                const line = lines[i].trim();
                if (!line) continue;

                // 支持两种格式:
                // 1. 只有域名: example.com
                // 2. 完整格式: 域名,模块,回源,CDN厂商,域名到期,备注 (用逗号或制表符分隔)
                const parts = line.split(/[,\t]+/).map(s => s.trim());
                
                if (parts.length === 0 || !parts[0]) continue;

                const domain = {
                    project: this.batchDomainProject,
                    domain_name: parts[0],
                    module: parts[1] || '',
                    origin: parts[2] || '',
                    cdn_provider: parts[3] || '',
                    expire_time: parts[4] || '',
                    remark: parts[5] || '',
                    status: 'active'
                };

                records.push(domain);
            }

            this.batchDomainRecords = records;
        },

        // 提交批量添加域名
        async submitBatchDomains() {
            if (this.batchDomainRecords.length === 0) {
                this.showToast('没有可添加的域名', 'error');
                return;
            }

            try {
                const res = await API.batchAddDomains(this.batchDomainRecords, this.currentUser?.username);
                const data = await res.json();
                
                if (data.success) {
                    this.showToast(`成功添加 ${data.success_count} 个域名`, 'success');
                    if (data.failed_count > 0) {
                        console.warn('添加失败的域名:', data.failed_domains);
                    }
                    this.showBatchDomainModal = false;
                    await this.loadDomains();
                } else {
                    this.showToast(data.message || '批量添加失败', 'error');
                }
            } catch (e) {
                this.showToast('批量添加失败: ' + e.message, 'error');
            }
        },

        // ========== 自定义指标管理 ==========
        async loadMetrics() {
            try {
                const res = await API.getMetrics();
                const metrics = await res.json() || [];
                // 转换格式供巡检使用
                this.availableMetrics = metrics.map(m => ({
                    key: m.name,
                    label: m.label,
                    promql: m.promql,
                    unit: m.unit,
                    group: m.group,
                    enabled: m.enabled,
                    id: m.id,
                    sort_order: m.sort_order,
                    description: m.description
                }));
                // 如果没有指标，初始化默认指标
                if (this.availableMetrics.length === 0) {
                    await API.initDefaultMetrics();
                    await this.loadMetrics();
                }
            } catch (e) {
                console.error('加载指标失败', e);
            }
        },

        openMetricModal(metric = null) {
            if (metric) {
                this.editingMetric = true;
                this.metricForm = {
                    id: metric.id,
                    name: metric.key || metric.name,
                    label: metric.label,
                    promql: metric.promql,
                    unit: metric.unit || '',
                    group: metric.group || 'k8s',
                    description: metric.description || '',
                    enabled: metric.enabled !== false,
                    sort_order: metric.sort_order || 0
                };
            } else {
                this.editingMetric = false;
                this.metricForm = { id: '', name: '', label: '', promql: '', unit: '', group: 'k8s', description: '', enabled: true, sort_order: 0 };
            }
            this.showMetricModal = true;
        },

        async saveMetric() {
            if (!this.metricForm.name || !this.metricForm.label || !this.metricForm.promql) {
                this.showToast('名称、标签和PromQL不能为空', 'error');
                return;
            }

            try {
                const data = {
                    ...this.metricForm,
                    group_name: this.metricForm.group,
                    created_by: this.currentUser.username
                };

                let res;
                if (this.editingMetric) {
                    res = await API.updateMetric(data);
                } else {
                    res = await API.createMetric(data);
                }

                if (res.ok) {
                    this.showToast(this.editingMetric ? '更新成功' : '创建成功', 'success');
                    this.showMetricModal = false;
                    await this.loadMetrics();
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('保存失败: ' + e.message, 'error');
            }
        },

        async deleteMetric(metric) {
            if (!confirm(`确定删除指标 "${metric.label}"？`)) return;

            try {
                const res = await API.deleteMetric(metric.id);
                if (res.ok) {
                    this.showToast('删除成功', 'success');
                    await this.loadMetrics();
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('删除失败: ' + e.message, 'error');
            }
        },

        // 切换指标选中状态
        toggleMetricSelection(metricId) {
            const idx = this.selectedMetricIds.indexOf(metricId);
            if (idx > -1) {
                this.selectedMetricIds.splice(idx, 1);
            } else {
                this.selectedMetricIds.push(metricId);
            }
        },

        // 全选/取消全选当前显示的指标
        toggleSelectAllMetrics() {
            const currentIds = this.filteredMetrics.map(m => m.id);
            const allSelected = currentIds.every(id => this.selectedMetricIds.includes(id));
            if (allSelected) {
                this.selectedMetricIds = this.selectedMetricIds.filter(id => !currentIds.includes(id));
            } else {
                currentIds.forEach(id => {
                    if (!this.selectedMetricIds.includes(id)) {
                        this.selectedMetricIds.push(id);
                    }
                });
            }
        },

        // 批量启用指标
        async batchEnableMetrics() {
            if (this.selectedMetricIds.length === 0) {
                this.showToast('请先选择要操作的指标', 'error');
                return;
            }
            await this.batchUpdateMetricsEnabled(true);
        },

        // 批量停用指标
        async batchDisableMetrics() {
            if (this.selectedMetricIds.length === 0) {
                this.showToast('请先选择要操作的指标', 'error');
                return;
            }
            await this.batchUpdateMetricsEnabled(false);
        },

        // 批量更新指标启用状态
        async batchUpdateMetricsEnabled(enabled) {
            let successCount = 0;
            for (const id of this.selectedMetricIds) {
                const metric = this.availableMetrics.find(m => m.id === id);
                if (metric) {
                    try {
                        const res = await API.updateMetric({
                            id: metric.id,
                            name: metric.key || metric.name,
                            label: metric.label,
                            promql: metric.promql,
                            unit: metric.unit || '',
                            group_name: metric.group || 'custom',
                            description: metric.description || '',
                            enabled: enabled,
                            sort_order: metric.sort_order || 0
                        });
                        if (res.ok) successCount++;
                    } catch (e) {
                        console.error('更新失败', e);
                    }
                }
            }
            this.showToast(`已${enabled ? '启用' : '停用'} ${successCount} 个指标`, 'success');
            this.selectedMetricIds = [];
            await this.loadMetrics();
        },

        // 批量删除指标
        async batchDeleteMetrics() {
            if (this.selectedMetricIds.length === 0) {
                this.showToast('请先选择要删除的指标', 'error');
                return;
            }
            if (!confirm(`确定删除选中的 ${this.selectedMetricIds.length} 个指标？`)) return;

            let successCount = 0;
            for (const id of this.selectedMetricIds) {
                try {
                    const res = await API.deleteMetric(id);
                    if (res.ok) successCount++;
                } catch (e) {
                    console.error('删除失败', e);
                }
            }
            this.showToast(`已删除 ${successCount} 个指标`, 'success');
            this.selectedMetricIds = [];
            await this.loadMetrics();
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

        // ========== 历史记录相关 ==========
        async openRecordHistory(record) {
            this.historyRecord = record;
            this.recordHistories = [];
            this.showHistoryModal = true;
            try {
                const res = await API.getRecordHistory(record.id);
                if (res.ok) {
                    this.recordHistories = await res.json() || [];
                }
            } catch (e) {
                this.showToast('加载历史记录失败', 'error');
            }
        },

        getHistoryActionText(action) {
            const map = { create: '创建', update: '修改', delete: '删除', rollback: '回滚' };
            return map[action] || action;
        },

        parseChanges(changesStr) {
            try {
                return JSON.parse(changesStr);
            } catch (e) {
                return {};
            }
        },

        getFieldLabel(field) {
            const map = {
                project: '项目', env: '环境', vid: 'VID', src_ip: '源IP', 
                dest_ip: '目标IP', port: '端口', connection_id: '连接ID', 
                status: '状态', remark: '备注'
            };
            return map[field] || field;
        },

        previewHistoryVersion(history, idx) {
            try {
                this.previewData = JSON.parse(history.snapshot);
                this.previewHistory = history;
                this.previewVersion = this.recordHistories.length - idx;
                this.showPreviewModal = true;
            } catch (e) {
                this.showToast('解析快照失败', 'error');
            }
        },

        rollbackFromPreview() {
            if (this.previewHistory) {
                this.showPreviewModal = false;
                this.rollbackToVersion(this.previewHistory);
            }
        },

        async rollbackToVersion(history) {
            if (!confirm(`确定回滚到此版本吗？当前数据将被覆盖。`)) return;
            try {
                const operator = this.currentUser?.username || 'admin';
                const res = await API.rollbackRecord(this.historyRecord.id, history.id, operator);
                if (res.ok) {
                    this.showToast('回滚成功', 'success');
                    this.showHistoryModal = false;
                    await this.loadRecords();
                } else {
                    this.showToast('回滚失败', 'error');
                }
            } catch (e) {
                this.showToast('回滚失败', 'error');
            }
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
                this.userForm = { 
                    id: user.id,
                    username: user.username,
                    password: '',
                    display_name: user.display_name,
                    role: user.role || 'user',
                    status: user.status || 'active',
                    permissions: perms.length > 0 ? perms : [],
                    mfa_enabled: user.mfa_enabled || false,
                    mfa_bound: user.mfa_bound || false
                };
                console.log('编辑用户:', this.userForm); // 调试
            } else {
                this.userForm = { id: '', username: '', password: '', display_name: '', role: 'user', status: 'active', permissions: ['records', 'audit'], mfa_enabled: false, mfa_bound: false };
            }
            this.showUserModal = true;
            // 确保弹窗滚动到顶部
            this.$nextTick(() => {
                const modal = document.querySelector('.modal-overlay.show .modal-body');
                if (modal) modal.scrollTop = 0;
            });
        },

        async saveUser() {
            try {
                const userData = { 
                    ...this.userForm, 
                    permissions: this.userForm.permissions.join(','),
                    mfa_enabled: this.userForm.mfa_enabled || false,
                    status: this.userForm.status || 'active' // 确保 status 有值
                };
                console.log('保存用户数据:', userData); // 调试
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

        exportDomains() {
            const params = {};
            if (this.domainProjectFilter) params.project = this.domainProjectFilter;
            if (this.domainStatusFilter) params.status = this.domainStatusFilter;
            if (this.domainCdnFilter) params.cdn_provider = this.domainCdnFilter;
            if (this.domainExpireFilter) params.expire_filter = this.domainExpireFilter;
            if (this.domainSearchQuery) params.search = this.domainSearchQuery;
            API.exportDomains(params);
            this.showToast('正在导出...', 'success');
        },

        // ========== 巡检相关 ==========
        openInspectionModal() {
            this.loadDataSources();
            // 保留上次的选择，只在第一次时初始化
            if (!this.inspectionForm || this.inspectionForm.includeMetrics === undefined) {
                this.inspectionForm = {
                    selectedDataSources: [],
                    reportType: 'daily',
                    includeMetrics: [] // 默认不勾选
                };
            }
            this.inspectionRunning = false; // 确保状态重置
            this.showInspectionModal = true;
        },

        // 指标全选/取消全选
        selectAllMetrics() {
            const enabledMetrics = this.availableMetrics.filter(m => m.enabled !== false);
            if (this.inspectionForm.includeMetrics.length === enabledMetrics.length) {
                this.inspectionForm.includeMetrics = [];
            } else {
                this.inspectionForm.includeMetrics = enabledMetrics.map(m => m.key);
            }
        },

        // 按分组全选/取消
        selectGroupMetrics(group) {
            const groupMetrics = this.availableMetrics.filter(m => m.group === group && m.enabled !== false);
            const groupKeys = groupMetrics.map(m => m.key);
            const allSelected = groupKeys.every(k => this.inspectionForm.includeMetrics.includes(k));
            
            if (allSelected) {
                // 取消该分组
                this.inspectionForm.includeMetrics = this.inspectionForm.includeMetrics.filter(k => !groupKeys.includes(k));
            } else {
                // 选中该分组
                const newMetrics = [...this.inspectionForm.includeMetrics];
                groupKeys.forEach(k => {
                    if (!newMetrics.includes(k)) newMetrics.push(k);
                });
                this.inspectionForm.includeMetrics = newMetrics;
            }
        },

        // 检查分组是否全选
        isGroupAllSelected(group) {
            const groupMetrics = this.availableMetrics.filter(m => m.group === group && m.enabled !== false);
            return groupMetrics.length > 0 && groupMetrics.every(m => this.inspectionForm.includeMetrics.includes(m.key));
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
                    // 追加新的巡检结果到历史记录（最新的在前面）
                    const newResults = result.results || [];
                    this.inspectionResults = [...newResults, ...this.inspectionResults];
                    this.showToast(`巡检完成，共检查 ${result.total || 0} 项`, 'success');
                    this.showInspectionModal = false;
                    this.inspectionSubTab = 'reports';
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('巡检执行失败: ' + e.message, 'error');
            } finally {
                this.inspectionRunning = false;
            }
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

