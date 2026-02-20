// API 调用封装（支持 HttpOnly Cookie 认证）
const API = {
    baseURL: '/api',
    csrfToken: null,
    sessionTimeout: 30,
    sessionExpiresAt: null,
    lastActivity: Date.now(),
    sessionCheckInterval: null,

    getOperator() {
        try {
            const user = JSON.parse(localStorage.getItem('currentUser'));
            return user ? user.username : 'system';
        } catch {
            return 'system';
        }
    },

    async getCSRFToken() {
        if (this.csrfToken) return this.csrfToken;
        try {
            const response = await fetch(this.baseURL + '/csrf-token', { credentials: 'include' });
            if (response.ok) {
                const data = await response.json();
                this.csrfToken = data.csrf_token;
                return this.csrfToken;
            }
        } catch (e) {
            console.warn('获取 CSRF Token 失败:', e);
        }
        return null;
    },

    clearCSRFToken() { this.csrfToken = null; },

    getToken() { return localStorage.getItem('authToken') || ''; },

    setToken(token) { if (token) localStorage.setItem('authToken', token); },

    clearToken() {
        localStorage.removeItem('authToken');
        this.csrfToken = null;
    },

    async request(method, url, data = null) {
        const headers = { 
            'Content-Type': 'application/json',
            'X-Operator': this.getOperator()
        };

        if (method === 'POST' || method === 'PUT' || method === 'DELETE') {
            const csrfToken = await this.getCSRFToken();
            if (csrfToken) headers['X-CSRF-Token'] = csrfToken;
            this.clearCSRFToken();
        }

        const token = this.getToken();
        if (token) headers['Authorization'] = 'Bearer ' + token;

        const options = { method, headers, credentials: 'include' };
        if (data) options.body = JSON.stringify(data);

        this.lastActivity = Date.now();
        const response = await fetch(this.baseURL + url, options);

        if (response.status === 401) {
            this.clearToken();
            localStorage.removeItem('currentUser');
            this.stopSessionCheck();
            window.location.href = '/login.html';
        }

        return response;
    },

    startSessionCheck(timeoutMinutes, expiresAt) {
        this.sessionTimeout = timeoutMinutes || 30;
        this.sessionExpiresAt = expiresAt ? new Date(expiresAt) : null;
        this.lastActivity = Date.now();
        if (this.sessionCheckInterval) clearInterval(this.sessionCheckInterval);
        this.sessionCheckInterval = setInterval(() => this.checkSession(), 60000);
        ['click', 'keydown', 'mousemove', 'scroll'].forEach(event => {
            document.addEventListener(event, () => { this.lastActivity = Date.now(); }, { passive: true });
        });
    },

    stopSessionCheck() {
        if (this.sessionCheckInterval) {
            clearInterval(this.sessionCheckInterval);
            this.sessionCheckInterval = null;
        }
    },

    checkSession() {
        const inactiveMinutes = (Date.now() - this.lastActivity) / 60000;
        const remainingMinutes = this.sessionTimeout - inactiveMinutes;
        if (remainingMinutes <= 5 && remainingMinutes > 0) {
            this.onSessionWarning && this.onSessionWarning(Math.ceil(remainingMinutes));
        }
        if (remainingMinutes <= 0) {
            this.onSessionExpired && this.onSessionExpired();
            this.stopSessionCheck();
        }
    },

    onSessionExpired: null,
    onSessionWarning: null,

    async getSessionInfo() { return this.request('GET', '/session/info'); },
    async refreshSession() {
        const response = await this.request('POST', '/session/refresh');
        if (response.ok) {
            const data = await response.json();
            if (data.expires_at) this.sessionExpiresAt = new Date(data.expires_at);
        }
        return response;
    },

    async login(username, password) {
        return fetch(this.baseURL + '/login', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username, password }),
            credentials: 'include'
        });
    },

    async logout() {
        const response = await this.request('POST', '/logout');
        this.clearToken();
        localStorage.removeItem('currentUser');
        this.stopSessionCheck();
        return response;
    },

    async getUsers() { return this.request('GET', '/users'); },
    async createUser(user, createdBy) { return this.request('POST', '/users', { user, created_by: createdBy }); },
    async updateUser(id, user) { return this.request('PUT', `/users/${id}`, user); },
    async deleteUser(id) { return this.request('DELETE', `/users/${id}`); },
    async changePassword(userId, newPassword, operator) { return this.request('PUT', `/users/${userId}/password`, { password: newPassword, operator }); },

    async getRecords() { return this.request('GET', '/records'); },
    async getRecordHistory(recordId) { return this.request('GET', `/records/${recordId}/history`); },
    async rollbackRecord(recordId, historyId, operator) { return this.request('POST', `/records/${recordId}/rollback`, { history_id: historyId, operator }); },
    async createRecord(record, operator) { return this.request('POST', '/records', { record, operator }); },
    async updateRecord(id, record, operator) { return this.request('PUT', `/records/${id}`, { record, operator }); },
    async deleteRecord(id, operator) { return this.request('DELETE', `/records/${id}`, { operator }); },
    async batchAddRecords(records, operator) { return this.request('POST', '/records/batch', { records, operator }); },
    async batchCheckRecords(records) { return this.request('POST', '/records/batch-check', { records }); },
    async batchDeleteRecords(ids, operator) { return this.request('POST', '/records/batch-delete', { ids, operator }); },
    async getGroupedRecords() { return this.request('GET', '/records/grouped'); },
    async searchByConnectionID(connectionID) { return this.request('GET', `/records/search-by-connid?connection_id=${encodeURIComponent(connectionID)}`); },

    async getDataSources() { return this.request('GET', '/datasources'); },
    async createDataSource(datasource, operator) { return this.request('POST', '/datasources', { datasource, operator }); },
    async updateDataSource(id, datasource) { return this.request('PUT', `/datasources/${id}`, datasource); },
    async deleteDataSource(id) { return this.request('DELETE', `/datasources/${id}`); },
    async testDataSource(datasource) { return this.request('POST', '/datasources/test', datasource); },

    async getAuditLogs() { return this.request('GET', '/audit-logs'); },

    async exportRecords(params = {}) {
        const query = new URLSearchParams(params).toString();
        const res = await this.request('GET', `/records/export${query ? '?' + query : ''}`);
        if (res.ok) {
            const blob = await res.blob();
            const link = document.createElement('a');
            link.href = URL.createObjectURL(blob);
            link.download = `records_${new Date().toISOString().slice(0,10)}.csv`;
            link.click();
            URL.revokeObjectURL(link.href);
        }
    },

    async exportAuditLogs() {
        const res = await this.request('GET', '/audit-logs/export');
        if (res.ok) {
            const blob = await res.blob();
            const link = document.createElement('a');
            link.href = URL.createObjectURL(blob);
            link.download = `audit_logs_${new Date().toISOString().slice(0,10)}.csv`;
            link.click();
            URL.revokeObjectURL(link.href);
        }
    },

    async exportDomains(params = {}) {
        const query = new URLSearchParams(params).toString();
        const res = await this.request('GET', `/domains/export${query ? '?' + query : ''}`);
        if (res.ok) {
            const blob = await res.blob();
            const link = document.createElement('a');
            link.href = URL.createObjectURL(blob);
            link.download = `domains_${new Date().toISOString().slice(0,10)}.csv`;
            link.click();
            URL.revokeObjectURL(link.href);
        }
    },

    async mfaSetup(userId) { return this.request('POST', '/mfa/setup', { user_id: userId }); },
    async mfaBind(userId, code) { return this.request('POST', '/mfa/bind', { user_id: userId, code }); },
    async mfaVerify(userId, code) {
        return fetch(this.baseURL + '/mfa/verify', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ user_id: userId, code }),
            credentials: 'include'
        });
    },
    async mfaDisable(userId, password) { return this.request('POST', '/mfa/disable', { user_id: userId, password }); },
    async mfaReset(userId) { return this.request('POST', '/mfa/reset', { user_id: userId }); },

    async getMetrics() { return this.request('GET', '/metrics'); },
    async createMetric(metric) { return this.request('POST', '/metrics', metric); },
    async updateMetric(metric) { return this.request('PUT', '/metrics', metric); },
    async deleteMetric(id) { return this.request('DELETE', `/metrics?id=${id}`); },
    async initDefaultMetrics() { return this.request('POST', '/metrics/init'); },

    async getDomains() { return this.request('GET', '/domains'); },
    async createDomain(domain) { return this.request('POST', '/domains', domain); },
    async updateDomain(id, domain) { return this.request('PUT', `/domains/${id}`, domain); },
    async deleteDomain(id) { return this.request('DELETE', `/domains/${id}`); },
    async batchDomains(ids, action, operator) { return this.request('POST', '/domains/batch', { ids, action, operator }); },
    async batchAddDomains(domains, createdBy, fetchExpiry = false) { return this.request('POST', '/domains/batch-add', { domains, created_by: createdBy, fetch_expiry: fetchExpiry }); },
    async checkDomainCert(domain) { return this.request('GET', `/domains/check-cert?domain=${encodeURIComponent(domain)}`); },

    async getSchedule(year, month) { return this.request('GET', `/schedule?year=${year}&month=${month}`); },
    async saveSchedule(employees) { return this.request('POST', '/schedule', employees); },
    async addScheduleEmployee(employee) { return this.request('POST', '/schedule/employee', employee); },
    async deleteScheduleEmployee(id) { return this.request('DELETE', `/schedule/employee?id=${id}`); },
    async updateShift(employeeId, date, shiftType) { return this.request('POST', '/schedule/shift', { employeeId, date, shiftType }); },
    async getShiftConfig() { return this.request('GET', '/schedule/config'); },
    async saveShiftConfig(configs) { return this.request('POST', '/schedule/config', configs); },

    // ===== 密码库管理 =====
    async getVaultStatus() { return this.request('GET', '/vault/status'); },
    async initVault(masterPassword) { return this.request('POST', '/vault/init', { master_password: masterPassword }); },
    async unlockVault(masterPassword) { return this.request('POST', '/vault/unlock', { master_password: masterPassword }); },

    async lockVault(sessionToken) {
        return fetch(this.baseURL + '/vault/lock', {
            method: 'POST',
            credentials: 'include',
            headers: { 'Content-Type': 'application/json', 'X-Vault-Session': sessionToken }
        });
    },

    async resetVaultPassword(recoveryKey, newMasterPassword) {
        return this.request('POST', '/vault/reset-password', { recovery_key: recoveryKey, new_master_password: newMasterPassword });
    },

    async getVaultItems(sessionToken) {
        return fetch(this.baseURL + '/vault/items', {
            method: 'GET',
            credentials: 'include',
            headers: { 'X-Vault-Session': sessionToken }
        });
    },

    async addVaultItem(sessionToken, item) {
        return fetch(this.baseURL + '/vault/items', {
            method: 'POST',
            credentials: 'include',
            headers: { 'Content-Type': 'application/json', 'X-Vault-Session': sessionToken },
            body: JSON.stringify(item)
        });
    },

    async updateVaultItem(sessionToken, id, item) {
        return fetch(this.baseURL + '/vault/items/' + id, {
            method: 'PUT',
            credentials: 'include',
            headers: { 'Content-Type': 'application/json', 'X-Vault-Session': sessionToken },
            body: JSON.stringify(item)
        });
    },

    async deleteVaultItem(sessionToken, id) {
        return fetch(this.baseURL + '/vault/items/' + id, {
            method: 'DELETE',
            credentials: 'include',
            headers: { 'X-Vault-Session': sessionToken }
        });
    },

    async getVaultFolders(sessionToken) {
        return fetch(this.baseURL + '/vault/folders', {
            method: 'GET',
            credentials: 'include',
            headers: { 'X-Vault-Session': sessionToken }
        });
    },

    async addVaultFolder(sessionToken, folder) {
        return fetch(this.baseURL + '/vault/folders', {
            method: 'POST',
            credentials: 'include',
            headers: { 'Content-Type': 'application/json', 'X-Vault-Session': sessionToken },
            body: JSON.stringify(folder)
        });
    },

    async updateVaultFolder(sessionToken, id, folder) {
        return fetch(this.baseURL + '/vault/folders/' + id, {
            method: 'PUT',
            credentials: 'include',
            headers: { 'Content-Type': 'application/json', 'X-Vault-Session': sessionToken },
            body: JSON.stringify(folder)
        });
    },

    async deleteVaultFolder(sessionToken, id) {
        return fetch(this.baseURL + '/vault/folders/' + id, {
            method: 'DELETE',
            credentials: 'include',
            headers: { 'X-Vault-Session': sessionToken }
        });
    },

    async generateVaultPassword(options = {}) {
        const params = new URLSearchParams({
            length: options.length || 16,
            upper: options.upper !== false,
            lower: options.lower !== false,
            numbers: options.numbers !== false,
            symbols: options.symbols !== false
        });
        return this.request('GET', '/vault/generate-password?' + params.toString());
    },

    // ========== 用户组管理 ==========
    async getVaultGroups() {
        return this.request('GET', '/vault/groups');
    },

    async addVaultGroup(group) {
        return this.request('POST', '/vault/groups', group);
    },

    async updateVaultGroup(id, group) {
        return this.request('PUT', '/vault/groups/' + id, group);
    },

    async deleteVaultGroup(id) {
        return this.request('DELETE', '/vault/groups/' + id);
    },

    async getVaultGroupMembers(groupId) {
        return this.request('GET', '/vault/groups/' + groupId + '/members');
    },

    async addVaultGroupMember(groupId, member) {
        return this.request('POST', '/vault/groups/' + groupId + '/members', member);
    },

    async removeVaultGroupMember(groupId, memberId) {
        return this.request('DELETE', '/vault/groups/' + groupId + '/members/' + memberId);
    },

    // ========== 分享管理 ==========
    async getVaultShares() {
        return this.request('GET', '/vault/shares');
    },

    async addVaultShare(share) {
        return this.request('POST', '/vault/shares', share);
    },

    async deleteVaultShare(id) {
        return this.request('DELETE', '/vault/shares/' + id);
    },

    async getVaultUsers() {
        return this.request('GET', '/vault/users');
    },

    // ========== 权限管理 (RBAC) ==========
    async getRoles() {
        return this.request('GET', '/roles');
    },

    async createRole(role) {
        return this.request('POST', '/roles', role);
    },

    async updateRole(id, role) {
        return this.request('PUT', '/roles/' + id, role);
    },

    async deleteRole(id) {
        return this.request('DELETE', '/roles/' + id);
    },

    async getRolePermissions(roleId) {
        return this.request('GET', '/roles/' + roleId + '/permissions');
    },

    async updateRolePermissions(roleId, permissionIds) {
        return this.request('PUT', '/roles/' + roleId + '/permissions', permissionIds);
    },

    async getPermissions(type = '') {
        const params = type ? '?type=' + type : '';
        return this.request('GET', '/permissions' + params);
    },

    async createPermission(permission) {
        return this.request('POST', '/permissions', permission);
    },

    async updatePermission(id, permission) {
        return this.request('PUT', '/permissions/' + id, permission);
    },

    async deletePermission(id) {
        return this.request('DELETE', '/permissions/' + id);
    },

    async getUserRoles(userId) {
        return this.request('GET', '/users/' + userId + '/roles');
    },

    async updateUserRoles(userId, roleIds) {
        return this.request('PUT', '/users/' + userId + '/roles', roleIds);
    },

    async getMyPermissions() {
        return this.request('GET', '/my/permissions');
    },

    // ========== 网站管理 ==========
    async getWebsites() {
        return this.request('GET', '/websites');
    },

    async createWebsite(website) {
        return this.request('POST', '/websites', website);
    },

    async updateWebsite(id, website) {
        return this.request('PUT', '/websites/' + id, website);
    },

    async deleteWebsite(id) {
        return this.request('DELETE', '/websites/' + id);
    },

    async getWebsitePassword(id) {
        return this.request('GET', '/websites/' + id + '/password');
    },

    // ========== 厅方管理 ==========
    async getWebsiteHalls(websiteId) {
        return this.request('GET', '/websites/' + websiteId + '/halls');
    },

    async createWebsiteHall(websiteId, hall) {
        return this.request('POST', '/websites/' + websiteId + '/halls', hall);
    },

    async updateWebsiteHall(websiteId, hallId, hall) {
        return this.request('PUT', '/websites/' + websiteId + '/halls/' + hallId, hall);
    },

    async deleteWebsiteHall(websiteId, hallId) {
        return this.request('DELETE', '/websites/' + websiteId + '/halls/' + hallId);
    },

    async getHallPassword(websiteId, hallId) {
        return this.request('GET', '/websites/' + websiteId + '/halls/' + hallId + '/password');
    },

    // ========== 任务池 ==========
    async getTasks(params = {}) {
        let p = [];
        if (params.status) p.push('status=' + encodeURIComponent(params.status));
        if (params.project) p.push('project=' + encodeURIComponent(params.project));
        if (params.assignee) p.push('assignee=' + encodeURIComponent(params.assignee));
        if (params.priority) p.push('priority=' + encodeURIComponent(params.priority));
        if (params.delayed !== undefined && params.delayed !== '') p.push('delayed=' + params.delayed);
        if (params.completion) p.push('completion=' + encodeURIComponent(params.completion));
        const query = p.length ? '?' + p.join('&') : '';
        return this.request('GET', '/tasks' + query);
    },
    async createTask(task) { return this.request('POST', '/tasks', task); },
    async batchCreateTasks(tasks) { return this.request('POST', '/tasks/batch', tasks); },
    async updateTask(id, task) { return this.request('PUT', '/tasks/' + id, task); },
    async deleteTask(id) { return this.request('DELETE', '/tasks/' + id); },
    async getTaskStats() { return this.request('GET', '/tasks/stats'); },
    async getTaskProjects() { return this.request('GET', '/tasks/projects'); },
    async exportTasks() {
        const res = await this.request('GET', '/tasks/export');
        if (res.ok) { const blob = await res.blob(); const link = document.createElement('a'); link.href = URL.createObjectURL(blob); link.download = `tasks_${new Date().toISOString().slice(0,10)}.csv`; link.click(); URL.revokeObjectURL(link.href); }
    },

    // ========== 员工失误记录 ==========
    async getIncidents(status = '', type = '', operator = '') {
        let params = [];
        if (status) params.push('status=' + encodeURIComponent(status));
        if (type) params.push('type=' + encodeURIComponent(type));
        if (operator) params.push('operator=' + encodeURIComponent(operator));
        const query = params.length ? '?' + params.join('&') : '';
        return this.request('GET', '/incidents' + query);
    },

    async createIncident(incident) {
        return this.request('POST', '/incidents', incident);
    },

    async updateIncident(id, incident) {
        return this.request('PUT', '/incidents/' + id, incident);
    },

    async deleteIncident(id) {
        return this.request('DELETE', '/incidents/' + id);
    },

    async getIncidentStats() {
        return this.request('GET', '/incidents/stats');
    },

    async exportIncidents() {
        const res = await this.request('GET', '/incidents/export');
        if (res.ok) {
            const blob = await res.blob();
            const link = document.createElement('a');
            link.href = URL.createObjectURL(blob);
            link.download = `incidents_${new Date().toISOString().slice(0,10)}.csv`;
            link.click();
            URL.revokeObjectURL(link.href);
        }
    },

    // ========== 商户管理 ==========
    async getMerchants(project = '', env = '') {
        let p = [];
        if (project) p.push('project=' + encodeURIComponent(project));
        if (env) p.push('env=' + encodeURIComponent(env));
        return this.request('GET', '/merchants' + (p.length ? '?' + p.join('&') : ''));
    },
    async createMerchant(m) { return this.request('POST', '/merchants', m); },
    async batchCreateMerchants(list) { return this.request('POST', '/merchants/batch', list); },
    async updateMerchant(id, m) { return this.request('PUT', '/merchants/' + id, m); },
    async deleteMerchant(id) { return this.request('DELETE', '/merchants/' + id); },
    async exportMerchants() {
        const res = await this.request('GET', '/merchants/export');
        if (res.ok) { const blob = await res.blob(); const link = document.createElement('a'); link.href = URL.createObjectURL(blob); link.download = `merchants_${new Date().toISOString().slice(0,10)}.csv`; link.click(); URL.revokeObjectURL(link.href); }
    },

    // ========== 服务配置管理 ==========
    async getServiceConfigs(project = '', env = '', type = '') {
        let params = [];
        if (project) params.push('project=' + encodeURIComponent(project));
        if (env) params.push('env=' + encodeURIComponent(env));
        if (type) params.push('type=' + encodeURIComponent(type));
        const query = params.length ? '?' + params.join('&') : '';
        return this.request('GET', '/service-configs' + query);
    },

    async getServiceConfig(id) {
        return this.request('GET', '/service-configs/' + id);
    },

    async createServiceConfig(config) {
        return this.request('POST', '/service-configs', config);
    },

    async batchCreateServiceConfigs(configs) {
        return this.request('POST', '/service-configs/batch', configs);
    },

    async updateServiceConfig(id, config) {
        return this.request('PUT', '/service-configs/' + id, config);
    },

    async deleteServiceConfig(id) {
        return this.request('DELETE', '/service-configs/' + id);
    },

    async getServiceProjects() {
        return this.request('GET', '/service-configs/projects');
    },

    async getServiceDependencies(serviceId) {
        return this.request('GET', '/service-configs/' + serviceId + '/deps');
    },

    async createServiceDependency(serviceId, dep) {
        return this.request('POST', '/service-configs/' + serviceId + '/deps', dep);
    },

    async updateServiceDependency(serviceId, depId, dep) {
        return this.request('PUT', '/service-configs/' + serviceId + '/deps/' + depId, dep);
    },

    async deleteServiceDependency(serviceId, depId) {
        return this.request('DELETE', '/service-configs/' + serviceId + '/deps/' + depId);
    },

    async getServiceDepPassword(serviceId, depId) {
        return this.request('GET', '/service-configs/' + serviceId + '/deps/' + depId + '/password');
    },

    // ========== Kubernetes 管理 ==========
    async getK8sNamespaces() {
        return this.request('GET', '/k8s/namespaces');
    },

    async getK8sDeployments(namespace = 'default') {
        return this.request('GET', '/k8s/deployments?namespace=' + encodeURIComponent(namespace));
    },

    async getK8sDeploymentYaml(name, namespace = 'default') {
        const res = await this.request('GET', '/k8s/deployments/' + encodeURIComponent(name) + '/yaml?namespace=' + encodeURIComponent(namespace));
        if (res.ok) {
            return { ok: true, text: await res.text() };
        }
        return res;
    },

    async getK8sRolloutStatus(name, namespace = 'default') {
        return this.request('GET', '/k8s/deployments/' + encodeURIComponent(name) + '/rollout-status?namespace=' + encodeURIComponent(namespace));
    },

    async getK8sRolloutHistory(name, namespace = 'default') {
        const res = await this.request('GET', '/k8s/deployments/' + encodeURIComponent(name) + '/rollout-history?namespace=' + encodeURIComponent(namespace));
        if (res.ok) {
            return { ok: true, text: await res.text() };
        }
        return res;
    },

    async k8sRollback(name, namespace = 'default', revision = 0) {
        return this.request('POST', '/k8s/deployments/' + encodeURIComponent(name) + '/rollback', { namespace, revision });
    },

    async getK8sServices(namespace = 'default') {
        return this.request('GET', '/k8s/services?namespace=' + encodeURIComponent(namespace));
    },

    async getK8sPods(namespace = 'default') {
        return this.request('GET', '/k8s/pods?namespace=' + encodeURIComponent(namespace));
    },

    async getK8sPodLogs(name, namespace = 'default', container = '', tail = 100) {
        let url = '/k8s/pods/' + encodeURIComponent(name) + '/logs?namespace=' + encodeURIComponent(namespace) + '&tail=' + tail;
        if (container) url += '&container=' + encodeURIComponent(container);
        const res = await this.request('GET', url);
        if (res.ok) {
            return { ok: true, text: await res.text() };
        }
        return res;
    },

    async deleteK8sPod(name, namespace = 'default') {
        return this.request('DELETE', '/k8s/pods/' + encodeURIComponent(name) + '?namespace=' + encodeURIComponent(namespace));
    },

    async getK8sNodes() {
        return this.request('GET', '/k8s/nodes');
    },

    async k8sApply(yamlPath = '', yamlContent = '', namespace = '') {
        return this.request('POST', '/k8s/apply', { yaml_path: yamlPath, yaml_content: yamlContent, namespace });
    },

    async k8sRestart(namespace, deployment) {
        return this.request('POST', '/k8s/restart', { namespace, deployment });
    },

    async k8sScale(namespace, deployment, replicas) {
        return this.request('POST', '/k8s/scale', { namespace, deployment, replicas });
    },

    async k8sUpdateImage(namespace, deployment, container, image) {
        return this.request('POST', '/k8s/update-image', { namespace, deployment, container, image });
    }
};
