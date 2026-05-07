export default {
  // 通用
  common: {
    all: '全部',
    search: '搜索',
    confirm: '确定',
    cancel: '取消',
    save: '保存',
    delete: '删除',
    edit: '编辑',
    add: '添加',
    export: '导出',
    import: '导入',
    upload: '上传',
    download: '下载',
    close: '关闭',
    yes: '是',
    no: '否',
    none: '无',
    loading: '加载中...',
    success: '成功',
    failed: '失败',
    error: '错误',
    warning: '警告',
    info: '信息',
    total: '总计',
    records: '条记录',
    operation: '操作',
    actions: '操作',
    view: '查看',
    details: '详情',
    back: '返回',
    reset: '重置',
    apply: '应用',
    selectAll: '全选',
    deselectAll: '取消全选',
    batchOperation: '批量操作',
    batchModifyStatus: '批量修改状态',
    selected: '已选择',
    items: '项'
  },

  // 值班记录页面
  dutyRecords: {
    pageTitle: '值班记录',
    recordCount: '共 {count} 条记录',
    projectConfig: '项目配置',
    hideStats: '隐藏统计',
    showStats: '统计面板',
    addRecord: '添加记录',
    
    // Tab
    tabs: {
      list: '记录列表',
      stats: '统计分析'
    },
    
    // 统计面板
    stats: {
      total: '总记录',
      normal: '检测正常',
      resolved: '已解决',
      pending: '待解决',
      inProgress: '正在解决',
      temporary: '临时解决',
      overdue: '逾期',
      thisMonth: '本月记录',
      pendingHandover: '待处理记录',
      pendingCount: '共 {count} 条待处理',
      noHandover: '暂无待处理记录'
    },
    
    // 交接记录
    handover: {
      project: '项目',
      feedbackType: '反馈类型',
      eventType: '事件类型',
      handoverPerson: '交接人',
      status: '状态',
      dutyPerson: '值班人',
      handler: '处理人',
      handoverTime: '交接时间',
      handoverContent: '交接内容',
      noContent: '无交接内容',
      unknownProject: '未知项目'
    },
    
    // 筛选条件
    filters: {
      status: '处理结果',
      eventType: '事件类型',
      project: '项目',
      handler: '处理人',
      dutyPerson: '值班人',
      responseTime: '响应时长',
      startDate: '开始日期',
      endDate: '结束日期',
      overdueOnly: '仅逾期',
      
      // 响应时长选项
      responseTimeOptions: {
        within2min: '2分钟内',
        within5min: '5分钟内',
        within10min: '10分钟内',
        over10min: '10分钟以上'
      }
    },
    
    // 表格列
    columns: {
      select: '选择',
      date: '日期',
      dutyPerson: '值班人',
      project: '项目',
      taskDesc: '任务描述',
      feedbackType: '反馈类型',
      eventType: '事件类型',
      problemDesc: '问题描述',
      handler: '处理人',
      status: '处理结果',
      solution: '解决方案',
      plannedFixTime: '计划修复时间',
      overdue: '逾期',
      firstCallTime: '首次拨打',
      answerTime: '接听时间',
      callCount: '拨打次数',
      answered: '是否接听',
      responseTime: '响应(分)',
      handoverPerson: '交接人',
      handoverContent: '交接内容',
      attachments: '附件',
      escalate: '上报问题',
      operator: '操作人',
      createdAt: '创建时间',
      updatedAt: '更新时间',
      actions: '操作'
    },
    
    // 状态选项
    statusOptions: {
      normal: '检测正常',
      pending: '待解决',
      inProgress: '正在解决',
      resolved: '已解决',
      temporary: '临时解决'
    },
    
    // 反馈类型选项
    feedbackTypeOptions: {
      proactive: '主动发现',
      customer: '客户反馈'
    },
    
    // 事件类型选项
    eventTypeOptions: {
      inspection: '巡检发现',
      alert: '监控告警',
      customerFeedback: '客户反馈',
      proactiveCheck: '值班人员主动排查'
    },
    
    // 上报选项
    escalateOptions: {
      leader: '组长',
      hod: 'HOD',
      backend: '后端',
      frontend: '前端',
      dba: 'DBA',
      network: '网络',
      security: '安全',
      devops: '运维'
    },
    
    // 表单
    form: {
      addTitle: '添加值班记录',
      editTitle: '编辑值班记录',
      viewTitle: '值班记录详情',
      
      // 基本信息
      basicInfo: '基本信息',
      dutyTime: '值班时间',
      dutyPerson: '值班人',
      dutyPersonPlaceholder: '输入值班人姓名',
      project: '项目',
      selectProject: '请选择项目',
      feedbackType: '反馈类型',
      eventType: '事件类型',
      taskDesc: '任务描述',
      taskDescPlaceholder: '描述值班任务内容...',
      problemDesc: '问题描述',
      problemDescPlaceholder: '详细描述遇到的问题...',
      
      // 处理信息
      handleInfo: '处理信息',
      handler: '处理人',
      handlerPlaceholder: '处理该问题的人',
      status: '处理结果',
      selectStatus: '请选择处理结果',
      plannedFixTime: '计划修复时间',
      plannedFixTimeEdited: '此字段已被编辑过，需要"编辑计划修复时间"权限才能修改',
      plannedFixTimeFirstEdit: '首次编辑，可修改一次',
      solution: '解决方案',
      solutionPlaceholder: '描述解决方案或处理措施',
      markOverdue: '标记为逾期',
      overdueReason: '逾期原因',
      overdueReasonPlaceholder: '说明逾期原因',

      // 通话信息
      callInfo: '通话信息',
      firstCallTime: '首次拨打时间',
      answerTime: '接听时间',
      callCount: '拨打次数',
      answered: '已接听',
      responseTime: '响应时间(分钟)',
      minutes: '分钟',
      autoCalculated: '自动计算：接听时间 - 首次拨打时间',
      
      // 升级与交接
      escalateAndHandover: '升级与交接',
      escalate: '升级问题',
      escalateTo: '升级给',
      hasHandover: '工作交接',
      handoverPerson: '交接人',
      handoverPersonPlaceholder: '交接给谁',
      handoverContent: '交接内容',
      handoverContentPlaceholder: '交接的具体内容...',
      
      // 附件
      attachments: '附件',
      dropToUpload: '拖拽文件到此处上传',
      orClickToSelect: '或点击选择',
      clickToSelect: '点击选择',
      dragOrPaste: '、拖拽或 Ctrl+V 粘贴图片',
      multipleImages: '支持多张图片同时上传',
      uploadingFile: '上传中...',
      fileCount: '{count} 个文件'
    },
    
    // 操作
    actions: {
      confirmDelete: '确认删除',
      confirmDeleteMsg: '确定要删除这条值班记录吗？此操作不可恢复。',
      confirmDeleteBtn: '确定删除',
      deleteSuccess: '删除成功',
      deleteFailed: '删除失败',
      saveSuccess: '保存成功',
      saveFailed: '保存失败',
      exportSuccess: '导出成功',
      exportFailed: '导出失败',
      editPlannedFixTime: '修改计划修复时间',
      viewDetails: '查看详情',
      createSuccess: '创建成功',
      updateSuccess: '更新成功',
      loadFailed: '加载失败',
      shareSuccess: '分享链接创建成功',
      shareFailed: '创建分享失败',
      linkCopied: '链接已复制',
      loadStatsFailed: '加载统计失败',
      exportDeveloping: '导出功能开发中',
      plannedFixTimeUpdated: '计划修复时间更新成功',
      updateFailed: '更新失败',
      fillRequired: '请填写必填项（值班时间、值班人、项目）',
      selectStatus: '请选择处理结果',
      selectRecordsFirst: '请先选择要修改的记录',
      batchModifyFailed: '批量修改失败',
      selectDeleteFirst: '请先选择要删除的记录',
      batchDeleteFailed: '批量删除失败',
      selectImageFile: '请选择图片文件',
      uploadFailed: '上传失败',
      noPermissionAdd: '无权限：不可添加项目',
      noPermissionEdit: '无权限：不可编辑项目',
      noPermissionDelete: '无权限：不可删除项目',
      fillProjectNameCode: '请填写项目名称和代码'
    },
    
    // 详情弹窗
    detail: {
      title: '值班记录详情',
      basicInfo: '基本信息',
      handleInfo: '处理信息',
      callInfo: '通话信息',
      handoverInfo: '交接信息',
      attachments: '附件'
    },
    
    // 项目管理弹窗
    projectModal: {
      title: '项目配置',
      addProject: '添加项目',
      editProject: '编辑项目',
      name: '项目名称',
      code: '项目代码',
      description: '描述',
      status: '状态',
      sortOrder: '排序',
      active: '启用',
      inactive: '禁用',
      confirmDelete: '确认删除',
      confirmDeleteMsg: '确定要删除项目「{name}」吗？',
      confirmDeleteBtn: '确定删除',
      deleteSuccess: '删除成功',
      deleteFailed: '删除失败',
      saveSuccess: '保存成功',
      saveFailed: '保存失败'
    },
    
    // 修改计划修复时间弹窗
    plannedFixModal: {
      title: '修改计划修复时间',
      currentTime: '当前计划修复时间',
      newTime: '新计划修复时间',
      notSet: '未设置'
    },
    
    // 批量操作
    batch: {
      modifyStatus: '批量修改状态',
      selectStatus: '选择新状态',
      confirmModify: '确认修改',
      modifySuccess: '批量修改成功',
      modifyFailed: '批量修改失败',
      batchDelete: '批量删除',
      cancelSelect: '取消选择'
    },

    // 统计分析页
    statsPage: {
      title: '值班数据统计分析',
      exportReport: '导出报表',
      filters: {
        project: '项目',
        handler: '处理人',
        dutyPerson: '值班人',
        eventType: '事件类型',
        startDate: '开始日期',
        endDate: '结束日期',
        today: '今日',
        thisWeek: '本周',
        thisMonth: '本月',
        thisQuarter: '本季度',
        all: '全部',
        allProjects: '全部项目',
        allHandlers: '全部处理人',
        allDutyPersons: '全部值班人',
        allTypes: '全部类型'
      },
      overview: {
        title: '数据总览',
        totalRecords: '总记录数',
        avgResponseTime: '平均响应时长',
        minutes: '分钟',
        resolvedRate: '解决率',
        overdueRate: '逾期率'
      },
      charts: {
        byStatus: '按处理结果统计',
        byProject: '按项目统计',
        byHandler: '按处理人统计',
        byEventType: '按事件类型统计',
        byDutyPerson: '按值班人统计',
        byFeedback: '按反馈类型统计',
        responseTime: '响应时长分布',
        trend: '趋势分析',
        callDistribution: '拨打次数分布',
        byEscalate: '按上报类型统计',
        showPercent: '显示百分比',
        showCount: '显示数量'
      }
    },

    // 表格单元格
    table: {
      answered: '已接听',
      notAnswered: '未接听'
    },

    // 空状态
    empty: {
      noRecords: '暂无值班记录',
      noData: '暂无数据'
    },

    // 分页
    pagination: {
      page: '第',
      of: '/',
      total: '共',
      perPage: '条/页',
      items: '条',
      first: '首页',
      prev: '上一页',
      next: '下一页',
      last: '末页'
    },

    // 时间相关
    time: {
      justNow: '刚刚',
      minutesAgo: '{n}分钟前',
      hoursAgo: '{n}小时前',
      daysAgo: '{n}天前'
    }
  },

  // 侧边栏菜单
  menu: {
    groups: {
      system: '系统管理',
      resource: '资源管理',
      monitor: '监控告警',
      k8s: 'K8S管理',
      ticket: '工单系统',
      automation: '自动化运维',
      aiops: 'AIOps',
      release: '发布管理',
      logs: '日志管理',
      security: '安全管理',
      settings: '系统设置'
    },
    items: {
      welcome: '欢迎页',
      users: '用户管理',
      roles: '角色管理',
      permissions: '权限配置',
      audit: '审计日志',
      api: '接口管理',
      schedule: '排班管理',
      taskpool: '任务池',
      incidents: '失误记录',
      duty: '值班记录',
      tableMaintenance: '桌台维护记录',
      tableHierarchyConfig: '桌台配置',
      apiKeys: 'API Key 管理',
      assets: '资产管理',
      domains: '域名管理',
      merchants: '商户管理',
      network: '网络管理',
      serviceconfig: '服务配置',
      topology: '网络拓扑',
      metrics: '指标监控',
      alerts: '告警管理',
      alertRules: '告警规则',
      alertNotify: '告警通知',
      dashboardScreen: '监控大屏',
      clusters: '集群管理',
      workloads: '工作负载',
      configmaps: '配置管理',
      storage: '存储管理',
      terminal: 'Web终端',
      tickets: '工单列表',
      workflow: '流程配置',
      sla: 'SLA配置',
      ticketTemplate: '工单模板',
      jobs: '作业管理',
      crontab: '定时任务',
      inspection: '巡检任务',
      selfHealing: '自愈策略',
      anomaly: '异常检测',
      rootCause: '根因分析',
      predict: '容量预测',
      smartAlert: '智能告警',
      capacity: '容量规划',
      deploy: '应用部署',
      change: '变更管理',
      rollback: '回滚记录',
      logQuery: '日志查询',
      logAnalysis: '日志分析',
      logAlert: '日志告警',
      vault: '密钥管理',
      secrets: '凭据管理',
      certificates: '证书管理',
      datasources: '数据源',
      sysParams: '系统参数',
      settings: '个人设置',
      ssoConfig: 'SSO配置',
      appsManage: '应用管理'
    }
  },

  // 欢迎页
  welcome: {
    greeting: '欢迎回来，{name}',
    today: '今天是 {date}',
    ssoNotice: '您已通过统一身份认证 (SSO) 登录',
    quickEntry: '快捷入口',
    quickLinks: {
      domains: '域名管理',
      domainsDesc: '管理域名和证书',
      merchants: '商户管理',
      merchantsDesc: '管理商户信息',
      network: '网络管理',
      networkDesc: '管理网络节点',
      users: '用户管理',
      usersDesc: '管理系统用户',
      audit: '审计日志',
      auditDesc: '查看操作记录',
      duty: '值班记录',
      dutyDesc: '管理值班记录'
    },
    stats: {
      domains: '域名总数',
      expiring: '即将到期',
      merchants: '商户总数',
      active: '运营中',
      users: '用户总数',
      activeUsers: '活跃用户'
    }
  },

  // 桌台维护记录页面
  tableMaintenance: {
    pageTitle: '桌台维护记录',
    recordCount: '共 {count} 条记录',
    hierarchyConfig: '层级配置',
    addRecord: '添加记录',
    pasteRecord: '粘贴记录',
    deleteSelected: '删除选中',
    exportExcel: '导出Excel',

    tabs: {
      list: '记录列表',
      stats: '统计分析'
    },

    stats: {
      total: '总记录',
      today: '今日记录',
      week: '本周记录',
      month: '本月记录'
    },

    filters: {
      searchPlaceholder: '搜索桌号/描述/处理人...',
      searchAllPlaceholder: '搜索桌号/局号/操作人/原因...',
      filterConditions: '筛选条件',
      qcStatusLabel: '质检状态',
      projectLabel: '项目',
      durationLabel: '维护时长',
      operatorLabel: '操作人',
      operationLabel: '操作类型',
      dateRangeLabel: '日期范围',
      dateTo: '至',
      allOperations: '全部操作',
      tableStatusLabel: '桌台状态',
      allTableStatus: '全部状态',
      allQcStatus: '全部质检状态',
      allProjects: '全部项目',
      allDurations: '全部时长',
      allOperators: '全部操作人',
      roundIdPlaceholder: '搜索局号...',
      normal: '正常',
      abnormal: '异常',
      notInspected: '未质检',
      search: '搜索',
      reset: '重置'
    },

    columns: {
      date: '日期',
      startTime: '开始时间',
      notifyStartScreenshot: '通知开始截图',
      startDuration: '开始维护时长',
      endTime: '结束时间',
      notifyEndScreenshot: '结束截图',
      closeDuration: '关闭维护时长',
      affectedProjects: '受影响项目',
      site: '现场',
      table: '桌台',
      tableStatus: '桌台状态',
      gameType: '游戏类型',
      maintenanceType: '维护类型',
      reason: '原因',
      affectSettlement: '影响结算',
      affectedRoundIds: '影响局号',
      operation: '操作',
      operator: '实际操作人',
      inspector: '质检人',
      qcStatus: '质检状态',
      remark: '备注',
      createdBy: '创建人',
      createdAt: '创建时间',
      updatedAt: '更新时间',
      actions: '操作'
    },

    options: {
      duration: {
        twoMin: '两分钟内',
        fiveMin: '五分钟内',
        tenMin: '十分钟内',
        overTenMin: '十分钟以上'
      },
      maintenanceType: {
        none: '无',
        emergency: '紧急维护',
        temporary: '临时维护',
        routine: '例行维护'
      },
      operation: {
        maintenance: '维护',
        cancel: '取消',
        recalculate: '重算',
        repayout: '重派彩',
        vipTable: '包桌T人',
        missed: '漏操作',
        missedScreenshot: '漏截图'
      },
      yesNo: {
        yes: '是',
        no: '否'
      },
      qcStatus: {
        normal: '正常',
        abnormal: '异常'
      }
    },

    placeholders: {
      select: '请选择{field}',
      remarkRequired: '质检异常，请填写异常原因（必填）'
    },

    actions: {
      view: '查看',
      copy: '复制',
      edit: '编辑',
      delete: '删除',
      loadFailed: '加载记录失败',
      saveFailed: '保存失败',
      saveSuccess: '保存成功',
      deleteFailed: '删除失败',
      deleteSuccess: '删除成功',
      confirmDelete: '确定要删除这条记录吗？',
      confirmBatchDelete: '确定要删除选中的 {count} 条记录吗？',
      exportSuccess: '导出成功',
      exportFailed: '导出失败',
      copySuccess: '复制成功，点击"粘贴记录"按钮来批量添加',
      pasteSuccess: '粘贴成功，已添加 {count} 条记录',
      batchModifyInspector: '批量修改质检人',
      batchModifyInspectorTitle: '批量修改质检人',
      batchModifyInspectorHint: '将为选中的 {count} 条记录更新质检人',
      inspectorPlaceholder: '请输入或选择质检人'
    },

    form: {
      addTitle: '添加维护记录',
      editTitle: '编辑维护记录',
      pasteTitle: '粘贴记录',
      pasteCount: '粘贴数量',
      pasteHint: '输入要粘贴的记录数量（1-100）',
      sectionOperation: '操作信息',
      sectionTimeScreenshot: '时间与截图',
      auto: '自动',
      maintTypeRequired: '操作为维护时，维护类型为必填项'
    },

    hints: {
      noGameTypesForTables: '所选桌台暂无游戏类型',
      selectTableFirst: '请先选择桌台',
      noSitesConfigured: '暂无配置的现场',
      goConfigure: '去配置'
    },

    detail: {
      title: '记录详情',
      basicInfo: '基本信息',
      attachments: '附件'
    },

    statsPanel: {
      title: '统计分析',
      dateRange: '日期范围',
      searchBtn: '搜索',
      export: '导出统计',
      overview: {
        title: '总览',
        totalRecords: '总记录数',
        maintenanceCount: '维护次数',
        cancelCount: '取消次数',
        recalculateCount: '重算次数',
        repayoutCount: '重派彩次数'
      },
      byOperation: '按操作类型',
      byMaintenanceType: '按维护类型',
      byStartDuration: '维护-开始时长',
      byCloseDuration: '维护-关闭时长',
      byTotalDuration: '维护总时长',
      byOperator: '操作人统计',
      byInspector: '质检人统计',
      byQcStatus: '质检状态',
      byGameType: '游戏类型',
      bySite: '现场统计',
      byTableSummary: '桌台汇总',
      byTable: '桌台明细（按项目）',
      filterProject: '项目',
      filterDuration: '维护时长',
      filterOperator: '操作人',
      filterRoundId: '局号',
      excludeFilterLabel: '忽略',
      excludeCreators: '忽略创建人',
      excludeApiKey: '排除所有 API Key 创建（apikey:*）',
      excludeNoCreators: '暂无创建人',
      excludeClear: '清空',
      excludeApplyHint: '点查询生效',
      filterTableStatus: '桌台状态',
      allTableStatus: '全部状态',
      pendingMaintCard: '未接入维护',
      cancelDetail: '取消操作明细',
      recalcDetail: '重算操作明细',
      repayoutDetail: '重派彩操作明细',
      vipTableDetail: '包桌T人明细',
      missedDetail: '漏操作明细',
      missedScreenshotDetail: '漏截图明细',
      byProjectOperation: '按项目 × 操作类型',
      projectTotal: '项目合计',
      detailDate: '日期',
      detailTable: '桌号',
      detailProject: '项目',
      detailRoundId: '局号',
      detailDuration: '时长',
      detailOperator: '操作人',
      tableColumns: {
        tableNo: '桌号',
        gameType: '游戏类型',
        site: '现场',
        projects: '涉及项目',
        maintCount: '维护次数',
        status: '状态'
      },
      byAffectedProject: '受影响项目',
      notFilled: '未填写',
      unknown: '未知',
      qcPending: '未质检',
      maintDurationDetail: '维护时长明细',
      detailMaintType: '维护类型',
      detailStartDuration: '开始时长',
      detailCloseDuration: '关闭时长',
      detailTotalDuration: '总时长',
      tableTimes: '次',
      involves: '，涉及',
      csvDateRange: '统计时间范围',
      csvTotalRecords: '总记录数',
      csvTotalTables: '总台次',
      csvNoLimit: '不限',
      csvAllTime: '全部时间',
      csvByMaintType: '按维护类型统计（含时长分布）',
      csvMaintType: '维护类型',
      csvTotal: '总次数',
      csvByOpType: '按操作类型统计（含开始时长分布）',
      csvOpType: '操作类型',
      csvMaintStartDuration: '维护 - 开始时长',
      csvDuration: '时长',
      csvCount: '次数',
      csvMaintCloseDuration: '维护 - 关闭时长',
      csvMaintTotalDuration: '维护 - 总时长（取开始和关闭时长中较大值）',
      csvByOperator: '操作人统计',
      csvOperator: '操作人',
      csvByInspector: '质检人统计',
      csvInspector: '质检人',
      csvQcStatus: '质检状态统计',
      csvStatus: '状态',
      csvQcNormal: '正常',
      csvQcAbnormal: '异常',
      csvQcPending: '未质检',
      csvByAffectedProject: '影响项目统计',
      csvProject: '项目',
      csvNoAffect: '无影响',
      csvByGameType: '游戏类型统计',
      csvGameType: '游戏类型',
      csvBySite: '现场统计',
      csvSite: '现场',
      csvTableSummary: '桌台汇总',
      csvTableNo: '桌号',
      csvMaintCount: '维护次数',
      csvProjects: '涉及项目',
      csvSites: '涉及现场',
      csvGameTypes: '游戏类型',
      csvStatus: '状态',
      csvTableDetail: '桌台明细（按项目）',
      csvByProjectOperation: '按项目 × 操作类型统计',
      csvFileName: '桌台维护统计'
    },

    table: {
      loading: '加载中...',
      empty: '暂无数据',
      pagination: {
        total: '共',
        records: '条记录',
        perPage: '每页',
        items: '条',
        page: '页',
        first: '首页',
        prev: '上一页',
        next: '下一页',
        last: '末页'
      }
    },

    upload: {
      click: '点击或拖拽上传',
      hint: '支持 JPG、PNG、GIF 格式，最大 5MB',
      empty: '{name} 为空文件，无法上传'
    },

    selectAll: '全选/取消全选'
  },

  // 桌台配置页面
  tableHierarchy: {
    pageTitle: '桌台配置',
    subtitle: '全局配置项目、现场、游戏类型和桌台的对应关系',
    recordCount: '共 {count} 个桌台',

    stats: {
      projects: '项目',
      sites: '现场',
      tables: '桌台'
    },

    search: {
      placeholder: '搜索项目、现场、桌台...',
      searchBtn: '搜索',
      reset: '重置'
    },

    filters: {
      title: '筛选条件',
      project: '项目',
      site: '现场',
      gameType: '游戏类型',
      tableNo: '桌台号',
      status: '状态',
      allProjects: '全部项目',
      allSites: '全部现场',
      allGameTypes: '全部游戏类型',
      allStatus: '全部状态',
      tableNoPlaceholder: '输入桌台号...'
    },

    add: {
      button: '添加',
      single: '单个添加',
      batch: '批量添加',
      addProject: '添加项目',
      addSite: '添加现场',
      addTable: '添加桌台',
      addGameType: '添加游戏类型',
      batchProject: '批量添加项目',
      batchSite: '批量添加现场',
      batchGameType: '批量添加游戏类型',
      batchTable: '批量添加桌台'
    },

    columns: {
      expand: '',
      project: '项目',
      site: '现场',
      table: '桌台',
      gameType: '游戏类型',
      status: '状态',
      siteCount: '现场数',
      tableCount: '桌台数',
      actions: '操作'
    },

    status: {
      enabled: '启用',
      disabled: '关闭',
      pending: '未接入',
      unconfigured: '未配置'
    },

    types: {
      project: '项目',
      site: '现场',
      table: '桌台'
    },

    form: {
      addProject: '添加项目',
      editProject: '编辑项目',
      addSite: '添加现场',
      editSite: '编辑现场',
      addTable: '添加桌台',
      addGameType: '添加游戏类型',
      editGameType: '编辑游戏类型',
      gameTypeNameZh: '中文名称',
      gameTypeNameEn: '英文名称',
      gameTypeNameZhPlaceholder: '如：百家乐',
      gameTypeNameEnPlaceholder: '如：Baccarat',
      gameTypeNameZhRequired: '中文名称为必填项',
      editTable: '编辑桌台',
      batchAddProject: '批量添加项目',
      batchAddSite: '批量添加现场',
      batchAddGameType: '批量添加游戏类型',
      batchAddTable: '批量添加桌台',
      gameTypeName: '游戏类型名称',
      belongProject: '所属项目',
      belongSite: '所属现场',
      projectName: '项目名称',
      projectNameZh: '中文名称',
      projectNameEn: '英文名称',
      projectNameZhPlaceholder: '如：项目A',
      projectNameEnPlaceholder: '如：Project A',
      projectNameZhRequired: '项目中文名称为必填项',
      siteName: '现场名称',
      siteNameZh: '中文名称',
      siteNameEn: '英文名称',
      siteNameZhPlaceholder: '如：1号现场',
      siteNameEnPlaceholder: '如：Site 1',
      siteNameZhRequired: '中文名称为必填项',
      optional: '（选填）',
      tableNumber: '桌台编号',
      gameType: '游戏类型',
      gameTypeHint: '（可多选）',
      batchGameTypeHint: '（每行一个，格式：中文名称|英文名称）',
      selectProject: '请选择项目',
      selectSite: '请选择现场',
      projectPlaceholder: '如：项目A',
      sitePlaceholder: '如：1号现场',
      tablePlaceholder: '如：B01',
      tableStatus: '桌台状态',
      statusPendingHint: '未接入桌台不属于任何项目',
      siteNone: '(无)',
      siteNotInProjectWarn: '当前现场"{site}"未接入项目"{project}"，请先到现场管理把该现场添加到此项目',
      siteNotInProjectToast: '当前现场未接入所选项目，请先到现场管理添加',
      batchHint: '（每行一个）',
      batchGameTypeHintBatch: '（每行一个，格式：中文名称|英文名称）',
      batchProjectPlaceholder: '项目A|Project A\n项目B|Project B\n项目C|Project C',
      batchSitePlaceholder: '1号现场|Site 1\n2号现场|Site 2\nVIP现场|VIP Site',
      batchGameTypePlaceholder: '百家乐|Baccarat\n龙虎|Dragon Tiger\n轮盘|Roulette',
      batchTablePlaceholder: 'A01\nA02\nA03'
    },

    actions: {
      addSite: '添加现场',
      addTable: '添加桌台',
      edit: '编辑',
      delete: '删除',
      save: '保存',
      saving: '保存中...',
      cancel: '取消'
    },

    messages: {
      loadFailed: '加载配置失败',
      saveSuccess: '配置保存成功',
      saveFailed: '保存失败',
      noPermission: '无权限保存桌台配置',
      deleteSuccess: '删除成功',
      confirmDeleteProject: '确定要删除项目 "{name}" 吗？',
      confirmDeleteSite: '确定要删除现场 "{name}" 吗？',
      confirmDeleteTable: '确定要删除桌台 "{name}" 吗？',
      confirmDeleteGameType: '确定要删除游戏类型 "{name}" 吗？',
      cascadeWarningProject: '该项目下的所有现场和桌台也将被删除',
      cascadeWarningSite: '该现场下的所有桌台也将被删除',
      hasTablesCannotDelete: '有桌台关联，无法删除'
    },

    configBtn: {
      project: '项目配置',
      site: '现场配置',
      gameType: '游戏类型',
      table: '桌台配置'
    },

    gameTypeSection: {
      title: '游戏类型',
      badge: '游戏',
      empty: '暂无游戏类型，请点击上方"添加"按钮创建'
    },

    projectSection: {
      title: '项目管理',
      empty: '暂无项目，请点击上方"添加"按钮创建'
    },

    siteSection: {
      title: '现场管理',
      empty: '暂无现场，请点击上方"添加"按钮创建'
    },

    pagination: {
      total: '共',
      records: '个桌台',
      perPage: '每页',
      items: '条'
    },

    empty: {
      noData: '暂无配置数据',
      noMatch: '没有找到匹配的数据',
      noDataHint: '点击上方"添加"按钮创建',
      noMatchHint: '请尝试其他搜索关键词'
    },

    loading: '加载中...',

    gameTypes: {
      baccarat: '百家乐',
      dragonTiger: '龙虎',
      roulette: '轮盘',
      sicBo: '骰宝',
      other: '其他'
    }
  }
}
