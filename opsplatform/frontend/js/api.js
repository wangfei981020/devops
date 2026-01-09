// API 调用封装
const API = {
    baseURL: '/api',

    // 获取当前登录用户名
    getOperator() {
        try {
            const user = JSON.parse(localStorage.getItem('currentUser'));
            return user ? user.username : 'system';
        } catch {
            return 'system';
        }
    },

    // 获取 JWT token
    getToken() {
        return localStorage.getItem('authToken') || '';
    },

    // 保存 JWT token
    setToken(token) {
        if (token) {
            localStorage.setItem('authToken', token);
        }
    },

    // 清除 token
    clearToken() {
        localStorage.removeItem('authToken');
    },

    async request(method, url, data = null) {
        const headers = { 
            'Content-Type': 'application/json',
            'X-Operator': this.getOperator()
        };

        // 添加 JWT token
        const token = this.getToken();
        if (token) {
            headers['Authorization'] = 'Bearer ' + token;
        }

        const options = {
            method,
            headers
        };
        if (data) {
            options.body = JSON.stringify(data);
        }
        const response = await fetch(this.baseURL + url, options);

        // 如果返回 401，清除 token 并跳转到登录
        if (response.status === 401) {
            this.clearToken();
            localStorage.removeItem('currentUser');
            window.location.reload();
        }

        return response;
    },

    // 用户相关
    async login(username, password) {
        return this.request('POST', '/login', { username, password });
    },

    async getUsers() {
        return this.request('GET', '/users');
    },

    async createUser(user, createdBy) {
        return this.request('POST', '/users', { user, created_by: createdBy });
    },

    async updateUser(id, user) {
        return this.request('PUT', `/users/${id}`, user);
    },

    async deleteUser(id) {
        return this.request('DELETE', `/users/${id}`);
    },

    // 记录相关
    async getRecords() {
        return this.request('GET', '/records');
    },

    async getRecordHistory(recordId) {
        return this.request('GET', `/records/${recordId}/history`);
    },

    async rollbackRecord(recordId, historyId, operator) {
        return this.request('POST', `/records/${recordId}/rollback`, { history_id: historyId, operator: operator });
    },

    async createRecord(record, operator) {
        return this.request('POST', '/records', { record, operator });
    },

    async updateRecord(id, record, operator) {
        return this.request('PUT', `/records/${id}`, { record, operator });
    },

    async deleteRecord(id, operator) {
        return this.request('DELETE', `/records/${id}`, { operator });
    },

    async batchAddRecords(records, operator) {
        return this.request('POST', '/records/batch', { records, operator });
    },

    async batchDeleteRecords(ids, operator) {
        return this.request('POST', '/records/batch-delete', { ids, operator });
    },

    // 数据源相关
    async getDataSources() {
        return this.request('GET', '/datasources');
    },

    async createDataSource(datasource, operator) {
        return this.request('POST', '/datasources', { datasource, operator });
    },

    async updateDataSource(id, datasource) {
        return this.request('PUT', `/datasources/${id}`, datasource);
    },

    async deleteDataSource(id) {
        return this.request('DELETE', `/datasources/${id}`);
    },

    async testDataSource(datasource) {
        return this.request('POST', '/datasources/test', datasource);
    },

    // 审计日志
    async getAuditLogs() {
        return this.request('GET', '/audit-logs');
    },

    // 导出
    exportRecords(params = {}) {
        const query = new URLSearchParams(params).toString();
        window.open(`${this.baseURL}/records/export${query ? '?' + query : ''}`, '_blank');
    },

    exportAuditLogs() {
        window.open(`${this.baseURL}/audit-logs/export`, '_blank');
    },

    exportDomains(params = {}) {
        const query = new URLSearchParams(params).toString();
        window.open(`${this.baseURL}/domains/export${query ? '?' + query : ''}`, '_blank');
    },

    // MFA 多因素认证
    async mfaSetup(userId) {
        return this.request('POST', '/mfa/setup', { user_id: userId });
    },

    async mfaBind(userId, code) {
        return this.request('POST', '/mfa/bind', { user_id: userId, code });
    },

    async mfaVerify(userId, code) {
        return this.request('POST', '/mfa/verify', { user_id: userId, code });
    },

    async mfaDisable(userId, password) {
        return this.request('POST', '/mfa/disable', { user_id: userId, password });
    },

    async mfaReset(userId) {
        return this.request('POST', '/mfa/reset', { user_id: userId });
    },

    // 自定义指标管理
    async getMetrics() {
        return this.request('GET', '/metrics');
    },

    async createMetric(metric) {
        return this.request('POST', '/metrics', metric);
    },

    async updateMetric(metric) {
        return this.request('PUT', '/metrics', metric);
    },

    async deleteMetric(id) {
        return this.request('DELETE', `/metrics?id=${id}`);
    },

    async initDefaultMetrics() {
        return this.request('POST', '/metrics/init');
    },

    // 域名管理
    async getDomains() {
        return this.request('GET', '/domains');
    },

    async createDomain(domain) {
        return this.request('POST', '/domains', domain);
    },

    async updateDomain(id, domain) {
        return this.request('PUT', `/domains/${id}`, domain);
    },

    async deleteDomain(id) {
        return this.request('DELETE', `/domains/${id}`);
    },

    async batchDomains(ids, action, operator) {
        return this.request('POST', '/domains/batch', { ids, action, operator });
    },

    async batchAddDomains(domains, createdBy) {
        return this.request('POST', '/domains/batch-add', { domains, created_by: createdBy });
    },

    async checkDomainCert(domain) {
        return this.request('GET', `/domains/check-cert?domain=${encodeURIComponent(domain)}`);
    }
};





