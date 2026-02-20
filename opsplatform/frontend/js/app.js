// Vue 应用
const { createApp } = Vue;

const vueApp = createApp({
    data() {
        return {
            // 主题状态
            isDarkMode: localStorage.getItem('theme') === 'dark',

            // 语言状态
            currentLanguage: localStorage.getItem('language') || 'zh-CN',
            availableLanguages: [
                { code: 'zh-CN', name: '中文', flag: '🇨🇳' },
                { code: 'en-US', name: 'English', flag: '🇺🇸' }
            ],

            // 用户状态
            currentUser: JSON.parse(localStorage.getItem('currentUser')),
            loginForm: { username: '', password: '' },
            loginLoading: false,

            // 国际化
            currentLang: localStorage.getItem('language') || 'zh-CN',
            availableLanguages: [
                { code: 'zh-CN', name: '中文', flag: '🇨🇳' },
                { code: 'en-US', name: 'English', flag: '🇺🇸' }
            ],

            // MFA 状态
            mfaPending: false,           // 是否等待 MFA 验证
            mfaPendingUserId: '',        // 待验证的用户 ID
            mfaCode: '',                 // MFA 验证码
            showMFASetupModal: false,    // MFA 设置弹窗
            showMFADisableModal: false,  // MFA 禁用弹窗
            mfaSetup: {},                // MFA 设置信息（二维码、密钥）
            mfaBindCode: '',             // MFA 绑定验证码
            mfaDisablePassword: '',      // MFA 禁用确认密码

            // 会话状态
            sessionWarningShown: false,  // 会话过期警告是否已显示
            showSessionModal: false,     // 会话延长弹窗
            sessionExpiryMinutes: 5,     // 会话剩余分钟数

            // 页面状态（从 localStorage 恢复）
            currentTab: localStorage.getItem('currentTab') || 'records',
            expandedGroups: { 
                system: true,      // 系统管理（默认展开）
                resource: true,    // 资源管理（默认展开）
                monitor: false,    // 监控告警
                k8s: false,        // K8S运维
                ticket: false,     // 工单系统
                automation: false, // 自动化运维
                aiops: false,      // 智能运维
                release: false,    // 变更发布
                logs: false,       // 日志服务
                security: false,   // 安全工具
                settings: false    // 系统设置
            },
            inspectionSubTab: 'templates',
            metricsGroupFilter: 'all', // 指标分组筛选: all, k8s, container, node, custom
            metricsCurrentPage: 1,
            metricsPageSize: 10,
            sidebarCollapsed: false,

            // 系统设置
            showLogoModal: false,
            showSessionSettingsModal: false,
            showSecuritySettingsModal: false,
            globalSessionTimeout: '180',  // 默认 3 小时
            currentUserIP: '',
            securitySettings: {
                login_limit_enabled: false,
                max_login_attempts: '5',
                lockout_duration: '30',
                ip_whitelist_enabled: false,
                ip_whitelist: ''
            },
            logoSettings: {
                logo_type: 'text',
                logo_text: 'AI-CloudOps',
                logo_icon: 'cloud',
                logo_image: '',
                logo_layout: 'horizontal',
                logo_color1: '#3b82f6',
                logo_color2: '#8b5cf6',
                logo_color3: '#06b6d4',
                site_title: 'AI-CloudOps 运维平台'
            },
            savingSettings: false,

            // 排班管理
            scheduleYear: new Date().getFullYear(),
            scheduleMonth: new Date().getMonth() + 1,
            scheduleDays: [],
            scheduleData: [],  // 员工排班数据
            scheduleStats: { total: 0, shiftA: 0, shiftB: 0, shiftC: 0 },
            scheduleEmployeeSearch: '',  // 员工搜索
            scheduleShiftFilter: 'all',   // 班次筛选
            showShiftModal: false,
            showEmployeeModal: false,
            shiftEditEmployee: null,
            shiftEditDay: null,
            shiftEditDate: '',
            shiftEditValue: '',
            newEmployee: { name: '', role: '', color: '#667eea' },
            // 班次配置（可自定义） - 初始化时从 localStorage 读取或使用默认值
            shiftTypes: (() => {
                try {
                    const saved = localStorage.getItem('shiftConfig');
                    if (saved) {
                        const parsed = JSON.parse(saved);
                        if (Array.isArray(parsed) && parsed.length > 0) {
                            return parsed;
                        }
                    }
                } catch (e) { }
                // 返回默认配置
                return [
                    { code: 'A', label: 'A', name: '早班', time: '09:00-18:00', color: '#52c41a', isDuty: false },
                    { code: 'A*', label: 'A★', name: '早班值班', time: '09:00-18:00', color: '#faad14', isDuty: true },
                    { code: 'B', label: 'B', name: '中班', time: '15:00-00:00', color: '#1890ff', isDuty: false },
                    { code: 'C', label: 'C', name: '晚班', time: '00:00-09:00', color: '#722ed1', isDuty: false },
                    { code: 'H', label: 'H', name: '休息', time: '-', color: '#bfbfbf', isDuty: false },
                    { code: 'PL', label: 'PL', name: '事假', time: '-', color: '#ff7a45', isDuty: false },
                    { code: 'SL', label: 'SL', name: '病假', time: '-', color: '#ff85c0', isDuty: false },
                    { code: 'AL', label: 'AL', name: '年假', time: '-', color: '#73d13d', isDuty: false },
                    { code: 'CT', label: 'CT', name: '调休', time: '-', color: '#36cfc9', isDuty: false }
                ];
            })(),
            defaultShiftTypes: [
                { code: 'A', label: 'A', name: '早班', time: '09:00-18:00', color: '#52c41a', isDuty: false },
                { code: 'A*', label: 'A★', name: '早班值班', time: '09:00-18:00', color: '#faad14', isDuty: true },
                { code: 'B', label: 'B', name: '中班', time: '15:00-00:00', color: '#1890ff', isDuty: false },
                { code: 'C', label: 'C', name: '晚班', time: '00:00-09:00', color: '#722ed1', isDuty: false },
                { code: 'H', label: 'H', name: '休息', time: '-', color: '#bfbfbf', isDuty: false },
                { code: 'PL', label: 'PL', name: '事假', time: '-', color: '#ff7a45', isDuty: false },
                { code: 'SL', label: 'SL', name: '病假', time: '-', color: '#ff85c0', isDuty: false },
                { code: 'AL', label: 'AL', name: '年假', time: '-', color: '#73d13d', isDuty: false },
                { code: 'CT', label: 'CT', name: '调休', time: '-', color: '#36cfc9', isDuty: false }
            ],
            showShiftConfigModal: false,
            showBatchShiftModal: false,
            batchShiftText: '',
            editingShift: null,
            newShiftForm: { code: '', name: '', time: '', color: '#52c41a', isDuty: false },
            presetColors: ['#52c41a', '#1890ff', '#722ed1', '#faad14', '#ff4d4f', '#13c2c2', '#eb2f96', '#fa8c16', '#a0d911', '#2f54eb'],
            colorIndex: 0,
            // 拖拽排序
            dragIndex: null,
            dragOverIndex: null,
            // 批量选择
            selectedShifts: [],
            showImportModal: false,
            importData: '',
            importStartDate: '',
            importEndDate: '',
            scheduleSearch: '',
            scheduleShiftFilter: '',
            
            // 员工管理 - 临时数据
            pendingEmployees: [],      // 待添加的员工（未保存）
            selectedEmployeeIds: [],   // 选中的员工ID（用于批量删除）
            showBatchAddEmployeeModal: false,
            batchEmployeeText: '',

            // 密码库
            vaultStatus: 'loading', // loading, locked, unlocked, uninitialized
            vaultSession: null,
            vaultItems: [],
            vaultFolders: [],
            vaultSearch: '',
            vaultSelectedFolder: '',
            vaultSelectedType: 'all',
            showVaultInitModal: false,
            showVaultUnlockModal: false,
            showVaultItemModal: false,
            showVaultResetModal: false,
            vaultMasterPassword: '',
            vaultConfirmPassword: '',
            vaultRecoveryKey: '',
            vaultNewItem: {
                name: '',
                username: '',
                password: '',
                url: '',
                notes: '',
                folder_id: '',
                type: 'login',
                favorite: false
            },
            vaultEditMode: false,
            vaultGeneratedPassword: '',
            showPasswordInForm: false,
            vaultPasswordOptions: {
                length: 16,
                upper: true,
                lower: true,
                numbers: true,
                symbols: true
            },
            vaultShowPassword: {},
            
            // 密码库批量导入
            showVaultBatchModal: false,
            vaultBatchText: '',
            vaultBatchFormat: 'csv',
            
            // 密码库文件夹管理
            showVaultFolderModal: false,
            vaultNewFolder: { name: '', icon: 'folder' },
            vaultEditFolderMode: false,
            
            // 密码库用户组和分享
            vaultGroups: [],
            vaultShares: [],
            vaultAvailableUsers: [],
            showVaultGroupModal: false,
            showVaultShareModal: false,
            showVaultGroupMembersModal: false,
            vaultNewGroup: { name: '', description: '' },
            vaultEditGroupMode: false,
            vaultCurrentGroup: null,
            vaultGroupMembers: [],
            vaultNewShare: { target_type: 'folder', target_id: '', shared_with: '', permission: 'read' },

            // RBAC 权限管理
            roles: [],
            permissions: [],
            allPermissions: [],
            selectedRole: null,
            selectedRolePermissions: {},
            showRoleModal: false,
            showRolePermissionModal: false,
            showUserRoleModal: false,
            showPermissionModal: false,
            editRoleMode: false,
            editPermissionMode: false,
            newRole: { code: '', name: '', description: '', status: 'active' },
            newPermission: { code: '', name: '', type: 'button', resource: '', parent_id: '', description: '' },
            currentUserRoles: [],
            selectedUserForRole: null,
            myPermissions: {},
            myMenus: [],

            // 数据
            records: [],
            users: [],
            auditLogs: [],
            datasources: [],
            domains: [],
            websites: [],
            selectedRecords: [],
            selectedDomains: [],
            activeDomainMenu: null,
            activeMetricMenu: null,
            activeDsMenu: null,
            activeUserMenu: null,

            // 搜索和过滤
            searchQuery: '',
            auditSearchQuery: '',
            auditTargetFilter: '',
            auditStatusFilter: '',
            auditStartDate: '',
            auditEndDate: '',
            projectFilter: '',
            statusFilter: '',
            duplicateFilter: '',
            envFilter: '',
            actionFilter: '',
            auditTargetFilter: '',
            auditStatusFilter: '',
            auditStartDate: '',
            auditEndDate: '',
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
            userSearchQuery: '',
            userStatusFilter: '',
            userRoleFilter: '',
            
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
            showWebsiteModal: false,
            showBatchDomainModal: false,
            batchDomainLoading: false,
            showBatchModal: false,
            batchLoading: false,
            showBatchCheckModal: false,
            batchCheckText: '',
            batchCheckRecords: [],
            batchCheckError: '',
            batchCheckLoading: false,
            batchCheckResult: null,
            showBatchEditModal: false,
            batchEditText: '',
            batchEditRecordIds: [],
            showUserModal: false,
            showPasswordModal: false,
            showViewUserModal: false,
            viewUserData: null,
            passwordForm: {
                userId: '',
                username: '',
                newPassword: '',
                confirmPassword: ''
            },
            showNewPassword: false,
            showConfirmPassword: false,
            showHistoryModal: false,
            showPreviewModal: false,
            showRollbackModal: false,
            rollbackTarget: null,
            
            // 历史记录
            historyRecord: null,
            recordHistories: [],
            previewData: null,
            previewHistory: null,
            previewVersion: 0,
            showAuditDetailModal: false,
            showDataSourceModal: false,
            showProfileModal: false,
            showProfilePassword: false,
            showUserPassword: false,

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
            recordForm: { id: '', connection_id: '', project: '', env: 'uat', module: '', vid: '', src_ip: '', src_port: '', dest_ip: '', dest_port: '', status: 'active' },
            userForm: { id: '', username: '', password: '', display_name: '', role: 'user', status: 'active', permissions: [], phone: '', email: '', description: '', session_timeout: '', language: 'zh-CN' },
            dataSourceForm: { id: '', name: '', type: 'prometheus', url: '', username: '', password: '', token: '', description: '', status: 'active' },
            domainForm: { id: '', project: '', module: '', domain_name: '', origin: '', cdn_provider: '', env: 'PROD', expire_time: '', cert_expire_time: '', status: 'active', remark: '' },
            websiteForm: { id: '', name: '', url: '', category: 'internal', description: '', icon: '', biz_contact: '', biz_phone: '', tech_contact: '', tech_phone: '', username: '', password: '', contract_no: '', contract_start: '', contract_end: '', cost_info: '', sort_order: 0, status: 'active' },
            // 厅方管理
            websiteHalls: [],
            hallForm: { id: '', website_id: '', hall_name: '', contact: '', phone: '', username: '', password: '', remark: '', status: 'active' },
            showHallModal: false,
            currentWebsiteForHall: null,
            profileForm: { id: '', username: '', password: '', display_name: '', role: '', session_timeout: '' },

            // 域名批量刷新
            batchRefreshing: false,
            batchRefreshProgress: '',

            // 域名筛选
            domainSearchQuery: '',
            domainProjectFilter: '',
            domainStatusFilter: '',
            domainCdnFilter: '',
            domainEnvFilter: '',
            domainExpireFilter: '',
            domainDuplicateFilter: '',

            // 网站管理
            websiteSearchQuery: '',
            websiteCategoryFilter: '',
            websiteStatusFilter: '',
            websiteCurrentPage: 1,
            websitePageSize: 10,

            // 任务池
            tasks: [],
            taskStats: {},
            taskProjects: [],
            taskSearch: '',
            taskStatusFilter: '',
            taskProjectFilter: '',
            taskPriorityFilter: '',
            taskDelayedFilter: '',   // '', '1', '0'
            taskCompletionFilter: '', // '', 'normal', 'delayed'
            taskCurrentPage: 1,
            taskPageSize: 10,
            showTaskModal: false,
            editTaskMode: false,
            taskForm: {
                id: '', project: '', title: '', source: 'other', category: 'feature', priority: 'P2',
                assignee: '', start_time: '', end_time: '', status: 'pending', result: '', remark: '',
                is_delayed: false, delay_reason: '', delay_desc: '', delay_end_time: ''
            },

            // 批量添加任务
            showBatchTaskModal: false,
            batchTaskText: '',
            batchTaskLoading: false,
            batchTaskResult: null,

            // 员工失误记录
            incidents: [],
            incidentStats: {},
            incidentSearch: '',
            incidentStatusFilter: '',
            incidentTypeFilter: '',
            incidentCurrentPage: 1,
            incidentPageSize: 10,
            showIncidentModal: false,
            editIncidentMode: false,
            incidentForm: {
                id: '', incident_time: '', operator: '', operation_type: 'other', operation_desc: '',
                status: 'pending', severity: 'medium', reason: '', impact: '', solution: '',
                checker: '', check_time: '', check_result: '', remark: ''
            },

            // 商户管理
            merchants: [],
            merchantSearch: '',
            merchantProjectFilter: '',
            merchantEnvFilter: '',
            merchantCurrentPage: 1,
            merchantPageSize: 10,
            showMerchantModal: false,
            editMerchantMode: false,
            merchantForm: {
                id: '', project: '', env: 'prod', website_name: '', contact_emails: [], website_urls: [],
                player_regions: [], estimated_players: '', game_types: [], handicaps: [], languages: [],
                currencies: [], supported_ports: [], wallet_types: [], callback_domains: [],
                whitelist_ips: '', hall_domains: [], site_domains: [], site_accounts: [], app_keys: [],
                app_secrets: '', game_domains: [], redirect_domains: [], remark: '', status: 'active'
            },
            merchantTagInput: {},
            showMerchantDetail: null,
            selectedMerchantIds: [],
            showBatchMerchantModal: false,
            batchMerchantText: '',
            batchMerchantLoading: false,
            batchMerchantResult: null,

            // 服务配置管理
            serviceConfigs: [],
            serviceProjects: [],
            serviceConfigSearch: '',
            serviceConfigProjectFilter: '',
            serviceConfigEnvFilter: '',
            serviceConfigTypeFilter: '',
            serviceConfigCurrentPage: 1,
            serviceConfigPageSize: 10,
            showServiceConfigModal: false,
            editServiceConfigMode: false,
            serviceConfigForm: {
                id: '', project: '', service_name: '', service_type: 'backend', domain: '', port: '',
                env: 'prod', namespace: '', replicas: 1, image: '', remark: '', status: 'active', sort_order: 0,
                dependencies: []
            },
            // 服务依赖
            currentServiceForDeps: null,
            showServiceDepsPanel: false,
            serviceDeps: [],
            showServiceDepModal: false,
            editServiceDepMode: false,
            serviceDepForm: {
                id: '', dependency_type: 'mysql', dependency_name: '', host: '', port: '',
                database: '', username: '', password: '', conn_string: '', remark: '', status: 'active'
            },
            serviceDepShowPassword: {},
            // 批量添加服务
            showBatchServiceModal: false,
            batchServiceText: '',
            batchServiceLoading: false,
            batchServiceResult: null,

            // 网络架构图
            topoLoading: false,
            topoProjectFilter: '',
            topoEnvFilter: '',
            topoZoom: 1,
            topoPanX: 0,
            topoPanY: 0,
            topoDragging: false,
            topoDragStartX: 0,
            topoDragStartY: 0,
            topoNodes: [],
            topoLines: [],
            topoLayers: [],
            topoCanvasW: 2000,
            topoCanvasH: 1200,
            topoActiveNode: null,
            topoSelectedNode: null,

            // Kubernetes 管理
            k8sNamespaces: [],
            k8sDeployments: [],
            k8sServices: [],
            k8sPods: [],
            k8sNodes: [],
            k8sSelectedNamespace: 'default',
            k8sLoading: false,
            k8sSubTab: 'deployments', // deployments, services, pods, nodes
            k8sSearchQuery: '',
            k8sCurrentPage: 1,
            k8sPageSize: 10,
            // K8s Apply 弹窗
            showK8sApplyModal: false,
            k8sApplyYaml: '',
            k8sApplyNamespace: '',
            k8sApplyLoading: false,
            k8sApplyResult: null,
            // K8s Scale 弹窗
            showK8sScaleModal: false,
            k8sScaleDeployment: null,
            k8sScaleReplicas: 1,
            // K8s 更新镜像弹窗
            showK8sImageModal: false,
            k8sImageDeployment: null,
            k8sImageContainer: '',
            k8sImageTag: '',
            // K8s Pod 日志弹窗
            showK8sPodLogsModal: false,
            k8sPodLogsName: '',
            k8sPodLogsContent: '',
            k8sPodLogsTail: 100,
            k8sPodLogsLoading: false,
            // K8s Deployment YAML 弹窗
            showK8sYamlModal: false,
            k8sYamlContent: '',
            k8sYamlName: '',
            // K8s 回滚弹窗
            showK8sRollbackModal: false,
            k8sRollbackDeployment: null,
            k8sRollbackRevision: 0,
            k8sRollbackHistory: '',

            // 批量添加域名
            batchDomainText: '',
            batchDomainRecords: [],
            batchDomainError: '',
            batchDomainProject: '',  // 批量添加时的默认项目
            batchDomainEnv: 'PROD',  // 批量添加时的默认环境
            batchDomainFetchExpiry: 'skip',  // 'skip' 跳过到期时间查询，'fetch' 自动获取

            // 批量添加
            batchText: '',
            batchRecords: [],
            batchError: '',
            batchWarning: '',
            batchEnv: 'uat',
            batchStatus: 'active',

            // 删除 (popconfirm)
            activePopconfirm: null,

            // 自定义确认弹窗
            confirmDialog: {
                show: false,
                type: 'warning', // warning, danger, info, success
                title: '',
                message: '',
                okText: '确定',
                cancelText: '取消',
                resolve: null
            },
            // 批量删除弹窗
            showDeleteModal: false,
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

            // 指标批量选择完成
            selectedMetricIds_placeholder: null
        };
    },

    computed: {
        sessionTimeoutDisplay() {
            const minutes = parseInt(this.globalSessionTimeout) || 30;
            if (minutes >= 1440) return '24 小时';
            if (minutes >= 480) return '8 小时';
            if (minutes >= 180) return '3 小时';
            if (minutes >= 60) return '1 小时';
            return minutes + ' 分钟';
        },
        projectList() {
            const projects = [...new Set((this.records || []).map(r => r.project).filter(Boolean))];
            return projects.sort((a, b) => a.localeCompare(b, 'zh-CN'));
        },
        filteredRecords() {
            let r = this.records || [];
            if (this.projectFilter) r = r.filter(x => x.project === this.projectFilter);
            if (this.envFilter) r = r.filter(x => x.env === this.envFilter);
            if (this.statusFilter) r = r.filter(x => x.status === this.statusFilter);
            if (this.duplicateFilter) {
                const connIdCount = {};
                this.records.forEach(rec => {
                    if (rec.connection_id) {
                        connIdCount[rec.connection_id] = (connIdCount[rec.connection_id] || 0) + 1;
                    }
                });
                if (this.duplicateFilter === 'duplicate') {
                    r = r.filter(x => x.connection_id && connIdCount[x.connection_id] > 1);
                } else if (this.duplicateFilter === 'unique') {
                    r = r.filter(x => !x.connection_id || connIdCount[x.connection_id] === 1);
                }
            }
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
            if (this.auditTargetFilter) r = r.filter(x => x.target_type === this.auditTargetFilter);
            if (this.auditSearchQuery) {
                const q = this.auditSearchQuery.toLowerCase();
                r = r.filter(x => [x.operator, x.changes, x.target_id].some(v => v && v.toLowerCase().includes(q)));
            }
            return r;
        },
        
        // 审计日志统计
        auditTodayCount() {
            const today = new Date().toISOString().slice(0, 10);
            return (this.auditLogs || []).filter(x => x.created_at && x.created_at.startsWith(today)).length;
        },
        auditDeleteCount() {
            return (this.auditLogs || []).filter(x => x.action === 'delete').length;
        },
        auditCreateCount() {
            return (this.auditLogs || []).filter(x => x.action === 'create').length;
        },
        auditLoginCount() {
            return (this.auditLogs || []).filter(x => x.action === 'login').length;
        },
        auditAvgDuration() {
            const logs = (this.auditLogs || []).filter(x => x.duration && x.duration > 0);
            if (logs.length === 0) return 0;
            const total = logs.reduce((sum, x) => sum + (x.duration || 0), 0);
            return Math.round(total / logs.length);
        },
        auditErrorCount() {
            return (this.auditLogs || []).filter(x => x.status_code && x.status_code >= 400).length;
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
        filteredUsers() {
            let result = this.users || [];
            if (this.userSearchQuery) {
                const q = this.userSearchQuery.toLowerCase();
                result = result.filter(u => 
                    (u.username || '').toLowerCase().includes(q) || 
                    (u.display_name || '').toLowerCase().includes(q)
                );
            }
            if (this.userStatusFilter) {
                result = result.filter(u => u.status === this.userStatusFilter);
            }
            if (this.userRoleFilter) {
                result = result.filter(u => u.role === this.userRoleFilter);
            }
            return result;
        },
        pagedUsers() {
            const start = (this.userCurrentPage - 1) * this.userPageSize;
            return this.filteredUsers.slice(start, start + this.userPageSize);
        },
        userTotalPages() {
            return Math.max(1, Math.ceil(this.filteredUsers.length / this.userPageSize));
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
        // 过滤后的排班数据
        filteredScheduleData() {
            let data = this.scheduleData || [];
            // 按员工名搜索
            if (this.scheduleEmployeeSearch) {
                const q = this.scheduleEmployeeSearch.toLowerCase();
                data = data.filter(emp => emp.name && emp.name.toLowerCase().includes(q));
            }
            // 按班次筛选
            if (this.scheduleShiftFilter && this.scheduleShiftFilter !== 'all') {
                data = data.filter(emp => {
                    if (!emp.shifts) return false;
                    return Object.values(emp.shifts).some(s => s === this.scheduleShiftFilter);
                });
            }
            return data;
        },
        
        // 过滤后的域名
        filteredDomains() {
            let d = this.domains || [];
            if (this.domainProjectFilter) d = d.filter(x => x.project === this.domainProjectFilter);
            if (this.domainStatusFilter) d = d.filter(x => x.status === this.domainStatusFilter);
            if (this.domainCdnFilter) d = d.filter(x => x.cdn_provider === this.domainCdnFilter);
            if (this.domainEnvFilter) d = d.filter(x => (x.env || 'PROD') === this.domainEnvFilter);
            // 重复域名筛选
            if (this.domainDuplicateFilter) {
                const domainCounts = {};
                (this.domains || []).forEach(x => {
                    domainCounts[x.domain_name] = (domainCounts[x.domain_name] || 0) + 1;
                });
                if (this.domainDuplicateFilter === 'duplicate') {
                    d = d.filter(x => domainCounts[x.domain_name] > 1);
                } else if (this.domainDuplicateFilter === 'unique') {
                    d = d.filter(x => domainCounts[x.domain_name] === 1);
                }
            }
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

        // 网站管理计算属性
        filteredWebsites() {
            let result = this.websites || [];
            if (this.websiteSearchQuery) {
                const q = this.websiteSearchQuery.toLowerCase();
                result = result.filter(s => 
                    (s.name && s.name.toLowerCase().includes(q)) ||
                    (s.url && s.url.toLowerCase().includes(q)) ||
                    (s.description && s.description.toLowerCase().includes(q))
                );
            }
            if (this.websiteCategoryFilter) {
                result = result.filter(s => s.category === this.websiteCategoryFilter);
            }
            if (this.websiteStatusFilter) {
                result = result.filter(s => s.status === this.websiteStatusFilter);
            }
            return result;
        },
        pagedWebsites() {
            const start = (this.websiteCurrentPage - 1) * this.websitePageSize;
            return this.filteredWebsites.slice(start, start + this.websitePageSize);
        },
        websiteTotalPages() {
            return Math.max(1, Math.ceil(this.filteredWebsites.length / this.websitePageSize));
        },
        websiteDisplayedPages() {
            return this.getDisplayedPages(this.websiteTotalPages, this.websiteCurrentPage);
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
        },

        // 过滤后的密码库条目
        filteredVaultItems() {
            let items = this.vaultItems || [];
            
            // 按搜索词过滤
            if (this.vaultSearch) {
                const search = this.vaultSearch.toLowerCase();
                items = items.filter(item => 
                    (item.name && item.name.toLowerCase().includes(search)) ||
                    (item.username && item.username.toLowerCase().includes(search)) ||
                    (item.url && item.url.toLowerCase().includes(search))
                );
            }
            
            // 按文件夹过滤
            if (this.vaultSelectedFolder) {
                items = items.filter(item => item.folder_id === this.vaultSelectedFolder);
            }
            
            // 按类型过滤
            if (this.vaultSelectedType && this.vaultSelectedType !== 'all') {
                items = items.filter(item => item.type === this.vaultSelectedType);
            }
            
            return items;
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
            this.updatePageTitle(val);
        }
    },

    mounted() {
        // 初始化主题
        this.initTheme();

        // 初始化语言
        this.initLanguage();

        // 加载 Logo 配置（无需登录）
        this.loadLogoSettings();

        // 检查登录状态，未登录跳转到登录页
        if (!this.currentUser) {
            window.location.href = '/login.html';
            return;
        }

        // 设置会话超时检测
        this.setupSessionTimeout();

        // 根据当前页面加载对应数据
        this.loadDataForCurrentTab();
        this.refreshCurrentUser();
        this.updateBreadcrumbs();
        this.updatePageTitle(this.currentTab);
        // 加载自定义指标
        this.loadMetrics();

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
            // 点击外部关闭 popconfirm
            if (!e.target.closest('.popconfirm-wrapper')) {
                this.activePopconfirm = null;
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
                case 'schedule':
                    this.loadScheduleData();
                    break;
                case 'vault':
                    this.initVault();
                    break;
                case 'roles':
                    this.loadRoles();
                    break;
                case 'permissions':
                    this.loadAllPermissions();
                    break;
                case 'merchants':
                    this.loadMerchants();
                    break;
                case 'taskPool':
                    this.loadTaskData();
                    break;
                case 'incidents':
                    this.loadIncidentData();
                    break;
                case 'serviceConfig':
                    this.loadServiceConfigData();
                    break;
                case 'topology':
                    this.loadTopologyData();
                    break;
                case 'websites':
                    this.loadWebsites();
                    break;
                default:
                    this.loadRecords();
            }
        },

        // ========== 主题相关 ==========
        initTheme() {
            const savedTheme = localStorage.getItem('theme');
            if (savedTheme === 'dark') {
                document.documentElement.setAttribute('data-theme', 'dark');
                this.isDarkMode = true;
            } else {
                document.documentElement.removeAttribute('data-theme');
                this.isDarkMode = false;
            }
        },

        toggleTheme() {
            this.isDarkMode = !this.isDarkMode;
            if (this.isDarkMode) {
                document.documentElement.setAttribute('data-theme', 'dark');
                localStorage.setItem('theme', 'dark');
            } else {
                document.documentElement.removeAttribute('data-theme');
                localStorage.removeItem('theme');
            }
        },

        // ========== 语言切换 ==========
        t(key) {
            const lang = window.i18n && window.i18n[this.currentLanguage];
            if (!lang) return key;
            
            const keys = key.split('.');
            let value = lang;
            for (const k of keys) {
                if (value && typeof value === 'object' && k in value) {
                    value = value[k];
                } else {
                    return key;
                }
            }
            return typeof value === 'string' ? value : key;
        },

        async setLanguage(langCode) {
            this.currentLanguage = langCode;
            localStorage.setItem('language', langCode);
            document.documentElement.setAttribute('lang', langCode);
            
            // 如果用户已登录，同步到服务器
            if (this.currentUser) {
                try {
                    const res = await API.updateUser(this.currentUser.id, { language: langCode });
                    if (res.ok) {
                        this.currentUser.language = langCode;
                        localStorage.setItem('currentUser', JSON.stringify(this.currentUser));
                    }
                } catch (e) {
                    console.error('保存语言设置失败:', e);
                }
            }
            
            this.showToast(langCode === 'zh-CN' ? '语言已切换为中文' : 'Language changed to English', 'success');
        },

        initLanguage() {
            // 优先从用户设置读取，其次从本地存储，最后默认中文
            let lang = 'zh-CN';
            if (this.currentUser && this.currentUser.language) {
                lang = this.currentUser.language;
            } else if (localStorage.getItem('language')) {
                lang = localStorage.getItem('language');
            }
            this.currentLanguage = lang;
            localStorage.setItem('language', lang);
            document.documentElement.setAttribute('lang', lang);
        },

        // ========== 面包屑导航 ==========
        updateBreadcrumbs() {
            const tabMap = {
                'dashboard': '欢迎页',
                'records': '网络管理',
                'domains': '域名管理',
                'inspection': '一键巡检',
                'datasources': '数据源配置',
                'audit': '审计日志',
                'users': '用户管理',
                'roles': '角色管理',
                'permissions': '权限配置',
                'apiManagement': '接口管理',
                'cmdb': '资产管理',
                'topology': '服务拓扑',
                'metrics': '指标监控',
                'alerts': '告警管理',
                'alertRules': '告警规则',
                'alertNotify': '通知配置',
                'bigScreen': '大屏展示',
                'clusters': '集群管理',
                'nodes': '节点管理',
                'workloads': '工作负载',
                'services': '服务管理',
                'configMaps': '配置管理',
                'storage': '存储管理',
                'webTerminal': '容器终端',
                'tickets': '工单管理',
                'workflow': '流程引擎',
                'sla': 'SLA管理',
                'ticketTemplates': '工单模板',
                'jobPlatform': '作业平台',
                'cronTasks': '定时任务',
                'autoInspection': '自动巡检',
                'selfHealing': '自愈策略',
                'anomalyDetection': '异常检测',
                'rootCauseAnalysis': '根因分析',
                'faultPrediction': '故障预测',
                'smartAlerts': '智能告警',
                'capacityPrediction': '容量预测',
                'releaseManagement': '发布管理',
                'changeManagement': '变更管理',
                'rollbackManagement': '回滚管理',
                'logQuery': '日志查询',
                'logAnalysis': '日志分析',
                'logAlerts': '日志告警',
                'settings': '系统参数',
                'themeSettings': '主题设置'
            };
            const groupMap = {
                'records': '资源管理',
                'domains': '资源管理',
                'cmdb': '资源管理',
                'topology': '资源管理',
                'inspection': '运维工具',
                'datasources': '系统设置',
                'audit': '系统管理',
                'users': '系统管理',
                'roles': '系统管理',
                'permissions': '系统管理',
                'apiManagement': '系统管理',
                'dashboard': '系统管理'
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

        // 动态更新页面标题
        updatePageTitle(tab) {
            const tabTitles = {
                'dashboard': '欢迎页',
                'records': '网络管理',
                'domains': '域名管理',
                'inspection': '一键巡检',
                'datasources': '数据源配置',
                'audit': '审计日志',
                'users': '用户管理',
                'roles': '角色管理',
                'permissions': '权限配置',
                'apiManagement': '接口管理',
                'cmdb': '资产管理',
                'topology': '服务拓扑',
                'metrics': '指标监控',
                'alerts': '告警管理',
                'alertRules': '告警规则',
                'alertNotify': '通知配置',
                'bigScreen': '大屏展示',
                'clusters': '集群管理',
                'nodes': '节点管理',
                'workloads': '工作负载',
                'services': '服务管理',
                'configMaps': '配置管理',
                'storage': '存储管理',
                'webTerminal': '容器终端',
                'tickets': '工单管理',
                'workflow': '流程引擎',
                'sla': 'SLA管理',
                'ticketTemplates': '工单模板',
                'jobPlatform': '作业平台',
                'cronTasks': '定时任务',
                'autoInspection': '自动巡检',
                'selfHealing': '自愈策略',
                'anomalyDetection': '异常检测',
                'rootCauseAnalysis': '根因分析',
                'faultPrediction': '故障预测',
                'smartAlerts': '智能告警',
                'capacityPrediction': '容量预测',
                'releaseManagement': '发布管理',
                'changeManagement': '变更管理',
                'rollbackManagement': '回滚管理',
                'logQuery': '日志查询',
                'logAnalysis': '日志分析',
                'logAlerts': '日志告警',
                'settings': '系统参数',
                'themeSettings': '主题设置'
            };
            const title = tabTitles[tab] || '运维平台';
            document.title = title + ' - 运维平台';
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
                role: this.currentUser.role,
                session_timeout: localStorage.getItem('sessionTimeout') || '30'
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
                    // 保存会话超时设置
                    if (this.profileForm.session_timeout) {
                        localStorage.setItem('sessionTimeout', this.profileForm.session_timeout);
                        // 重新启动会话检测
                        API.stopSessionCheck();
                        API.startSessionCheck(parseInt(this.profileForm.session_timeout), localStorage.getItem('sessionExpiresAt'));
                    }
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
        
        filterUsers() {
            this.userCurrentPage = 1;
        },
        
        resetUserFilter() {
            this.userSearchQuery = '';
            this.userStatusFilter = '';
            this.userRoleFilter = '';
            this.userCurrentPage = 1;
        },

        resetAuditFilter() {
            this.auditSearchQuery = '';
            this.actionFilter = '';
            this.auditTargetFilter = '';
            this.auditStatusFilter = '';
            this.auditStartDate = '';
            this.auditEndDate = '';
            this.auditCurrentPage = 1;
        },
        
        resetRecordFilter() {
            this.searchQuery = '';
            this.projectFilter = '';
            this.envFilter = '';
            this.statusFilter = '';
            this.duplicateFilter = '';
            this.currentPage = 1;
        },
        
        async toggleUserStatus(user) {
            const newStatus = user.status === 'active' ? 'inactive' : 'active';
            try {
                const res = await API.updateUser(user.id, { ...user, status: newStatus });
                if (res.ok) {
                    user.status = newStatus;
                    this.showToast(`用户状态已${newStatus === 'active' ? '启用' : '停用'}`, 'success');
                }
            } catch (e) {
                this.showToast('操作失败', 'error');
            }
        },
        
        formatDate(dateStr) {
            if (!dateStr) return '-';
            const d = new Date(dateStr);
            return `${d.getFullYear()}/${String(d.getMonth()+1).padStart(2,'0')}/${String(d.getDate()).padStart(2,'0')}`;
        },
        
        viewUser(user) {
            this.showToast(`查看用户: ${user.username}`, 'info');
        },
        
        openPasswordModal(user) {
            this.passwordForm = {
                userId: user.id,
                username: user.username,
                newPassword: '',
                confirmPassword: ''
            };
            this.showNewPassword = false;
            this.showConfirmPassword = false;
            this.showPasswordModal = true;
        },
        
        generatePassword() {
            const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$%';
            let password = '';
            for (let i = 0; i < 12; i++) {
                password += chars.charAt(Math.floor(Math.random() * chars.length));
            }
            this.passwordForm.newPassword = password;
            this.passwordForm.confirmPassword = password;
            this.showNewPassword = true;
            this.showConfirmPassword = true;
            this.showToast('已生成随机密码', 'success');
        },
        
        copyPassword() {
            if (!this.passwordForm.newPassword) {
                this.showToast('请先生成或输入密码', 'warning');
                return;
            }
            navigator.clipboard.writeText(this.passwordForm.newPassword).then(() => {
                this.showToast('密码已复制到剪贴板', 'success');
            }).catch(() => {
                // 降级方案
                const input = document.createElement('input');
                input.value = this.passwordForm.newPassword;
                document.body.appendChild(input);
                input.select();
                document.execCommand('copy');
                document.body.removeChild(input);
                this.showToast('密码已复制到剪贴板', 'success');
            });
        },
        
        // 编辑用户弹窗中的密码生成
        generateUserPassword() {
            const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$%';
            let password = '';
            for (let i = 0; i < 12; i++) {
                password += chars.charAt(Math.floor(Math.random() * chars.length));
            }
            this.userForm.password = password;
            this.showUserPassword = true;
            this.showToast('已生成随机密码', 'success');
        },
        
        copyUserPassword() {
            if (!this.userForm.password) {
                this.showToast('请先生成或输入密码', 'warning');
                return;
            }
            navigator.clipboard.writeText(this.userForm.password).then(() => {
                this.showToast('密码已复制到剪贴板', 'success');
            }).catch(() => {
                const input = document.createElement('input');
                input.value = this.userForm.password;
                document.body.appendChild(input);
                input.select();
                document.execCommand('copy');
                document.body.removeChild(input);
                this.showToast('密码已复制到剪贴板', 'success');
            });
        },
        
        async submitPasswordChange() {
            if (!this.passwordForm.newPassword) {
                this.showToast('请输入新密码', 'error');
                return;
            }
            if (this.passwordForm.newPassword.length < 6) {
                this.showToast('密码长度至少6位', 'error');
                return;
            }
            try {
                const res = await API.changePassword(this.passwordForm.userId, this.passwordForm.newPassword, this.currentUser?.username);
                if (res.ok) {
                    this.showToast('密码修改成功', 'success');
                    this.showPasswordModal = false;
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('修改失败', 'error');
            }
        },
        
        openViewUserModal(user) {
            this.viewUserData = user;
            this.showViewUserModal = true;
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
                        // 保存 JWT token（兼容旧方式）
                        if (data.token) {
                            API.setToken(data.token);
                        }
                        // 保存会话超时信息
                        if (data.timeout_minutes) {
                            localStorage.setItem('sessionTimeout', data.timeout_minutes);
                        }
                        if (data.expires_at) {
                            localStorage.setItem('sessionExpiresAt', data.expires_at);
                        }
                        // 启动会话超时检测
                        this.setupSessionTimeout();
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
                    const data = await res.json();
                    this.currentUser = data.user;
                    localStorage.setItem('currentUser', JSON.stringify(this.currentUser));
                    // 保存 JWT token（兼容旧方式）
                    if (data.token) {
                        API.setToken(data.token);
                    }
                    // 保存会话超时信息
                    if (data.timeout_minutes) {
                        localStorage.setItem('sessionTimeout', data.timeout_minutes);
                    }
                    if (data.expires_at) {
                        localStorage.setItem('sessionExpiresAt', data.expires_at);
                    }
                    // 启动会话超时检测
                    this.setupSessionTimeout();
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

        async logout() {
            try {
                await API.logout();
            } catch (e) {
                console.warn('登出请求失败:', e);
            }
            this.currentUser = null;
            localStorage.removeItem('currentUser');
            localStorage.removeItem('authToken');
            API.clearToken();
            this.loginForm = { username: '', password: '' };
            this.mfaPending = false;
            this.mfaPendingUserId = '';
            // 跳转到登录页面
            window.location.href = '/login.html';
        },

        // ========== 国际化 ==========
        t(key) {
            const lang = window.i18n?.[this.currentLang] || window.i18n?.['zh-CN'] || {};
            const keys = key.split('.');
            let value = lang;
            for (const k of keys) {
                value = value?.[k];
                if (value === undefined) break;
            }
            return value || key;
        },

        async changeLanguage(langCode) {
            this.currentLang = langCode;
            localStorage.setItem('language', langCode);
            
            // 如果用户已登录，同步到服务器
            if (this.currentUser) {
                try {
                    const res = await API.updateUser(this.currentUser.id, { language: langCode });
                    if (res.ok) {
                        this.currentUser.language = langCode;
                        localStorage.setItem('currentUser', JSON.stringify(this.currentUser));
                        this.showToast(this.currentLang === 'zh-CN' ? '语言已切换' : 'Language changed', 'success');
                    }
                } catch (e) {
                    console.error('保存语言设置失败:', e);
                }
            }
        },

        initLanguage() {
            // 优先使用用户设置的语言，否则使用本地存储，最后默认中文
            if (this.currentUser?.language) {
                this.currentLang = this.currentUser.language;
            } else {
                this.currentLang = localStorage.getItem('language') || 'zh-CN';
            }
            localStorage.setItem('language', this.currentLang);
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

        // ========== 会话超时管理 ==========
        setupSessionTimeout() {
            // 从 localStorage 获取会话超时设置
            const timeout = parseInt(localStorage.getItem('sessionTimeout')) || 30;
            const expiresAt = localStorage.getItem('sessionExpiresAt');
            
            // 设置会话过期回调
            API.onSessionExpired = () => {
                this.showToast('会话已过期，请重新登录', 'warning');
                setTimeout(() => {
                    this.logout();
                }, 2000);
            };
            
            // 设置会话即将过期提醒
            API.onSessionWarning = (remainingMinutes) => {
                if (!this.sessionWarningShown) {
                    this.sessionWarningShown = true;
                    this.showSessionExpiryWarning(remainingMinutes);
                }
            };
            
            // 启动会话检测
            API.startSessionCheck(timeout, expiresAt);
        },
        
        showSessionExpiryWarning(remainingMinutes) {
            // 显示会话即将过期的自定义弹窗
            this.sessionExpiryMinutes = remainingMinutes;
            this.showSessionModal = true;
        },
        
        confirmExtendSession() {
            this.showSessionModal = false;
            this.sessionWarningShown = false;
            this.refreshSession();
        },
        
        cancelExtendSession() {
            this.showSessionModal = false;
            this.sessionWarningShown = false;
        },
        
        async refreshSession() {
            try {
                const res = await API.refreshSession();
                if (res.ok) {
                    const data = await res.json();
                    if (data.expires_at) {
                        localStorage.setItem('sessionExpiresAt', data.expires_at);
                    }
                    this.showToast('会话已延长', 'success');
                } else {
                    this.showToast('刷新会话失败', 'error');
                }
            } catch (e) {
                this.showToast('刷新会话失败', 'error');
            }
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
        confirmResetUserMFA(userId) {
            this.deleteTarget = { id: userId };
            this.deleteType = 'mfa';
            this.deleteMessage = '确定要重置该用户的 MFA 吗？用户需要重新绑定认证器。';
            this.showDeleteModal = true;
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

        // ========== 网站管理 ==========
        async loadWebsites() {
            try {
                const res = await API.getWebsites();
                const data = await res.json();
                this.websites = data.websites || [];
            } catch (e) {
                this.showToast('加载网站列表失败', 'error');
            }
        },

        openWebsiteModal(website = null) {
            if (website) {
                this.websiteForm = { ...website, password: '' };
            } else {
                this.websiteForm = { id: '', name: '', url: '', category: 'internal', description: '', icon: '', biz_contact: '', biz_phone: '', tech_contact: '', tech_phone: '', username: '', password: '', contract_no: '', contract_start: '', contract_end: '', cost_info: '', sort_order: 0, status: 'active' };
            }
            this.showWebsiteModal = true;
        },

        async saveWebsite() {
            if (!this.websiteForm.name || !this.websiteForm.url) {
                this.showToast('请填写网站名称和URL', 'error');
                return;
            }
            try {
                let res;
                if (this.websiteForm.id) {
                    res = await API.updateWebsite(this.websiteForm.id, {
                        ...this.websiteForm,
                        updated_by: this.currentUser?.username
                    });
                } else {
                    res = await API.createWebsite({
                        ...this.websiteForm,
                        created_by: this.currentUser?.username
                    });
                }
                if (res.ok) {
                    this.showToast(this.websiteForm.id ? '网站更新成功' : '网站添加成功', 'success');
                    this.showWebsiteModal = false;
                    await this.loadWebsites();
                } else {
                    const err = await res.text();
                    this.showToast(err || '操作失败', 'error');
                }
            } catch (e) {
                this.showToast('操作失败: ' + e.message, 'error');
            }
        },

        async deleteWebsite(website) {
            try {
                const res = await API.deleteWebsite(website.id);
                if (res.ok) {
                    this.showToast('删除成功', 'success');
                    await this.loadWebsites();
                } else {
                    this.showToast('删除失败', 'error');
                }
            } catch (e) {
                this.showToast('删除失败', 'error');
            }
            this.activePopconfirm = null;
        },

        async viewWebsitePassword(website) {
            try {
                const res = await API.getWebsitePassword(website.id);
                const data = await res.json();
                if (data.password) {
                    navigator.clipboard.writeText(data.password).then(() => {
                        this.showToast('密码已复制到剪贴板', 'success');
                    }).catch(() => {
                        this.showToast('复制失败，密码: ' + data.password, 'warning');
                    });
                } else {
                    this.showToast(data.message || '获取密码失败', 'error');
                }
            } catch (e) {
                this.showToast('获取密码失败', 'error');
            }
        },

        getCategoryLabel(category) {
            const labels = { 'internal': '内部系统', 'external': '外部服务', 'tool': '运维工具' };
            return labels[category] || category;
        },

        getContractExpireClass(dateStr) {
            if (!dateStr) return '';
            const date = new Date(dateStr);
            const now = new Date();
            const daysLeft = Math.ceil((date - now) / (1000 * 60 * 60 * 24));
            if (daysLeft < 0) return 'text-danger';
            if (daysLeft <= 30) return 'text-warning';
            return '';
        },

        // ========== 甲方管理 ==========
        async openHallsView(website) {
            this.currentWebsiteForHall = website;
            try {
                const res = await API.getWebsiteHalls(website.id);
                const data = await res.json();
                this.websiteHalls = data.halls || [];
            } catch (e) {
                this.showToast('加载厅方列表失败', 'error');
            }
        },

        openHallModal(hall = null) {
            if (hall) {
                this.hallForm = { ...hall, password: '' };
            } else {
                this.hallForm = { id: '', website_id: this.currentWebsiteForHall?.id || '', hall_name: '', contact: '', phone: '', username: '', password: '', remark: '', status: 'active' };
            }
            this.showHallModal = true;
        },

        async saveHall() {
            if (!this.hallForm.hall_name) {
                this.showToast('请填写厅方名称', 'error');
                return;
            }
            try {
                let res;
                const websiteId = this.currentWebsiteForHall?.id;
                if (this.hallForm.id) {
                    res = await API.updateWebsiteHall(websiteId, this.hallForm.id, {
                        ...this.hallForm,
                        updated_by: this.currentUser?.username
                    });
                } else {
                    res = await API.createWebsiteHall(websiteId, {
                        ...this.hallForm,
                        created_by: this.currentUser?.username
                    });
                }
                if (res.ok) {
                    this.showToast(this.hallForm.id ? '厅方更新成功' : '厅方添加成功', 'success');
                    this.showHallModal = false;
                    await this.openHallsView(this.currentWebsiteForHall);
                } else {
                    const err = await res.text();
                    this.showToast(err || '操作失败', 'error');
                }
            } catch (e) {
                this.showToast('操作失败: ' + e.message, 'error');
            }
        },

        async deleteHall(hall) {
            try {
                const res = await API.deleteWebsiteHall(this.currentWebsiteForHall?.id, hall.id);
                if (res.ok) {
                    this.showToast('删除成功', 'success');
                    await this.openHallsView(this.currentWebsiteForHall);
                } else {
                    this.showToast('删除失败', 'error');
                }
            } catch (e) {
                this.showToast('删除失败', 'error');
            }
            this.activePopconfirm = null;
        },

        async viewHallPassword(hall) {
            try {
                const res = await API.getHallPassword(this.currentWebsiteForHall?.id, hall.id);
                const data = await res.json();
                if (data.password) {
                    navigator.clipboard.writeText(data.password).then(() => {
                        this.showToast('密码已复制到剪贴板', 'success');
                    }).catch(() => {
                        this.showToast('复制失败，密码: ' + data.password, 'warning');
                    });
                } else {
                    this.showToast(data.message || '获取密码失败', 'error');
                }
            } catch (e) {
                this.showToast('获取密码失败', 'error');
            }
        },

        closeHallsView() {
            this.currentWebsiteForHall = null;
            this.websiteHalls = [];
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

        async batchRefreshDomains() {
            if (this.batchRefreshing) return;
            
            const confirmed = await this.showConfirm({
                type: 'warning',
                title: '批量刷新到期时间',
                message: '这将刷新所有启用域名的证书和域名到期时间。\n如果域名数量较多，可能需要几分钟时间。',
                okText: '开始刷新',
                cancelText: '取消'
            });
            
            if (!confirmed) return;

            this.batchRefreshing = true;
            this.batchRefreshProgress = '准备中...';

            try {
                const res = await fetch('/api/domains/batch-refresh', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'X-CSRF-Token': this.csrfToken
                    },
                    credentials: 'include'
                });

                const data = await res.json();
                if (data.success) {
                    await this.loadDomains();
                    
                    // 根据结果显示不同类型的提示
                    if (data.total === 0) {
                        this.showToast('没有找到启用状态的域名', 'warning');
                    } else if (data.success_count === 0 && data.fail_count > 0) {
                        // 全部失败
                        let msg = `刷新失败：共 ${data.total} 个域名全部失败`;
                        if (data.fail_details && data.fail_details.length > 0) {
                            msg += '\n\n失败原因：\n' + data.fail_details.slice(0, 5).join('\n');
                            if (data.fail_details.length > 5) {
                                msg += `\n... 等 ${data.fail_details.length} 条`;
                            }
                        }
                        this.showToast(msg, 'error');
                    } else if (data.fail_count > 0) {
                        // 部分失败
                        let msg = `刷新完成：成功 ${data.success_count} 个，失败 ${data.fail_count} 个`;
                        if (data.fail_details && data.fail_details.length > 0) {
                            msg += '\n\n失败详情：\n' + data.fail_details.slice(0, 3).join('\n');
                        }
                        this.showToast(msg, 'warning');
                    } else {
                        // 全部成功
                        this.showToast(`刷新成功：共更新 ${data.success_count} 个域名的到期时间`, 'success');
                    }
                } else {
                    this.showToast(data.message || '批量刷新失败', 'error');
                }
            } catch (e) {
                this.showToast('批量刷新失败: ' + e.message, 'error');
            } finally {
                this.batchRefreshing = false;
                this.batchRefreshProgress = '';
            }
        },

        openDomainModal(domain = null) {
            if (domain) {
                this.domainForm = { ...domain };
                if (!this.domainForm.env) {
                    this.domainForm.env = 'PROD';
                }
            } else {
                this.domainForm = { id: '', project: '', module: '', domain_name: '', origin: '', origin_ip: '', cdn_provider: '', env: 'PROD', expire_time: '', cert_expire_time: '', status: 'active', remark: '' };
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
                    const data = await res.json();
                    if (data.warning) {
                        this.showToast(data.warning, 'warning');
                    } else {
                        this.showToast(this.domainForm.id ? '域名更新成功' : '域名添加成功', 'success');
                    }
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
            try {
                const res = await API.deleteDomain(domain.id);
                if (res.ok) {
                    this.showToast('删除成功', 'success');
                    // 从已选中列表中移除该域名
                    const idx = this.selectedDomains.indexOf(domain.id);
                    if (idx !== -1) {
                        this.selectedDomains.splice(idx, 1);
                    }
                    await this.loadDomains();
                } else {
                    this.showToast('删除失败', 'error');
                }
            } catch (e) {
                this.showToast('删除失败', 'error');
            }
            this.activePopconfirm = null;
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
            if (action === 'delete') {
                this.deleteType = 'batchDomain';
                this.deleteMessage = `确定删除选中的 ${this.selectedDomains.length} 个域名吗？`;
                this.showDeleteModal = true;
                return;
            }
            // 启用/停用不需要确认弹窗
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

        // 获取到期时间徽章样式类
        // 30天以上: 绿色, 15-30天: 黄色, 7-15天: 橙色, 7天以下: 红色
        getExpiryBadgeClass(expireTime) {
            if (!expireTime) return 'expiry-none';
            const days = this.getCertDaysLeft(expireTime);
            if (days === null) return 'expiry-none';
            if (days <= 0) return 'expiry-expired';    // 已过期 - 深红
            if (days <= 7) return 'expiry-critical';   // 7天以下 - 红色
            if (days <= 15) return 'expiry-danger';    // 7-15天 - 橙色
            if (days <= 30) return 'expiry-warning';   // 15-30天 - 黄色
            return 'expiry-safe';                       // 30天以上 - 绿色
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

        // 格式化日期时间显示（精确到秒）
        formatDateTime(dateStr) {
            if (!dateStr) return '-';
            try {
                const date = new Date(dateStr);
                if (isNaN(date.getTime())) return dateStr;
                const y = date.getFullYear();
                const m = String(date.getMonth() + 1).padStart(2, '0');
                const d = String(date.getDate()).padStart(2, '0');
                const h = String(date.getHours()).padStart(2, '0');
                const min = String(date.getMinutes()).padStart(2, '0');
                const s = String(date.getSeconds()).padStart(2, '0');
                return `${y}-${m}-${d} ${h}:${min}:${s}`;
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
            this.batchDomainFetchExpiry = 'skip';  // 默认快速添加
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
                // 2. 完整格式: 模块,域名,回源,源站IP,CDN厂商,域名到期,备注 (用逗号或制表符分隔)
                const parts = line.split(/[,\t]+/).map(s => s.trim());
                
                if (parts.length === 0 || !parts[0]) continue;

                let domain;
                if (parts.length === 1) {
                    // 只有一个字段，当做域名处理
                    domain = {
                        project: this.batchDomainProject,
                        env: this.batchDomainEnv,
                        domain_name: parts[0],
                        module: '',
                        origin: '',
                        origin_ip: '',
                        cdn_provider: '',
                        expire_time: '',
                        remark: '',
                        status: 'active'
                    };
                } else {
                    // 完整格式: 模块,域名,回源,源站IP,CDN厂商,域名到期,备注
                    domain = {
                        project: this.batchDomainProject,
                        env: this.batchDomainEnv,
                        module: parts[0] || '',
                        domain_name: parts[1] || '',
                        origin: parts[2] || '',
                        origin_ip: parts[3] || '',
                        cdn_provider: parts[4] || '',
                        expire_time: parts[5] || '',
                        remark: parts[6] || '',
                        status: 'active'
                    };
                }

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

            this.batchDomainLoading = true;
            const fetchExpiry = this.batchDomainFetchExpiry === 'fetch';
            if (fetchExpiry && this.batchDomainRecords.length > 10) {
                this.showToast(`正在添加 ${this.batchDomainRecords.length} 个域名并获取到期时间，请耐心等待...`, 'info');
            } else {
                this.showToast('正在批量添加域名...', 'info');
            }
            
            try {
                const res = await API.batchAddDomains(this.batchDomainRecords, this.currentUser?.username, fetchExpiry);
                const data = await res.json();
                
                if (data.success) {
                    // 先显示重复警告
                    if (data.warning) {
                        this.showToast(data.warning, 'warning');
                    } else if (data.failed_count > 0) {
                        this.showToast(`成功 ${data.success_count} 个，失败 ${data.failed_count} 个`, 'warning');
                        console.warn('添加失败的域名:', data.failed_domains);
                    } else {
                        this.showToast(`成功添加 ${data.success_count} 个域名`, 'success');
                    }
                    this.showBatchDomainModal = false;
                    await this.loadDomains();
                } else {
                    this.showToast(data.message || '批量添加失败', 'error');
                }
            } catch (e) {
                this.showToast('批量添加失败: ' + e.message, 'error');
            } finally {
                this.batchDomainLoading = false;
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
            try {
                const res = await API.deleteMetric(metric.id);
                if (res.ok) {
                    this.showToast('删除成功', 'success');
                    await this.loadMetrics();
                } else {
                    this.showToast('删除失败', 'error');
                }
            } catch (e) {
                this.showToast('删除失败', 'error');
            }
            this.activePopconfirm = null;
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
        confirmBatchDeleteMetrics() {
            if (this.selectedMetricIds.length === 0) {
                this.showToast('请先选择要删除的指标', 'error');
                return;
            }
            this.deleteType = 'batchMetric';
            this.deleteMessage = `确定删除选中的 ${this.selectedMetricIds.length} 个指标吗？`;
            this.showDeleteModal = true;
        },

        // ========== 工具方法 ==========
        getStatusText(s) {
            return { active: '启用', inactive: '停用', pending: '待定' }[s] || s;
        },

        getActionText(a) {
            return { create: 'CREATE', update: 'UPDATE', delete: 'DELETE', login: 'LOGIN' }[a] || a.toUpperCase();
        },

        getMethodClass(action) {
            const map = { create: 'post', update: 'put', delete: 'delete', login: 'post' };
            return map[action] || 'get';
        },

        getMethodName(action) {
            const map = { create: 'POST', update: 'PUT', delete: 'DELETE', login: 'POST' };
            return map[action] || 'GET';
        },

        getStatusClass(code) {
            if (!code || code === 200) return 'success';
            if (code >= 400 && code < 500) return 'warning';
            if (code >= 500) return 'error';
            return 'success';
        },

        async deleteAuditLog(log) {
            this.showToast('审计日志暂不支持删除', 'warning');
            this.activePopconfirm = null;
        },

        formatDateTime(dateStr) {
            if (!dateStr) return '';
            const d = new Date(dateStr);
            const pad = n => n.toString().padStart(2, '0');
            return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
        },

        // 根据用户名生成头像背景颜色
        getAvatarColor(username) {
            if (!username) return '#6366f1';
            const colors = [
                '#6366f1', '#8b5cf6', '#a855f7', '#d946ef', 
                '#ec4899', '#f43f5e', '#ef4444', '#f97316',
                '#f59e0b', '#eab308', '#84cc16', '#22c55e',
                '#10b981', '#14b8a6', '#06b6d4', '#0ea5e9',
                '#3b82f6', '#6366f1'
            ];
            let hash = 0;
            for (let i = 0; i < username.length; i++) {
                hash = username.charCodeAt(i) + ((hash << 5) - hash);
            }
            return colors[Math.abs(hash) % colors.length];
        },
        
        // Logo 配置相关
        getLogoIconEmoji(icon) {
            const icons = {
                'cloud': '☁️',
                'server': '🖥️',
                'gear': '⚙️',
                'rocket': '🚀',
                'shield': '🛡️',
                'cube': '📦'
            };
            return icons[icon] || '☁️';
        },
        
        async loadLogoSettings() {
            try {
                const res = await fetch(API.baseURL + '/logo-config');
                if (res.ok) {
                    const data = await res.json();
                    this.logoSettings = { ...this.logoSettings, ...data };
                    // 加载会话超时设置
                    if (data.session_timeout) {
                        this.globalSessionTimeout = data.session_timeout;
                        // 同步到 localStorage，确保会话检测使用最新值
                        const currentTimeout = localStorage.getItem('sessionTimeout');
                        if (currentTimeout !== data.session_timeout) {
                            localStorage.setItem('sessionTimeout', data.session_timeout);
                            // 重新启动会话检测
                            if (this.currentUser) {
                                API.stopSessionCheck();
                                API.startSessionCheck(parseInt(data.session_timeout), localStorage.getItem('sessionExpiresAt'));
                            }
                        }
                    }
                    // 更新页面标题
                    if (data.site_title) {
                        document.title = data.site_title;
                    }
                    // 更新 Logo 显示
                    this.updateLogoDisplay();
                }
            } catch (e) {
                console.error('加载 Logo 配置失败:', e);
            }
        },
        
        updateLogoDisplay() {
            const logoEl = document.querySelector('.sidebar-logo');
            if (!logoEl) return;
            
            const { logo_type, logo_text, logo_icon, logo_image, logo_layout, logo_color1, logo_color2, logo_color3 } = this.logoSettings;
            
            let logoContent = '';
            
            // 图片模式
            if (logo_type === 'image' && logo_image) {
                const layout = logo_layout || 'horizontal';
                if (layout === 'image_only') {
                    logoContent = `<img src="${logo_image}" class="sidebar-logo-image" alt="Logo" style="max-height: 32px; max-width: 180px;">`;
                } else if (layout === 'vertical') {
                    logoContent = `
                        <div class="sidebar-logo-vertical">
                            <img src="${logo_image}" alt="Logo" style="max-height: 24px; max-width: 180px; margin-bottom: 4px;">
                            <span style="font-size: 12px; font-weight: 600; background: linear-gradient(135deg, ${logo_color1}, ${logo_color2}, ${logo_color3}); -webkit-background-clip: text; -webkit-text-fill-color: transparent;">${logo_text || '运维平台'}</span>
                        </div>
                    `;
                } else {
                    logoContent = `
                        <div class="sidebar-logo-horizontal" style="display: flex; align-items: center; gap: 8px;">
                            <img src="${logo_image}" alt="Logo" style="max-height: 28px; max-width: 120px;">
                            <span style="font-size: 14px; font-weight: 700; background: linear-gradient(135deg, ${logo_color1}, ${logo_color2}, ${logo_color3}); -webkit-background-clip: text; -webkit-text-fill-color: transparent;">${logo_text || '运维平台'}</span>
                        </div>
                    `;
                }
            }
            // 图标模式
            else if (logo_type === 'icon') {
                const iconPaths = {
                    'cloud': '<path d="M8 20c-2.2 0-4-1.8-4-4 0-1.9 1.3-3.4 3-3.9C7.2 9.8 9.4 8 12 8c2.2 0 4.1 1.2 5.2 3 .3 0 .5-.1.8-.1 2.2 0 4 1.8 4 4s-1.8 4-4 4H8z" fill="url(#logoGrad)" opacity="0.9"/>',
                    'server': '<rect x="4" y="6" width="16" height="5" rx="1" fill="url(#logoGrad)"/><rect x="4" y="13" width="16" height="5" rx="1" fill="url(#logoGrad)" opacity="0.7"/>',
                    'gear': '<circle cx="12" cy="12" r="3" fill="none" stroke="url(#logoGrad)" stroke-width="2"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3M4.2 4.2l2.1 2.1M17.7 17.7l2.1 2.1M4.2 19.8l2.1-2.1M17.7 6.3l2.1-2.1" stroke="url(#logoGrad)" stroke-width="2" stroke-linecap="round"/>',
                    'rocket': '<path d="M12 2c-1 3-2 5-2 8 0 2 1 4 2 5 1-1 2-3 2-5 0-3-1-5-2-8zm-3 13l-2 4 4-2-2-2zm6 0l2 4-4-2 2-2z" fill="url(#logoGrad)"/>',
                    'shield': '<path d="M12 2L4 6v6c0 5.5 3.4 10.7 8 12 4.6-1.3 8-6.5 8-12V6l-8-4z" fill="url(#logoGrad)"/>',
                    'cube': '<path d="M12 2L2 7v10l10 5 10-5V7L12 2z" fill="url(#logoGrad)"/><path d="M12 22V12M2 7l10 5M22 7l-10 5" stroke="white" stroke-width="1" opacity="0.5"/>'
                };
                logoContent = `
                    <svg class="logo-icon" viewBox="0 0 140 32" fill="none" xmlns="http://www.w3.org/2000/svg" style="width: auto; height: 28px;">
                        <defs>
                            <linearGradient id="logoGrad" x1="0%" y1="0%" x2="100%" y2="100%">
                                <stop offset="0%" style="stop-color:${logo_color1}"/>
                                <stop offset="50%" style="stop-color:${logo_color2}"/>
                                <stop offset="100%" style="stop-color:${logo_color3}"/>
                            </linearGradient>
                        </defs>
                        <g transform="translate(4, 4) scale(1)">${iconPaths[logo_icon] || iconPaths['cloud']}</g>
                        <text x="32" y="22" font-family="system-ui, -apple-system, sans-serif" font-size="14" font-weight="700" fill="url(#logoGrad)">${logo_text || 'AI-CloudOps'}</text>
                    </svg>
                `;
            }
            // 纯文字模式
            else {
                logoContent = `
                    <svg class="logo-icon" viewBox="0 0 120 32" fill="none" xmlns="http://www.w3.org/2000/svg" style="width: auto; height: 28px;">
                        <defs>
                            <linearGradient id="logoGrad" x1="0%" y1="0%" x2="100%" y2="0%">
                                <stop offset="0%" style="stop-color:${logo_color1}"/>
                                <stop offset="50%" style="stop-color:${logo_color2}"/>
                                <stop offset="100%" style="stop-color:${logo_color3}"/>
                            </linearGradient>
                        </defs>
                        <text x="4" y="22" font-family="system-ui, -apple-system, sans-serif" font-size="16" font-weight="700" fill="url(#logoGrad)">${logo_text || 'AI-CloudOps'}</text>
                    </svg>
                `;
            }
            
            logoEl.innerHTML = logoContent + '<span class="logo-text" style="display: none;"></span>';
        },
        
        handleLogoImageUpload(event) {
            const file = event.target.files[0];
            if (!file) return;
            
            // 检查文件大小（最大 500KB）
            if (file.size > 500 * 1024) {
                this.showToast('图片文件过大，请选择 500KB 以内的图片', 'error');
                return;
            }
            
            // 检查文件类型
            if (!file.type.startsWith('image/')) {
                this.showToast('请选择图片文件', 'error');
                return;
            }
            
            const reader = new FileReader();
            reader.onload = (e) => {
                this.logoSettings.logo_image = e.target.result;
            };
            reader.readAsDataURL(file);
        },
        
        async saveLogoSettings() {
            this.savingSettings = true;
            try {
                const res = await API.request('POST', '/settings', this.logoSettings);
                if (res.ok) {
                    this.showToast('Logo 设置已保存', 'success');
                    this.showLogoModal = false;
                    // 更新显示
                    if (this.logoSettings.site_title) {
                        document.title = this.logoSettings.site_title;
                    }
                    this.updateLogoDisplay();
                } else {
                    this.showToast('保存失败', 'error');
                }
            } catch (e) {
                this.showToast('保存失败: ' + e.message, 'error');
            }
            this.savingSettings = false;
        },
        
        async saveSessionSettings() {
            this.savingSettings = true;
            try {
                const res = await API.request('POST', '/settings', {
                    session_timeout: this.globalSessionTimeout
                });
                if (res.ok) {
                    // 同时更新本地的会话超时设置
                    localStorage.setItem('sessionTimeout', this.globalSessionTimeout);
                    // 重新启动会话检测
                    API.stopSessionCheck();
                    API.startSessionCheck(parseInt(this.globalSessionTimeout), localStorage.getItem('sessionExpiresAt'));
                    this.showToast('会话设置已保存并立即生效', 'success');
                    this.showSessionSettingsModal = false;
                } else {
                    this.showToast('保存失败', 'error');
                }
            } catch (e) {
                this.showToast('保存失败: ' + e.message, 'error');
            }
            this.savingSettings = false;
        },
        
        async openSecuritySettings() {
            this.showSecuritySettingsModal = true;
            // 加载安全设置
            try {
                const res = await API.request('GET', '/security-settings');
                if (res.ok) {
                    const data = await res.json();
                    this.securitySettings = {
                        login_limit_enabled: data.login_limit_enabled || false,
                        max_login_attempts: String(data.max_login_attempts || '5'),
                        lockout_duration: String(data.lockout_duration || '30'),
                        ip_whitelist_enabled: data.ip_whitelist_enabled || false,
                        ip_whitelist: data.ip_whitelist || ''
                    };
                }
            } catch (e) {
                console.error('加载安全设置失败:', e);
            }
            // 获取当前用户 IP
            this.getCurrentIP();
        },
        
        async getCurrentIP() {
            try {
                const res = await fetch('https://api.ipify.org?format=json');
                if (res.ok) {
                    const data = await res.json();
                    this.currentUserIP = data.ip;
                }
            } catch (e) {
                this.currentUserIP = '无法获取';
            }
        },
        
        async saveSecuritySettings() {
            this.savingSettings = true;
            try {
                const res = await API.request('POST', '/security-settings', {
                    login_limit_enabled: this.securitySettings.login_limit_enabled,
                    max_login_attempts: parseInt(this.securitySettings.max_login_attempts) || 5,
                    lockout_duration: parseInt(this.securitySettings.lockout_duration) || 30,
                    ip_whitelist_enabled: this.securitySettings.ip_whitelist_enabled,
                    ip_whitelist: this.securitySettings.ip_whitelist
                });
                if (res.ok) {
                    this.showToast('安全设置已保存', 'success');
                    this.showSecuritySettingsModal = false;
                } else {
                    const data = await res.json();
                    this.showToast(data.error || '保存失败', 'error');
                }
            } catch (e) {
                this.showToast('保存失败: ' + e.message, 'error');
            }
            this.savingSettings = false;
        },

        getRandomTime() {
            return Math.floor(Math.random() * 30000 + 1000);
        },

        generateTraceId(id) {
            // 基于 log.id 生成一个模拟的 trace id
            const hex = id.toString(16).padStart(8, '0');
            const timestamp = Date.now().toString(16).slice(-8);
            return `${hex}${timestamp}`;
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

        // 自定义确认弹窗
        showConfirm(options) {
            return new Promise((resolve) => {
                this.confirmDialog = {
                    show: true,
                    type: options.type || 'warning',
                    title: options.title || '确认操作',
                    message: options.message || '确定要执行此操作吗？',
                    okText: options.okText || '确定',
                    cancelText: options.cancelText || '取消',
                    resolve: resolve
                };
            });
        },

        handleConfirmOk() {
            if (this.confirmDialog.resolve) {
                this.confirmDialog.resolve(true);
            }
            this.confirmDialog.show = false;
        },

        handleConfirmCancel() {
            if (this.confirmDialog.resolve) {
                this.confirmDialog.resolve(false);
            }
            this.confirmDialog.show = false;
        },

        // ========== 记录操作 ==========
        openRecordModal(mode, record = null) {
            this.recordModalMode = mode;
            this.recordForm = record ? { ...record } : { id: '', connection_id: '', project: '', env: 'uat', module: '', vid: '', src_ip: '', src_port: '', dest_ip: '', dest_port: '', status: 'active' };
            this.showRecordModal = true;
        },

        async saveRecord() {
            try {
                const url = this.recordModalMode === 'edit' ? `/records/${this.recordForm.id}` : '/records';
                const method = this.recordModalMode === 'edit' ? 'PUT' : 'POST';
                const res = await API.request(method, url, { record: this.recordForm, operator: this.currentUser.username });
                if (res.ok) {
                    const data = await res.json();
                    if (data.warning) {
                        this.showToast(data.warning, 'warning');
                    } else {
                        this.showToast(this.recordModalMode === 'edit' ? '更新成功' : '添加成功', 'success');
                    }
                    this.showRecordModal = false;
                    this.loadRecords();
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('保存失败', 'error');
            }
        },

        // Popconfirm 切换
        togglePopconfirm(type, id) {
            const key = `${type}-${id}`;
            if (this.activePopconfirm === key) {
                this.activePopconfirm = null;
            } else {
                this.activePopconfirm = key;
            }
        },

        async deleteRecord(record) {
            try {
                const res = await API.deleteRecord(record.id, this.currentUser.username);
                if (res.ok) {
                    this.showToast('删除成功', 'success');
                    this.loadRecords();
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('删除失败', 'error');
            }
            this.activePopconfirm = null;
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
                this.confirmRollback(this.previewHistory);
            }
        },

        // 显示回滚确认弹窗
        confirmRollback(history) {
            this.rollbackTarget = history;
            this.showRollbackModal = true;
        },

        // 执行回滚
        async executeRollback() {
            if (!this.rollbackTarget) return;
            this.showRollbackModal = false;
            try {
                const operator = this.currentUser?.username || 'admin';
                const res = await API.rollbackRecord(this.historyRecord.id, this.rollbackTarget.id, operator);
                if (res.ok) {
                    this.showToast('回滚成功', 'success');
                    this.showHistoryModal = false;
                    this.rollbackTarget = null;
                    await this.loadRecords();
                } else {
                    this.showToast('回滚失败', 'error');
                }
            } catch (e) {
                this.showToast('回滚失败', 'error');
            }
        },

        // 保留旧方法兼容
        async rollbackToVersion(history) {
            this.confirmRollback(history);
        },

        confirmBatchDelete() {
            this.deleteTarget = null;
            this.deleteType = 'batch';
            this.deleteMessage = `确定删除选中的 ${this.selectedRecords.length} 条记录吗？`;
            this.showDeleteModal = true;
        },

        openBatchEditModal() {
            if (this.selectedRecords.length === 0) {
                this.showToast('请先选择要编辑的记录', 'error');
                return;
            }
            // 按页面展示顺序获取选中记录（使用filteredRecords保持排序）
            const selectedData = this.filteredRecords.filter(r => this.selectedRecords.includes(r.id));
            this.batchEditRecordIds = selectedData.map(r => r.id);
            // 格式: 项目,环境,模块,VID,源地址,目标地址,连接ID,状态（与表格一致，逗号分隔）
            const statusMap = { active: '启用', inactive: '停用', pending: '待定' };
            this.batchEditText = selectedData.map(r => 
                `${r.project},${r.env},${r.module || ''},${r.vid},${r.src_ip}:${r.src_port},${r.dest_ip}:${r.dest_port},${r.connection_id},${statusMap[r.status] || r.status}`
            ).join('\n');
            this.showBatchEditModal = true;
        },

        async submitBatchEdit() {
            const lines = this.batchEditText.trim().split('\n').filter(l => l.trim());
            if (lines.length !== this.batchEditRecordIds.length) {
                this.showToast(`行数不匹配：原${this.batchEditRecordIds.length}条，现${lines.length}条`, 'error');
                return;
            }

            const updates = [];
            for (let i = 0; i < lines.length; i++) {
                const parts = lines[i].split(',').map(p => p.trim());
                if (parts.length < 8) {
                    this.showToast(`第${i+1}行格式错误，需要8个字段`, 'error');
                    return;
                }
                // 格式: 项目,环境,模块,VID,源地址,目标地址,连接ID,状态
                const [project, env, module, vid, srcAddr, destAddr, connId, statusText] = parts;
                const [srcIp, srcPort] = srcAddr.split(':');
                const [destIp, destPort] = destAddr.split(':');
                // 中文状态转英文
                const statusReverseMap = { '启用': 'active', '停用': 'inactive', '待定': 'pending' };
                const status = statusReverseMap[statusText] || statusText;
                updates.push({
                    id: this.batchEditRecordIds[i],
                    connection_id: connId,
                    project, env, module, vid,
                    src_ip: srcIp, src_port: srcPort || '',
                    dest_ip: destIp, dest_port: destPort || '',
                    status
                });
            }

            try {
                const res = await API.request('POST', '/records/batch-update', {
                    records: updates,
                    operator: this.currentUser.username
                });
                const data = await res.json();
                if (res.ok) {
                    this.showToast(data.message || '批量修改成功', 'success');
                    this.showBatchEditModal = false;
                    this.selectedRecords = [];
                    this.loadRecords();
                } else {
                    this.showToast(data.message || '批量编辑失败', 'error');
                }
            } catch (e) {
                this.showToast('批量编辑失败', 'error');
            }
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
            this.batchWarning = '';
            this.showBatchModal = true;
        },

        parseBatchText() {
            this.batchError = '';
            this.batchRecords = [];
            this.batchWarning = '';
            if (!this.batchText.trim()) return;
            const lines = this.batchText.trim().split('\n');
            const seenConnIds = new Map(); // 记录连接ID和详细信息
            const duplicateErrors = [];
            
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
                    this.batchError = `第 ${i + 1} 行: 需要6个字段（项目、模块名、VID、源地址、目标地址、连接ID）`;
                    return;
                }
                const connId = parts[5]; // 连接ID在最后
                const project = parts[0];
                const vid = parts[2];
                
                // 检测重复的连接ID，直接报错阻止添加
                if (seenConnIds.has(connId)) {
                    const firstInfo = seenConnIds.get(connId);
                    duplicateErrors.push(`第 ${i + 1} 行与第 ${firstInfo.line} 行连接ID重复: "${connId}"（项目: ${project}, VID: ${vid}）`);
                } else {
                    seenConnIds.set(connId, { line: i + 1, project, vid });
                }
                
                // 解析源地址和目标地址（格式：IP:端口）
                const srcAddr = parts[3].split(':');
                const destAddr = parts[4].split(':');
                if (srcAddr.length !== 2 || destAddr.length !== 2) {
                    this.batchError = `第 ${i + 1} 行: 源地址和目标地址格式应为 IP:端口`;
                    return;
                }
                this.batchRecords.push({
                    project: parts[0],
                    module: parts[1],
                    vid: parts[2].replace(/;/g, '\n'), // 分号转换为换行
                    src_ip: srcAddr[0],
                    src_port: srcAddr[1],
                    dest_ip: destAddr[0],
                    dest_port: destAddr[1],
                    connection_id: parts[5], // 连接ID在最后
                    env: this.batchEnv,
                    status: this.batchStatus
                });
            }
            
            // 如果有重复的连接ID，直接报错，不允许添加
            if (duplicateErrors.length > 0) {
                this.batchError = '存在重复的连接ID，禁止添加：\n' + duplicateErrors.join('\n');
                this.batchRecords = []; // 清空记录，阻止添加
            }
        },

        async submitBatch() {
            this.batchLoading = true;
            this.showToast('正在批量添加...', 'info');
            try {
                const res = await API.batchAddRecords(this.batchRecords, this.currentUser.username);
                if (res.ok) {
                    const r = await res.json();
                    if (r.warning || (r.duplicates && r.duplicates.length > 0)) {
                        this.showToast(r.message, 'warning');
                        if (r.duplicates && r.duplicates.length > 0) {
                            console.warn('重复的连接ID:', r.duplicates);
                        }
                    } else {
                        this.showToast(r.message, 'success');
                    }
                    this.showBatchModal = false;
                    this.loadRecords();
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('批量添加失败', 'error');
            } finally {
                this.batchLoading = false;
            }
        },

        // ========== 批量检测 ==========
        openBatchCheckModal() {
            this.batchCheckText = '';
            this.batchCheckRecords = [];
            this.batchCheckError = '';
            this.batchCheckResult = null;
            this.showBatchCheckModal = true;
        },

        parseBatchCheckText() {
            this.batchCheckError = '';
            this.batchCheckRecords = [];
            this.batchCheckResult = null;
            if (!this.batchCheckText.trim()) return;
            
            const lines = this.batchCheckText.trim().split('\n');
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
                    this.batchCheckError = `第 ${i + 1} 行: 需要6个字段（项目、模块名、VID、源地址、目标地址、连接ID）`;
                    return;
                }
                const srcAddr = parts[3].split(':');
                const destAddr = parts[4].split(':');
                if (srcAddr.length !== 2 || destAddr.length !== 2) {
                    this.batchCheckError = `第 ${i + 1} 行: 源地址和目标地址格式应为 IP:端口`;
                    return;
                }
                this.batchCheckRecords.push({
                    project: parts[0],
                    module: parts[1],
                    vid: parts[2].replace(/;/g, '\n'),
                    src_ip: srcAddr[0],
                    src_port: srcAddr[1],
                    dest_ip: destAddr[0],
                    dest_port: destAddr[1],
                    connection_id: parts[5]
                });
            }
        },

        async doBatchCheck() {
            this.batchCheckLoading = true;
            this.batchCheckResult = null;
            try {
                const res = await API.batchCheckRecords(this.batchCheckRecords);
                if (res.ok) {
                    this.batchCheckResult = await res.json();
                    this.showToast(`检测完成：${this.batchCheckResult.new_count} 条可添加，${this.batchCheckResult.exists_count} 条已存在`, 
                        this.batchCheckResult.exists_count > 0 ? 'warning' : 'success');
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('检测失败', 'error');
            } finally {
                this.batchCheckLoading = false;
            }
        },

        copyCheckResult(type) {
            if (!this.batchCheckResult) return;
            const records = type === 'exists' ? this.batchCheckResult.exists : this.batchCheckResult.new;
            if (!records || records.length === 0) {
                this.showToast('没有可复制的数据', 'warning');
                return;
            }
            // 转换为原始输入格式：项目,模块名,VID,源地址,目标地址,连接ID
            const lines = records.map(r => {
                // VID 换行符转换回分号
                const vid = (r.vid || '').replace(/\n/g, ';');
                return `${r.project},${r.module},${vid},${r.src_addr},${r.dest_addr},${r.connection_id}`;
            });
            const text = lines.join('\n');
            navigator.clipboard.writeText(text).then(() => {
                this.showToast(`已复制 ${records.length} 条记录`, 'success');
            }).catch(() => {
                // 降级方案
                const textarea = document.createElement('textarea');
                textarea.value = text;
                document.body.appendChild(textarea);
                textarea.select();
                document.execCommand('copy');
                document.body.removeChild(textarea);
                this.showToast(`已复制 ${records.length} 条记录`, 'success');
            });
        },

        // ========== 用户管理 ==========
        async openUserModal(mode, user = null) {
            // 确保角色列表已加载
            if (this.roles.length === 0) {
                await this.loadRoles();
            }
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
                    mfa_bound: user.mfa_bound || false,
                    phone: user.phone || '',
                    email: user.email || '',
                    description: user.description || '',
                    session_timeout: user.session_timeout || this.globalSessionTimeout,
                    language: user.language || 'zh-CN'
                };
            } else {
                this.userForm = { id: '', username: '', password: '', display_name: '', role: 'user', status: 'active', permissions: ['records', 'audit'], mfa_enabled: false, mfa_bound: false, phone: '', email: '', description: '', session_timeout: this.globalSessionTimeout, language: 'zh-CN' };
            }
            this.showUserModal = true;
            this.showUserPassword = false;
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

        async deleteUser(user) {
            try {
                const res = await API.deleteUser(user.id);
                if (res.ok) {
                    this.showToast('删除成功', 'success');
                    this.loadUsers();
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('删除失败', 'error');
            }
            this.activePopconfirm = null;
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

        async deleteDataSource(ds) {
            try {
                const res = await API.deleteDataSource(ds.id);
                if (res.ok) {
                    this.showToast('删除成功', 'success');
                    this.loadDataSources();
                } else {
                    this.showToast(await res.text(), 'error');
                }
            } catch (e) {
                this.showToast('删除失败', 'error');
            }
            this.activePopconfirm = null;
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

        // ========== 排班管理 ==========
        async loadSchedule() {
            await this.loadShiftConfig();
            this.generateScheduleDays();
            try {
                const res = await API.getSchedule(this.scheduleYear, this.scheduleMonth);
                if (res.ok) {
                    const data = await res.json();
                    this.scheduleData = data || [];
                }
            } catch (e) {
                console.error('加载排班失败:', e);
            }
            this.calculateScheduleStats();
        },
        
        // 加载班次配置（优先从后端API，后备 localStorage）
        async loadShiftConfig() {
            try {
                // 优先从后端 API 加载
                const res = await API.getShiftConfig();
                if (res.ok) {
                    const data = await res.json();
                    if (Array.isArray(data) && data.length > 0) {
                        this.shiftTypes = data;
                        // 同步到 localStorage
                        localStorage.setItem('shiftConfig', JSON.stringify(data));
                        return;
                    }
                }
            } catch (e) {
                console.warn('从后端加载班次配置失败，尝试 localStorage:', e);
            }
            
            // 后备：从 localStorage 加载
            try {
                const saved = localStorage.getItem('shiftConfig');
                if (saved) {
                    const parsed = JSON.parse(saved);
                    if (Array.isArray(parsed) && parsed.length > 0) {
                        this.shiftTypes = parsed;
                        return;
                    }
                }
            } catch (e) {
                console.error('加载班次配置失败:', e);
            }
            
            // 使用默认配置
            this.shiftTypes = JSON.parse(JSON.stringify(this.defaultShiftTypes));
        },
        
        // 保存班次配置（保存到后端API和localStorage）
        async saveShiftConfig() {
            try {
                // 保存到后端 API
                const res = await API.saveShiftConfig(this.shiftTypes);
                if (res.ok) {
                    // 同步到 localStorage
                    localStorage.setItem('shiftConfig', JSON.stringify(this.shiftTypes));
                    this.showToast('班次配置已保存', 'success');
                } else {
                    throw new Error('保存失败');
                }
            } catch (e) {
                console.error('保存班次配置失败:', e);
                // 至少保存到 localStorage
                localStorage.setItem('shiftConfig', JSON.stringify(this.shiftTypes));
                this.showToast('保存到服务器失败，已保存到本地', 'warning');
            }
        },
        
        // 打开班次配置弹窗
        openShiftConfigModal() {
            this.getNextColor();
            this.newShiftForm = { code: '', name: '', time: '', color: this.presetColors[this.colorIndex], isDuty: false };
            this.editingShift = null;
            this.showShiftConfigModal = true;
        },
        
        // 获取下一个预设颜色
        getNextColor() {
            this.colorIndex = (this.colorIndex + 1) % this.presetColors.length;
            return this.presetColors[this.colorIndex];
        },
        
        // 表格内直接编辑颜色
        updateShiftColor(shift, color) {
            shift.color = color;
        },
        
        // 表格内直接编辑名称
        updateShiftName(shift, name) {
            shift.name = name;
        },
        
        // 表格内直接编辑时间
        updateShiftTime(shift, time) {
            shift.time = time;
        },
        
        // 表格内直接编辑值班状态
        updateShiftDuty(shift, isDuty) {
            shift.isDuty = isDuty;
            shift.label = isDuty && !shift.label.includes('★') ? shift.code : shift.code;
        },
        
        // 保存班次（添加）
        saveShiftType() {
            if (!this.newShiftForm.code.trim() || !this.newShiftForm.name.trim()) {
                this.showToast('请填写代码和名称', 'warning');
                return;
            }
            
            const code = this.newShiftForm.code.trim().toUpperCase();
            
            // 检查代码是否重复
            if (this.shiftTypes.some(s => s.code === code)) {
                this.showToast('班次代码已存在', 'warning');
                return;
            }
            // 添加新班次
            this.shiftTypes.push({
                code: code,
                label: code,
                name: this.newShiftForm.name.trim(),
                time: this.newShiftForm.time.trim(),
                color: this.newShiftForm.color,
                isDuty: this.newShiftForm.isDuty
            });
            this.showToast('班次已添加', 'success');
            
            // 重置表单并切换到下一个颜色
            this.newShiftForm = { code: '', name: '', time: '', color: this.getNextColor(), isDuty: false };
        },
        
        // 批量添加班次
        parseBatchShifts() {
            if (!this.batchShiftText.trim()) {
                this.showToast('请输入班次信息', 'warning');
                return;
            }
            
            const lines = this.batchShiftText.trim().split('\n');
            let addedCount = 0;
            
            lines.forEach((line, index) => {
                const parts = line.split(/[,，]/);
                const code = parts[0]?.trim().toUpperCase();
                const name = parts[1]?.trim();
                const time = parts[2]?.trim() || '';
                
                if (!code || !name) return;
                if (this.shiftTypes.some(s => s.code === code)) return;
                
                const color = this.presetColors[(this.shiftTypes.length + addedCount) % this.presetColors.length];
                
                this.shiftTypes.push({
                    code,
                    label: code,
                    name,
                    time,
                    color,
                    isDuty: false
                });
                addedCount++;
            });
            
            this.batchShiftText = '';
            this.showBatchShiftModal = false;
            if (addedCount > 0) {
                this.showToast(`成功添加 ${addedCount} 个班次`, 'success');
            } else {
                this.showToast('没有新班次被添加（可能代码重复）', 'warning');
            }
        },
        
        // 删除班次
        deleteShiftType(shift) {
            const index = this.shiftTypes.findIndex(s => s.code === shift.code);
            if (index > -1) {
                this.shiftTypes.splice(index, 1);
                this.showToast('班次已删除', 'success');
            }
        },
        
        // 拖拽开始
        onDragStart(index, event) {
            this.dragIndex = index;
            event.dataTransfer.effectAllowed = 'move';
        },
        
        // 拖拽经过
        onDragOver(index) {
            if (this.dragIndex !== index) {
                this.dragOverIndex = index;
            }
        },
        
        // 拖拽离开
        onDragLeave() {
            this.dragOverIndex = null;
        },
        
        // 放下
        onDrop(index) {
            if (this.dragIndex !== null && this.dragIndex !== index) {
                const item = this.shiftTypes.splice(this.dragIndex, 1)[0];
                this.shiftTypes.splice(index, 0, item);
            }
            this.dragIndex = null;
            this.dragOverIndex = null;
        },
        
        // 拖拽结束
        onDragEnd() {
            this.dragIndex = null;
            this.dragOverIndex = null;
        },
        
        // 切换选择单个班次
        toggleSelectShift(code, checked) {
            if (checked) {
                if (!this.selectedShifts.includes(code)) {
                    this.selectedShifts.push(code);
                }
            } else {
                this.selectedShifts = this.selectedShifts.filter(c => c !== code);
            }
        },
        
        // 全选/取消全选班次
        toggleSelectAllShifts(checked) {
            if (checked) {
                this.selectedShifts = this.shiftTypes.map(s => s.code);
            } else {
                this.selectedShifts = [];
            }
        },
        
        // 批量删除班次
        async batchDeleteShifts() {
            if (this.selectedShifts.length === 0) return;
            const confirmed = await this.showConfirm({
                type: 'danger',
                title: '删除班次',
                message: `确定要删除选中的 ${this.selectedShifts.length} 个班次吗？\n删除后需要点击"保存"才会生效。`,
                okText: '删除',
                cancelText: '取消'
            });
            if (confirmed) {
                this.shiftTypes = this.shiftTypes.filter(s => !this.selectedShifts.includes(s.code));
                this.selectedShifts = [];
                this.showToast('已删除选中班次，请点击保存', 'success');
            }
        },
        
        // 恢复默认班次配置（只更新界面，不保存）
        resetShiftConfig() {
            this.shiftTypes = JSON.parse(JSON.stringify(this.defaultShiftTypes));
            this.showToast('已恢复默认配置，请点击保存生效', 'info');
        },
        
        // 关闭班次配置弹窗（不保存）
        cancelShiftConfigModal() {
            // 重新加载配置，放弃未保存的修改
            this.loadShiftConfig();
            this.showShiftConfigModal = false;
        },
        
        // 保存并关闭班次配置弹窗
        async saveAndCloseShiftConfigModal() {
            await this.saveShiftConfig();
            this.showShiftConfigModal = false;
        },
        
        // 获取班次显示信息
        getShiftInfo(code) {
            return this.shiftTypes.find(s => s.code === code) || null;
        },
        
        generateScheduleDays() {
            const year = this.scheduleYear;
            const month = this.scheduleMonth;
            const daysInMonth = new Date(year, month, 0).getDate();
            const today = new Date();
            const weekdays = ['日', '一', '二', '三', '四', '五', '六'];
            
            this.scheduleDays = [];
            for (let day = 1; day <= daysInMonth; day++) {
                const date = new Date(year, month - 1, day);
                const dayOfWeek = date.getDay();
                this.scheduleDays.push({
                    day,
                    date: `${year}-${String(month).padStart(2, '0')}-${String(day).padStart(2, '0')}`,
                    weekday: weekdays[dayOfWeek],
                    isWeekend: dayOfWeek === 0 || dayOfWeek === 6,
                    isToday: date.toDateString() === today.toDateString()
                });
            }
        },
        
        
        calculateScheduleStats() {
            let total = 0, shiftA = 0, shiftB = 0, shiftC = 0;
            this.scheduleData.forEach(emp => {
                Object.values(emp.shifts).forEach(shift => {
                    if (shift && shift !== 'H') total++;
                    if (shift === 'A' || shift === 'A*') shiftA++;
                    if (shift === 'B') shiftB++;
                    if (shift === 'C') shiftC++;
                });
            });
            this.scheduleStats = { total, shiftA, shiftB, shiftC };
        },
        
        getShift(employee, day) {
            return employee.shifts?.[day.date] || '';
        },
        
        getShiftLabel(employee, day) {
            const code = this.getShift(employee, day);
            if (!code) return '';
            const shift = this.shiftTypes.find(s => s.code === code);
            if (shift) {
                return shift.isDuty ? shift.label + '★' : shift.label;
            }
            return code;
        },
        
        getShiftStyle(employee, day) {
            const code = this.getShift(employee, day);
            if (!code) return {};
            const shift = this.shiftTypes.find(s => s.code === code);
            if (shift) {
                return {
                    background: shift.color,
                    color: '#fff',
                    border: shift.isDuty ? '2px solid #faad14' : 'none'
                };
            }
            return { background: '#d9d9d9', color: '#fff' };
        },
        
        getShiftClass(employee, day) {
            // 保留用于兼容，但主要使用 getShiftStyle
            return '';
        },
        
        getDutyPerson(day) {
            for (const emp of this.scheduleData) {
                if (emp.shifts?.[day.date] === 'A*') {
                    return emp.name;
                }
            }
            return '';
        },
        
        prevMonth() {
            if (this.scheduleMonth === 1) {
                this.scheduleMonth = 12;
                this.scheduleYear--;
            } else {
                this.scheduleMonth--;
            }
            this.loadSchedule();
        },
        
        nextMonth() {
            if (this.scheduleMonth === 12) {
                this.scheduleMonth = 1;
                this.scheduleYear++;
            } else {
                this.scheduleMonth++;
            }
            this.loadSchedule();
        },
        
        // 直接选择年月
        setScheduleYearMonth(year, month) {
            this.scheduleYear = parseInt(year);
            this.scheduleMonth = parseInt(month);
            this.loadSchedule();
        },
        
        // 生成年份选项
        getYearOptions() {
            const currentYear = new Date().getFullYear();
            const years = [];
            for (let y = currentYear - 2; y <= currentYear + 2; y++) {
                years.push(y);
            }
            return years;
        },
        
        editShift(employee, day) {
            this.shiftEditEmployee = employee;
            this.shiftEditDay = day;
            this.shiftEditDate = `${day.date} (${day.weekday})`;
            this.shiftEditValue = this.getShift(employee, day);
            this.showShiftModal = true;
        },
        
        async saveShift() {
            if (this.shiftEditEmployee && this.shiftEditDay) {
                try {
                    const res = await API.updateShift(
                        this.shiftEditEmployee.id,
                        this.shiftEditDay.date,
                        this.shiftEditValue
                    );
                    if (res.ok) {
                        this.shiftEditEmployee.shifts[this.shiftEditDay.date] = this.shiftEditValue;
                        this.calculateScheduleStats();
                        this.showToast('排班已更新', 'success');
                    } else {
                        this.showToast('保存失败', 'error');
                    }
                } catch (e) {
                    this.showToast('保存失败', 'error');
                }
            }
            this.showShiftModal = false;
        },
        
        openEmployeeModal() {
            this.newEmployee = { name: '', role: '', color: '#667eea' };
            this.pendingEmployees = [];
            this.selectedEmployeeIds = [];
            this.showEmployeeModal = true;
        },
        
        // 添加到待添加列表（不立即保存）
        addToPending() {
            if (!this.newEmployee.name.trim()) {
                this.showToast('请输入姓名', 'warning');
                return;
            }
            const avatarColor = `linear-gradient(135deg, ${this.newEmployee.color}, ${this.lightenColor(this.newEmployee.color)})`;
            this.pendingEmployees.push({
                tempId: Date.now(),
                name: this.newEmployee.name.trim(),
                role: this.newEmployee.role.trim() || '运维工程师',
                avatarColor: avatarColor,
                color: this.newEmployee.color
            });
            this.newEmployee = { name: '', role: '', color: '#667eea' };
            this.showToast('已加入待添加列表', 'success');
        },
        
        // 从待添加列表移除
        removeFromPending(tempId) {
            const index = this.pendingEmployees.findIndex(e => e.tempId === tempId);
            if (index > -1) {
                this.pendingEmployees.splice(index, 1);
            }
        },
        
        // 点击完成时保存所有待添加员工
        async saveAllEmployees() {
            if (this.pendingEmployees.length === 0) {
                this.showEmployeeModal = false;
                return;
            }
            
            let successCount = 0;
            for (const emp of this.pendingEmployees) {
                try {
                    const res = await API.addScheduleEmployee({
                        name: emp.name,
                        role: emp.role,
                        avatarColor: emp.avatarColor
                    });
                    if (res.ok) {
                        const savedEmp = await res.json();
                        savedEmp.shifts = {};
                        this.scheduleData.push(savedEmp);
                        successCount++;
                    }
                } catch (e) {
                    console.error('添加员工失败:', e);
                }
            }
            
            this.pendingEmployees = [];
            this.showEmployeeModal = false;
            this.calculateScheduleStats();
            if (successCount > 0) {
                this.showToast(`成功添加 ${successCount} 名员工`, 'success');
            }
        },
        
        // 批量添加员工
        openBatchAddEmployeeModal() {
            this.batchEmployeeText = '';
            this.showBatchAddEmployeeModal = true;
        },
        
        parseBatchEmployees() {
            if (!this.batchEmployeeText.trim()) {
                this.showToast('请输入员工信息', 'warning');
                return;
            }
            const lines = this.batchEmployeeText.trim().split('\n');
            const colors = ['#667eea', '#f093fb', '#4facfe', '#43e97b', '#fa709a', '#a8edea', '#ff6b6b', '#feca57'];
            
            lines.forEach((line, index) => {
                const parts = line.split(/[,，\t]/);
                const name = parts[0]?.trim();
                if (!name) return;
                const role = parts[1]?.trim() || '运维工程师';
                const color = colors[(this.pendingEmployees.length + index) % colors.length];
                const avatarColor = `linear-gradient(135deg, ${color}, ${this.lightenColor(color)})`;
                
                this.pendingEmployees.push({
                    tempId: Date.now() + index,
                    name,
                    role,
                    avatarColor,
                    color
                });
            });
            
            this.showBatchAddEmployeeModal = false;
            this.showToast(`已添加 ${lines.length} 名员工到待添加列表`, 'success');
        },
        
        // 切换员工选中状态
        toggleEmployeeSelect(id) {
            const index = this.selectedEmployeeIds.indexOf(id);
            if (index > -1) {
                this.selectedEmployeeIds.splice(index, 1);
            } else {
                this.selectedEmployeeIds.push(id);
            }
        },
        
        // 全选/取消全选员工
        toggleSelectAllEmployees() {
            if (this.selectedEmployeeIds.length === this.scheduleData.length) {
                this.selectedEmployeeIds = [];
            } else {
                this.selectedEmployeeIds = this.scheduleData.map(e => e.id);
            }
        },
        
        // 批量删除选中员工
        async batchDeleteEmployees() {
            if (this.selectedEmployeeIds.length === 0) {
                this.showToast('请先选择员工', 'warning');
                return;
            }
            
            let successCount = 0;
            for (const id of this.selectedEmployeeIds) {
                try {
                    const res = await API.deleteScheduleEmployee(id);
                    if (res.ok) {
                        const index = this.scheduleData.findIndex(e => e.id === id);
                        if (index > -1) {
                            this.scheduleData.splice(index, 1);
                        }
                        successCount++;
                    }
                } catch (e) {
                    console.error('删除员工失败:', e);
                }
            }
            
            this.selectedEmployeeIds = [];
            this.calculateScheduleStats();
            this.showToast(`成功删除 ${successCount} 名员工`, 'success');
        },
        
        async removeEmployee(id) {
            try {
                const res = await API.deleteScheduleEmployee(id);
                if (res.ok) {
                    const index = this.scheduleData.findIndex(e => e.id === id);
                    if (index > -1) {
                        this.scheduleData.splice(index, 1);
                    }
                    this.calculateScheduleStats();
                    this.showToast('员工已删除', 'success');
                } else {
                    this.showToast('删除失败', 'error');
                }
            } catch (e) {
                this.showToast('删除失败', 'error');
            }
        },
        
        lightenColor(hex) {
            // 简单的颜色变亮函数
            let r = parseInt(hex.slice(1, 3), 16);
            let g = parseInt(hex.slice(3, 5), 16);
            let b = parseInt(hex.slice(5, 7), 16);
            r = Math.min(255, r + 50);
            g = Math.min(255, g + 50);
            b = Math.min(255, b + 50);
            return `#${r.toString(16).padStart(2, '0')}${g.toString(16).padStart(2, '0')}${b.toString(16).padStart(2, '0')}`;
        },
        
        exportSchedule() {
            if (this.scheduleData.length === 0) {
                this.showToast('暂无数据可导出', 'warning');
                return;
            }
            // 构建 CSV
            const days = this.scheduleDays.map(d => d.day);
            let csv = '姓名,职位,' + days.join(',') + '\n';
            
            this.scheduleData.forEach(emp => {
                const row = [emp.name, emp.role];
                this.scheduleDays.forEach(day => {
                    row.push(emp.shifts[day.date] || '');
                });
                csv += row.join(',') + '\n';
            });
            
            // 下载
            const blob = new Blob(['\ufeff' + csv], { type: 'text/csv;charset=utf-8;' });
            const link = document.createElement('a');
            link.href = URL.createObjectURL(blob);
            link.download = `排班表_${this.scheduleYear}年${this.scheduleMonth}月.csv`;
            link.click();
            this.showToast('导出成功', 'success');
        },
        
        importSchedule() {
            this.importData = '';
            // 默认设置为当前月份
            const year = this.scheduleYear;
            const month = String(this.scheduleMonth).padStart(2, '0');
            this.importStartDate = `${year}-${month}-01`;
            const lastDay = new Date(year, this.scheduleMonth, 0).getDate();
            this.importEndDate = `${year}-${month}-${String(lastDay).padStart(2, '0')}`;
            this.showImportModal = true;
        },
        
        async doImportSchedule() {
            if (!this.importData.trim()) {
                this.showToast('请输入数据', 'warning');
                return;
            }
            
            try {
                const lines = this.importData.trim().split('\n');
                if (lines.length < 2) {
                    this.showToast('数据格式错误', 'error');
                    return;
                }
                
                // 解析表头获取日期（格式：姓名,职位,1,2,3,... 或 姓名,职位,2026-01-01,...）
                const header = lines[0].split(',');
                const startCol = 2; // 姓名、职位之后是日期
                
                // 解析表头中的日期
                const headerDates = [];
                for (let j = startCol; j < header.length; j++) {
                    const dateStr = header[j].trim();
                    // 如果是数字（1,2,3,...），转换为当月日期
                    if (/^\d{1,2}$/.test(dateStr)) {
                        const day = parseInt(dateStr);
                        const date = `${this.scheduleYear}-${String(this.scheduleMonth).padStart(2, '0')}-${String(day).padStart(2, '0')}`;
                        headerDates.push(date);
                    } else if (/^\d{4}-\d{2}-\d{2}$/.test(dateStr)) {
                        // 已经是完整日期格式
                        headerDates.push(dateStr);
                    } else {
                        headerDates.push(null);
                    }
                }
                
                console.log('解析的表头日期:', headerDates);
                
                // 解析员工数据
                const importList = [];
                const colors = ['#667eea', '#f093fb', '#4facfe', '#43e97b', '#fa709a', '#a8edea', '#ff6b6b', '#feca57'];
                
                for (let i = 1; i < lines.length; i++) {
                    const cols = lines[i].split(',');
                    if (cols.length < 3) continue;
                    
                    const name = cols[0].trim();
                    if (!name) continue;
                    
                    const role = cols[1].trim() || '运维工程师';
                    const color = colors[(i - 1) % colors.length];
                    
                    const shifts = {};
                    for (let j = startCol; j < cols.length; j++) {
                        const date = headerDates[j - startCol];
                        const shift = cols[j].trim().toUpperCase();
                        if (date && shift) {
                            shifts[date] = shift;
                        }
                    }
                    
                    console.log(`员工 ${name} 解析到 ${Object.keys(shifts).length} 个排班`);
                    
                    importList.push({
                        name,
                        role,
                        avatarColor: `linear-gradient(135deg, ${color}, ${this.lightenColor(color)})`,
                        shifts
                    });
                }
                
                if (importList.length === 0) {
                    this.showToast('未解析到有效数据', 'error');
                    return;
                }
                
                // 逐个导入员工和排班到后端
                this.showToast('正在导入...', 'info');
                let successCount = 0;
                let failCount = 0;
                let shiftSaveCount = 0;
                
                console.log('scheduleDays:', this.scheduleDays);
                console.log('importList:', importList);
                
                for (const emp of importList) {
                    try {
                        console.log('处理员工:', emp.name, '排班数据:', emp.shifts);
                        
                        // 检查员工是否已存在
                        let existingEmp = this.scheduleData.find(e => e.name === emp.name);
                        let empId;
                        
                        if (existingEmp) {
                            // 已存在，使用现有 ID
                            empId = existingEmp.id;
                            console.log('员工已存在，ID:', empId);
                        } else {
                            // 不存在，添加新员工
                            const res = await API.addScheduleEmployee({
                                name: emp.name,
                                role: emp.role,
                                avatarColor: emp.avatarColor
                            });
                            if (res.ok) {
                                const data = await res.json();
                                empId = data.id;
                                console.log('新员工添加成功，ID:', empId);
                            } else {
                                console.error('添加员工失败:', await res.text());
                                failCount++;
                                continue;
                            }
                        }
                        
                        // 保存排班数据
                        const shiftEntries = Object.entries(emp.shifts);
                        console.log('排班条目数:', shiftEntries.length);
                        for (const [date, shiftType] of shiftEntries) {
                            console.log('保存排班:', empId, date, shiftType);
                            const shiftRes = await API.updateShift(empId, date, shiftType);
                            if (shiftRes.ok) {
                                shiftSaveCount++;
                            } else {
                                console.error('保存排班失败:', await shiftRes.text());
                            }
                        }
                        successCount++;
                    } catch (e) {
                        console.error('导入失败:', emp.name, e);
                        failCount++;
                    }
                }
                
                console.log('总共保存排班条目:', shiftSaveCount);
                
                // 重新加载数据
                await this.loadSchedule();
                this.showImportModal = false;
                
                if (failCount === 0) {
                    this.showToast(`成功导入 ${successCount} 名员工的排班`, 'success');
                } else {
                    this.showToast(`导入完成：成功 ${successCount}，失败 ${failCount}`, 'warning');
                }
            } catch (e) {
                console.error(e);
                this.showToast('解析失败，请检查数据格式', 'error');
            }
        },

        // ========== 批量删除确认 ==========
        async executeDelete() {
            try {
                if (this.deleteType === 'batch') {
                    await this.executeBatchDelete();
                } else if (this.deleteType === 'batchDomain') {
                    const res = await API.batchDomains(this.selectedDomains, 'delete', this.currentUser?.username);
                    if (res.ok) {
                        this.showToast('批量删除成功', 'success');
                        this.selectedDomains = [];
                        await this.loadDomains();
                    } else {
                        this.showToast('操作失败', 'error');
                    }
                } else if (this.deleteType === 'batchMetric') {
                    let successCount = 0;
                    for (const id of this.selectedMetricIds) {
                        try {
                            const res = await API.deleteMetric(id);
                            if (res.ok) successCount++;
                        } catch (e) {}
                    }
                    this.showToast(`成功删除 ${successCount} 个指标`, 'success');
                    this.selectedMetricIds = [];
                    await this.loadMetrics();
                } else if (this.deleteType === 'mfa') {
                    const res = await API.mfaReset(this.deleteTarget.id);
                    if (res.ok) {
                        this.showToast('MFA 已重置，用户需重新绑定', 'success');
                        this.userForm.mfa_bound = false;
                        this.loadUsers();
                    } else {
                        this.showToast(await res.text(), 'error');
                    }
                }
            } catch (e) {
                this.showToast('操作失败', 'error');
            }
            this.showDeleteModal = false;
        },

        // ========== 密码库相关方法 ==========
        async initVault() {
            console.log('initVault called');
            try {
                console.log('Calling API.getVaultStatus...');
                const res = await API.getVaultStatus();
                console.log('API.getVaultStatus response:', res);
                if (res.ok) {
                    const data = await res.json();
                    console.log('Vault status data:', data);
                    if (!data.initialized) {
                        this.vaultStatus = 'uninitialized';
                    } else {
                        this.vaultStatus = 'locked';
                    }
                } else {
                    console.error('Vault status not ok:', res.status);
                    this.vaultStatus = 'error';
                }
            } catch (e) {
                console.error('检查密码库状态失败:', e);
                this.vaultStatus = 'error';
            }
            console.log('vaultStatus set to:', this.vaultStatus);
        },

        openVaultInitModal() {
            this.vaultMasterPassword = '';
            this.vaultConfirmPassword = '';
            this.showVaultInitModal = true;
        },

        async initVaultSubmit() {
            if (this.vaultMasterPassword.length < 8) {
                this.showToast('主密码至少需要8个字符', 'error');
                return;
            }
            if (this.vaultMasterPassword !== this.vaultConfirmPassword) {
                this.showToast('两次输入的密码不一致', 'error');
                return;
            }

            try {
                const res = await API.initVault(this.vaultMasterPassword);
                if (res.ok) {
                    const data = await res.json();
                    this.vaultRecoveryKey = data.recovery_key;
                    this.showVaultInitModal = false;
                    this.vaultStatus = 'locked';
                    this.showToast('密码库初始化成功！请妥善保存恢复密钥', 'success');
                    // 显示恢复密钥
                    alert('重要！请保存恢复密钥（用于重置主密码）:\n\n' + data.recovery_key + '\n\n此密钥只显示一次，请妥善保管！');
                } else {
                    const err = await res.text();
                    this.showToast(err || '初始化失败', 'error');
                }
            } catch (e) {
                this.showToast('初始化失败: ' + e.message, 'error');
            }
        },

        openVaultUnlockModal() {
            this.vaultMasterPassword = '';
            this.showVaultUnlockModal = true;
        },

        async unlockVault() {
            if (!this.vaultMasterPassword) {
                this.showToast('请输入主密码', 'error');
                return;
            }

            try {
                const res = await API.unlockVault(this.vaultMasterPassword);
                if (res.ok) {
                    const data = await res.json();
                    this.vaultSession = data.session_token;
                    this.vaultStatus = 'unlocked';
                    this.showVaultUnlockModal = false;
                    this.showToast('密码库已解锁', 'success');
                    await this.loadVaultItems();
                    await this.loadVaultFolders();
                    await this.loadVaultGroups();
                    await this.loadVaultShares();
                } else {
                    const err = await res.text();
                    this.showToast(err || '解锁失败', 'error');
                }
            } catch (e) {
                this.showToast('解锁失败: ' + e.message, 'error');
            }
        },

        async lockVault() {
            try {
                await API.lockVault(this.vaultSession);
                this.vaultSession = null;
                this.vaultItems = [];
                this.vaultFolders = [];
                this.vaultStatus = 'locked';
                this.showToast('密码库已锁定', 'info');
            } catch (e) {
                console.error('锁定密码库失败:', e);
            }
        },

        openVaultResetModal() {
            this.vaultRecoveryKey = '';
            this.vaultMasterPassword = '';
            this.vaultConfirmPassword = '';
            this.showVaultResetModal = true;
        },

        async resetVaultPassword() {
            if (!this.vaultRecoveryKey) {
                this.showToast('请输入恢复密钥', 'error');
                return;
            }
            if (this.vaultMasterPassword.length < 8) {
                this.showToast('新主密码至少需要8个字符', 'error');
                return;
            }
            if (this.vaultMasterPassword !== this.vaultConfirmPassword) {
                this.showToast('两次输入的密码不一致', 'error');
                return;
            }

            try {
                const res = await API.resetVaultPassword(this.vaultRecoveryKey, this.vaultMasterPassword);
                if (res.ok) {
                    const data = await res.json();
                    this.showVaultResetModal = false;
                    this.showToast('主密码重置成功！', 'success');
                    alert('重要！新的恢复密钥:\n\n' + data.new_recovery_key + '\n\n请妥善保管！');
                } else {
                    const err = await res.text();
                    this.showToast(err || '重置失败', 'error');
                }
            } catch (e) {
                this.showToast('重置失败: ' + e.message, 'error');
            }
        },

        async loadVaultItems() {
            try {
                const res = await API.getVaultItems(this.vaultSession);
                if (res.ok) {
                    this.vaultItems = await res.json() || [];
                }
            } catch (e) {
                console.error('加载密码条目失败:', e);
            }
        },

        async loadVaultFolders() {
            try {
                const res = await API.getVaultFolders(this.vaultSession);
                if (res.ok) {
                    this.vaultFolders = await res.json() || [];
                }
            } catch (e) {
                console.error('加载文件夹失败:', e);
            }
        },

        getFilteredVaultItems() {
            let items = this.vaultItems || [];
            if (this.vaultSelectedFolder) {
                items = items.filter(i => i.folder_id === this.vaultSelectedFolder);
            }
            if (this.vaultSelectedType !== 'all') {
                items = items.filter(i => i.type === this.vaultSelectedType);
            }
            if (this.vaultSearch) {
                const q = this.vaultSearch.toLowerCase();
                items = items.filter(i => 
                    i.name.toLowerCase().includes(q) || 
                    i.username.toLowerCase().includes(q) ||
                    i.url.toLowerCase().includes(q)
                );
            }
            return items;
        },

        openVaultItemModal(item = null) {
            if (item) {
                this.vaultEditMode = true;
                this.vaultNewItem = { ...item };
            } else {
                this.vaultEditMode = false;
                this.vaultNewItem = {
                    name: '',
                    username: '',
                    password: '',
                    url: '',
                    notes: '',
                    folder_id: this.vaultSelectedFolder || '',
                    type: 'login',
                    favorite: false
                };
            }
            this.showPasswordInForm = false;
            this.vaultGeneratedPassword = '';
            this.showVaultItemModal = true;
        },

        async saveVaultItem() {
            if (!this.vaultNewItem.name) {
                this.showToast('请输入名称', 'error');
                return;
            }

            try {
                let res;
                if (this.vaultEditMode) {
                    res = await API.updateVaultItem(this.vaultSession, this.vaultNewItem.id, this.vaultNewItem);
                } else {
                    res = await API.addVaultItem(this.vaultSession, this.vaultNewItem);
                }

                if (res.ok) {
                    this.showVaultItemModal = false;
                    this.showToast(this.vaultEditMode ? '密码已更新' : '密码已添加', 'success');
                    await this.loadVaultItems();
                } else if (res.status === 401) {
                    // 会话过期，需要重新解锁
                    this.showVaultItemModal = false;
                    this.vaultStatus = 'locked';
                    this.vaultSession = null;
                    this.showToast('会话已过期，请重新解锁密码库', 'warning');
                    this.openVaultUnlockModal();
                } else {
                    const err = await res.text();
                    this.showToast(err || '保存失败', 'error');
                }
            } catch (e) {
                this.showToast('保存失败: ' + e.message, 'error');
            }
        },

        async deleteVaultItem(item) {
            const confirmed = await this.showConfirm({
                type: 'danger',
                title: '删除密码',
                message: `确定要删除 "${item.name}" 吗？\n此操作不可恢复。`,
                okText: '删除',
                cancelText: '取消'
            });
            if (!confirmed) return;

            try {
                const res = await API.deleteVaultItem(this.vaultSession, item.id);
                if (res.ok) {
                    this.showToast('已删除', 'success');
                    await this.loadVaultItems();
                } else {
                    this.showToast('删除失败', 'error');
                }
            } catch (e) {
                this.showToast('删除失败: ' + e.message, 'error');
            }
        },

        async generatePassword() {
            try {
                const opts = this.vaultPasswordOptions;
                const params = new URLSearchParams({
                    length: opts.length,
                    upper: opts.upper,
                    lower: opts.lower,
                    numbers: opts.numbers,
                    symbols: opts.symbols
                });
                const res = await fetch(`/api/vault/generate-password?${params}`);
                if (res.ok) {
                    const data = await res.json();
                    this.vaultGeneratedPassword = data.password;
                    this.vaultNewItem.password = data.password;
                }
            } catch (e) {
                console.error('生成密码失败:', e);
            }
        },

        copyToClipboard(text, type = '内容') {
            navigator.clipboard.writeText(text).then(() => {
                this.showToast(`${type}已复制`, 'success');
            }).catch(() => {
                this.showToast('复制失败', 'error');
            });
        },

        togglePasswordVisibility(itemId) {
            this.vaultShowPassword[itemId] = !this.vaultShowPassword[itemId];
        },

        // ========== 批量导入密码 ==========
        openVaultBatchModal() {
            this.vaultBatchText = '';
            this.vaultBatchFormat = 'csv';
            this.showVaultBatchModal = true;
        },

        async importVaultBatch() {
            if (!this.vaultBatchText.trim()) {
                this.showToast('请输入要导入的数据', 'error');
                return;
            }

            const lines = this.vaultBatchText.trim().split('\n');
            let successCount = 0;
            let failCount = 0;

            for (const line of lines) {
                if (!line.trim()) continue;
                
                let parts;
                if (this.vaultBatchFormat === 'csv') {
                    // CSV 格式: 名称,用户名,密码,网址,备注
                    parts = line.split(',').map(p => p.trim());
                } else {
                    // TAB 分隔格式
                    parts = line.split('\t').map(p => p.trim());
                }

                if (parts.length < 3) {
                    failCount++;
                    continue;
                }

                const item = {
                    name: parts[0] || '',
                    username: parts[1] || '',
                    password: parts[2] || '',
                    url: parts[3] || '',
                    notes: parts[4] || '',
                    folder_id: this.vaultSelectedFolder || '',
                    type: 'login',
                    favorite: false
                };

                if (!item.name) {
                    failCount++;
                    continue;
                }

                try {
                    const res = await API.addVaultItem(this.vaultSession, item);
                    if (res.ok) {
                        successCount++;
                    } else {
                        failCount++;
                    }
                } catch (e) {
                    failCount++;
                }
            }

            this.showVaultBatchModal = false;
            await this.loadVaultItems();
            
            if (successCount > 0) {
                this.showToast(`成功导入 ${successCount} 条${failCount > 0 ? '，失败 ' + failCount + ' 条' : ''}`, 'success');
            } else {
                this.showToast('导入失败，请检查数据格式', 'error');
            }
        },

        // ========== 文件夹管理 ==========
        openVaultFolderModal(folder = null) {
            if (folder) {
                this.vaultEditFolderMode = true;
                this.vaultNewFolder = { ...folder };
            } else {
                this.vaultEditFolderMode = false;
                this.vaultNewFolder = { name: '', icon: 'folder' };
            }
            this.showVaultFolderModal = true;
        },

        async saveVaultFolder() {
            if (!this.vaultNewFolder.name) {
                this.showToast('请输入文件夹名称', 'error');
                return;
            }

            try {
                let res;
                if (this.vaultEditFolderMode) {
                    res = await API.updateVaultFolder(this.vaultSession, this.vaultNewFolder.id, this.vaultNewFolder);
                } else {
                    res = await API.addVaultFolder(this.vaultSession, this.vaultNewFolder);
                }

                if (res.ok) {
                    this.showVaultFolderModal = false;
                    this.showToast(this.vaultEditFolderMode ? '文件夹已更新' : '文件夹已创建', 'success');
                    await this.loadVaultFolders();
                } else {
                    const err = await res.text();
                    this.showToast(err || '保存失败', 'error');
                }
            } catch (e) {
                this.showToast('保存失败: ' + e.message, 'error');
            }
        },

        async deleteVaultFolder(folder) {
            const confirmed = await this.showConfirm({
                type: 'warning',
                title: '删除文件夹',
                message: `确定要删除文件夹 "${folder.name}" 吗？\n\n文件夹中的密码不会被删除，只会变为无文件夹状态。`,
                okText: '删除',
                cancelText: '取消'
            });
            if (!confirmed) return;

            try {
                const res = await API.deleteVaultFolder(this.vaultSession, folder.id);
                if (res.ok) {
                    this.showToast('文件夹已删除', 'success');
                    if (this.vaultSelectedFolder === folder.id) {
                        this.vaultSelectedFolder = '';
                    }
                    await this.loadVaultFolders();
                    await this.loadVaultItems();
                } else {
                    this.showToast('删除失败', 'error');
                }
            } catch (e) {
                this.showToast('删除失败: ' + e.message, 'error');
            }
        },

        // ========== 用户组管理 ==========
        async loadVaultGroups() {
            try {
                const res = await API.getVaultGroups();
                if (res.ok) {
                    this.vaultGroups = await res.json() || [];
                }
            } catch (e) {
                console.error('加载用户组失败:', e);
            }
        },

        async loadVaultAvailableUsers() {
            try {
                const res = await API.getVaultUsers();
                if (res.ok) {
                    this.vaultAvailableUsers = await res.json() || [];
                }
            } catch (e) {
                console.error('加载用户列表失败:', e);
            }
        },

        openVaultGroupModal(group = null) {
            if (group) {
                this.vaultEditGroupMode = true;
                this.vaultNewGroup = { ...group };
            } else {
                this.vaultEditGroupMode = false;
                this.vaultNewGroup = { name: '', description: '' };
            }
            this.showVaultGroupModal = true;
        },

        async saveVaultGroup() {
            if (!this.vaultNewGroup.name) {
                this.showToast('请输入用户组名称', 'error');
                return;
            }

            try {
                let res;
                if (this.vaultEditGroupMode) {
                    res = await API.updateVaultGroup(this.vaultNewGroup.id, this.vaultNewGroup);
                } else {
                    res = await API.addVaultGroup(this.vaultNewGroup);
                }

                if (res.ok) {
                    this.showVaultGroupModal = false;
                    this.showToast(this.vaultEditGroupMode ? '用户组已更新' : '用户组已创建', 'success');
                    await this.loadVaultGroups();
                } else {
                    this.showToast('保存失败', 'error');
                }
            } catch (e) {
                this.showToast('保存失败: ' + e.message, 'error');
            }
        },

        async deleteVaultGroup(group) {
            const confirmed = await this.showConfirm({
                type: 'danger',
                title: '删除用户组',
                message: `确定要删除用户组 "${group.name}" 吗？\n此操作不可恢复。`,
                okText: '删除',
                cancelText: '取消'
            });
            if (!confirmed) return;

            try {
                const res = await API.deleteVaultGroup(group.id);
                if (res.ok) {
                    this.showToast('用户组已删除', 'success');
                    await this.loadVaultGroups();
                } else {
                    this.showToast('删除失败', 'error');
                }
            } catch (e) {
                this.showToast('删除失败: ' + e.message, 'error');
            }
        },

        async openGroupMembersModal(group) {
            this.vaultCurrentGroup = group;
            await this.loadVaultAvailableUsers();
            await this.loadGroupMembers(group.id);
            this.showVaultGroupMembersModal = true;
        },

        async loadGroupMembers(groupId) {
            try {
                const res = await API.getVaultGroupMembers(groupId);
                if (res.ok) {
                    this.vaultGroupMembers = await res.json() || [];
                }
            } catch (e) {
                console.error('加载组成员失败:', e);
            }
        },

        async addGroupMember(userId) {
            if (!this.vaultCurrentGroup) return;

            try {
                const res = await API.addVaultGroupMember(this.vaultCurrentGroup.id, {
                    user_id: userId,
                    role: 'member'
                });

                if (res.ok) {
                    this.showToast('成员已添加', 'success');
                    await this.loadGroupMembers(this.vaultCurrentGroup.id);
                    await this.loadVaultGroups();
                } else {
                    this.showToast('添加失败', 'error');
                }
            } catch (e) {
                this.showToast('添加失败: ' + e.message, 'error');
            }
        },

        async removeGroupMember(member) {
            if (!this.vaultCurrentGroup) return;
            const confirmed = await this.showConfirm({
                type: 'warning',
                title: '移除成员',
                message: `确定要移除成员 "${member.user_id}" 吗？`,
                okText: '移除',
                cancelText: '取消'
            });
            if (!confirmed) return;

            try {
                const res = await API.removeVaultGroupMember(this.vaultCurrentGroup.id, member.id);
                if (res.ok) {
                    this.showToast('成员已移除', 'success');
                    await this.loadGroupMembers(this.vaultCurrentGroup.id);
                    await this.loadVaultGroups();
                } else {
                    this.showToast('移除失败', 'error');
                }
            } catch (e) {
                this.showToast('移除失败: ' + e.message, 'error');
            }
        },

        // ========== 分享管理 ==========
        async loadVaultShares() {
            try {
                const res = await API.getVaultShares();
                if (res.ok) {
                    this.vaultShares = await res.json() || [];
                }
            } catch (e) {
                console.error('加载分享列表失败:', e);
            }
        },

        openVaultShareModal(item = null, type = 'item') {
            this.vaultNewShare = {
                target_type: type,
                target_id: item ? item.id : '',
                shared_with: '',
                permission: 'read'
            };
            this.loadVaultAvailableUsers();
            this.showVaultShareModal = true;
        },

        async saveVaultShare() {
            if (!this.vaultNewShare.target_id || !this.vaultNewShare.shared_with) {
                this.showToast('请选择要分享的内容和用户', 'error');
                return;
            }

            try {
                const res = await API.addVaultShare(this.vaultNewShare);
                if (res.ok) {
                    this.showVaultShareModal = false;
                    this.showToast('分享成功', 'success');
                    await this.loadVaultShares();
                } else {
                    this.showToast('分享失败', 'error');
                }
            } catch (e) {
                this.showToast('分享失败: ' + e.message, 'error');
            }
        },

        async deleteVaultShare(share) {
            const confirmed = await this.showConfirm({
                type: 'warning',
                title: '取消分享',
                message: '确定要取消此分享吗？',
                okText: '取消分享',
                cancelText: '返回'
            });
            if (!confirmed) return;

            try {
                const res = await API.deleteVaultShare(share.id);
                if (res.ok) {
                    this.showToast('已取消分享', 'success');
                    await this.loadVaultShares();
                } else {
                    this.showToast('取消失败', 'error');
                }
            } catch (e) {
                this.showToast('取消失败: ' + e.message, 'error');
            }
        },

        getPermissionLabel(perm) {
            const labels = { read: '只读', write: '读写', admin: '管理' };
            return labels[perm] || perm;
        },

        // ========== RBAC 权限管理 ==========
        async loadRoles() {
            try {
                const res = await API.getRoles();
                if (res.ok) {
                    this.roles = await res.json() || [];
                }
            } catch (e) {
                console.error('加载角色失败:', e);
            }
        },

        async loadPermissions(type = '') {
            try {
                const res = await API.getPermissions(type);
                if (res.ok) {
                    this.permissions = await res.json() || [];
                }
            } catch (e) {
                console.error('加载权限失败:', e);
            }
        },

        async loadAllPermissions() {
            try {
                const res = await API.getPermissions();
                if (res.ok) {
                    const data = await res.json() || [];
                    // 后端返回平铺数据，前端构建树形结构
                    this.allPermissions = this.buildPermissionTree(data);
                    console.log('加载权限完成，共', this.allPermissions.length, '条');
                }
            } catch (e) {
                console.error('加载权限失败:', e);
            }
        },

        // 根据 parent_id 构建权限树形结构
        buildPermissionTree(permissions) {
            const permMap = {};
            const roots = [];

            // 第一遍：建立映射
            for (const perm of permissions) {
                perm.children = [];
                permMap[perm.id] = perm;
            }

            // 第二遍：构建树
            for (const perm of permissions) {
                if (!perm.parent_id || perm.parent_id === '') {
                    roots.push(perm);
                } else if (permMap[perm.parent_id]) {
                    permMap[perm.parent_id].children.push(perm);
                } else {
                    roots.push(perm);
                }
            }

            return roots;
        },

        // 获取所有权限的平铺列表（用于权限配置页面显示）
        getAllPermissionsFlat() {
            const result = [];
            const flatten = (perms) => {
                for (const perm of perms) {
                    result.push(perm);
                    if (perm.children && perm.children.length > 0) {
                        flatten(perm.children);
                    }
                }
            };
            flatten(this.allPermissions);
            return result;
        },

        async loadMyPermissions() {
            try {
                const res = await API.getMyPermissions();
                if (res.ok) {
                    const data = await res.json();
                    this.myPermissions = data.permissions || {};
                    this.myMenus = data.menus || [];
                }
            } catch (e) {
                console.error('加载我的权限失败:', e);
            }
        },

        hasPermission(code) {
            // 如果是管理员，拥有所有权限
            if (this.currentUser === 'admin') return true;
            return this.myPermissions[code] === true;
        },

        openRoleModal(role = null) {
            if (role) {
                this.editRoleMode = true;
                this.newRole = { ...role };
            } else {
                this.editRoleMode = false;
                this.newRole = { code: '', name: '', description: '', status: 'active' };
            }
            this.showRoleModal = true;
        },

        async saveRole() {
            if (!this.newRole.code || !this.newRole.name) {
                this.showToast('请输入角色代码和名称', 'error');
                return;
            }

            try {
                let res;
                if (this.editRoleMode) {
                    res = await API.updateRole(this.newRole.id, this.newRole);
                } else {
                    res = await API.createRole(this.newRole);
                }

                if (res.ok) {
                    this.showRoleModal = false;
                    this.showToast(this.editRoleMode ? '角色已更新' : '角色已创建', 'success');
                    await this.loadRoles();
                } else {
                    const err = await res.text();
                    this.showToast(err || '保存失败', 'error');
                }
            } catch (e) {
                this.showToast('保存失败: ' + e.message, 'error');
            }
        },

        async deleteRole(role) {
            if (role.is_system) {
                this.showToast('系统内置角色不能删除', 'error');
                return;
            }
            const confirmed = await this.showConfirm({
                type: 'danger',
                title: '删除角色',
                message: `确定要删除角色 "${role.name}" 吗？\n删除后，使用此角色的用户将失去相关权限。`,
                okText: '删除',
                cancelText: '取消'
            });
            if (!confirmed) return;

            try {
                const res = await API.deleteRole(role.id);
                if (res.ok) {
                    this.showToast('角色已删除', 'success');
                    await this.loadRoles();
                } else {
                    const err = await res.text();
                    this.showToast(err || '删除失败', 'error');
                }
            } catch (e) {
                this.showToast('删除失败: ' + e.message, 'error');
            }
        },

        async openRolePermissionModal(role) {
            this.selectedRole = role;
            await this.loadAllPermissions();
            
            // 获取角色已有权限
            try {
                const res = await API.getRolePermissions(role.id);
                if (res.ok) {
                    this.selectedRolePermissions = await res.json() || {};
                }
            } catch (e) {
                console.error('加载角色权限失败:', e);
            }
            
            this.showRolePermissionModal = true;
        },

        togglePermission(permId) {
            if (this.selectedRolePermissions[permId]) {
                delete this.selectedRolePermissions[permId];
            } else {
                this.selectedRolePermissions[permId] = true;
            }
            // 触发响应式更新
            this.selectedRolePermissions = { ...this.selectedRolePermissions };
        },

        async saveRolePermissions() {
            if (!this.selectedRole) return;

            try {
                const permIds = Object.keys(this.selectedRolePermissions).filter(k => this.selectedRolePermissions[k]);
                const res = await API.updateRolePermissions(this.selectedRole.id, permIds);
                if (res.ok) {
                    this.showRolePermissionModal = false;
                    this.showToast('权限已更新', 'success');
                } else {
                    this.showToast('保存失败', 'error');
                }
            } catch (e) {
                this.showToast('保存失败: ' + e.message, 'error');
            }
        },

        async openUserRoleModal(user) {
            this.selectedUserForRole = user;
            await this.loadRoles();
            
            // 获取用户已有角色
            try {
                const res = await API.getUserRoles(user.id);
                if (res.ok) {
                    const userRoles = await res.json() || [];
                    this.currentUserRoles = userRoles.map(ur => ur.role_id);
                }
            } catch (e) {
                console.error('加载用户角色失败:', e);
                this.currentUserRoles = [];
            }
            
            this.showUserRoleModal = true;
        },

        toggleUserRole(roleId) {
            const idx = this.currentUserRoles.indexOf(roleId);
            if (idx > -1) {
                this.currentUserRoles.splice(idx, 1);
            } else {
                this.currentUserRoles.push(roleId);
            }
        },

        async saveUserRoles() {
            if (!this.selectedUserForRole) return;

            try {
                const res = await API.updateUserRoles(this.selectedUserForRole.id, this.currentUserRoles);
                if (res.ok) {
                    this.showUserRoleModal = false;
                    this.showToast('用户角色已更新', 'success');
                } else {
                    this.showToast('保存失败', 'error');
                }
            } catch (e) {
                this.showToast('保存失败: ' + e.message, 'error');
            }
        },

        getRoleStatusLabel(status) {
            return status === 'active' ? '启用' : '禁用';
        },

        getPermissionTypeLabel(type) {
            const labels = { menu: '菜单', button: '按钮', data: '数据', api: 'API' };
            return labels[type] || type;
        },

        // ========== 权限增删改 ==========
        openPermissionModal(perm = null) {
            if (perm) {
                this.editPermissionMode = true;
                this.newPermission = { ...perm };
            } else {
                this.editPermissionMode = false;
                this.newPermission = { code: '', name: '', type: 'button', resource: '', parent_id: '', description: '' };
            }
            this.showPermissionModal = true;
        },

        async savePermission() {
            if (!this.newPermission.code || !this.newPermission.name) {
                this.showToast('请输入权限代码和名称', 'error');
                return;
            }

            try {
                let res;
                if (this.editPermissionMode) {
                    res = await API.updatePermission(this.newPermission.id, this.newPermission);
                } else {
                    res = await API.createPermission(this.newPermission);
                }

                if (res.ok) {
                    this.showPermissionModal = false;
                    this.showToast(this.editPermissionMode ? '权限已更新' : '权限已创建', 'success');
                    await this.loadAllPermissions();
                } else {
                    const err = await res.text();
                    this.showToast(err || '保存失败', 'error');
                }
            } catch (e) {
                this.showToast('保存失败: ' + e.message, 'error');
            }
        },

        async deletePermission(perm) {
            const confirmed = await this.showConfirm({
                type: 'danger',
                title: '删除权限',
                message: `确定要删除权限 "${perm.name}" 吗？\n此操作不可恢复。`,
                okText: '删除',
                cancelText: '取消'
            });
            if (!confirmed) return;

            try {
                const res = await API.deletePermission(perm.id);
                if (res.ok) {
                    this.showToast('权限已删除', 'success');
                    await this.loadAllPermissions();
                } else {
                    const err = await res.text();
                    this.showToast(err || '删除失败', 'error');
                }
            } catch (e) {
                this.showToast('删除失败: ' + e.message, 'error');
            }
        },

        // ========== 任务池 ==========
        async loadTasks() {
            try {
                const res = await API.getTasks({
                    status: this.taskStatusFilter, project: this.taskProjectFilter,
                    priority: this.taskPriorityFilter, delayed: this.taskDelayedFilter,
                    completion: this.taskCompletionFilter
                });
                if (res.ok) this.tasks = await res.json() || [];
            } catch (e) { this.showToast('加载任务失败', 'error'); }
        },

        async loadTaskStats() {
            try { const res = await API.getTaskStats(); if (res.ok) this.taskStats = await res.json() || {}; } catch (e) { }
        },

        async loadTaskProjects() {
            try { const res = await API.getTaskProjects(); if (res.ok) this.taskProjects = await res.json() || []; } catch (e) { }
        },

        async loadTaskData() {
            await this.loadTaskProjects();
            await this.loadTasks();
            await this.loadTaskStats();
        },

        openTaskModal(task = null) {
            if (task) {
                this.editTaskMode = true;
                this.taskForm = { ...task };
                // 日期字段去掉 T00:00:00Z 后缀，只保留 YYYY-MM-DD
                ['start_time', 'end_time', 'delay_end_time'].forEach(f => {
                    if (this.taskForm[f]) this.taskForm[f] = this.taskForm[f].replace(/T.*$/, '').replace(/\s.*$/, '');
                });
            } else {
                this.editTaskMode = false;
                const today = new Date().toISOString().slice(0, 10);
                this.taskForm = {
                    id: '', project: '', title: '', source: 'other', category: 'feature', priority: 'P2',
                    assignee: '', start_time: today, end_time: '', status: 'pending', result: '', remark: '',
                    is_delayed: false, delay_reason: '', delay_desc: '', delay_end_time: ''
                };
            }
            this.showTaskModal = true;
        },

        async saveTask() {
            if (!this.taskForm.title) { this.showToast('请输入需求描述', 'error'); return; }
            if (this.taskForm.is_delayed) {
                if (!this.taskForm.delay_reason) { this.showToast('请选择延期原因', 'error'); return; }
                if (!this.taskForm.delay_desc) { this.showToast('请填写延期说明', 'error'); return; }
                if (!this.taskForm.delay_end_time) { this.showToast('请填写延期后结束时间', 'error'); return; }
            }
            try {
                let res;
                if (this.editTaskMode) {
                    res = await API.updateTask(this.taskForm.id, this.taskForm);
                } else {
                    res = await API.createTask(this.taskForm);
                }
                if (res.ok) {
                    this.showTaskModal = false;
                    this.showToast(this.editTaskMode ? '任务已更新' : '任务已创建', 'success');
                    await this.loadTaskData();
                } else {
                    const err = await res.text();
                    this.showToast(err || '保存失败', 'error');
                }
            } catch (e) { this.showToast('保存失败: ' + e.message, 'error'); }
        },

        async deleteTask(task) {
            const confirmed = await this.showConfirm({
                type: 'danger',
                title: '删除任务',
                message: `确定要删除任务 "${task.title}" 吗？`,
                okText: '删除',
                cancelText: '取消'
            });
            if (!confirmed) return;
            try {
                const res = await API.deleteTask(task.id);
                if (res.ok) { this.showToast('任务已删除', 'success'); await this.loadTaskData(); }
            } catch (e) { this.showToast('删除失败', 'error'); }
        },

        getTaskSourceLabel(s) {
            const m = { 'product': '产品需求', 'operation': '运营需求', 'tech': '技术优化', 'customer': '客户反馈', 'leader': '领导安排', 'other': '其他' };
            return m[s] || s;
        },
        getTaskCategoryLabel(c) {
            const m = { 'feature': '新功能', 'bugfix': 'Bug修复', 'optimization': '优化', 'infrastructure': '基础设施', 'security': '安全', 'other': '其他' };
            return m[c] || c;
        },
        getTaskStatusLabel(s) {
            const m = { 'pending': '待开始', 'in_progress': '进行中', 'testing': '测试中', 'completed': '已完成', 'cancelled': '已取消' };
            return m[s] || s;
        },
        getDelayReasonLabel(r) {
            const m = { 'difficulty': '难度大', 'workload': '任务多', 'dependency': '依赖阻塞', 'requirement_change': '需求变更', 'other': '其他原因' };
            return m[r] || r;
        },
        getPriorityClass(p) {
            const m = { 'P0': 'badge-error', 'P1': 'badge-warning', 'P2': 'badge-info', 'P3': 'badge-default' };
            return m[p] || 'badge-default';
        },
        getTaskStatusClass(s) {
            const m = { 'pending': 'badge-default', 'in_progress': 'badge-info', 'testing': 'badge-warning', 'completed': 'badge-success', 'cancelled': 'badge-default' };
            return m[s] || 'badge-default';
        },

        // 批量添加任务
        openBatchTaskModal() {
            this.batchTaskText = '';
            this.batchTaskResult = null;
            this.showBatchTaskModal = true;
        },

        parseBatchTaskText() {
            // 格式: 需求描述,项目,来源,分类,优先级,负责人,开始时间,结束时间,备注
            // 支持逗号、Tab、| 三种分隔符自动识别
            const lines = this.batchTaskText.trim().split('\n').filter(l => l.trim() && !l.startsWith('#'));
            const tasks = [];
            for (const line of lines) {
                let parts;
                if (line.includes('\t')) parts = line.split('\t');
                else if (line.includes(',')) parts = line.split(',');
                else if (line.includes('|')) parts = line.split('|');
                else parts = [line];
                parts = parts.map(p => p.trim());
                if (parts.length < 1 || !parts[0]) continue;
                tasks.push({
                    title: parts[0] || '',
                    project: parts[1] || '',
                    source: parts[2] || 'other',
                    category: parts[3] || 'feature',
                    priority: parts[4] || 'P2',
                    assignee: parts[5] || '',
                    start_time: parts[6] || '',
                    end_time: parts[7] || '',
                    remark: parts[8] || '',
                    status: 'pending',
                    is_delayed: false,
                    delay_reason: '',
                    delay_desc: '',
                    delay_end_time: ''
                });
            }
            return tasks;
        },

        async executeBatchTask() {
            const tasks = this.parseBatchTaskText();
            if (tasks.length === 0) {
                this.showToast('请输入至少一条任务', 'error');
                return;
            }
            this.batchTaskLoading = true;
            this.batchTaskResult = null;
            try {
                const res = await API.batchCreateTasks(tasks);
                if (res.ok) {
                    this.batchTaskResult = await res.json();
                    if (this.batchTaskResult.fail_count === 0) {
                        this.showToast(`成功添加 ${this.batchTaskResult.success_count} 个任务`, 'success');
                        this.showBatchTaskModal = false;
                    } else {
                        this.showToast(this.batchTaskResult.message, 'warning');
                    }
                    await this.loadTaskData();
                } else {
                    this.showToast('批量添加失败', 'error');
                }
            } catch (e) {
                this.showToast('批量添加失败: ' + e.message, 'error');
            } finally {
                this.batchTaskLoading = false;
            }
        },

        formatTaskDate(dateStr) {
            if (!dateStr) return '-';
            // 去掉 T00:00:00Z 等后缀，只显示日期部分
            return dateStr.replace(/T.*$/, '').replace(/\s.*$/, '');
        },

        getFilteredTasks() {
            let list = this.tasks || [];
            if (this.taskSearch) {
                const q = this.taskSearch.toLowerCase();
                list = list.filter(t => t.title.toLowerCase().includes(q) || (t.assignee || '').toLowerCase().includes(q) || (t.project || '').toLowerCase().includes(q));
            }
            return list;
        },
        getTaskTotalPages() { return Math.ceil(this.getFilteredTasks().length / this.taskPageSize) || 1; },
        getPagedTasks() {
            const start = (this.taskCurrentPage - 1) * this.taskPageSize;
            return this.getFilteredTasks().slice(start, start + this.taskPageSize);
        },

        // ========== 员工失误记录 ==========
        async loadIncidents() {
            try {
                const res = await API.getIncidents(this.incidentStatusFilter, this.incidentTypeFilter);
                if (res.ok) {
                    this.incidents = await res.json() || [];
                }
            } catch (e) {
                this.showToast('加载失误记录失败', 'error');
            }
        },

        async loadIncidentStats() {
            try {
                const res = await API.getIncidentStats();
                if (res.ok) {
                    this.incidentStats = await res.json() || {};
                }
            } catch (e) { }
        },

        async loadIncidentData() {
            await this.loadIncidents();
            await this.loadIncidentStats();
        },

        openIncidentModal(inc = null) {
            if (inc) {
                this.editIncidentMode = true;
                this.incidentForm = { ...inc };
            } else {
                this.editIncidentMode = false;
                const now = new Date();
                const timeStr = now.getFullYear() + '-' + String(now.getMonth()+1).padStart(2,'0') + '-' + String(now.getDate()).padStart(2,'0') + ' ' + String(now.getHours()).padStart(2,'0') + ':' + String(now.getMinutes()).padStart(2,'0');
                this.incidentForm = {
                    id: '', incident_time: timeStr, operator: '', operation_type: 'other', operation_desc: '',
                    status: 'pending', severity: 'medium', reason: '', impact: '', solution: '',
                    checker: '', check_time: '', check_result: '', remark: ''
                };
            }
            this.showIncidentModal = true;
        },

        async saveIncident() {
            if (!this.incidentForm.operator || !this.incidentForm.incident_time) {
                this.showToast('请填写操作人和发生时间', 'error');
                return;
            }
            try {
                let res;
                if (this.editIncidentMode) {
                    res = await API.updateIncident(this.incidentForm.id, this.incidentForm);
                } else {
                    res = await API.createIncident(this.incidentForm);
                }
                if (res.ok) {
                    this.showIncidentModal = false;
                    this.showToast(this.editIncidentMode ? '记录已更新' : '记录已创建', 'success');
                    await this.loadIncidentData();
                } else {
                    const err = await res.text();
                    this.showToast(err || '保存失败', 'error');
                }
            } catch (e) {
                this.showToast('保存失败: ' + e.message, 'error');
            }
        },

        async deleteIncident(inc) {
            const confirmed = await this.showConfirm({
                type: 'danger',
                title: '删除失误记录',
                message: '确定要删除该失误记录吗？',
                okText: '删除',
                cancelText: '取消'
            });
            if (!confirmed) return;
            try {
                const res = await API.deleteIncident(inc.id);
                if (res.ok) {
                    this.showToast('记录已删除', 'success');
                    await this.loadIncidentData();
                }
            } catch (e) {
                this.showToast('删除失败', 'error');
            }
        },

        getIncidentTypeLabel(type) {
            const labels = { 'deploy': '发布部署', 'config_change': '配置变更', 'db_operation': '数据库操作', 'permission': '权限操作', 'network': '网络变更', 'monitoring': '监控告警', 'security': '安全事件', 'other': '其他' };
            return labels[type] || type;
        },

        getIncidentSeverityLabel(sev) {
            const labels = { 'low': '低', 'medium': '中', 'high': '高', 'critical': '严重' };
            return labels[sev] || sev;
        },

        getIncidentStatusLabel(st) {
            const labels = { 'pending': '待处理', 'resolved': '已解决', 'closed': '已关闭' };
            return labels[st] || st;
        },

        getFilteredIncidents() {
            let list = this.incidents || [];
            if (this.incidentSearch) {
                const q = this.incidentSearch.toLowerCase();
                list = list.filter(i => i.operator.toLowerCase().includes(q) || (i.operation_desc || '').toLowerCase().includes(q) || (i.reason || '').toLowerCase().includes(q) || (i.checker || '').toLowerCase().includes(q));
            }
            return list;
        },

        getIncidentTotalPages() {
            return Math.ceil(this.getFilteredIncidents().length / this.incidentPageSize) || 1;
        },

        getPagedIncidents() {
            const start = (this.incidentCurrentPage - 1) * this.incidentPageSize;
            return this.getFilteredIncidents().slice(start, start + this.incidentPageSize);
        },

        // ========== 商户管理 ==========
        async loadMerchants() {
            try {
                const res = await API.getMerchants(this.merchantProjectFilter, this.merchantEnvFilter);
                if (res.ok) {
                    let list = await res.json() || [];
                    // 解析JSON字符串为数组
                    list.forEach(m => {
                        ['contact_emails','website_urls','player_regions','game_types','handicaps','languages','currencies','supported_ports','wallet_types','callback_domains','hall_domains','site_domains','site_accounts','app_keys','game_domains','redirect_domains'].forEach(f => {
                            try { if (typeof m[f] === 'string') m[f] = JSON.parse(m[f]); } catch(e) { m[f] = []; }
                            if (!Array.isArray(m[f])) m[f] = [];
                        });
                    });
                    this.merchants = list;
                }
            } catch (e) { this.showToast('加载商户失败', 'error'); }
        },

        openMerchantModal(m = null) {
            this.merchantTagInput = {};
            if (m) {
                this.editMerchantMode = true;
                this.merchantForm = JSON.parse(JSON.stringify(m));
            } else {
                this.editMerchantMode = false;
                this.merchantForm = {
                    id: '', project: '', env: 'prod', website_name: '', contact_emails: [], website_urls: [],
                    player_regions: [], estimated_players: '', game_types: [], handicaps: [], languages: [],
                    currencies: [], supported_ports: [], wallet_types: [], callback_domains: [],
                    whitelist_ips: '', hall_domains: [], site_domains: [], site_accounts: [], app_keys: [],
                    app_secrets: '', game_domains: [], redirect_domains: [], remark: '', status: 'active'
                };
            }
            this.showMerchantModal = true;
        },

        async saveMerchant() {
            if (!this.merchantForm.website_name) { this.showToast('请输入网站方名称', 'error'); return; }
            try {
                // 将数组序列化为JSON字符串
                const data = { ...this.merchantForm };
                ['contact_emails','website_urls','player_regions','game_types','handicaps','languages','currencies','supported_ports','wallet_types','callback_domains','hall_domains','site_domains','site_accounts','app_keys','game_domains','redirect_domains'].forEach(f => {
                    if (Array.isArray(data[f])) data[f] = JSON.stringify(data[f]);
                });
                let res;
                if (this.editMerchantMode) {
                    res = await API.updateMerchant(data.id, data);
                } else {
                    res = await API.createMerchant(data);
                }
                if (res.ok) {
                    this.showMerchantModal = false;
                    this.showToast(this.editMerchantMode ? '商户已更新' : '商户已创建', 'success');
                    await this.loadMerchants();
                } else { this.showToast('保存失败', 'error'); }
            } catch (e) { this.showToast('保存失败: ' + e.message, 'error'); }
        },

        async deleteMerchant(m) {
            const confirmed = await this.showConfirm({
                type: 'danger',
                title: '删除商户',
                message: `确定要删除商户「${m.website_name}」吗？此操作不可撤销。`,
                okText: '确认删除',
                cancelText: '取消'
            });
            if (!confirmed) return;
            
            try {
                const res = await API.deleteMerchant(m.id);
                if (res.ok) { this.showToast('已删除', 'success'); await this.loadMerchants(); }
            } catch (e) { this.showToast('删除失败', 'error'); }
        },

        async batchDeleteMerchants() {
            if (this.selectedMerchantIds.length === 0) { this.showToast('请先选择商户', 'error'); return; }
            
            const confirmed = await this.showConfirm({
                type: 'danger',
                title: '批量删除',
                message: `确定要删除选中的 ${this.selectedMerchantIds.length} 个商户吗？此操作不可撤销。`,
                okText: `删除 ${this.selectedMerchantIds.length} 个`,
                cancelText: '取消'
            });
            if (!confirmed) return;
            
            let success = 0;
            for (const id of this.selectedMerchantIds) {
                try {
                    const res = await API.deleteMerchant(id);
                    if (res.ok) success++;
                } catch (e) { }
            }
            this.showToast(`成功删除 ${success} 个商户`, 'success');
            this.selectedMerchantIds = [];
            await this.loadMerchants();
        },

        toggleMerchantSelect(id) {
            const idx = this.selectedMerchantIds.indexOf(id);
            if (idx > -1) this.selectedMerchantIds.splice(idx, 1);
            else this.selectedMerchantIds.push(id);
        },

        toggleAllMerchants() {
            const current = this.getPagedMerchants();
            if (this.selectedMerchantIds.length === current.length) {
                this.selectedMerchantIds = [];
            } else {
                this.selectedMerchantIds = current.map(m => m.id);
            }
        },

        // 标签输入辅助
        addMerchantTag(field) {
            const val = (this.merchantTagInput[field] || '').trim();
            if (!val) return;
            if (!this.merchantForm[field]) this.merchantForm[field] = [];
            if (!this.merchantForm[field].includes(val)) {
                this.merchantForm[field].push(val);
            }
            this.merchantTagInput[field] = '';
        },

        removeMerchantTag(field, index) {
            this.merchantForm[field].splice(index, 1);
        },

        getFilteredMerchants() {
            let list = this.merchants || [];
            if (this.merchantProjectFilter) {
                list = list.filter(m => m.project === this.merchantProjectFilter);
            }
            if (this.merchantSearch) {
                const q = this.merchantSearch.toLowerCase();
                list = list.filter(m => m.website_name.toLowerCase().includes(q) || (m.project||'').toLowerCase().includes(q));
            }
            return list;
        },

        getMerchantTotalPages() { return Math.ceil(this.getFilteredMerchants().length / this.merchantPageSize) || 1; },

        getPagedMerchants() {
            const start = (this.merchantCurrentPage - 1) * this.merchantPageSize;
            return this.getFilteredMerchants().slice(start, start + this.merchantPageSize);
        },

        formatTags(arr) {
            if (!arr || !Array.isArray(arr) || arr.length === 0) return '-';
            return arr.join(', ');
        },

        // 根据项目名称生成颜色名称（用于 CSS data-color 属性）
        getProjectColor(project) {
            if (!project) return 'blue';
            // 预定义的项目颜色映射（使用语义化颜色名）
            const colorMap = {
                '皇冠项目': 'red',
                '金沙项目': 'amber',
                '星辰项目': 'blue',
                '银河项目': 'violet',
                '永利项目': 'emerald',
                '威尼斯项目': 'rose',
                '澳门项目': 'cyan',
                '新濠项目': 'orange'
            };
            if (colorMap[project]) return colorMap[project];
            // 如果没有预定义，根据项目名生成一个稳定的颜色
            const colors = ['red', 'orange', 'amber', 'emerald', 'teal', 'cyan', 'blue', 'indigo', 'violet', 'purple', 'pink', 'rose'];
            let hash = 0;
            for (let i = 0; i < project.length; i++) {
                hash = project.charCodeAt(i) + ((hash << 5) - hash);
            }
            return colors[Math.abs(hash) % colors.length];
        },

        // 根据网站方名称生成颜色名称
        getWebsiteColor(websiteName) {
            if (!websiteName) return 'teal';
            // 预定义的网站方颜色映射
            const colorMap = {
                '星辰娱乐': 'indigo',
                '皇冠体育': 'rose',
                '金沙娱乐': 'amber',
                '银河娱乐': 'purple',
                '永利娱乐': 'emerald',
                '威尼斯人': 'pink',
                '澳门娱乐': 'cyan',
                '新濠天地': 'orange'
            };
            if (colorMap[websiteName]) return colorMap[websiteName];
            // 如果没有预定义，根据名称生成一个稳定的颜色
            const colors = ['teal', 'indigo', 'purple', 'pink', 'rose', 'orange', 'amber', 'lime', 'emerald', 'cyan', 'sky', 'blue'];
            let hash = 0;
            for (let i = 0; i < websiteName.length; i++) {
                hash = websiteName.charCodeAt(i) + ((hash << 5) - hash);
            }
            return colors[Math.abs(hash) % colors.length];
        },

        // 批量添加商户
        openBatchMerchantModal() {
            this.batchMerchantText = '';
            this.batchMerchantResult = null;
            this.showBatchMerchantModal = true;
        },

        parseBatchMerchantText() {
            // 完整字段顺序（与表格一致）: 
            // 1.项目, 2.环境, 3.网站方, 4.对接邮箱, 5.网站方网址, 6.玩家地区, 7.预计玩家, 8.游戏种类, 
            // 9.盘口, 10.语言, 11.币种, 12.支持端口, 13.钱包类型, 14.三方回调域名, 15.三方白名单, 
            // 16.厅房域名, 17.站点系统域名, 18.站点账号, 19.AppKey, 20.游戏域名, 21.301域名
            // 空值: 连续的分隔符表示空，如: 项目,,网站方 表示环境为空
            const lines = this.batchMerchantText.trim().split('\n').filter(l => l.trim() && !l.startsWith('#'));
            const merchants = [];
            for (const line of lines) {
                let parts;
                if (line.includes('\t')) parts = line.split('\t');
                else if (line.includes('|')) parts = line.split('|');
                else parts = line.split(',');
                parts = parts.map(p => p.trim());
                if (parts.length < 3 || !parts[2]) continue; // 至少需要项目、环境、网站方
                const toArr = (s) => s ? JSON.stringify(s.split(';').map(x => x.trim()).filter(x => x)) : '[]';
                // 环境值转换为小写存储（支持大写输入）
                const envValue = (parts[1] || 'prod').toLowerCase();
                merchants.push({
                    project: parts[0] || '',
                    env: envValue,
                    website_name: parts[2] || '',
                    contact_emails: toArr(parts[3]),
                    website_urls: toArr(parts[4]),
                    player_regions: toArr(parts[5]),
                    estimated_players: parts[6] || '',
                    game_types: toArr(parts[7]),
                    handicaps: toArr(parts[8]),
                    languages: toArr(parts[9]),
                    currencies: toArr(parts[10]),
                    supported_ports: toArr(parts[11]),
                    wallet_types: toArr(parts[12]),
                    callback_domains: toArr(parts[13]),
                    whitelist_ips: parts[14] || '',
                    hall_domains: toArr(parts[15]),
                    site_domains: toArr(parts[16]),
                    site_accounts: toArr(parts[17]),
                    app_keys: toArr(parts[18]),
                    game_domains: toArr(parts[19]),
                    redirect_domains: toArr(parts[20]),
                    app_secrets: '', remark: '', status: 'active'
                });
            }
            return merchants;
        },

        async executeBatchMerchant() {
            const merchants = this.parseBatchMerchantText();
            if (merchants.length === 0) { this.showToast('请输入至少一条商户', 'error'); return; }
            this.batchMerchantLoading = true;
            this.batchMerchantResult = null;
            try {
                const res = await API.batchCreateMerchants(merchants);
                if (res.ok) {
                    this.batchMerchantResult = await res.json();
                    if (this.batchMerchantResult.fail_count === 0) {
                        this.showToast(`成功添加 ${this.batchMerchantResult.success_count} 个商户`, 'success');
                        this.showBatchMerchantModal = false;
                    } else {
                        // 有失败的，显示详细信息，不关闭弹窗
                        this.showToast(this.batchMerchantResult.message, 'warning');
                    }
                    await this.loadMerchants();
                } else { this.showToast('批量添加失败', 'error'); }
            } catch (e) { this.showToast('批量添加失败: ' + e.message, 'error'); }
            finally { this.batchMerchantLoading = false; }
        },

        fillBatchMerchantExample() {
            this.batchMerchantText = `# 字段顺序（21个字段，空值用连续逗号表示）:
# 1.项目, 2.环境(PROD/UAT/DEV), 3.网站方, 4.对接邮箱, 5.网站方网址, 6.玩家地区, 7.预计玩家, 8.游戏种类, 9.盘口, 10.语言, 11.币种, 12.支持端口, 13.钱包类型, 14.三方回调域名, 15.三方白名单, 16.厅房域名, 17.站点系统域名, 18.站点账号, 19.AppKey, 20.游戏域名, 21.301域名
# 多个值用 ; 分号分隔，空值直接留空（连续逗号）
星辰项目, PROD, 星辰娱乐, contact@star.com;tech@star.com, www.star.com;m.star.com, 中国;菲律宾, 5000, 真人;体育;电竞, A盘;B盘, 中文;英语, CNY;USDT, H5;APP;PC, 转账钱包, api.star.com, 1.1.1.1;2.2.2.2, hall.star.com, site.star.com, admin, key123, game.star.com, jump.star.com
皇冠项目, PROD, 皇冠体育, admin@crown.com, www.crown.com, 泰国;印尼, 3000, 体育;真人, A盘, 中文;泰语, THB;USDT, H5;APP, 单一钱包, , , , , , , ,
金沙项目, UAT, 金沙娱乐, test@sands.com, test.sands.com, 中国, 2000, 真人;电子, A盘, 中文, CNY, H5;PC, 转账钱包, , , , , , , ,`;
        },

        // ========== 服务配置管理 ==========
        async loadServiceConfigs() {
            try {
                const res = await API.getServiceConfigs(this.serviceConfigProjectFilter, this.serviceConfigEnvFilter, this.serviceConfigTypeFilter);
                if (res.ok) {
                    this.serviceConfigs = await res.json() || [];
                } else {
                    this.showToast('获取服务配置失败', 'error');
                }
            } catch (e) {
                this.showToast('获取服务配置失败: ' + e.message, 'error');
            }
        },

        async loadServiceProjects() {
            try {
                const res = await API.getServiceProjects();
                if (res.ok) {
                    this.serviceProjects = await res.json() || [];
                }
            } catch (e) {
                console.error('获取项目列表失败:', e);
            }
        },

        async loadServiceConfigData() {
            await this.loadServiceProjects();
            await this.loadServiceConfigs();
        },

        openServiceConfigModal(config = null) {
            if (config) {
                this.editServiceConfigMode = true;
                this.serviceConfigForm = { ...config, dependencies: config.dependencies ? config.dependencies.map(d => ({...d})) : [] };
            } else {
                this.editServiceConfigMode = false;
                this.serviceConfigForm = {
                    id: '', project: '', service_name: '', service_type: 'backend', domain: '', port: '',
                    env: 'prod', namespace: '', replicas: 1, image: '', remark: '', status: 'active', sort_order: 0,
                    dependencies: []
                };
            }
            this.showServiceConfigModal = true;
        },

        addFormDependency() {
            this.serviceConfigForm.dependencies.push({
                dependency_type: 'mysql', dependency_name: '', host: '', port: '',
                database: '', username: '', password: '', conn_string: '', remark: '', status: 'active'
            });
        },

        removeFormDependency(index) {
            this.serviceConfigForm.dependencies.splice(index, 1);
        },

        getDepDefaultPort(type) {
            const ports = { 'mysql': '3306', 'redis': '6379', 'mongodb': '27017', 'elasticsearch': '9200', 'mq': '5672' };
            return ports[type] || '';
        },

        onDepTypeChange(dep) {
            if (!dep.port) {
                dep.port = this.getDepDefaultPort(dep.dependency_type);
            }
        },

        async saveServiceConfig() {
            if (!this.serviceConfigForm.service_name) {
                this.showToast('请输入服务名称', 'error');
                return;
            }

            try {
                let res;
                if (this.editServiceConfigMode) {
                    res = await API.updateServiceConfig(this.serviceConfigForm.id, this.serviceConfigForm);
                } else {
                    res = await API.createServiceConfig(this.serviceConfigForm);
                }

                if (res.ok) {
                    this.showServiceConfigModal = false;
                    this.showToast(this.editServiceConfigMode ? '服务配置已更新' : '服务配置已创建', 'success');
                    await this.loadServiceConfigData();
                } else {
                    const err = await res.text();
                    this.showToast(err || '保存失败', 'error');
                }
            } catch (e) {
                this.showToast('保存失败: ' + e.message, 'error');
            }
        },

        async deleteServiceConfig(config) {
            const confirmed = await this.showConfirm({
                type: 'danger',
                title: '删除服务配置',
                message: `确定要删除服务 "${config.service_name}" 及其所有依赖吗？\n此操作不可恢复。`,
                okText: '删除',
                cancelText: '取消'
            });
            if (!confirmed) return;

            try {
                const res = await API.deleteServiceConfig(config.id);
                if (res.ok) {
                    this.showToast('服务配置已删除', 'success');
                    if (this.currentServiceForDeps && this.currentServiceForDeps.id === config.id) {
                        this.showServiceDepsPanel = false;
                        this.currentServiceForDeps = null;
                    }
                    await this.loadServiceConfigData();
                } else {
                    this.showToast('删除失败', 'error');
                }
            } catch (e) {
                this.showToast('删除失败: ' + e.message, 'error');
            }
        },

        // 服务依赖管理
        async openServiceDeps(config) {
            this.currentServiceForDeps = config;
            this.showServiceDepsPanel = true;
            await this.loadServiceDeps(config.id);
        },

        async loadServiceDeps(serviceId) {
            try {
                const res = await API.getServiceDependencies(serviceId);
                if (res.ok) {
                    this.serviceDeps = await res.json() || [];
                }
            } catch (e) {
                this.showToast('获取依赖列表失败', 'error');
            }
        },

        openServiceDepModal(dep = null) {
            if (dep) {
                this.editServiceDepMode = true;
                this.serviceDepForm = { ...dep, password: '' }; // 密码不回填
            } else {
                this.editServiceDepMode = false;
                this.serviceDepForm = {
                    id: '', dependency_type: 'mysql', dependency_name: '', host: '', port: '',
                    database: '', username: '', password: '', conn_string: '', remark: '', status: 'active'
                };
            }
            this.showServiceDepModal = true;
        },

        async saveServiceDep() {
            if (!this.serviceDepForm.dependency_name) {
                this.showToast('请输入依赖名称', 'error');
                return;
            }
            if (!this.currentServiceForDeps) return;

            try {
                let res;
                if (this.editServiceDepMode) {
                    res = await API.updateServiceDependency(this.currentServiceForDeps.id, this.serviceDepForm.id, this.serviceDepForm);
                } else {
                    res = await API.createServiceDependency(this.currentServiceForDeps.id, this.serviceDepForm);
                }

                if (res.ok) {
                    this.showServiceDepModal = false;
                    this.showToast(this.editServiceDepMode ? '依赖已更新' : '依赖已添加', 'success');
                    await this.loadServiceDeps(this.currentServiceForDeps.id);
                    await this.loadServiceConfigs(); // 刷新主列表的依赖数
                } else {
                    const err = await res.text();
                    this.showToast(err || '保存失败', 'error');
                }
            } catch (e) {
                this.showToast('保存失败: ' + e.message, 'error');
            }
        },

        async deleteServiceDep(dep) {
            const confirmed = await this.showConfirm({
                type: 'warning',
                title: '删除依赖',
                message: `确定要删除依赖 "${dep.dependency_name}" 吗？`,
                okText: '删除',
                cancelText: '取消'
            });
            if (!confirmed) return;
            if (!this.currentServiceForDeps) return;

            try {
                const res = await API.deleteServiceDependency(this.currentServiceForDeps.id, dep.id);
                if (res.ok) {
                    this.showToast('依赖已删除', 'success');
                    await this.loadServiceDeps(this.currentServiceForDeps.id);
                    await this.loadServiceConfigs();
                } else {
                    this.showToast('删除失败', 'error');
                }
            } catch (e) {
                this.showToast('删除失败: ' + e.message, 'error');
            }
        },

        async showDepPassword(dep) {
            if (!this.currentServiceForDeps) return;
            try {
                const res = await API.getServiceDepPassword(this.currentServiceForDeps.id, dep.id);
                if (res.ok) {
                    const data = await res.json();
                    this.serviceDepShowPassword = { ...this.serviceDepShowPassword, [dep.id]: data.password || '(空)' };
                }
            } catch (e) {
                this.showToast('获取密码失败', 'error');
            }
        },

        hideDepPassword(depId) {
            const updated = { ...this.serviceDepShowPassword };
            delete updated[depId];
            this.serviceDepShowPassword = updated;
        },

        getServiceTypeLabel(type) {
            const labels = {
                'web': '前端/Web', 'backend': '后端服务', 'gateway': 'API网关',
                'middleware': '中间件', 'database': '数据库', 'cache': '缓存',
                'mq': '消息队列', 'third_party': '第三方服务'
            };
            return labels[type] || type;
        },

        getServiceTypeIcon(type) {
            const icons = {
                'web': '🌐', 'backend': '⚙️', 'gateway': '🚪',
                'middleware': '🔧', 'database': '🗄️', 'cache': '⚡',
                'mq': '📨', 'third_party': '🔗'
            };
            return icons[type] || '📦';
        },

        getDepTypeLabel(type) {
            const labels = {
                'mysql': 'MySQL', 'redis': 'Redis', 'mongodb': 'MongoDB',
                'elasticsearch': 'ES', 'mq': 'MQ', 'api': 'API',
                'third_party': '第三方', 'other': '其他'
            };
            return labels[type] || type;
        },

        getDepTypeIcon(type) {
            const icons = {
                'mysql': '🐬', 'redis': '🔴', 'mongodb': '🍃',
                'elasticsearch': '🔍', 'mq': '📨', 'api': '🔌',
                'third_party': '🌐', 'other': '📎'
            };
            return icons[type] || '📎';
        },

        // 批量添加服务
        openBatchServiceModal() {
            this.batchServiceText = '';
            this.batchServiceResult = null;
            this.showBatchServiceModal = true;
        },

        parseBatchServiceText() {
            // 服务行: 服务名称,类型,项目,域名,端口,环境,备注
            // 依赖行: -> 类型,名称,地址,端口,数据库名,用户名
            // 支持逗号、Tab、| 三种分隔符
            const lines = this.batchServiceText.trim().split('\n').filter(l => l.trim() && !l.startsWith('#'));
            const services = [];
            let currentService = null;

            const splitLine = (str) => {
                if (str.includes('\t')) return str.split('\t');
                if (str.includes(',')) return str.split(',');
                if (str.includes('|')) return str.split('|');
                return [str];
            };

            for (const line of lines) {
                const trimmed = line.trim();
                if (trimmed.startsWith('->') || trimmed.startsWith('→')) {
                    // 依赖行
                    if (!currentService) continue;
                    const depLine = trimmed.replace(/^(->|→)\s*/, '');
                    const parts = splitLine(depLine).map(p => p.trim());
                    if (parts.length < 2 || !parts[1]) continue;
                    currentService.dependencies.push({
                        dependency_type: parts[0] || 'other',
                        dependency_name: parts[1] || '',
                        host: parts[2] || '',
                        port: parts[3] || '',
                        database: parts[4] || '',
                        username: parts[5] || '',
                        password: '',
                        conn_string: '',
                        remark: '',
                        status: 'active'
                    });
                } else {
                    // 服务行
                    const parts = splitLine(trimmed).map(p => p.trim());
                    if (parts.length < 1 || !parts[0]) continue;
                    currentService = {
                        service_name: parts[0] || '',
                        service_type: parts[1] || 'backend',
                        project: parts[2] || '',
                        domain: parts[3] || '',
                        port: parts[4] || '',
                        env: parts[5] || 'prod',
                        remark: parts[6] || '',
                        namespace: '',
                        image: '',
                        replicas: 1,
                        status: 'active',
                        sort_order: 0,
                        dependencies: []
                    };
                    services.push(currentService);
                }
            }
            return services;
        },

        getBatchDepCount() {
            return this.parseBatchServiceText().reduce((sum, s) => sum + (s.dependencies ? s.dependencies.length : 0), 0);
        },

        async executeBatchService() {
            const services = this.parseBatchServiceText();
            if (services.length === 0) {
                this.showToast('请输入至少一条服务信息', 'error');
                return;
            }

            this.batchServiceLoading = true;
            this.batchServiceResult = null;
            try {
                const res = await API.batchCreateServiceConfigs(services);
                if (res.ok) {
                    this.batchServiceResult = await res.json();
                    if (this.batchServiceResult.fail_count === 0) {
                        this.showToast(`成功添加 ${this.batchServiceResult.success_count} 个服务`, 'success');
                        this.showBatchServiceModal = false;
                    } else {
                        this.showToast(this.batchServiceResult.message, 'warning');
                    }
                    await this.loadServiceConfigData();
                } else {
                    const err = await res.text();
                    this.showToast(err || '批量添加失败', 'error');
                }
            } catch (e) {
                this.showToast('批量添加失败: ' + e.message, 'error');
            } finally {
                this.batchServiceLoading = false;
            }
        },

        getFilteredServiceConfigs() {
            let list = this.serviceConfigs || [];
            if (this.serviceConfigSearch) {
                const q = this.serviceConfigSearch.toLowerCase();
                list = list.filter(s =>
                    s.service_name.toLowerCase().includes(q) ||
                    (s.project || '').toLowerCase().includes(q) ||
                    (s.domain || '').toLowerCase().includes(q) ||
                    (s.image || '').toLowerCase().includes(q)
                );
            }
            return list;
        },

        getServiceConfigTotalPages() {
            return Math.ceil(this.getFilteredServiceConfigs().length / this.serviceConfigPageSize) || 1;
        },

        getPagedServiceConfigs() {
            const start = (this.serviceConfigCurrentPage - 1) * this.serviceConfigPageSize;
            return this.getFilteredServiceConfigs().slice(start, start + this.serviceConfigPageSize);
        },

        // ========== 网络架构图 ==========
        getTopoFilteredServices() {
            let list = this.serviceConfigs || [];
            if (this.topoProjectFilter) list = list.filter(s => s.project === this.topoProjectFilter);
            if (this.topoEnvFilter) list = list.filter(s => s.env === this.topoEnvFilter);
            return list;
        },

        async loadTopologyData() {
            this.topoLoading = true;
            try {
                await this.loadServiceProjects();
                await this.loadServiceConfigs();
                this.$nextTick(() => {
                    this.renderTopology();
                    this.topoLoading = false;
                });
            } catch (e) {
                this.showToast('加载数据失败', 'error');
                this.topoLoading = false;
            }
        },

        renderTopology() {
            const services = this.getTopoFilteredServices();
            if (services.length === 0) {
                this.topoNodes = [];
                this.topoLines = [];
                this.topoLayers = [];
                return;
            }

            // 分层: 按服务类型分组
            const layerOrder = ['web', 'gateway', 'backend', 'middleware', 'cache', 'mq', 'database', 'third_party'];
            const layerLabels = {
                'web': '🌐 前端 / Web 层',
                'gateway': '🚪 API 网关层',
                'backend': '⚙️ 后端服务层',
                'middleware': '🔧 中间件层',
                'cache': '⚡ 缓存层',
                'mq': '📨 消息队列层',
                'database': '🗄️ 数据库层',
                'third_party': '🔗 第三方服务'
            };

            // 按层分组服务
            const layerMap = {};
            services.forEach(svc => {
                const layer = svc.service_type || 'backend';
                if (!layerMap[layer]) layerMap[layer] = [];
                layerMap[layer].push(svc);
            });

            // 收集所有依赖节点（这些不在服务列表中的外部依赖）
            const depNodes = {};
            services.forEach(svc => {
                if (!svc.dependencies) return;
                svc.dependencies.forEach(dep => {
                    const depKey = dep.dependency_type + ':' + dep.host + ':' + dep.port;
                    // 检查是否已经作为同环境服务存在
                    const existsAsService = services.some(s => 
                        (!svc.env || !s.env || svc.env === s.env) &&
                        ((s.domain && s.domain === dep.host) || (s.service_name === dep.dependency_name))
                    );
                    if (!existsAsService && !depNodes[depKey]) {
                        depNodes[depKey] = {
                            id: 'dep_' + depKey,
                            name: dep.dependency_name,
                            type: dep.dependency_type,
                            host: dep.host,
                            port: dep.port,
                            database: dep.database,
                            fromServices: [svc.id]
                        };
                    } else if (depNodes[depKey]) {
                        depNodes[depKey].fromServices.push(svc.id);
                    }
                });
            });

            // 将依赖节点归入对应层
            Object.values(depNodes).forEach(dn => {
                let layer = dn.type;
                if (layer === 'redis') layer = 'cache';
                if (layer === 'mongodb' || layer === 'elasticsearch') layer = 'database';
                if (layer === 'api') layer = 'third_party';
                if (!layerMap[layer]) layerMap[layer] = [];
                layerMap[layer].push({
                    id: dn.id,
                    service_name: dn.name,
                    service_type: dn.type,
                    domain: dn.host,
                    port: dn.port,
                    _isDepNode: true,
                    _depData: dn
                });
            });

            // 布局参数
            const nodeW = 200;
            const nodeH = 60;
            const layerGap = 120;
            const nodeGap = 40;
            const paddingLeft = 160;
            const paddingTop = 40;

            const nodes = [];
            const layers = [];
            let currentY = paddingTop;

            // 服务类型图标和颜色
            const typeIcons = {
                'web': '🌐', 'gateway': '🚪', 'backend': '⚙️', 'middleware': '🔧',
                'database': '🗄️', 'cache': '⚡', 'mq': '📨', 'third_party': '🔗',
                'mysql': '🐬', 'redis': '🔴', 'mongodb': '🍃', 'elasticsearch': '🔍'
            };

            const nodePositions = {}; // id -> {x, y, w, h}

            // 按层级顺序排列
            layerOrder.forEach(layerType => {
                const items = layerMap[layerType];
                if (!items || items.length === 0) return;

                layers.push({
                    id: layerType,
                    label: layerLabels[layerType] || layerType,
                    y: currentY
                });

                const totalWidth = items.length * nodeW + (items.length - 1) * nodeGap;
                const startX = paddingLeft + Math.max(0, (1000 - totalWidth) / 2);

                items.forEach((svc, idx) => {
                    const x = startX + idx * (nodeW + nodeGap);
                    const y = currentY + 10;
                    const id = svc.id;
                    const icon = typeIcons[svc.service_type] || '📦';
                    let detail = '';
                    if (svc.domain) detail = svc.domain;
                    else if (svc.port) detail = ':' + svc.port;

                    const depCount = svc.dependencies ? svc.dependencies.length : 0;

                    nodes.push({
                        id: id,
                        name: svc.service_name,
                        type: svc._isDepNode ? svc.service_type : svc.service_type,
                        icon: icon,
                        detail: detail,
                        depCount: depCount,
                        x: x,
                        y: y,
                        w: nodeW,
                        // 原始数据
                        project: svc.project,
                        domain: svc.domain,
                        port: svc.port,
                        env: svc.env,
                        namespace: svc.namespace,
                        image: svc.image,
                        deps: svc.dependencies || []
                    });

                    nodePositions[id] = { x: x, y: y, w: nodeW, h: nodeH };
                });

                currentY += nodeH + layerGap;
            });

            // 生成连线
            const lines = [];
            const lineColors = {
                'mysql': '#f5222d', 'redis': '#eb2f96', 'mongodb': '#52c41a',
                'elasticsearch': '#722ed1', 'mq': '#13c2c2', 'api': '#1890ff',
                'third_party': '#fa8c16', 'other': '#8c8c8c'
            };
            const arrowColors = {
                'mysql': 'red', 'redis': 'red', 'mongodb': 'green',
                'elasticsearch': 'blue', 'mq': 'orange', 'api': 'blue',
                'third_party': 'orange', 'other': 'blue'
            };

            let lineIdx = 0;
            services.forEach(svc => {
                if (!svc.dependencies) return;
                const fromPos = nodePositions[svc.id];
                if (!fromPos) return;

                svc.dependencies.forEach(dep => {
                    // 找目标节点
                    const depKey = dep.dependency_type + ':' + dep.host + ':' + dep.port;
                    const targetId = 'dep_' + depKey;
                    
                    // 先尝试匹配同环境的服务节点
                    let toPos = null;
                    let toId = null;
                    services.forEach(s => {
                        if (s.id !== svc.id && 
                            (!svc.env || !s.env || svc.env === s.env) &&
                            ((s.domain && s.domain === dep.host) || s.service_name === dep.dependency_name)) {
                            toPos = nodePositions[s.id];
                            toId = s.id;
                        }
                    });
                    if (!toPos && nodePositions[targetId]) {
                        toPos = nodePositions[targetId];
                        toId = targetId;
                    }
                    if (!toPos) return;

                    // 计算连线路径（贝塞尔曲线）
                    const fromCx = fromPos.x + fromPos.w / 2;
                    const fromCy = fromPos.y + fromPos.h;
                    const toCx = toPos.x + toPos.w / 2;
                    const toCy = toPos.y;

                    // 判断方向
                    let x1, y1, x2, y2;
                    if (fromCy < toCy) {
                        // 从上到下
                        x1 = fromCx; y1 = fromPos.y + fromPos.h;
                        x2 = toCx; y2 = toPos.y;
                    } else if (fromCy > toCy) {
                        // 从下到上
                        x1 = fromCx; y1 = fromPos.y;
                        x2 = toCx; y2 = toPos.y + toPos.h;
                    } else {
                        // 同层
                        if (fromCx < toCx) {
                            x1 = fromPos.x + fromPos.w; y1 = fromPos.y + fromPos.h / 2;
                            x2 = toPos.x; y2 = toPos.y + toPos.h / 2;
                        } else {
                            x1 = fromPos.x; y1 = fromPos.y + fromPos.h / 2;
                            x2 = toPos.x + toPos.w; y2 = toPos.y + toPos.h / 2;
                        }
                    }

                    const midY = (y1 + y2) / 2;
                    const ctrlOffset = Math.abs(y2 - y1) * 0.4 + 20;
                    const path = `M ${x1} ${y1} C ${x1} ${y1 + ctrlOffset}, ${x2} ${y2 - ctrlOffset}, ${x2} ${y2}`;

                    const color = lineColors[dep.dependency_type] || '#8c8c8c';
                    const arrowC = arrowColors[dep.dependency_type] || 'blue';

                    lines.push({
                        id: 'line_' + lineIdx++,
                        path: path,
                        color: color,
                        arrowColor: arrowC,
                        dashed: dep.dependency_type === 'third_party' || dep.dependency_type === 'api',
                        label: dep.dependency_name,
                        labelX: (x1 + x2) / 2,
                        labelY: midY - 6,
                        fromId: svc.id,
                        toId: toId
                    });
                });
            });

            this.topoNodes = nodes;
            this.topoLines = lines;
            this.topoLayers = layers;
            this.topoCanvasW = Math.max(1600, paddingLeft + 1200);
            this.topoCanvasH = Math.max(800, currentY + 60);
        },

        topoClickNode(node) {
            this.topoSelectedNode = this.topoSelectedNode && this.topoSelectedNode.id === node.id ? null : node;
        },

        topoStartDrag(e) {
            if (e.target.closest('.topo-node')) return;
            this.topoDragging = true;
            this.topoDragStartX = e.clientX - this.topoPanX;
            this.topoDragStartY = e.clientY - this.topoPanY;
            const onMove = (ev) => {
                if (!this.topoDragging) return;
                this.topoPanX = ev.clientX - this.topoDragStartX;
                this.topoPanY = ev.clientY - this.topoDragStartY;
            };
            const onUp = () => {
                this.topoDragging = false;
                document.removeEventListener('mousemove', onMove);
                document.removeEventListener('mouseup', onUp);
            };
            document.addEventListener('mousemove', onMove);
            document.addEventListener('mouseup', onUp);
        },

        topoWheel(e) {
            const delta = e.deltaY > 0 ? -0.08 : 0.08;
            this.topoZoom = Math.max(0.3, Math.min(2, this.topoZoom + delta));
        },

        // ========== Kubernetes 管理 ==========
        async loadK8sNamespaces() {
            try {
                const res = await API.getK8sNamespaces();
                if (res.ok) {
                    this.k8sNamespaces = await res.json() || [];
                } else {
                    this.showToast('获取命名空间失败', 'error');
                }
            } catch (e) {
                this.showToast('获取命名空间失败: ' + e.message, 'error');
            }
        },

        async loadK8sDeployments() {
            this.k8sLoading = true;
            try {
                const res = await API.getK8sDeployments(this.k8sSelectedNamespace);
                if (res.ok) {
                    this.k8sDeployments = await res.json() || [];
                } else {
                    this.showToast('获取部署列表失败', 'error');
                }
            } catch (e) {
                this.showToast('获取部署列表失败: ' + e.message, 'error');
            } finally {
                this.k8sLoading = false;
            }
        },

        async loadK8sServices() {
            this.k8sLoading = true;
            try {
                const res = await API.getK8sServices(this.k8sSelectedNamespace);
                if (res.ok) {
                    this.k8sServices = await res.json() || [];
                } else {
                    this.showToast('获取服务列表失败', 'error');
                }
            } catch (e) {
                this.showToast('获取服务列表失败: ' + e.message, 'error');
            } finally {
                this.k8sLoading = false;
            }
        },

        async loadK8sPods() {
            this.k8sLoading = true;
            try {
                const res = await API.getK8sPods(this.k8sSelectedNamespace);
                if (res.ok) {
                    this.k8sPods = await res.json() || [];
                } else {
                    this.showToast('获取 Pod 列表失败', 'error');
                }
            } catch (e) {
                this.showToast('获取 Pod 列表失败: ' + e.message, 'error');
            } finally {
                this.k8sLoading = false;
            }
        },

        async loadK8sNodes() {
            this.k8sLoading = true;
            try {
                const res = await API.getK8sNodes();
                if (res.ok) {
                    this.k8sNodes = await res.json() || [];
                } else {
                    this.showToast('获取节点列表失败', 'error');
                }
            } catch (e) {
                this.showToast('获取节点列表失败: ' + e.message, 'error');
            } finally {
                this.k8sLoading = false;
            }
        },

        async loadK8sData() {
            await this.loadK8sNamespaces();
            switch (this.k8sSubTab) {
                case 'deployments':
                    await this.loadK8sDeployments();
                    break;
                case 'services':
                    await this.loadK8sServices();
                    break;
                case 'pods':
                    await this.loadK8sPods();
                    break;
                case 'nodes':
                    await this.loadK8sNodes();
                    break;
            }
        },

        async changeK8sNamespace() {
            this.k8sCurrentPage = 1;
            switch (this.k8sSubTab) {
                case 'deployments':
                    await this.loadK8sDeployments();
                    break;
                case 'services':
                    await this.loadK8sServices();
                    break;
                case 'pods':
                    await this.loadK8sPods();
                    break;
            }
        },

        async changeK8sSubTab(tab) {
            this.k8sSubTab = tab;
            this.k8sCurrentPage = 1;
            await this.loadK8sData();
        },

        // K8s Apply
        openK8sApplyModal() {
            this.k8sApplyYaml = '';
            this.k8sApplyNamespace = this.k8sSelectedNamespace;
            this.k8sApplyResult = null;
            this.showK8sApplyModal = true;
        },

        async executeK8sApply() {
            if (!this.k8sApplyYaml.trim()) {
                this.showToast('请输入 YAML 内容', 'error');
                return;
            }

            this.k8sApplyLoading = true;
            this.k8sApplyResult = null;
            try {
                const res = await API.k8sApply('', this.k8sApplyYaml, this.k8sApplyNamespace);
                if (res.ok) {
                    this.k8sApplyResult = await res.json();
                    if (this.k8sApplyResult.success) {
                        this.showToast('Apply 成功', 'success');
                        await this.loadK8sData();
                    } else {
                        this.showToast('Apply 失败', 'error');
                    }
                } else {
                    this.showToast('Apply 请求失败', 'error');
                }
            } catch (e) {
                this.showToast('Apply 失败: ' + e.message, 'error');
            } finally {
                this.k8sApplyLoading = false;
            }
        },

        // K8s Restart
        async restartDeployment(dep) {
            const confirmed = await this.showConfirm({
                type: 'warning',
                title: '重启部署',
                message: `确定要重启部署 "${dep.name}" 吗？\n这将导致服务短暂不可用。`,
                okText: '重启',
                cancelText: '取消'
            });
            if (!confirmed) return;

            try {
                const res = await API.k8sRestart(dep.namespace || this.k8sSelectedNamespace, dep.name);
                if (res.ok) {
                    const result = await res.json();
                    if (result.success) {
                        this.showToast('重启命令已发送', 'success');
                        setTimeout(() => this.loadK8sDeployments(), 2000);
                    } else {
                        this.showToast(result.message || '重启失败', 'error');
                    }
                } else {
                    this.showToast('重启失败', 'error');
                }
            } catch (e) {
                this.showToast('重启失败: ' + e.message, 'error');
            }
        },

        // K8s Scale
        openK8sScaleModal(dep) {
            this.k8sScaleDeployment = dep;
            this.k8sScaleReplicas = dep.replicas || 1;
            this.showK8sScaleModal = true;
        },

        async executeK8sScale() {
            if (!this.k8sScaleDeployment) return;

            try {
                const res = await API.k8sScale(
                    this.k8sScaleDeployment.namespace || this.k8sSelectedNamespace,
                    this.k8sScaleDeployment.name,
                    this.k8sScaleReplicas
                );
                if (res.ok) {
                    const result = await res.json();
                    if (result.success) {
                        this.showToast('扩缩容成功', 'success');
                        this.showK8sScaleModal = false;
                        await this.loadK8sDeployments();
                    } else {
                        this.showToast(result.message || '扩缩容失败', 'error');
                    }
                } else {
                    this.showToast('扩缩容失败', 'error');
                }
            } catch (e) {
                this.showToast('扩缩容失败: ' + e.message, 'error');
            }
        },

        // K8s Update Image
        openK8sImageModal(dep) {
            this.k8sImageDeployment = dep;
            // 从 images 中解析容器和镜像
            const images = (dep.images || '').split(', ');
            if (images.length > 0) {
                const parts = images[0].split(':');
                const imageName = parts.slice(0, -1).join(':') || images[0];
                const tag = parts[parts.length - 1] || 'latest';
                // 从镜像名推断容器名（通常是最后一段）
                const containerName = imageName.split('/').pop().split(':')[0];
                this.k8sImageContainer = containerName;
                this.k8sImageTag = tag;
            }
            this.showK8sImageModal = true;
        },

        async executeK8sUpdateImage() {
            if (!this.k8sImageDeployment || !this.k8sImageContainer || !this.k8sImageTag) {
                this.showToast('请填写完整信息', 'error');
                return;
            }

            try {
                // 构建完整镜像路径
                const images = (this.k8sImageDeployment.images || '').split(', ');
                let fullImage = '';
                if (images.length > 0) {
                    const parts = images[0].split(':');
                    const imageName = parts.slice(0, -1).join(':') || images[0];
                    fullImage = imageName + ':' + this.k8sImageTag;
                }

                const res = await API.k8sUpdateImage(
                    this.k8sImageDeployment.namespace || this.k8sSelectedNamespace,
                    this.k8sImageDeployment.name,
                    this.k8sImageContainer,
                    fullImage
                );
                if (res.ok) {
                    const result = await res.json();
                    if (result.success) {
                        this.showToast('镜像更新成功', 'success');
                        this.showK8sImageModal = false;
                        await this.loadK8sDeployments();
                    } else {
                        this.showToast(result.message || '更新失败', 'error');
                    }
                } else {
                    this.showToast('更新失败', 'error');
                }
            } catch (e) {
                this.showToast('更新失败: ' + e.message, 'error');
            }
        },

        // K8s View YAML
        async viewDeploymentYaml(dep) {
            this.k8sYamlName = dep.name;
            this.k8sYamlContent = '加载中...';
            this.showK8sYamlModal = true;

            try {
                const res = await API.getK8sDeploymentYaml(dep.name, dep.namespace || this.k8sSelectedNamespace);
                if (res.ok) {
                    this.k8sYamlContent = res.text;
                } else {
                    this.k8sYamlContent = '获取 YAML 失败';
                }
            } catch (e) {
                this.k8sYamlContent = '获取 YAML 失败: ' + e.message;
            }
        },

        // K8s Pod Logs
        async viewPodLogs(pod) {
            this.k8sPodLogsName = pod.name;
            this.k8sPodLogsContent = '';
            this.k8sPodLogsLoading = true;
            this.showK8sPodLogsModal = true;

            try {
                const res = await API.getK8sPodLogs(pod.name, pod.namespace || this.k8sSelectedNamespace, '', this.k8sPodLogsTail);
                if (res.ok) {
                    this.k8sPodLogsContent = res.text || '(无日志)';
                } else {
                    this.k8sPodLogsContent = '获取日志失败';
                }
            } catch (e) {
                this.k8sPodLogsContent = '获取日志失败: ' + e.message;
            } finally {
                this.k8sPodLogsLoading = false;
            }
        },

        async refreshPodLogs() {
            if (!this.k8sPodLogsName) return;
            await this.viewPodLogs({ name: this.k8sPodLogsName, namespace: this.k8sSelectedNamespace });
        },

        // K8s Delete Pod
        async deletePod(pod) {
            const confirmed = await this.showConfirm({
                type: 'danger',
                title: '删除 Pod',
                message: `确定要删除 Pod "${pod.name}" 吗？\nPod 删除后会由 Deployment 自动重建。`,
                okText: '删除',
                cancelText: '取消'
            });
            if (!confirmed) return;

            try {
                const res = await API.deleteK8sPod(pod.name, pod.namespace || this.k8sSelectedNamespace);
                if (res.ok) {
                    const result = await res.json();
                    if (result.success) {
                        this.showToast('Pod 已删除', 'success');
                        await this.loadK8sPods();
                    } else {
                        this.showToast(result.message || '删除失败', 'error');
                    }
                } else {
                    this.showToast('删除失败', 'error');
                }
            } catch (e) {
                this.showToast('删除失败: ' + e.message, 'error');
            }
        },

        // K8s Rollback
        async openK8sRollbackModal(dep) {
            this.k8sRollbackDeployment = dep;
            this.k8sRollbackRevision = 0;
            this.k8sRollbackHistory = '加载中...';
            this.showK8sRollbackModal = true;

            try {
                const res = await API.getK8sRolloutHistory(dep.name, dep.namespace || this.k8sSelectedNamespace);
                if (res.ok) {
                    this.k8sRollbackHistory = res.text || '(无历史记录)';
                } else {
                    this.k8sRollbackHistory = '获取历史失败';
                }
            } catch (e) {
                this.k8sRollbackHistory = '获取历史失败: ' + e.message;
            }
        },

        async executeK8sRollback() {
            if (!this.k8sRollbackDeployment) return;

            try {
                const res = await API.k8sRollback(
                    this.k8sRollbackDeployment.name,
                    this.k8sRollbackDeployment.namespace || this.k8sSelectedNamespace,
                    this.k8sRollbackRevision
                );
                if (res.ok) {
                    const result = await res.json();
                    if (result.success) {
                        this.showToast('回滚成功', 'success');
                        this.showK8sRollbackModal = false;
                        await this.loadK8sDeployments();
                    } else {
                        this.showToast(result.message || '回滚失败', 'error');
                    }
                } else {
                    this.showToast('回滚失败', 'error');
                }
            } catch (e) {
                this.showToast('回滚失败: ' + e.message, 'error');
            }
        },

        // K8s 过滤和分页
        getFilteredK8sDeployments() {
            let list = this.k8sDeployments || [];
            if (this.k8sSearchQuery) {
                const q = this.k8sSearchQuery.toLowerCase();
                list = list.filter(d => d.name.toLowerCase().includes(q) || (d.images || '').toLowerCase().includes(q));
            }
            return list;
        },

        getFilteredK8sServices() {
            let list = this.k8sServices || [];
            if (this.k8sSearchQuery) {
                const q = this.k8sSearchQuery.toLowerCase();
                list = list.filter(s => s.name.toLowerCase().includes(q) || s.cluster_ip.includes(q));
            }
            return list;
        },

        getFilteredK8sPods() {
            let list = this.k8sPods || [];
            if (this.k8sSearchQuery) {
                const q = this.k8sSearchQuery.toLowerCase();
                list = list.filter(p => p.name.toLowerCase().includes(q) || (p.ip || '').includes(q));
            }
            return list;
        },

        getFilteredK8sNodes() {
            let list = this.k8sNodes || [];
            if (this.k8sSearchQuery) {
                const q = this.k8sSearchQuery.toLowerCase();
                list = list.filter(n => n.name.toLowerCase().includes(q) || (n.internal_ip || '').includes(q));
            }
            return list;
        },

        getPodStatusClass(status) {
            const classes = {
                'Running': 'success',
                'Pending': 'warning',
                'Succeeded': 'success',
                'Failed': 'error',
                'Unknown': 'default'
            };
            return classes[status] || 'default';
        },

        getNodeStatusClass(status) {
            return status === 'Ready' ? 'success' : 'error';
        }
    }
});

// 挂载并保存到全局变量
window.app = vueApp.mount('#app');

