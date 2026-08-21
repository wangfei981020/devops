/**
 * 自动生成，请勿手改。
 *
 * 来源：ops-cmdb/backend 的 swag 注解
 * 重新生成：pnpm --filter @ops/api gen
 *
 * 手改的结果是：后端真的改了字段时生成器会把改动冲掉；
 * 而在被冲掉之前，前端类型和后端实际返回已经对不上了，编译还是绿的。
 */

export interface paths {
    "/auth/sso": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * SSO 是否可用
         * @description 只返回"开没开"和按钮文案。没开时返回 enabled=false，不报错。
         */
        get: {
            parameters: {
                query?: never;
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": {
                            [key: string]: unknown;
                        };
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/auth/sso/callback": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * SSO 回调
         * @description 换 token、验 id_token、发会话，然后 302 回前端。
         */
        get: {
            parameters: {
                query?: never;
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description Found */
                302: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content?: never;
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/auth/sso/start": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 发起 SSO 登录
         * @description 302 跳到 IdP 的授权页。失败时跳回 /login?sso_error=…
         */
        get: {
            parameters: {
                query?: {
                    /** @description 登录后回到哪个页面 */
                    redirect?: string;
                };
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description Found */
                302: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content?: never;
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/cert-list": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 证书列表
         * @description 到期倒计时、自动续期与上次续期错误。**不返回证书内容与私钥。**
         */
        get: {
            parameters: {
                query?: {
                    /** @description 页码，从 1 开始 */
                    page?: number;
                    /** @description 每页条数 */
                    size?: number;
                    /** @description 按通用名/SAN 搜索 */
                    q?: string;
                    /** @description 健康度 */
                    health?: "all" | "expired" | "failing" | "unknown" | "soon" | "ok";
                    /** @description 排序字段，前缀 - 为降序 */
                    sort?: "cn" | "expiry";
                };
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.ListResponse-handlers_certOut"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/cloud-lb-list": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 负载均衡列表
         * @description 后端数分三态：null=没采过、0=看 backend_state 判断原因（可能由 target/K8s 承载，也可能真的没有）、n=正常。
         */
        get: {
            parameters: {
                query?: {
                    /** @description 页码，从 1 开始 */
                    page?: number;
                    /** @description 每页条数 */
                    size?: number;
                    /** @description 按名称/VIP 搜索 */
                    q?: string;
                    /** @description EXTERNAL / INTERNAL，all 或具体值 */
                    scheme?: string;
                    /** @description 健康度 */
                    health?: "all" | "stale" | "unknown" | "viaTarget" | "k8s" | "lost" | "empty" | "ok";
                };
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.ListResponse-handlers_lbOut"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/cloud-subnet-list": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 子网列表
         * @description 网段、区域与所属 VPC。云上已删除的保留并标注（别的资源可能还引用着它）。
         */
        get: {
            parameters: {
                query?: {
                    /** @description 页码，从 1 开始 */
                    page?: number;
                    /** @description 每页条数 */
                    size?: number;
                    /** @description 按子网名/网段/VPC 搜索 */
                    q?: string;
                    /** @description 区域，all 或具体值 */
                    region?: string;
                    /** @description 云项目，all 或具体值 */
                    project?: string;
                };
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.ListResponse-handlers_subnetOut"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/cost/overview": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 成本总览
         * @description **估算**月成本，按项目/环境拆分。明确给出没匹配到费率的资源数。
         */
        get: {
            parameters: {
                query?: never;
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["handlers.costOverviewOut"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/datasource-list": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 数据源接入状况
         * @description 云账号/集群/观测端点三类。**不返回任何凭据内容**，只给"配没配"。
         */
        get: {
            parameters: {
                query?: {
                    /** @description 类别 */
                    kind?: "all" | "cloud" | "cluster" | "obs";
                };
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.ListResponse-handlers_dataSourceOut"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/domain-list": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 域名列表
         * @description 注册到期、解析状态、证书到期三个维度分开显示（处理动作完全不同）。
         */
        get: {
            parameters: {
                query?: {
                    /** @description 页码，从 1 开始 */
                    page?: number;
                    /** @description 每页条数 */
                    size?: number;
                    /** @description 按域名搜索 */
                    q?: string;
                    /** @description 健康度 */
                    health?: "all" | "expired" | "unresolved" | "unknown" | "soon" | "cert_soon" | "ignored" | "ok";
                };
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.ListResponse-handlers_domainRowOut"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/exposure-list": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 暴露面
         * @description 公网可达的入口清单。判据与各资源页一致；无法判断防护状态时返回 null 而非 false。
         */
        get: {
            parameters: {
                query?: {
                    /** @description 类别 */
                    kind?: "all" | "lb" | "service" | "host";
                };
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.ListResponse-handlers_exposureOut"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/hosts": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 主机列表
         * @description 支持分页、关键词搜索、状态/项目/云厂商筛选与排序。
         *     不传 page/size 时返回裸数组（兼容尚未迁移的旧前端）。
         */
        get: {
            parameters: {
                query?: {
                    /** @description 页码，从 1 开始 */
                    page?: number;
                    /** @description 每页条数，上限 200 */
                    size?: number;
                    /** @description 搜索主机名或内网 IP */
                    q?: string;
                    /** @description 状态 */
                    status?: "running" | "stopped" | "destroyed";
                    /** @description 云项目 ID */
                    project?: string;
                    /** @description 云厂商 */
                    provider?: string;
                    /** @description 排序字段，前缀 - 为降序 */
                    sort?: "name" | "-name" | "project" | "-project" | "status" | "-status" | "vcpu" | "-vcpu" | "mem" | "-mem" | "disk" | "-disk" | "cost" | "-cost" | "created" | "-created";
                };
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.ListResponse-handlers_hostOut"];
                    };
                };
                /** @description Bad Request */
                400: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.APIError"];
                    };
                };
                /** @description Internal Server Error */
                500: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.APIError"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/hosts/{ciid}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 主机详情
         * @description 磁盘逐块（含用量与挂载点）、关联业务域名、成本明细。
         */
        get: {
            parameters: {
                query?: {
                    /** @description 累计成本算到哪一天，YYYY-MM-DD */
                    as_of?: string;
                };
                header?: never;
                path: {
                    /** @description CI ID */
                    ciid: number;
                };
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["handlers.hostDetailOut"];
                    };
                };
                /** @description Not Found */
                404: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.APIError"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/idp-config": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * SSO 配置
         * @description **不返回 client_secret**，只给 has_secret。
         */
        get: {
            parameters: {
                query?: never;
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["handlers.idpConfig"];
                    };
                };
            };
        };
        /**
         * 保存 SSO 配置
         * @description client_secret 留空表示保持不变。开启前会校验 issuer 可达且不是内网地址。
         */
        put: {
            parameters: {
                query?: never;
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": {
                            [key: string]: unknown;
                        };
                    };
                };
                /** @description Bad Request */
                400: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.APIError"];
                    };
                };
            };
        };
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/idp-config/discover": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * 探测 IdP 端点
         * @description 拉 {issuer}/.well-known/openid-configuration。拒绝内网地址。
         */
        post: {
            parameters: {
                query?: never;
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["handlers.oidcEndpoints"];
                    };
                };
                /** @description Bad Request */
                400: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.APIError"];
                    };
                };
            };
        };
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/impact": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 变更影响面
         * @description 沿已记录的关系往外找 3 层。**没有关系记录 ≠ 没有影响**。
         */
        get: {
            parameters: {
                query: {
                    /** @description 起点资源 ID */
                    ci_id: number;
                };
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["handlers.impactOut"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/k8s/cluster-list": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 集群列表
         * @description 含节点/Pod 计数、kubelet 版本分布与采集新鲜度。未采集的集群计数为 null 而非 0。
         */
        get: {
            parameters: {
                query?: {
                    /** @description 页码，从 1 开始 */
                    page?: number;
                    /** @description 每页条数 */
                    size?: number;
                    /** @description 按集群名/展示名搜索 */
                    q?: string;
                    /** @description 环境筛选，all 或任意环境值 */
                    env?: string;
                    /** @description 排序字段，前缀 - 为降序 */
                    sort?: "name" | "env" | "nodes" | "pods" | "synced" | "-name" | "-env" | "-nodes" | "-pods" | "-synced";
                };
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.ListResponse-handlers_clusterOut"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/k8s/event-list": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 事件（仅 Warning）
         * @description 只采集并保留 Warning 事件；etcd 只留 1 小时，这里是落库后的可回看副本。
         */
        get: {
            parameters: {
                query?: {
                    /** @description 页码，从 1 开始 */
                    page?: number;
                    /** @description 每页条数 */
                    size?: number;
                    /** @description 按对象名/原因/消息搜索 */
                    q?: string;
                    /** @description 集群名，all 或集群名 */
                    cluster?: string;
                    /** @description 命名空间，all 或具体值 */
                    namespace?: string;
                    /** @description 原因，all 或具体值 */
                    reason?: string;
                };
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.ListResponse-handlers_eventOut"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/k8s/namespace-list": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 命名空间列表
         * @description 含工作负载/Pod 计数与归属项目。未采集的计数为 null 而非 0。
         */
        get: {
            parameters: {
                query?: {
                    /** @description 页码，从 1 开始 */
                    page?: number;
                    /** @description 每页条数 */
                    size?: number;
                    /** @description 按命名空间名或项目搜索 */
                    q?: string;
                    /** @description 集群名，all 或集群名 */
                    cluster?: string;
                    /** @description 状态筛选，all 或 Active / Terminating */
                    phase?: string;
                    /** @description 排序字段，前缀 - 为降序 */
                    sort?: "name" | "cluster" | "workloads" | "pods";
                };
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.ListResponse-handlers_namespaceOut"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/k8s/node-list": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 节点列表
         * @description 含心跳新鲜度、压力位与主机台账关联。心跳过期时状态不可信，字段 heartbeat_stale 为 true。
         */
        get: {
            parameters: {
                query?: {
                    /** @description 页码，从 1 开始 */
                    page?: number;
                    /** @description 每页条数 */
                    size?: number;
                    /** @description 按节点名/IP/节点池搜索 */
                    q?: string;
                    /** @description 集群名筛选，all 或集群名 */
                    cluster?: string;
                    /** @description 状态筛选。pressure=磁盘/内存/PID 压力（节点仍 Ready 但快撑不住） */
                    status?: "all" | "ready" | "notready" | "stale" | "pressure";
                    /** @description 排序字段，前缀 - 为降序 */
                    sort?: "name" | "cluster" | "status" | "pods" | "heartbeat";
                };
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.ListResponse-handlers_nodeOut"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/k8s/pod-list": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Pod 列表
         * @description 分页、筛选、分面全部在 SQL 里做（Pod 是十万级，不能全取到内存）。
         */
        get: {
            parameters: {
                query?: {
                    /** @description 页码，从 1 开始 */
                    page?: number;
                    /** @description 每页条数 */
                    size?: number;
                    /** @description 按 Pod 名/工作负载/节点名搜索 */
                    q?: string;
                    /** @description 集群名，all 或集群名 */
                    cluster?: string;
                    /** @description 命名空间，all 或具体值 */
                    namespace?: string;
                    /** @description 健康度 */
                    health?: "all" | "bad" | "restarted" | "ok";
                    /** @description 排序字段，前缀 - 为降序 */
                    sort?: "name" | "namespace" | "restarts" | "started";
                };
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.ListResponse-handlers_podOut"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/k8s/pvc-list": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 存储卷列表
         * @description 含"当前没有使用者"标注（注意：那不等于可以删）。
         */
        get: {
            parameters: {
                query?: {
                    /** @description 页码，从 1 开始 */
                    page?: number;
                    /** @description 每页条数 */
                    size?: number;
                    /** @description 按名称/存储类搜索 */
                    q?: string;
                    /** @description 集群名，all 或集群名 */
                    cluster?: string;
                    /** @description 健康度 */
                    health?: "all" | "lost" | "pending" | "orphan" | "ok";
                };
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.ListResponse-handlers_pvcOut"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/k8s/service-list": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 服务与入口
         * @description Service 为行，指向它的 Ingress 主机名并入同一行。LoadBalancer 拿不到外部 IP 会单独标注。
         */
        get: {
            parameters: {
                query?: {
                    /** @description 页码，从 1 开始 */
                    page?: number;
                    /** @description 每页条数 */
                    size?: number;
                    /** @description 按服务名/主机名/IP 搜索 */
                    q?: string;
                    /** @description 集群名，all 或集群名 */
                    cluster?: string;
                    /** @description 命名空间，all 或具体值 */
                    namespace?: string;
                    /** @description 暴露情况 */
                    exposure?: "all" | "exposed" | "pending" | "internal";
                };
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.ListResponse-handlers_svcOut"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/k8s/workload-list": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 工作负载列表
         * @description 副本就绪情况、镜像 tag 与健康度。期望副本为 0 单独成一档（那是正常状态，不是故障）。
         */
        get: {
            parameters: {
                query?: {
                    /** @description 页码，从 1 开始 */
                    page?: number;
                    /** @description 每页条数 */
                    size?: number;
                    /** @description 按名称/镜像搜索 */
                    q?: string;
                    /** @description 集群名，all 或集群名 */
                    cluster?: string;
                    /** @description 命名空间，all 或具体值 */
                    namespace?: string;
                    /** @description 健康度 */
                    health?: "all" | "down" | "degraded" | "scaled_zero" | "ok";
                    /** @description 排序字段，前缀 - 为降序 */
                    sort?: "name" | "namespace" | "kind" | "ready";
                };
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.ListResponse-handlers_workloadOut"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/license": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 授权状态
         * @description 七态、容量与用量、安装指纹。未激活时同样返回 200，状态为 not_activated。
         */
        get: {
            parameters: {
                query?: never;
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["handlers.licenseOut"];
                    };
                };
            };
        };
        put?: never;
        /**
         * 激活授权
         * @description 验签通过才落库。库里换了之后，其余副本在 20s 内自行收敛。
         */
        post: {
            parameters: {
                query?: never;
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["handlers.licenseOut"];
                    };
                };
                /** @description Bad Request */
                400: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.APIError"];
                    };
                };
            };
        };
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/mcp/tokens": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * MCP 令牌列表
         * @description 不返回令牌本身，只有前 8 位提示。
         */
        get: {
            parameters: {
                query?: never;
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.ListResponse-handlers_mcpTokenOut"];
                    };
                };
            };
        };
        put?: never;
        /**
         * 新建 MCP 令牌
         * @description 明文令牌**只在这次响应里返回一次**，之后无法再取。
         */
        post: {
            parameters: {
                query?: never;
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": {
                            [key: string]: unknown;
                        };
                    };
                };
                /** @description Bad Request */
                400: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.APIError"];
                    };
                };
            };
        };
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/mcp/tokens/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        /** 改 MCP 令牌（启停 / 换角色） */
        put: {
            parameters: {
                query?: never;
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": {
                            [key: string]: unknown;
                        };
                    };
                };
            };
        };
        post?: never;
        /**
         * 删除 MCP 令牌
         * @description 立即失效，用它的接入方下一次调用就会 401。
         */
        delete: {
            parameters: {
                query?: never;
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": {
                            [key: string]: unknown;
                        };
                    };
                };
            };
        };
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/overview": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 全局态势
         * @description 汇总各列表页已有的判据，不新造判据。统计失败的项返回 null 而非 0。
         */
        get: {
            parameters: {
                query?: never;
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["handlers.situationOut"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/relation-list": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 资源关系
         * @description 已记录的资源间关系。没有关系记录 ≠ 该资源孤立。
         */
        get: {
            parameters: {
                query?: {
                    /** @description 按资源名搜索 */
                    q?: string;
                    /** @description 关系类型，all 或具体值 */
                    rel_type?: string;
                };
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.ListResponse-handlers_relationOut"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/task-list": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * 定时任务与巡检
         * @description 含"从没跑过"与"早该跑了却没跑"两种静默失败信号。
         */
        get: {
            parameters: {
                query?: {
                    /** @description 按任务名搜索 */
                    q?: string;
                    /** @description 状态 */
                    state?: "all" | "failed" | "overdue" | "never" | "ok" | "disabled";
                };
                header?: never;
                path?: never;
                cookie?: never;
            };
            requestBody?: never;
            responses: {
                /** @description OK */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": components["schemas"]["httpx.ListResponse-handlers_taskOut"];
                    };
                };
            };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
}
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        "handlers.attentionItem": {
            /**
             * @description Count 命中条数。
             *
             *     ⚠️ 指针：null = **这项没能统计出来**（查询失败/ 数据源没接），
             *     不是"0 条"。0 是好消息，null 是我们不知道 —— 把后者显示成 0
             *     等于在首页上给一个我们根本没看过的维度发合格证。
             */
            count?: number;
            /** @description Key 用于前端取文案与跳转目标。后端不返回句子（那样英文界面永远漏中文） */
            key?: string;
            /** @description Link 点进去看明细的目标（含筛选条件），前端直接用 */
            link?: string;
            /** @description Severity high / medium，决定前端色调与排序 */
            severity?: string;
        };
        "handlers.certOut": {
            auto_renew?: boolean;
            ca?: string;
            ci_id?: number;
            cn?: string;
            days_left?: number;
            /**
             * @description ExpiryAt / DaysLeft
             *
             *     ⚠️ DaysLeft 用指针：null = **采不到到期日**，不是"还有 0 天"。
             *     一张读不出到期日的证书绝不能显示成正常 —— 它可能已经过期了，
             *     只是我们不知道。0 是"今天到期"，是完全不同的事。
             */
            expiry_at?: string;
            /**
             * @description LastError 上一次续期的错误。
             *
             *     ⚠️ 这一条和 AutoRenew 必须**一起看**。最危险的组合是
             *     「自动续期开着 + 一直在失败」：界面上写着"自动续期"会让人放心，
             *     而它其实已经连续失败几周了，没有任何人在管。
             */
            last_error?: string;
            sans?: string;
            status?: string;
            updated_at?: string;
        };
        "handlers.clusterOut": {
            display_name?: string;
            enabled?: boolean;
            environment?: string;
            /**
             * @description HasKubeconfig 有没有配连接凭据。没有就只能靠别的途径采，
             *     很多能力（实时日志、诊断）直接不可用
             */
            has_kubeconfig?: boolean;
            id?: number;
            /**
             * @description Ingested 集群侧的资源是否采进来了。
             *
             *     ⚠️ false 时下面所有计数都是 null 而不是 0。
             *     「纳管了但没采到」和「采到了但里面是空的」在界面上必须长得不一样：
             *     前者是采集缺口（要去查连通性/凭据），后者什么都不用做。
             */
            ingested?: boolean;
            /**
             * @description KubeletVersions 节点上的 kubelet 版本清单（去重）。
             *     多于一个说明集群正在升级中或升级卡住了 —— 这是升级排期要看的第一眼。
             */
            kubelet_versions?: string[];
            location?: string;
            name?: string;
            nodes?: number;
            nodes_ready?: number;
            pods?: number;
            /**
             * @description PodsBad 起不来的：phase 不是 Running/Succeeded。
             *
             *     ⚠️ **不含"重启过"**。判据必须和 Pod 列表的 health=bad 一字不差，
             *     否则同一批 Pod 在两页上有两个说法 —— 实测撞到过：
             *     开发机休眠导致 40 个 Pod 全都 restarts>0，集群页报"32 异常"，
             *     而 Pod 页显示 0 个 bad、32 个 restarted。看的人不知道该信哪个。
             */
            pods_bad?: number;
            /**
             * @description PodsRestarted 在跑但重启过。是**较弱的信号**，单列一个数：
             *     三周前重启过一次、之后一直好好的 Pod 不该被叫做"异常"。
             */
            pods_restarted?: number;
            project_id?: string;
            provider?: string;
            /**
             * @description SyncedAt 最后一次采到数据的时刻。台账是快照不是实时，
             *     不显示它，人会以为看到的是此刻的集群
             */
            synced_at?: string;
        };
        "handlers.costOverviewOut": {
            by_env?: components["schemas"]["handlers.costRow"][];
            by_project?: components["schemas"]["handlers.costRow"][];
            /**
             * @description DestroyedExcluded 已销毁但仍在台账里的机器数（不计入成本）。
             *     说明它们为什么不在总额里，否则会被当成漏算
             */
            destroyed_excluded?: number;
            /** @description Estimated 恒为 true：这一页永远是估算，字段留着是为了将来接账单后能翻成 false */
            estimated?: boolean;
            /**
             * @description FallbackPricedHosts 按**默认档**估价的机器数。
             *
             *     	⚠️ 这是这一页真正的静默降级，比 UnpricedHosts 隐蔽得多：
             *     	算不出来（0 元）很醒目，而回退默认档会算出一个看起来正常的数。
             *     	原来它被记成"已定价"，界面上没有任何痕迹（OPSCMDB-031 P1-47）。
             */
            fallback_priced_hosts?: number;
            /**
             * @description FallbackRegions 缺费率的区域（最多 8 个）。
             *     光说"有 3 台按默认档估"没法行动，要知道去给哪个区域补费率
             */
            fallback_regions?: string[];
            /** @description TotalMonthly 估算月成本 */
            total_monthly?: number;
            /**
             * @description Unpriced 没能匹配到费率、按 0 计的资源数。
             *
             *     ⚠️ 必须显式给出。它们不是"免费的"，而是我们**算不出来**的 ——
             *     混进总数里会让总额偏低，而偏低的成本报表没人会去质疑。
             */
            unpriced_hosts?: number;
        };
        "handlers.costRow": {
            count?: number;
            key?: string;
            monthly?: number;
        };
        "handlers.dataSourceOut": {
            /**
             * @description CredRequired 这类数据源**没凭据就一定采不到**。
             *
             *     	🔴 「该有凭据却没有」和「凭据字段为空」是两件事。
             *
             *     	凭据管理页原来按 `has_credential=false` 分组，于是
             *     	3 个 2~4 分钟前刚同步成功的 K8s 集群被列进「启用了但没配凭据」——
             *     	它们走集群内 ServiceAccount / 云账号继承，**本来就不需要**在这里配
             *     	（OPSCMDB-031 P2-50）。
             *
             *     	误报和真问题混在同一个数字里，那个数字就没人信了：
             *     	「7 处缺凭据」里 3 处是假的，剩下 4 处真的也跟着被忽略。
             *
             *     	判据放后端：它知道每类数据源的认证方式，前端不该再猜一次。
             */
            cred_required?: boolean;
            /**
             * @description Credential 凭据从哪来：
             *       configured = 本条存了凭据
             *       inherited  = 不需要自己存（如 GKE 走绑定的云账号）
             *       none       = 没存。对无鉴权端点是正常的，对云账号才是问题
             */
            credential?: string;
            enabled?: boolean;
            /**
             * @description HasCredential 本条记录里有没有存凭据。**只给布尔，绝不给内容**。
             *
             *     🔴 不要拿它当健康判据。「没存凭据」≠「采不到东西」，三类都有反例：
             *       - GKE 集群走云账号的 service account，本来就不存 kubeconfig
             *       - 内网 Prometheus / Loki 无鉴权，留空是正常配置（编辑框自己写着"需要鉴权时填"）
             *     健康判定看 Health 字段。
             */
            has_credential?: boolean;
            /**
             * @description Health 这个数据源现在到底能不能用：
             *       ok / no_credential / stale / never_synced / not_applicable / disabled
             *
             *     🔴 判定顺序：**事实优先于推断**。有新鲜的同步记录就一定能采到，
             *     此时无论凭据字段是什么都判 ok —— 曾经反过来写，于是
             *     「缺凭据」和「2 分钟前同步成功」出现在同一行里自相矛盾。
             */
            health?: string;
            /** @description HealthNote 给人看的一句话，说明这个判定是怎么来的 */
            health_note?: string;
            /** @description Kind cloud / cluster / obs —— 三类接入 */
            kind?: string;
            /** @description LastResult 上次同步结果，原样透传（可能是错误串） */
            last_result?: string;
            /** @description LastSyncAt 空 = 从没同步过（不是"刚同步完"） */
            last_sync_at?: string;
            name?: string;
            /**
             * @description ScheduleKnown 停摆判定用的是**这个数据源自己的调度周期**还是回退的固定值。
             *
             *     	⚠️ false 时**不要**断言「早该同步了却没有」——
             *     	那句话在一个按时运行的每日任务上是错的，它把守时说成了失职。
             */
            schedule_known?: boolean;
            /** @description Stale 早该同步了却没有 —— 数据源静默停摆 */
            stale?: boolean;
            /** @description Type 各类下的细分（gcp / gke / prometheus / loki / n9e …），原样透传 */
            type?: string;
        };
        "handlers.domainRowOut": {
            /** @description CertCheckMsg 证书探测的错误原因，原样透传 */
            cert_check_msg?: string;
            cert_days_left?: number;
            cert_expiry_at?: string;
            ci_id?: number;
            /**
             * @description ⚠️ 两个到期天数都用指针：null = 不知道（没登记 / 没探测到），
             *     不是"还有 0 天"。域名注册到期读不出来时，它可能下周就被释放了。
             */
            days_left?: number;
            dns_provider?: string;
            /**
             * @description DNSRecords 注册商侧真实解析记录（dns_records）的条数。
             *
             *     🔴 与 Records 是**两张表、两件事**，绝不能互相冒充：
             *     	domain_records = 我们自己的台账（带 project/env/module/负责人）
             *     	dns_records    = 注册商上此刻真实存在的解析
             *     	实测 dev-example.com：台账 2 条、注册商侧 0 条。
             *     	「DNS 解析」页按域名视图里那个计数必须是后者 ——
             *     	写成前者的话，点开弹窗（管的是注册商解析）会看到"没有解析记录"，
             *     	而行上明明写着「2 条记录」。数字和它旁边的按钮说的不是一回事。
             */
            dns_records?: number;
            expiry_at?: string;
            ignore_reason?: string;
            /**
             * @description Ignored 人为忽略。
             *
             *     ⚠️ 忽略的域名**不能当成正常**：它只是"我们决定暂时不管"，
             *     客观状态一点没变。所以它单独一档，并把理由带出来 ——
             *     半年后没人记得当初为什么忽略，而那条理由往往已经不成立了。
             */
            ignored?: boolean;
            name?: string;
            /** @description Records 主机头台账（domain_records）的条数 —— 「这个域名下我们登记了几条业务解析」。 */
            records?: number;
            registrar?: string;
            /** @description ResolveStatus 解析状态，原样透传（ok / nxdomain / timeout …） */
            resolve_status?: string;
            synced_at?: string;
        };
        "handlers.eventOut": {
            cluster_id?: number;
            cluster_name?: string;
            /** @description Count k8s 自己聚合的重复次数。1 次和 300 次是完全不同的严重度 */
            count?: number;
            first_at?: string;
            kind?: string;
            last_at?: string;
            message?: string;
            namespace?: string;
            obj_name?: string;
            /**
             * @description Reason / Message 原样透传：FailedScheduling、BackOff 是运维
             *     直接拿去 kubectl 和搜索引擎里查的词
             */
            reason?: string;
        };
        "handlers.exceededOut": {
            current?: number;
            item?: string;
            limit?: number;
        };
        "handlers.exposureOut": {
            /** @description Endpoint 公网可达的入口（IP 或主机名） */
            endpoint?: string;
            /** @description Kind lb / service / host */
            kind?: string;
            name?: string;
            ports?: string;
            /**
             * @description PortsBasis 端口是从哪儿判出来的，便于复核时追到源头。
             *     	lb_rule / service_spec / firewall / none
             *
             *     ⚠️ **风险分级不在这里做**：前端已有一套 isAllPorts/portRisk，
             *     	且刻意与防火墙页保持同一判据（"两处给出不同结论会让人无所适从"）。
             *     	后端再加一套就是第三份实现 —— 本项目已经因为"同一判据写了两遍"
             *     	出过三次「两个页面对同一事实给出相反结论」。
             *     	这里只负责**把数据供全**，判档留给唯一那处。
             */
            ports_basis?: string;
            /**
             * @description PortsKnown 我们**知不知道**这个入口开了哪些端口。
             *
             *     ⚠️ 必须和 Ports 分开：空字符串会被读成"没开端口"，
             *     	而主机行原本就一直是空的（端口由防火墙规则决定，不在主机记录里）。
             *     	一张安全清单上，"不知道"被显示成"没有"是最危险的那种错
             *     	（OPSCMDB-031 P1-52：7 台有公网 IP 的主机端口列全是 –，
             *     	 于是"这台机器能被访问到什么"在界面上无从得知）。
             */
            ports_known?: boolean;
            /**
             * @description Protected 前面是否挂了 CDN/WAF 之类。
             *
             *     ⚠️ 用指针：null = **我们没法判断**（没接 CDN 数据源），不是"没有防护"。
             *     兜底成 false 会让一张安全清单声称"这 40 个入口全都裸奔"，
             *     而其中一半可能在 CDN 后面 —— 假报告比没有报告更糟。
             */
            protected?: boolean;
            scope?: string;
        };
        "handlers.hostDetailOut": {
            as_of?: string;
            cost_hourly?: number;
            disks?: components["schemas"]["handlers.hostDiskOut"][];
            host?: components["schemas"]["handlers.hostOut"];
            node?: components["schemas"]["handlers.hostNodeOut"];
            /**
             * @description NodeLink 这台机器和 k8s 节点的关系，三态。
             *
             *     ⚠️ 不能压成「有没有 Pod」一个布尔量。「不是集群节点」和
             *     「是节点但集群没接进来」在界面上必须长得不一样：
             *     前者是正常的（一台裸机就该没 Pod），后者是**采集缺口**，
             *     渲染成一样的空白，就等于把"我们没数据"伪装成"上面没东西"。
             *
             *     	linked        已关联，Pods 是真实清单（可以为空 = 节点上真的没 Pod）
             *     	not_ingested  hosts.is_k8s_node=1 但 k8s_nodes 里查不到 → 该集群未接入
             *     	none          不是 k8s 节点
             */
            node_link?: string;
            pods?: components["schemas"]["handlers.hostPodOut"][];
            rate_family?: string;
            rate_matched?: string;
            rate_ram_gb_hour?: number;
            rate_vcpu_hour?: number;
            related_domains?: components["schemas"]["handlers.relatedDomain"][];
        };
        "handlers.hostDiskOut": {
            is_boot?: boolean;
            /**
             * @description MountPoint 挂载点。排障时它比云盘名有用得多 ——
             *     人关心的是 /var/lib/kafka 满了，不是 persistent-disk-2 满了。
             */
            mount_point?: string;
            name?: string;
            size_gb?: number;
            type?: string;
            used_at?: string;
            /**
             * @description UsedPercent 用指针是为了能表达 null。
             *
             *     ⚠️ null ≠ 0：null 是「没采到」（集群没接 Prometheus、设备名对不上），
             *     0 是「真的是空盘」。用 0 当哨兵值会让没采到的盘被算进平均值，
             *     把整体用量算低；前端也没法区分该显示「未接入」还是「0%」。
             *     omitempty 是刻意的：Swagger 2.0 没有 nullable 概念，
             *     生成出的 TS 类型是 `used_percent?: number`（optional）。
             *     若 nil 时仍输出 `null`，类型与实际就对不上了 —— 编译期看着没问题，
             *     运行时拿到 null 去做算术会得到 0，把「没采到」变成「空盘」。
             *     省略字段则与 optional 语义严格一致。
             */
            used_percent?: number;
        };
        "handlers.hostNodeOut": {
            cluster_id?: number;
            cluster_name?: string;
            name?: string;
            /**
             * @description PodCount 是节点自己上报的数量，和 len(Pods) 可能对不上
             *     （两张表由不同轮次的同步写入）。对不上时前端要显式提示，
             *     而不是挑一个显示——挑一个就等于替用户判断哪份数据可信。
             */
            pod_count?: number;
            pool?: string;
            ready_status?: string;
        };
        "handlers.hostOut": {
            account_name?: string;
            ci_id?: number;
            /**
             * @description ClusterName 这台机器所属的 K8s 集群名（经 k8s_nodes 关联）。
             *     ⚠️ 空串对非 K8s 节点是**正确**的值，不是"没采到"。
             */
            cluster_name?: string;
            /** @description 成本估算（USD） */
            cost_daily?: number;
            cost_month?: number;
            /** @description estimate / bigquery */
            cost_source?: string;
            cost_total?: number;
            cpu_platform?: string;
            deletion_protection?: boolean;
            disk_total_gb?: number;
            /**
             * @description Disks 磁盘逐块明细。
             *
             *     刻意不做成主机级的一个总用量：一台 boot 盘 96%、数据盘 71% 的机器，
             *     加权算下来才 79%，看着很健康，但系统盘马上要写满 ——
             *     而 kubelet 的 DiskPressure 恰恰是按单块盘判的。加总会把它藏起来。
             */
            disks?: components["schemas"]["handlers.hostDiskOut"][];
            external_ip?: string;
            gcp_created_at?: string;
            /** @description GCP 只读技术字段 */
            hostname?: string;
            image?: string;
            internal_ip?: string;
            is_k8s_node?: boolean;
            k8s_pool?: string;
            labels?: {
                [key: string]: string;
            };
            /** @description present=云上还在 / gone=云上已查不到 */
            lifecycle?: string;
            machine_type?: string;
            mem_mb?: number;
            name?: string;
            network_tags?: string[];
            os?: string;
            preemptible?: boolean;
            /** @description project id */
            project?: string;
            /** @description GCP 显示名 */
            project_name?: string;
            provider?: string;
            region?: string;
            service_accounts?: string[];
            stale?: boolean;
            /**
             * @description Status 是**云上原样值**（RUNNING/TERMINATED/…），保持不动。
             *
             *     	⚠️ 已销毁的机器不能靠改这个字段来表达。markStaleHosts 只置 stale=1，
             *     	status 停在最后一次同步到的 RUNNING 上，于是界面同一行自相矛盾：
             *     	名字划了删除线、打了「已删」标签，状态列却是绿色的「运行」（CMDB-003）。
             *     	但也不能把 status 覆写成 DESTROYED——
             *     	  1. TERMINATED 在 GCP 语义里是"已停机、实例还在、磁盘还计费"，
             *     	     借用它会把"销毁"和"关机"混成一个值；
             *     	  2. stale 是**推断**（同步时 GCP 没返回≠一定销毁，也可能同步本身坏了、
             *     	     或实例被移出了这个 project）。覆写掉真值，一旦是误标就再也查不到
             *     	     最后一次观测到的真实状态。
             *     	所以另出一个派生字段 Lifecycle，展示层以它为准，status 留作证据。
             */
            status?: string;
            subnet?: string;
            /**
             * @description SyncedAt 最后一次同步到这台机器的时刻。
             *
             *     列表页必须能看到它：台账数据是**快照**，不是实时。
             *     不显示同步时间，用户会把三天前的快照当成当前状态 ——
             *     而"数据是旧的"和"数据是错的"在界面上长得一模一样。
             */
            synced_at?: string;
            vcpu?: number;
            vpc?: string;
            zone?: string;
        };
        "handlers.hostPodOut": {
            name?: string;
            namespace?: string;
            phase?: string;
            restarts?: number;
            workload?: string;
        };
        "handlers.idpConfig": {
            /** @description AllowPrivate 允许 issuer 指向内网 / 用 http。自建身份源的客户需要它 */
            allow_private?: boolean;
            client_id?: string;
            display_name?: string;
            enabled?: boolean;
            /** @description HasSecret 只暴露"配没配"。client_secret 任何接口都不回传 */
            has_secret?: boolean;
            issuer?: string;
            jit_enabled?: boolean;
            jit_role_code?: string;
            /** @description LastClaims 身份源实际返回的 claim 名字。配「用户名取自」时照着这个填即可 */
            last_claims?: string[];
            /** @description 最近一次登录失败。给管理员看的线索——出问题的人和能改配置的人不是同一个 */
            last_error?: string;
            last_error_at?: string;
            last_error_user?: string;
            name_claim?: string;
            /**
             * @description RedirectURI 回调地址，给客户去 IdP 那边登记用。
             *     不显示的话，客户得自己拼，拼错一个字符就是一句 redirect_uri_mismatch
             */
            redirect_uri?: string;
            scopes?: string;
            updated_at?: string;
            username_claim?: string;
        };
        "handlers.impactNode": {
            ci_id?: number;
            depth?: number;
            name?: string;
            type?: string;
            via?: string;
        };
        "handlers.impactOut": {
            affected?: components["schemas"]["handlers.impactNode"][];
            /**
             * @description HasRelations 这个资源**有没有任何关系记录**。
             *     false 时界面要说"我们没有它的关系数据"，而不是"影响面：无"
             */
            has_relations?: boolean;
            root?: string;
            /**
             * @description Truncated 是否因深度上限而截断。
             *     ⚠️ 截断了必须说 —— 一份"影响面"清单如果悄悄少了一半，
             *     比没有这份清单更危险
             */
            truncated?: boolean;
        };
        "handlers.lbOut": {
            /** @description BackendState 云上自报的健康状态，原样透传 */
            backend_state?: string;
            /** @description Backends 见文件头：null / 0 / n 是三件不同的事 */
            backends?: number;
            id?: number;
            /**
             * @description K8sService 命中的 K8s Service（"集群 · 命名空间/服务名"），空 = 没对上。
             *
             *     	⚠️ 这个字段原来只有 /api/cloud-lb（network_resources.go）有，
             *     	这个列表接口没有 —— 于是**同一批 LB 在两个接口下判定能力不一样**：
             *     	那边能认出「后端是 K8s Pod/NEG」，这边只能报「0 个后端」。
             *     	而前端 LB 列表页用的正是这个接口。
             */
            k8s_service?: string;
            name?: string;
            port_range?: string;
            project?: string;
            protocol?: string;
            provider?: string;
            region?: string;
            scheme?: string;
            stale?: boolean;
            synced_at?: string;
            target?: string;
            vip?: string;
        };
        "handlers.licenseOut": {
            capacity?: components["schemas"]["license.Capacity"];
            /**
             * @description DaysUntilExpiry 距到期还有几天。
             *     ⚠️ 用指针：null = 不适用（永久授权 / 未激活），0 = 今天到期。
             *     压成 0 的话，永久授权会显示成"今天到期"。
             */
            days_until_expiry?: number;
            /**
             * @description Exceeded 超限项。**返回非空不代表要拒绝操作** ——
             *     它只该被渲染成提示并计入续购报价（LICENSING §6）。
             *     客户临时扩容 20 台节点结果系统罢工，是会丢客户的设计。
             */
            exceeded?: components["schemas"]["handlers.exceededOut"][];
            expires_at?: string;
            /** @description Features 已授权的功能码，前端据此做 EE 标记与入口显隐。 */
            features?: string[];
            /**
             * @description FeaturesMissing 这一档**没有**的功能。只给"有什么"的话，
             *     客户看不出缺什么，分档就不可见了（P1-74）
             */
            features_missing?: string[];
            /** @description FeaturesTotal 产品一共有多少个功能项 —— 让「5 / 12」这种双计数成为可能 */
            features_total?: number;
            /**
             * @description Fingerprint 安装指纹，**完整值**。
             *
             *     	界面上本来就要完整显示（客户申请授权时要报给我们），所以只有这一个字段。
             *
             *     	🔴 这里原来同时返回截断版 `fingerprint` 和完整版 `fingerprint_full`。
             *     	截断版一个消费方都没有，而"掩码 + 全量并存"这种写法会被后来的人
             *     	照抄到真正的敏感字段上 —— 那时候它就是个**看起来有保护、实际没有**的
             *     	假防线（OPSCMDB-031 P2-67）。
             *
             *     	⚠️ 指纹不是密钥，掩码它没有安全意义；截断只在**日志**里有意义
             *     	（见下方指纹不匹配那段，那里仍然用 ShortFingerprint）。
             */
            fingerprint?: string;
            license_id?: string;
            licensee?: components["schemas"]["handlers.licenseeOut"];
            perpetual?: boolean;
            read_only?: boolean;
            should_remind?: boolean;
            /**
             * @description Status 七态之一：active / grace / expired / lapsed /
             *     finger_mismat / not_activated / not_licensed
             */
            status?: string;
            usage?: components["schemas"]["handlers.usageOut"];
        };
        "handlers.licenseeOut": {
            contact?: string;
            org?: string;
            scope_name?: string;
        };
        "handlers.mcpTokenOut": {
            created_at?: string;
            created_by?: string;
            enabled?: boolean;
            /** @description Expired 已经过期。过期的令牌调用会被拒，界面上要和"停用"一样显眼 */
            expired?: boolean;
            /**
             * @description ExpiresAt null = **永久有效**。
             *
             *     	⚠️ 界面必须把"永久"明说出来，不能只留一个空白 ——
             *     	空白会被读成"这一项没填"，而它的实际含义是
             *     	"这个能读全库的凭据永远不会失效"（OPSCMDB-031 P1-70）。
             */
            expires_at?: string;
            /**
             * @description ExpiringSoon 30 天内到期。给人留出换发的时间，
             *     而不是等 AI 全线 401 之后才发现
             */
            expiring_soon?: boolean;
            hint?: string;
            id?: number;
            /**
             * @description LastUsedAt null = 从没用过。⚠️ 不能压成"很久以前"——
             *     "建了没人用"和"用过但很久没用"要采取的动作不一样
             */
            last_used_at?: string;
            last_used_ip?: string;
            name?: string;
            role_code?: string;
            /**
             * @description Tools 这条令牌实际能看到的工具数（已过授权分档 + 角色两层）。
             *     MCP 信息卡上那个数只按授权档次算，不含角色 —— 这里给的才是真数
             */
            tools?: number;
            /** @description Unrestricted 这条令牌不受权限约束（升级前遗留）。界面上要显眼地标出来 */
            unrestricted?: boolean;
        };
        "handlers.namespaceOut": {
            cluster_id?: number;
            cluster_name?: string;
            name?: string;
            /**
             * @description Phase 原样透传：Active / Terminating。
             *
             *     ⚠️ Terminating 不是"正在正常删除"就完事了 —— 卡在 Terminating 的命名空间
             *     是最常见的一类僵局（finalizer 没被清），它会一直占着名字，
             *     让同名的重建一直失败。所以它要作为一个**显眼的状态**出现，
             *     而不是和 Active 一样淡。
             */
            phase?: string;
            pods?: number;
            pods_bad?: number;
            /**
             * @description Project 归属项目（KubeSphere 的 project 之类）。空 = 没有归属登记，
             *     不是"没有项目"——界面上要能看出是没登记，而不是留白
             */
            project?: string;
            synced_at?: string;
            /**
             * @description 计数。null = 该集群还没采过对应资源，与 0（确实是空的）区分。
             *     判据抬到集群层，与节点列表、主机抽屉保持一致。
             */
            workloads?: number;
        };
        "handlers.nodeOut": {
            /**
             * @description —— 容量与装箱 ——
             *
             *     这几项原本只有 /k8s/node-capacity 有，节点页看不到，于是"这台还能不能再排"
             *     只能回 kubectl describe。合并进列表接口而不是让前端发两个请求再 join：
             *     分两个接口的话，按节点池筛选就没法走服务端，翻页也会错位。
             *
             *     ⚠️ 这里全是 **request/limit 的装箱率**，不是实际用量。
             *     两者差别极大（UAT 实测 request 装箱 48%，实际 CPU 只用了 5%），
             *     不能拿装箱率回答"这台机器忙不忙"。实际用量要 Prometheus，见 UsageAvailable。
             */
            alloc_cpu_m?: number;
            alloc_mem_mi?: number;
            cluster_id?: number;
            cluster_name?: string;
            /**
             * @description Conditions 压力位摘要（MemoryPressure / DiskPressure 等）。
             *     空字符串 = 没有压力位，不是"没采到"——采不到时整行都不会有。
             */
            conditions?: string;
            cpu_cap?: string;
            cpu_lim_pct?: number;
            cpu_req_pct?: number;
            /**
             * @description HeartbeatStale 状态是否已经过期。
             *
             *     ⚠️ 这是这个接口最重要的一个字段。kubelet 停止上报时，
             *     ready_status 会**停在最后一次的值**上 —— 一个已经失联两小时的节点
             *     在库里仍然是 Ready。只显示 ready_status 等于告诉用户"它好着呢"。
             */
            heartbeat_stale?: boolean;
            /**
             * @description HostCIID 关联到的主机台账 ID。
             *
             *     ⚠️ 0 表示**没关联上**，不是"没有主机"。常见原因：自建机不在云台账里、
             *     节点名与实例名不一致且 IP 也对不上。界面要显式说"未关联"并给出
             *     可以去查什么，而不是留白——留白会被当成"这台机器不用管"。
             */
            host_ci_id?: number;
            host_name?: string;
            internal_ip?: string;
            kubelet_version?: string;
            last_heartbeat?: string;
            lim_cpu_m?: number;
            lim_mem_mi?: number;
            machine_type?: string;
            mem_cap?: string;
            mem_lim_pct?: number;
            mem_req_pct?: number;
            name?: string;
            os_image?: string;
            /**
             * @description PodCount 节点自报的 Pod 数；PodsCollected 是我们实际采到的行数。
             *     两个都给：对不上说明两张表来自不同轮次的同步，而我们不知道哪份新。
             */
            pod_count?: number;
            pods_collected?: number;
            pool?: string;
            /**
             * @description ReadyStatus 节点自报的状态，**原样透传**（Ready / NotReady / Unknown）。
             *     不要在后端把它归并成布尔量：Unknown 是"控制面也联系不上它"，
             *     与 NotReady（联系得上，但它说自己没准备好）是两回事。
             */
            ready_status?: string;
            req_cpu_m?: number;
            req_mem_mi?: number;
            roles?: string;
            /**
             * @description StaleReason 状态不可信的原因：heartbeat=节点心跳停了；collection=采集本身停了。
             *     ⚠️ 两者处置完全不同，界面不能都写成「节点失联」。
             */
            stale_reason?: string;
            synced_at?: string;
        };
        "handlers.oidcEndpoints": {
            authorization_endpoint?: string;
            issuer?: string;
            jwks_uri?: string;
            token_endpoint?: string;
        };
        "handlers.podOut": {
            cluster_id?: number;
            cluster_name?: string;
            cpu_lim_m?: number;
            /**
             * @description CPUReqM / MemReqMi / CPULimM / MemLimMi —— **配置**不是用量。
             *
             *     	「这个 Pod 申请了多少」是判断"是不是 request 写太大导致装不下"的唯一依据，
             *     	用量页有实际用量，但那回答不了这个问题。
             *
             *     	⚠️ 用指针是为了把 SQL NULL 原样透出去，**但当前采集写的是 0 不是 NULL**
             *     	（实测 kube-scheduler：cpu_req_m=100，其余三项都是 0）。
             *     	也就是说这一层现在区分不出「没配」和「配了 0」——
             *     	好在 k8s 里不存在 request=0 的有效配置，所以前端把 0 当"没配"是安全的。
             *     	留着指针是为了采集哪天改成写 NULL 时这里不用再动；
             *     	🔴 但别据此以为已经能区分两者了 —— 真要区分得先改采集。
             */
            cpu_req_m?: number;
            mem_lim_mi?: number;
            mem_req_mi?: number;
            name?: string;
            namespace?: string;
            node_name?: string;
            /**
             * @description Phase / Reason 都**原样透传**：运维是拿 CrashLoopBackOff、
             *     ImagePullBackOff 这些词直接去 kubectl 和搜索引擎里查的，
             *     翻译成中文就对不上了。
             */
            phase?: string;
            pod_ip?: string;
            reason?: string;
            restarts?: number;
            /**
             * @description StartTime 起来多久了。CrashLoop 的 Pod 会不断重启，
             *     这个值会一直很小 —— 它本身就是一条线索。
             */
            start_time?: string;
            synced_at?: string;
            workload?: string;
        };
        "handlers.pvcOut": {
            capacity?: string;
            cluster_id?: number;
            cluster_name?: string;
            /**
             * @description MonthlyUSD 这个卷每月多少钱。**只有能算出来的才有**（0 = 算不出来，不是免费）。
             *
             *     	⚠️ 后端一直在别处算这个数（orphans.go 里逐条算好了），
             *     	而这一页一个字都没显示 —— 于是「1Ti 当前无使用者」只是个中性事实，
             *     	紧跟一个「$102.4/月」才产生行动力。
             *     	实测 DEV 集群 21 个无使用者的卷，合计 **$866/月**（OPSCMDB-031 P1-23）。
             */
            monthly_usd?: number;
            name?: string;
            namespace?: string;
            /**
             * @description Orphan 没有任何 Pod 在用它。
             *
             *     ⚠️ 这**不等于可以删**：定时任务的卷、刚 drain 完的有状态服务，
             *     都会短暂没有使用者。所以只标注"当前没有使用者"，不写"可回收" ——
             *     后者会让人放心地删掉别人的数据。
             */
            orphan?: boolean;
            /** @description Status 原样透传：Bound / Pending / Lost */
            status?: string;
            storage_class?: string;
            synced_at?: string;
            volume_name?: string;
        };
        "handlers.relatedDomain": {
            fqdn?: string;
            ip?: string;
        };
        "handlers.relationOut": {
            dst_ci_id?: number;
            dst_name?: string;
            dst_type?: string;
            id?: number;
            /**
             * @description Origin 关系是怎么来的：sync（采集推断）/ manual（人工登记）。
             *     采集推断的关系会随下一轮同步消失，人工登记的不会 —— 处置方式不同
             */
            origin?: string;
            rel_type?: string;
            src_ci_id?: number;
            src_name?: string;
            src_type?: string;
        };
        "handlers.situationOut": {
            /** @description Attention 按严重度排好序的待办。空数组 = 全部正常且都统计成功了 */
            attention?: components["schemas"]["handlers.attentionItem"][];
            /**
             * @description Freshness 各类数据最后一次采集的时刻。台账是快照不是实时，
             *     首页尤其要显示它 —— 否则整页数字看起来都像"此刻"
             */
            freshness?: {
                [key: string]: string;
            };
            generated_at?: string;
            /** @description Inventory 家底盘点。同样用指针表达"没统计出来" */
            inventory?: {
                [key: string]: number;
            };
        };
        "handlers.subnetOut": {
            cidr?: string;
            gateway?: string;
            id?: number;
            name?: string;
            network?: string;
            /**
             * @description NetworkMode auto / custom。auto 模式的 VPC 会在每个区域自动建子网，
             *     网段是固定的 —— 看到意料之外的子网时，这一列能立刻解释"它哪来的"
             */
            network_mode?: string;
            project?: string;
            provider?: string;
            region?: string;
            /**
             * @description Stale 云上已经查不到它了。
             *
             *     ⚠️ 不能因为 stale 就把行删掉：别的资源可能还引用着这个网段
             *     （安全组规则、对端路由）。留着并显式标注，才查得出"这条规则指向的子网没了"。
             */
            stale?: boolean;
            synced_at?: string;
        };
        "handlers.svcOut": {
            cluster_id?: number;
            cluster_ip?: string;
            cluster_name?: string;
            /**
             * @description Exposed 是不是对外可达。
             *
             *     ⚠️ 判据是"有外部 IP 或有 Ingress 主机名"，不是"type==LoadBalancer"：
             *     内网 LB（scheme=INTERNAL）也是 LoadBalancer 类型，把它算成对外暴露
             *     会让暴露面清单里塞满误报，而误报多了真的就没人看了。
             */
            exposed?: boolean;
            external_ip?: string;
            /**
             * @description Hosts 指向这个 Service 的 Ingress 主机名（去重）。
             *     空数组 = 没有 Ingress 指向它，这是**信息**（内部服务本来就没有）
             */
            hosts?: string[];
            /**
             * @description InternalLB 拿到的是私网 VIP —— 内网负载均衡，不算对外暴露。
             *     单独出一个字段而不是只让它落进 internal：界面上要能说清
             *     "它有 VIP，只是那个 VIP 在内网"，否则看起来像没配好。
             */
            internal_lb?: boolean;
            name?: string;
            namespace?: string;
            /**
             * @description PendingLB type=LoadBalancer 但一直没拿到外部 IP。
             *     这不是"没暴露"，是**卡住了** —— 云厂商配额用尽、
             *     子网没空 IP 时就是这个现象，而 Service 看起来一切正常。
             */
            pending_lb?: boolean;
            ports?: string;
            synced_at?: string;
            /** @description Type 原样透传：ClusterIP / NodePort / LoadBalancer / ExternalName */
            type?: string;
        };
        "handlers.taskOut": {
            enabled?: boolean;
            /** @description LastOK 上一次是否成功。用指针：null = 从没跑过，无从谈成败 */
            last_ok?: boolean;
            last_result?: string;
            /** @description LastRunAt 空 = 从没跑过（不是"刚跑完"） */
            last_run_at?: string;
            /**
             * @description LastStatus 最近一次执行流水的状态（ok/partial/fail/skipped/...）。
             *     空 = 没有流水记录。它比 last_ok 精确，taskState 优先用它
             */
            last_status?: string;
            name?: string;
            notify_enabled?: boolean;
            /**
             * @description Overdue 按 schedule 早该跑了却没跑。
             *
             *     ⚠️ 这是"任务静默停摆"的唯一信号。调度器挂掉时任务不会报错，
             *     它只是**不再执行** —— 界面上永远显示着上一次的成功结果，
             *     而数据在慢慢变旧。
             */
            overdue?: boolean;
            schedule?: string;
            task_key?: string;
        };
        "handlers.usageOut": {
            cloud_accounts?: number;
            clusters?: number;
            nodes?: number;
            /** @description RetentionDays 当前配置的保留天数。⚠️ 0 = 永不清理（无限），不是"零天" */
            retention_days?: number;
            /**
             * @description RetentionUnlimited 把上面那个 0 的语义**显式**说出来。
             *
             *     	原来 usage 里压根没有保留天数这一项，界面因此渲染成「—」——
             *     	看起来像"没数据"，而实际是配了 0（永不清理）。
             *     	光给一个 0 也不够：前端还得自己知道"0 在这里表示无限"，
             *     	而那种约定迟早会在某一处被忘掉（本产品已经在授权页忘过一次）。
             */
            retention_unlimited?: boolean;
            seats?: number;
            tenants?: number;
        };
        "handlers.workloadOut": {
            cluster_id?: number;
            cluster_name?: string;
            /**
             * @description Image / ImageTag 分开存：排障时问的是"跑的是哪个 tag"，
             *     而完整镜像串里仓库地址往往长到把这一列挤没
             */
            image?: string;
            image_tag?: string;
            kind?: string;
            name?: string;
            namespace?: string;
            /**
             * @description ReplicasDesired / ReplicasReady 是这一页的**核心信号**。
             *
             *     ⚠️ 三种情况在界面上必须分开，它们看起来都"不是满副本"：
             *     	3/3   正常
             *     	1/3   降级中 —— 有实例在跑，但不够
             *     	0/3   全挂 —— 一个都没起来
             *     	0/0   **被刻意缩到 0**（停服、定时任务模板、金丝雀留空）
             *     	      这是正常状态，标红会在每套环境里制造一批假故障
             */
            replicas_desired?: number;
            replicas_ready?: number;
            status?: string;
            synced_at?: string;
        };
        "httpx.APIError": {
            /** @description Code 机器可读的错误码，如 "cluster_unreachable" */
            code?: string;
            /** @description Message 英文技术描述，给日志和运维排查用，前端不展示给终端用户 */
            message?: string;
            /** @description MessageKey 前端语言包里的 key，如 "error.clusterUnreachable" */
            message_key?: string;
            /** @description Params 插值参数，如 {"cluster":"g32-prod","timeout":10} */
            params?: {
                [key: string]: unknown;
            };
            /** @description RequestID 贯穿一次请求，用户截图里能看到，运维据此捞日志 */
            request_id?: string;
        };
        "httpx.Caveat": {
            /** @description Kind 机器可读的局限类型，前端据此决定色调（never 用 bad，partial 用 warn） */
            kind?: string;
            /**
             * @description NoteKey / NoteParams 前端语言包的 key 和插值参数。
             *
             *     ⚠️ 必须能说出"去哪儿做什么"。只说"数据缺失"等于告诉人有问题但不给出路。
             *     ⚠️ 不发拼好的句子：后端拼的中文在英文界面上永远是中文（OPSCMDB-054）。
             */
            note_key?: string;
            note_params?: {
                [key: string]: unknown;
            };
        };
        "httpx.Freshness": {
            /**
             * @description Known 这次有没有取到该任务的周期。
             *
             *     	⚠️ false 时前端**不能**下"数据是旧的"这个结论 ——
             *     	取不到周期和"数据很新"是两件事，压成同一种表现就又回到
             *     	"看起来一切正常"。
             */
            known?: boolean;
            /** @description StaleAfterSeconds 超过这个时长没更新就算旧了。Known=false 时无意义 */
            stale_after_seconds?: number;
            /** @description TaskKey 负责刷新这批数据的定时任务 */
            task_key?: string;
        };
        "httpx.ListResponse-handlers_certOut": {
            /**
             * @description Caveat 这批数据的**已知局限**：某一列为什么整片是空的。
             *
             *     	🔴 起因是一次生产 P0：证书列表里 890 张证书有 828 张到期日为空，
             *     	页面上没有任何地方说为什么 —— 而真相是"证书到期检测（443）"这个
             *     	定时任务被停用且从没跑过，也就是**证书临期提醒根本不工作**。
             *     	看的人只会以为"数据还没采到"，不会想到去开一个任务。
             *     	（那两张 8 月 21 日到期的生产网关证书就是这么活到剩 28 小时的。）
             *
             *     	⚠️ 与 Freshness 的区别：Freshness 说"这批数据旧了"，
             *     	Caveat 说"这批数据里有一整类信息压根不存在，以及为什么"。
             *     	两者都不能靠前端猜 —— 前端只看得见空值，看不见空值的来历。
             */
            caveat?: components["schemas"]["httpx.Caveat"];
            /**
             * @description Facets 每个筛选维度下各取值的**全量**计数，如 {"status":{"running":347}}。
             *
             *     必须按全量统计，不能按当前页。筛选下拉要显示"运行中 347"，
             *     若只统计当前页，那个数字就是错的 —— 而它看起来完全正常，没人会怀疑。
             */
            facets?: {
                [key: string]: {
                    [key: string]: number;
                };
            };
            /**
             * @description Freshness 这批数据"多久没更新就算旧了"的判据。
             *
             *     	🔴 判据必须由**后端**给：它来自那个采集任务自己的 cron 周期。
             *     	前端写死一个"6 小时"，任务周期一改就分叉，而分叉的表现是
             *     	页面安静地不报警（OPSCMDB-031 P1-5：主机页写着「17 小时前」，
             *     	既没标红，又和数据源页的「从没同步过」互相矛盾，三处三个说法）。
             */
            freshness?: components["schemas"]["httpx.Freshness"];
            /**
             * @description ⚠️ 绝不能是 nil。nil 切片序列化成 JSON null，
             *     前端 data.items.length 直接抛异常 —— 而这只在"结果为空"时发生，
             *     有数据的时候一切正常，最容易漏测。NewList 保证了这一点。
             */
            items?: components["schemas"]["handlers.certOut"][];
            page?: number;
            size?: number;
            total?: number;
        };
        "httpx.ListResponse-handlers_clusterOut": {
            /**
             * @description Caveat 这批数据的**已知局限**：某一列为什么整片是空的。
             *
             *     	🔴 起因是一次生产 P0：证书列表里 890 张证书有 828 张到期日为空，
             *     	页面上没有任何地方说为什么 —— 而真相是"证书到期检测（443）"这个
             *     	定时任务被停用且从没跑过，也就是**证书临期提醒根本不工作**。
             *     	看的人只会以为"数据还没采到"，不会想到去开一个任务。
             *     	（那两张 8 月 21 日到期的生产网关证书就是这么活到剩 28 小时的。）
             *
             *     	⚠️ 与 Freshness 的区别：Freshness 说"这批数据旧了"，
             *     	Caveat 说"这批数据里有一整类信息压根不存在，以及为什么"。
             *     	两者都不能靠前端猜 —— 前端只看得见空值，看不见空值的来历。
             */
            caveat?: components["schemas"]["httpx.Caveat"];
            /**
             * @description Facets 每个筛选维度下各取值的**全量**计数，如 {"status":{"running":347}}。
             *
             *     必须按全量统计，不能按当前页。筛选下拉要显示"运行中 347"，
             *     若只统计当前页，那个数字就是错的 —— 而它看起来完全正常，没人会怀疑。
             */
            facets?: {
                [key: string]: {
                    [key: string]: number;
                };
            };
            /**
             * @description Freshness 这批数据"多久没更新就算旧了"的判据。
             *
             *     	🔴 判据必须由**后端**给：它来自那个采集任务自己的 cron 周期。
             *     	前端写死一个"6 小时"，任务周期一改就分叉，而分叉的表现是
             *     	页面安静地不报警（OPSCMDB-031 P1-5：主机页写着「17 小时前」，
             *     	既没标红，又和数据源页的「从没同步过」互相矛盾，三处三个说法）。
             */
            freshness?: components["schemas"]["httpx.Freshness"];
            /**
             * @description ⚠️ 绝不能是 nil。nil 切片序列化成 JSON null，
             *     前端 data.items.length 直接抛异常 —— 而这只在"结果为空"时发生，
             *     有数据的时候一切正常，最容易漏测。NewList 保证了这一点。
             */
            items?: components["schemas"]["handlers.clusterOut"][];
            page?: number;
            size?: number;
            total?: number;
        };
        "httpx.ListResponse-handlers_dataSourceOut": {
            /**
             * @description Caveat 这批数据的**已知局限**：某一列为什么整片是空的。
             *
             *     	🔴 起因是一次生产 P0：证书列表里 890 张证书有 828 张到期日为空，
             *     	页面上没有任何地方说为什么 —— 而真相是"证书到期检测（443）"这个
             *     	定时任务被停用且从没跑过，也就是**证书临期提醒根本不工作**。
             *     	看的人只会以为"数据还没采到"，不会想到去开一个任务。
             *     	（那两张 8 月 21 日到期的生产网关证书就是这么活到剩 28 小时的。）
             *
             *     	⚠️ 与 Freshness 的区别：Freshness 说"这批数据旧了"，
             *     	Caveat 说"这批数据里有一整类信息压根不存在，以及为什么"。
             *     	两者都不能靠前端猜 —— 前端只看得见空值，看不见空值的来历。
             */
            caveat?: components["schemas"]["httpx.Caveat"];
            /**
             * @description Facets 每个筛选维度下各取值的**全量**计数，如 {"status":{"running":347}}。
             *
             *     必须按全量统计，不能按当前页。筛选下拉要显示"运行中 347"，
             *     若只统计当前页，那个数字就是错的 —— 而它看起来完全正常，没人会怀疑。
             */
            facets?: {
                [key: string]: {
                    [key: string]: number;
                };
            };
            /**
             * @description Freshness 这批数据"多久没更新就算旧了"的判据。
             *
             *     	🔴 判据必须由**后端**给：它来自那个采集任务自己的 cron 周期。
             *     	前端写死一个"6 小时"，任务周期一改就分叉，而分叉的表现是
             *     	页面安静地不报警（OPSCMDB-031 P1-5：主机页写着「17 小时前」，
             *     	既没标红，又和数据源页的「从没同步过」互相矛盾，三处三个说法）。
             */
            freshness?: components["schemas"]["httpx.Freshness"];
            /**
             * @description ⚠️ 绝不能是 nil。nil 切片序列化成 JSON null，
             *     前端 data.items.length 直接抛异常 —— 而这只在"结果为空"时发生，
             *     有数据的时候一切正常，最容易漏测。NewList 保证了这一点。
             */
            items?: components["schemas"]["handlers.dataSourceOut"][];
            page?: number;
            size?: number;
            total?: number;
        };
        "httpx.ListResponse-handlers_domainRowOut": {
            /**
             * @description Caveat 这批数据的**已知局限**：某一列为什么整片是空的。
             *
             *     	🔴 起因是一次生产 P0：证书列表里 890 张证书有 828 张到期日为空，
             *     	页面上没有任何地方说为什么 —— 而真相是"证书到期检测（443）"这个
             *     	定时任务被停用且从没跑过，也就是**证书临期提醒根本不工作**。
             *     	看的人只会以为"数据还没采到"，不会想到去开一个任务。
             *     	（那两张 8 月 21 日到期的生产网关证书就是这么活到剩 28 小时的。）
             *
             *     	⚠️ 与 Freshness 的区别：Freshness 说"这批数据旧了"，
             *     	Caveat 说"这批数据里有一整类信息压根不存在，以及为什么"。
             *     	两者都不能靠前端猜 —— 前端只看得见空值，看不见空值的来历。
             */
            caveat?: components["schemas"]["httpx.Caveat"];
            /**
             * @description Facets 每个筛选维度下各取值的**全量**计数，如 {"status":{"running":347}}。
             *
             *     必须按全量统计，不能按当前页。筛选下拉要显示"运行中 347"，
             *     若只统计当前页，那个数字就是错的 —— 而它看起来完全正常，没人会怀疑。
             */
            facets?: {
                [key: string]: {
                    [key: string]: number;
                };
            };
            /**
             * @description Freshness 这批数据"多久没更新就算旧了"的判据。
             *
             *     	🔴 判据必须由**后端**给：它来自那个采集任务自己的 cron 周期。
             *     	前端写死一个"6 小时"，任务周期一改就分叉，而分叉的表现是
             *     	页面安静地不报警（OPSCMDB-031 P1-5：主机页写着「17 小时前」，
             *     	既没标红，又和数据源页的「从没同步过」互相矛盾，三处三个说法）。
             */
            freshness?: components["schemas"]["httpx.Freshness"];
            /**
             * @description ⚠️ 绝不能是 nil。nil 切片序列化成 JSON null，
             *     前端 data.items.length 直接抛异常 —— 而这只在"结果为空"时发生，
             *     有数据的时候一切正常，最容易漏测。NewList 保证了这一点。
             */
            items?: components["schemas"]["handlers.domainRowOut"][];
            page?: number;
            size?: number;
            total?: number;
        };
        "httpx.ListResponse-handlers_eventOut": {
            /**
             * @description Caveat 这批数据的**已知局限**：某一列为什么整片是空的。
             *
             *     	🔴 起因是一次生产 P0：证书列表里 890 张证书有 828 张到期日为空，
             *     	页面上没有任何地方说为什么 —— 而真相是"证书到期检测（443）"这个
             *     	定时任务被停用且从没跑过，也就是**证书临期提醒根本不工作**。
             *     	看的人只会以为"数据还没采到"，不会想到去开一个任务。
             *     	（那两张 8 月 21 日到期的生产网关证书就是这么活到剩 28 小时的。）
             *
             *     	⚠️ 与 Freshness 的区别：Freshness 说"这批数据旧了"，
             *     	Caveat 说"这批数据里有一整类信息压根不存在，以及为什么"。
             *     	两者都不能靠前端猜 —— 前端只看得见空值，看不见空值的来历。
             */
            caveat?: components["schemas"]["httpx.Caveat"];
            /**
             * @description Facets 每个筛选维度下各取值的**全量**计数，如 {"status":{"running":347}}。
             *
             *     必须按全量统计，不能按当前页。筛选下拉要显示"运行中 347"，
             *     若只统计当前页，那个数字就是错的 —— 而它看起来完全正常，没人会怀疑。
             */
            facets?: {
                [key: string]: {
                    [key: string]: number;
                };
            };
            /**
             * @description Freshness 这批数据"多久没更新就算旧了"的判据。
             *
             *     	🔴 判据必须由**后端**给：它来自那个采集任务自己的 cron 周期。
             *     	前端写死一个"6 小时"，任务周期一改就分叉，而分叉的表现是
             *     	页面安静地不报警（OPSCMDB-031 P1-5：主机页写着「17 小时前」，
             *     	既没标红，又和数据源页的「从没同步过」互相矛盾，三处三个说法）。
             */
            freshness?: components["schemas"]["httpx.Freshness"];
            /**
             * @description ⚠️ 绝不能是 nil。nil 切片序列化成 JSON null，
             *     前端 data.items.length 直接抛异常 —— 而这只在"结果为空"时发生，
             *     有数据的时候一切正常，最容易漏测。NewList 保证了这一点。
             */
            items?: components["schemas"]["handlers.eventOut"][];
            page?: number;
            size?: number;
            total?: number;
        };
        "httpx.ListResponse-handlers_exposureOut": {
            /**
             * @description Caveat 这批数据的**已知局限**：某一列为什么整片是空的。
             *
             *     	🔴 起因是一次生产 P0：证书列表里 890 张证书有 828 张到期日为空，
             *     	页面上没有任何地方说为什么 —— 而真相是"证书到期检测（443）"这个
             *     	定时任务被停用且从没跑过，也就是**证书临期提醒根本不工作**。
             *     	看的人只会以为"数据还没采到"，不会想到去开一个任务。
             *     	（那两张 8 月 21 日到期的生产网关证书就是这么活到剩 28 小时的。）
             *
             *     	⚠️ 与 Freshness 的区别：Freshness 说"这批数据旧了"，
             *     	Caveat 说"这批数据里有一整类信息压根不存在，以及为什么"。
             *     	两者都不能靠前端猜 —— 前端只看得见空值，看不见空值的来历。
             */
            caveat?: components["schemas"]["httpx.Caveat"];
            /**
             * @description Facets 每个筛选维度下各取值的**全量**计数，如 {"status":{"running":347}}。
             *
             *     必须按全量统计，不能按当前页。筛选下拉要显示"运行中 347"，
             *     若只统计当前页，那个数字就是错的 —— 而它看起来完全正常，没人会怀疑。
             */
            facets?: {
                [key: string]: {
                    [key: string]: number;
                };
            };
            /**
             * @description Freshness 这批数据"多久没更新就算旧了"的判据。
             *
             *     	🔴 判据必须由**后端**给：它来自那个采集任务自己的 cron 周期。
             *     	前端写死一个"6 小时"，任务周期一改就分叉，而分叉的表现是
             *     	页面安静地不报警（OPSCMDB-031 P1-5：主机页写着「17 小时前」，
             *     	既没标红，又和数据源页的「从没同步过」互相矛盾，三处三个说法）。
             */
            freshness?: components["schemas"]["httpx.Freshness"];
            /**
             * @description ⚠️ 绝不能是 nil。nil 切片序列化成 JSON null，
             *     前端 data.items.length 直接抛异常 —— 而这只在"结果为空"时发生，
             *     有数据的时候一切正常，最容易漏测。NewList 保证了这一点。
             */
            items?: components["schemas"]["handlers.exposureOut"][];
            page?: number;
            size?: number;
            total?: number;
        };
        "httpx.ListResponse-handlers_hostOut": {
            /**
             * @description Caveat 这批数据的**已知局限**：某一列为什么整片是空的。
             *
             *     	🔴 起因是一次生产 P0：证书列表里 890 张证书有 828 张到期日为空，
             *     	页面上没有任何地方说为什么 —— 而真相是"证书到期检测（443）"这个
             *     	定时任务被停用且从没跑过，也就是**证书临期提醒根本不工作**。
             *     	看的人只会以为"数据还没采到"，不会想到去开一个任务。
             *     	（那两张 8 月 21 日到期的生产网关证书就是这么活到剩 28 小时的。）
             *
             *     	⚠️ 与 Freshness 的区别：Freshness 说"这批数据旧了"，
             *     	Caveat 说"这批数据里有一整类信息压根不存在，以及为什么"。
             *     	两者都不能靠前端猜 —— 前端只看得见空值，看不见空值的来历。
             */
            caveat?: components["schemas"]["httpx.Caveat"];
            /**
             * @description Facets 每个筛选维度下各取值的**全量**计数，如 {"status":{"running":347}}。
             *
             *     必须按全量统计，不能按当前页。筛选下拉要显示"运行中 347"，
             *     若只统计当前页，那个数字就是错的 —— 而它看起来完全正常，没人会怀疑。
             */
            facets?: {
                [key: string]: {
                    [key: string]: number;
                };
            };
            /**
             * @description Freshness 这批数据"多久没更新就算旧了"的判据。
             *
             *     	🔴 判据必须由**后端**给：它来自那个采集任务自己的 cron 周期。
             *     	前端写死一个"6 小时"，任务周期一改就分叉，而分叉的表现是
             *     	页面安静地不报警（OPSCMDB-031 P1-5：主机页写着「17 小时前」，
             *     	既没标红，又和数据源页的「从没同步过」互相矛盾，三处三个说法）。
             */
            freshness?: components["schemas"]["httpx.Freshness"];
            /**
             * @description ⚠️ 绝不能是 nil。nil 切片序列化成 JSON null，
             *     前端 data.items.length 直接抛异常 —— 而这只在"结果为空"时发生，
             *     有数据的时候一切正常，最容易漏测。NewList 保证了这一点。
             */
            items?: components["schemas"]["handlers.hostOut"][];
            page?: number;
            size?: number;
            total?: number;
        };
        "httpx.ListResponse-handlers_lbOut": {
            /**
             * @description Caveat 这批数据的**已知局限**：某一列为什么整片是空的。
             *
             *     	🔴 起因是一次生产 P0：证书列表里 890 张证书有 828 张到期日为空，
             *     	页面上没有任何地方说为什么 —— 而真相是"证书到期检测（443）"这个
             *     	定时任务被停用且从没跑过，也就是**证书临期提醒根本不工作**。
             *     	看的人只会以为"数据还没采到"，不会想到去开一个任务。
             *     	（那两张 8 月 21 日到期的生产网关证书就是这么活到剩 28 小时的。）
             *
             *     	⚠️ 与 Freshness 的区别：Freshness 说"这批数据旧了"，
             *     	Caveat 说"这批数据里有一整类信息压根不存在，以及为什么"。
             *     	两者都不能靠前端猜 —— 前端只看得见空值，看不见空值的来历。
             */
            caveat?: components["schemas"]["httpx.Caveat"];
            /**
             * @description Facets 每个筛选维度下各取值的**全量**计数，如 {"status":{"running":347}}。
             *
             *     必须按全量统计，不能按当前页。筛选下拉要显示"运行中 347"，
             *     若只统计当前页，那个数字就是错的 —— 而它看起来完全正常，没人会怀疑。
             */
            facets?: {
                [key: string]: {
                    [key: string]: number;
                };
            };
            /**
             * @description Freshness 这批数据"多久没更新就算旧了"的判据。
             *
             *     	🔴 判据必须由**后端**给：它来自那个采集任务自己的 cron 周期。
             *     	前端写死一个"6 小时"，任务周期一改就分叉，而分叉的表现是
             *     	页面安静地不报警（OPSCMDB-031 P1-5：主机页写着「17 小时前」，
             *     	既没标红，又和数据源页的「从没同步过」互相矛盾，三处三个说法）。
             */
            freshness?: components["schemas"]["httpx.Freshness"];
            /**
             * @description ⚠️ 绝不能是 nil。nil 切片序列化成 JSON null，
             *     前端 data.items.length 直接抛异常 —— 而这只在"结果为空"时发生，
             *     有数据的时候一切正常，最容易漏测。NewList 保证了这一点。
             */
            items?: components["schemas"]["handlers.lbOut"][];
            page?: number;
            size?: number;
            total?: number;
        };
        "httpx.ListResponse-handlers_mcpTokenOut": {
            /**
             * @description Caveat 这批数据的**已知局限**：某一列为什么整片是空的。
             *
             *     	🔴 起因是一次生产 P0：证书列表里 890 张证书有 828 张到期日为空，
             *     	页面上没有任何地方说为什么 —— 而真相是"证书到期检测（443）"这个
             *     	定时任务被停用且从没跑过，也就是**证书临期提醒根本不工作**。
             *     	看的人只会以为"数据还没采到"，不会想到去开一个任务。
             *     	（那两张 8 月 21 日到期的生产网关证书就是这么活到剩 28 小时的。）
             *
             *     	⚠️ 与 Freshness 的区别：Freshness 说"这批数据旧了"，
             *     	Caveat 说"这批数据里有一整类信息压根不存在，以及为什么"。
             *     	两者都不能靠前端猜 —— 前端只看得见空值，看不见空值的来历。
             */
            caveat?: components["schemas"]["httpx.Caveat"];
            /**
             * @description Facets 每个筛选维度下各取值的**全量**计数，如 {"status":{"running":347}}。
             *
             *     必须按全量统计，不能按当前页。筛选下拉要显示"运行中 347"，
             *     若只统计当前页，那个数字就是错的 —— 而它看起来完全正常，没人会怀疑。
             */
            facets?: {
                [key: string]: {
                    [key: string]: number;
                };
            };
            /**
             * @description Freshness 这批数据"多久没更新就算旧了"的判据。
             *
             *     	🔴 判据必须由**后端**给：它来自那个采集任务自己的 cron 周期。
             *     	前端写死一个"6 小时"，任务周期一改就分叉，而分叉的表现是
             *     	页面安静地不报警（OPSCMDB-031 P1-5：主机页写着「17 小时前」，
             *     	既没标红，又和数据源页的「从没同步过」互相矛盾，三处三个说法）。
             */
            freshness?: components["schemas"]["httpx.Freshness"];
            /**
             * @description ⚠️ 绝不能是 nil。nil 切片序列化成 JSON null，
             *     前端 data.items.length 直接抛异常 —— 而这只在"结果为空"时发生，
             *     有数据的时候一切正常，最容易漏测。NewList 保证了这一点。
             */
            items?: components["schemas"]["handlers.mcpTokenOut"][];
            page?: number;
            size?: number;
            total?: number;
        };
        "httpx.ListResponse-handlers_namespaceOut": {
            /**
             * @description Caveat 这批数据的**已知局限**：某一列为什么整片是空的。
             *
             *     	🔴 起因是一次生产 P0：证书列表里 890 张证书有 828 张到期日为空，
             *     	页面上没有任何地方说为什么 —— 而真相是"证书到期检测（443）"这个
             *     	定时任务被停用且从没跑过，也就是**证书临期提醒根本不工作**。
             *     	看的人只会以为"数据还没采到"，不会想到去开一个任务。
             *     	（那两张 8 月 21 日到期的生产网关证书就是这么活到剩 28 小时的。）
             *
             *     	⚠️ 与 Freshness 的区别：Freshness 说"这批数据旧了"，
             *     	Caveat 说"这批数据里有一整类信息压根不存在，以及为什么"。
             *     	两者都不能靠前端猜 —— 前端只看得见空值，看不见空值的来历。
             */
            caveat?: components["schemas"]["httpx.Caveat"];
            /**
             * @description Facets 每个筛选维度下各取值的**全量**计数，如 {"status":{"running":347}}。
             *
             *     必须按全量统计，不能按当前页。筛选下拉要显示"运行中 347"，
             *     若只统计当前页，那个数字就是错的 —— 而它看起来完全正常，没人会怀疑。
             */
            facets?: {
                [key: string]: {
                    [key: string]: number;
                };
            };
            /**
             * @description Freshness 这批数据"多久没更新就算旧了"的判据。
             *
             *     	🔴 判据必须由**后端**给：它来自那个采集任务自己的 cron 周期。
             *     	前端写死一个"6 小时"，任务周期一改就分叉，而分叉的表现是
             *     	页面安静地不报警（OPSCMDB-031 P1-5：主机页写着「17 小时前」，
             *     	既没标红，又和数据源页的「从没同步过」互相矛盾，三处三个说法）。
             */
            freshness?: components["schemas"]["httpx.Freshness"];
            /**
             * @description ⚠️ 绝不能是 nil。nil 切片序列化成 JSON null，
             *     前端 data.items.length 直接抛异常 —— 而这只在"结果为空"时发生，
             *     有数据的时候一切正常，最容易漏测。NewList 保证了这一点。
             */
            items?: components["schemas"]["handlers.namespaceOut"][];
            page?: number;
            size?: number;
            total?: number;
        };
        "httpx.ListResponse-handlers_nodeOut": {
            /**
             * @description Caveat 这批数据的**已知局限**：某一列为什么整片是空的。
             *
             *     	🔴 起因是一次生产 P0：证书列表里 890 张证书有 828 张到期日为空，
             *     	页面上没有任何地方说为什么 —— 而真相是"证书到期检测（443）"这个
             *     	定时任务被停用且从没跑过，也就是**证书临期提醒根本不工作**。
             *     	看的人只会以为"数据还没采到"，不会想到去开一个任务。
             *     	（那两张 8 月 21 日到期的生产网关证书就是这么活到剩 28 小时的。）
             *
             *     	⚠️ 与 Freshness 的区别：Freshness 说"这批数据旧了"，
             *     	Caveat 说"这批数据里有一整类信息压根不存在，以及为什么"。
             *     	两者都不能靠前端猜 —— 前端只看得见空值，看不见空值的来历。
             */
            caveat?: components["schemas"]["httpx.Caveat"];
            /**
             * @description Facets 每个筛选维度下各取值的**全量**计数，如 {"status":{"running":347}}。
             *
             *     必须按全量统计，不能按当前页。筛选下拉要显示"运行中 347"，
             *     若只统计当前页，那个数字就是错的 —— 而它看起来完全正常，没人会怀疑。
             */
            facets?: {
                [key: string]: {
                    [key: string]: number;
                };
            };
            /**
             * @description Freshness 这批数据"多久没更新就算旧了"的判据。
             *
             *     	🔴 判据必须由**后端**给：它来自那个采集任务自己的 cron 周期。
             *     	前端写死一个"6 小时"，任务周期一改就分叉，而分叉的表现是
             *     	页面安静地不报警（OPSCMDB-031 P1-5：主机页写着「17 小时前」，
             *     	既没标红，又和数据源页的「从没同步过」互相矛盾，三处三个说法）。
             */
            freshness?: components["schemas"]["httpx.Freshness"];
            /**
             * @description ⚠️ 绝不能是 nil。nil 切片序列化成 JSON null，
             *     前端 data.items.length 直接抛异常 —— 而这只在"结果为空"时发生，
             *     有数据的时候一切正常，最容易漏测。NewList 保证了这一点。
             */
            items?: components["schemas"]["handlers.nodeOut"][];
            page?: number;
            size?: number;
            total?: number;
        };
        "httpx.ListResponse-handlers_podOut": {
            /**
             * @description Caveat 这批数据的**已知局限**：某一列为什么整片是空的。
             *
             *     	🔴 起因是一次生产 P0：证书列表里 890 张证书有 828 张到期日为空，
             *     	页面上没有任何地方说为什么 —— 而真相是"证书到期检测（443）"这个
             *     	定时任务被停用且从没跑过，也就是**证书临期提醒根本不工作**。
             *     	看的人只会以为"数据还没采到"，不会想到去开一个任务。
             *     	（那两张 8 月 21 日到期的生产网关证书就是这么活到剩 28 小时的。）
             *
             *     	⚠️ 与 Freshness 的区别：Freshness 说"这批数据旧了"，
             *     	Caveat 说"这批数据里有一整类信息压根不存在，以及为什么"。
             *     	两者都不能靠前端猜 —— 前端只看得见空值，看不见空值的来历。
             */
            caveat?: components["schemas"]["httpx.Caveat"];
            /**
             * @description Facets 每个筛选维度下各取值的**全量**计数，如 {"status":{"running":347}}。
             *
             *     必须按全量统计，不能按当前页。筛选下拉要显示"运行中 347"，
             *     若只统计当前页，那个数字就是错的 —— 而它看起来完全正常，没人会怀疑。
             */
            facets?: {
                [key: string]: {
                    [key: string]: number;
                };
            };
            /**
             * @description Freshness 这批数据"多久没更新就算旧了"的判据。
             *
             *     	🔴 判据必须由**后端**给：它来自那个采集任务自己的 cron 周期。
             *     	前端写死一个"6 小时"，任务周期一改就分叉，而分叉的表现是
             *     	页面安静地不报警（OPSCMDB-031 P1-5：主机页写着「17 小时前」，
             *     	既没标红，又和数据源页的「从没同步过」互相矛盾，三处三个说法）。
             */
            freshness?: components["schemas"]["httpx.Freshness"];
            /**
             * @description ⚠️ 绝不能是 nil。nil 切片序列化成 JSON null，
             *     前端 data.items.length 直接抛异常 —— 而这只在"结果为空"时发生，
             *     有数据的时候一切正常，最容易漏测。NewList 保证了这一点。
             */
            items?: components["schemas"]["handlers.podOut"][];
            page?: number;
            size?: number;
            total?: number;
        };
        "httpx.ListResponse-handlers_pvcOut": {
            /**
             * @description Caveat 这批数据的**已知局限**：某一列为什么整片是空的。
             *
             *     	🔴 起因是一次生产 P0：证书列表里 890 张证书有 828 张到期日为空，
             *     	页面上没有任何地方说为什么 —— 而真相是"证书到期检测（443）"这个
             *     	定时任务被停用且从没跑过，也就是**证书临期提醒根本不工作**。
             *     	看的人只会以为"数据还没采到"，不会想到去开一个任务。
             *     	（那两张 8 月 21 日到期的生产网关证书就是这么活到剩 28 小时的。）
             *
             *     	⚠️ 与 Freshness 的区别：Freshness 说"这批数据旧了"，
             *     	Caveat 说"这批数据里有一整类信息压根不存在，以及为什么"。
             *     	两者都不能靠前端猜 —— 前端只看得见空值，看不见空值的来历。
             */
            caveat?: components["schemas"]["httpx.Caveat"];
            /**
             * @description Facets 每个筛选维度下各取值的**全量**计数，如 {"status":{"running":347}}。
             *
             *     必须按全量统计，不能按当前页。筛选下拉要显示"运行中 347"，
             *     若只统计当前页，那个数字就是错的 —— 而它看起来完全正常，没人会怀疑。
             */
            facets?: {
                [key: string]: {
                    [key: string]: number;
                };
            };
            /**
             * @description Freshness 这批数据"多久没更新就算旧了"的判据。
             *
             *     	🔴 判据必须由**后端**给：它来自那个采集任务自己的 cron 周期。
             *     	前端写死一个"6 小时"，任务周期一改就分叉，而分叉的表现是
             *     	页面安静地不报警（OPSCMDB-031 P1-5：主机页写着「17 小时前」，
             *     	既没标红，又和数据源页的「从没同步过」互相矛盾，三处三个说法）。
             */
            freshness?: components["schemas"]["httpx.Freshness"];
            /**
             * @description ⚠️ 绝不能是 nil。nil 切片序列化成 JSON null，
             *     前端 data.items.length 直接抛异常 —— 而这只在"结果为空"时发生，
             *     有数据的时候一切正常，最容易漏测。NewList 保证了这一点。
             */
            items?: components["schemas"]["handlers.pvcOut"][];
            page?: number;
            size?: number;
            total?: number;
        };
        "httpx.ListResponse-handlers_relationOut": {
            /**
             * @description Caveat 这批数据的**已知局限**：某一列为什么整片是空的。
             *
             *     	🔴 起因是一次生产 P0：证书列表里 890 张证书有 828 张到期日为空，
             *     	页面上没有任何地方说为什么 —— 而真相是"证书到期检测（443）"这个
             *     	定时任务被停用且从没跑过，也就是**证书临期提醒根本不工作**。
             *     	看的人只会以为"数据还没采到"，不会想到去开一个任务。
             *     	（那两张 8 月 21 日到期的生产网关证书就是这么活到剩 28 小时的。）
             *
             *     	⚠️ 与 Freshness 的区别：Freshness 说"这批数据旧了"，
             *     	Caveat 说"这批数据里有一整类信息压根不存在，以及为什么"。
             *     	两者都不能靠前端猜 —— 前端只看得见空值，看不见空值的来历。
             */
            caveat?: components["schemas"]["httpx.Caveat"];
            /**
             * @description Facets 每个筛选维度下各取值的**全量**计数，如 {"status":{"running":347}}。
             *
             *     必须按全量统计，不能按当前页。筛选下拉要显示"运行中 347"，
             *     若只统计当前页，那个数字就是错的 —— 而它看起来完全正常，没人会怀疑。
             */
            facets?: {
                [key: string]: {
                    [key: string]: number;
                };
            };
            /**
             * @description Freshness 这批数据"多久没更新就算旧了"的判据。
             *
             *     	🔴 判据必须由**后端**给：它来自那个采集任务自己的 cron 周期。
             *     	前端写死一个"6 小时"，任务周期一改就分叉，而分叉的表现是
             *     	页面安静地不报警（OPSCMDB-031 P1-5：主机页写着「17 小时前」，
             *     	既没标红，又和数据源页的「从没同步过」互相矛盾，三处三个说法）。
             */
            freshness?: components["schemas"]["httpx.Freshness"];
            /**
             * @description ⚠️ 绝不能是 nil。nil 切片序列化成 JSON null，
             *     前端 data.items.length 直接抛异常 —— 而这只在"结果为空"时发生，
             *     有数据的时候一切正常，最容易漏测。NewList 保证了这一点。
             */
            items?: components["schemas"]["handlers.relationOut"][];
            page?: number;
            size?: number;
            total?: number;
        };
        "httpx.ListResponse-handlers_subnetOut": {
            /**
             * @description Caveat 这批数据的**已知局限**：某一列为什么整片是空的。
             *
             *     	🔴 起因是一次生产 P0：证书列表里 890 张证书有 828 张到期日为空，
             *     	页面上没有任何地方说为什么 —— 而真相是"证书到期检测（443）"这个
             *     	定时任务被停用且从没跑过，也就是**证书临期提醒根本不工作**。
             *     	看的人只会以为"数据还没采到"，不会想到去开一个任务。
             *     	（那两张 8 月 21 日到期的生产网关证书就是这么活到剩 28 小时的。）
             *
             *     	⚠️ 与 Freshness 的区别：Freshness 说"这批数据旧了"，
             *     	Caveat 说"这批数据里有一整类信息压根不存在，以及为什么"。
             *     	两者都不能靠前端猜 —— 前端只看得见空值，看不见空值的来历。
             */
            caveat?: components["schemas"]["httpx.Caveat"];
            /**
             * @description Facets 每个筛选维度下各取值的**全量**计数，如 {"status":{"running":347}}。
             *
             *     必须按全量统计，不能按当前页。筛选下拉要显示"运行中 347"，
             *     若只统计当前页，那个数字就是错的 —— 而它看起来完全正常，没人会怀疑。
             */
            facets?: {
                [key: string]: {
                    [key: string]: number;
                };
            };
            /**
             * @description Freshness 这批数据"多久没更新就算旧了"的判据。
             *
             *     	🔴 判据必须由**后端**给：它来自那个采集任务自己的 cron 周期。
             *     	前端写死一个"6 小时"，任务周期一改就分叉，而分叉的表现是
             *     	页面安静地不报警（OPSCMDB-031 P1-5：主机页写着「17 小时前」，
             *     	既没标红，又和数据源页的「从没同步过」互相矛盾，三处三个说法）。
             */
            freshness?: components["schemas"]["httpx.Freshness"];
            /**
             * @description ⚠️ 绝不能是 nil。nil 切片序列化成 JSON null，
             *     前端 data.items.length 直接抛异常 —— 而这只在"结果为空"时发生，
             *     有数据的时候一切正常，最容易漏测。NewList 保证了这一点。
             */
            items?: components["schemas"]["handlers.subnetOut"][];
            page?: number;
            size?: number;
            total?: number;
        };
        "httpx.ListResponse-handlers_svcOut": {
            /**
             * @description Caveat 这批数据的**已知局限**：某一列为什么整片是空的。
             *
             *     	🔴 起因是一次生产 P0：证书列表里 890 张证书有 828 张到期日为空，
             *     	页面上没有任何地方说为什么 —— 而真相是"证书到期检测（443）"这个
             *     	定时任务被停用且从没跑过，也就是**证书临期提醒根本不工作**。
             *     	看的人只会以为"数据还没采到"，不会想到去开一个任务。
             *     	（那两张 8 月 21 日到期的生产网关证书就是这么活到剩 28 小时的。）
             *
             *     	⚠️ 与 Freshness 的区别：Freshness 说"这批数据旧了"，
             *     	Caveat 说"这批数据里有一整类信息压根不存在，以及为什么"。
             *     	两者都不能靠前端猜 —— 前端只看得见空值，看不见空值的来历。
             */
            caveat?: components["schemas"]["httpx.Caveat"];
            /**
             * @description Facets 每个筛选维度下各取值的**全量**计数，如 {"status":{"running":347}}。
             *
             *     必须按全量统计，不能按当前页。筛选下拉要显示"运行中 347"，
             *     若只统计当前页，那个数字就是错的 —— 而它看起来完全正常，没人会怀疑。
             */
            facets?: {
                [key: string]: {
                    [key: string]: number;
                };
            };
            /**
             * @description Freshness 这批数据"多久没更新就算旧了"的判据。
             *
             *     	🔴 判据必须由**后端**给：它来自那个采集任务自己的 cron 周期。
             *     	前端写死一个"6 小时"，任务周期一改就分叉，而分叉的表现是
             *     	页面安静地不报警（OPSCMDB-031 P1-5：主机页写着「17 小时前」，
             *     	既没标红，又和数据源页的「从没同步过」互相矛盾，三处三个说法）。
             */
            freshness?: components["schemas"]["httpx.Freshness"];
            /**
             * @description ⚠️ 绝不能是 nil。nil 切片序列化成 JSON null，
             *     前端 data.items.length 直接抛异常 —— 而这只在"结果为空"时发生，
             *     有数据的时候一切正常，最容易漏测。NewList 保证了这一点。
             */
            items?: components["schemas"]["handlers.svcOut"][];
            page?: number;
            size?: number;
            total?: number;
        };
        "httpx.ListResponse-handlers_taskOut": {
            /**
             * @description Caveat 这批数据的**已知局限**：某一列为什么整片是空的。
             *
             *     	🔴 起因是一次生产 P0：证书列表里 890 张证书有 828 张到期日为空，
             *     	页面上没有任何地方说为什么 —— 而真相是"证书到期检测（443）"这个
             *     	定时任务被停用且从没跑过，也就是**证书临期提醒根本不工作**。
             *     	看的人只会以为"数据还没采到"，不会想到去开一个任务。
             *     	（那两张 8 月 21 日到期的生产网关证书就是这么活到剩 28 小时的。）
             *
             *     	⚠️ 与 Freshness 的区别：Freshness 说"这批数据旧了"，
             *     	Caveat 说"这批数据里有一整类信息压根不存在，以及为什么"。
             *     	两者都不能靠前端猜 —— 前端只看得见空值，看不见空值的来历。
             */
            caveat?: components["schemas"]["httpx.Caveat"];
            /**
             * @description Facets 每个筛选维度下各取值的**全量**计数，如 {"status":{"running":347}}。
             *
             *     必须按全量统计，不能按当前页。筛选下拉要显示"运行中 347"，
             *     若只统计当前页，那个数字就是错的 —— 而它看起来完全正常，没人会怀疑。
             */
            facets?: {
                [key: string]: {
                    [key: string]: number;
                };
            };
            /**
             * @description Freshness 这批数据"多久没更新就算旧了"的判据。
             *
             *     	🔴 判据必须由**后端**给：它来自那个采集任务自己的 cron 周期。
             *     	前端写死一个"6 小时"，任务周期一改就分叉，而分叉的表现是
             *     	页面安静地不报警（OPSCMDB-031 P1-5：主机页写着「17 小时前」，
             *     	既没标红，又和数据源页的「从没同步过」互相矛盾，三处三个说法）。
             */
            freshness?: components["schemas"]["httpx.Freshness"];
            /**
             * @description ⚠️ 绝不能是 nil。nil 切片序列化成 JSON null，
             *     前端 data.items.length 直接抛异常 —— 而这只在"结果为空"时发生，
             *     有数据的时候一切正常，最容易漏测。NewList 保证了这一点。
             */
            items?: components["schemas"]["handlers.taskOut"][];
            page?: number;
            size?: number;
            total?: number;
        };
        "httpx.ListResponse-handlers_workloadOut": {
            /**
             * @description Caveat 这批数据的**已知局限**：某一列为什么整片是空的。
             *
             *     	🔴 起因是一次生产 P0：证书列表里 890 张证书有 828 张到期日为空，
             *     	页面上没有任何地方说为什么 —— 而真相是"证书到期检测（443）"这个
             *     	定时任务被停用且从没跑过，也就是**证书临期提醒根本不工作**。
             *     	看的人只会以为"数据还没采到"，不会想到去开一个任务。
             *     	（那两张 8 月 21 日到期的生产网关证书就是这么活到剩 28 小时的。）
             *
             *     	⚠️ 与 Freshness 的区别：Freshness 说"这批数据旧了"，
             *     	Caveat 说"这批数据里有一整类信息压根不存在，以及为什么"。
             *     	两者都不能靠前端猜 —— 前端只看得见空值，看不见空值的来历。
             */
            caveat?: components["schemas"]["httpx.Caveat"];
            /**
             * @description Facets 每个筛选维度下各取值的**全量**计数，如 {"status":{"running":347}}。
             *
             *     必须按全量统计，不能按当前页。筛选下拉要显示"运行中 347"，
             *     若只统计当前页，那个数字就是错的 —— 而它看起来完全正常，没人会怀疑。
             */
            facets?: {
                [key: string]: {
                    [key: string]: number;
                };
            };
            /**
             * @description Freshness 这批数据"多久没更新就算旧了"的判据。
             *
             *     	🔴 判据必须由**后端**给：它来自那个采集任务自己的 cron 周期。
             *     	前端写死一个"6 小时"，任务周期一改就分叉，而分叉的表现是
             *     	页面安静地不报警（OPSCMDB-031 P1-5：主机页写着「17 小时前」，
             *     	既没标红，又和数据源页的「从没同步过」互相矛盾，三处三个说法）。
             */
            freshness?: components["schemas"]["httpx.Freshness"];
            /**
             * @description ⚠️ 绝不能是 nil。nil 切片序列化成 JSON null，
             *     前端 data.items.length 直接抛异常 —— 而这只在"结果为空"时发生，
             *     有数据的时候一切正常，最容易漏测。NewList 保证了这一点。
             */
            items?: components["schemas"]["handlers.workloadOut"][];
            page?: number;
            size?: number;
            total?: number;
        };
        "license.Capacity": {
            cloud_accounts?: number;
            clusters?: number;
            nodes?: number;
            retention_days?: number;
            seats?: number;
            tenants?: number;
        };
    };
    responses: never;
    parameters: never;
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export type operations = Record<string, never>;
