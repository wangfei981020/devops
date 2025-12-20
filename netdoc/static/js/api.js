// API 调用封装
const API = {
    baseURL: '/api',

    async request(method, url, data = null) {
        const options = {
            method,
            headers: { 'Content-Type': 'application/json' }
        };
        if (data) {
            options.body = JSON.stringify(data);
        }
        const response = await fetch(this.baseURL + url, options);
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
    }
};





