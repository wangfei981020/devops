export default {
  // Common
  common: {
    all: 'All',
    search: 'Search',
    confirm: 'Confirm',
    cancel: 'Cancel',
    save: 'Save',
    delete: 'Delete',
    edit: 'Edit',
    add: 'Add',
    export: 'Export',
    import: 'Import',
    upload: 'Upload',
    download: 'Download',
    close: 'Close',
    yes: 'Yes',
    no: 'No',
    none: 'None',
    loading: 'Loading...',
    success: 'Success',
    failed: 'Failed',
    error: 'Error',
    warning: 'Warning',
    info: 'Info',
    total: 'Total',
    records: 'records',
    operation: 'Operation',
    actions: 'Actions',
    view: 'View',
    details: 'Details',
    back: 'Back',
    reset: 'Reset',
    apply: 'Apply',
    selectAll: 'Select All',
    deselectAll: 'Deselect All',
    batchOperation: 'Batch Operation',
    batchModifyStatus: 'Batch Modify Status',
    selected: 'Selected',
    items: 'items'
  },

  // Duty Records Page
  dutyRecords: {
    pageTitle: 'Duty Records',
    recordCount: '{count} records in total',
    projectConfig: 'Project Config',
    hideStats: 'Hide Stats',
    showStats: 'Stats Panel',
    addRecord: 'Add Record',
    
    // Tabs
    tabs: {
      list: 'Record List',
      stats: 'Statistics'
    },
    
    // Stats Panel
    stats: {
      total: 'Total',
      normal: 'Normal',
      resolved: 'Resolved',
      pending: 'Pending',
      inProgress: 'In Progress',
      temporary: 'Temporary',
      overdue: 'Overdue',
      thisMonth: 'This Month',
      pendingHandover: 'Pending Records',
      pendingCount: '{count} pending',
      noHandover: 'No pending records'
    },
    
    // Handover Records
    handover: {
      project: 'Project',
      feedbackType: 'Feedback Type',
      eventType: 'Event Type',
      handoverPerson: 'Handover By',
      status: 'Status',
      dutyPerson: 'Duty Person',
      handler: 'Handler',
      handoverTime: 'Handover Time',
      handoverContent: 'Handover Content',
      noContent: 'No handover content',
      unknownProject: 'Unknown Project'
    },
    
    // Filters
    filters: {
      status: 'Status',
      eventType: 'Event Type',
      project: 'Project',
      handler: 'Handler',
      dutyPerson: 'Duty Person',
      responseTime: 'Response Time',
      startDate: 'Start Date',
      endDate: 'End Date',
      overdueOnly: 'Overdue Only',
      
      // Response Time Options
      responseTimeOptions: {
        within2min: 'Within 2 min',
        within5min: 'Within 5 min',
        within10min: 'Within 10 min',
        over10min: 'Over 10 min'
      }
    },
    
    // Table Columns
    columns: {
      select: 'Select',
      date: 'Date',
      dutyPerson: 'Duty Person',
      project: 'Project',
      taskDesc: 'Task Description',
      feedbackType: 'Feedback',
      eventType: 'Event Type',
      problemDesc: 'Problem Description',
      handler: 'Handler',
      status: 'Status',
      solution: 'Solution',
      plannedFixTime: 'Planned Fix',
      overdue: 'Overdue',
      firstCallTime: 'First Call',
      answerTime: 'Answer Time',
      callCount: 'Call Count',
      answered: 'Answered',
      responseTime: 'Response(min)',
      handoverPerson: 'Handover By',
      handoverContent: 'Handover Content',
      attachments: 'Attachments',
      escalate: 'Escalate',
      operator: 'Operator',
      createdAt: 'Created At',
      updatedAt: 'Updated At',
      actions: 'Actions'
    },
    
    // Status Options
    statusOptions: {
      normal: 'Normal',
      pending: 'Pending',
      inProgress: 'In Progress',
      resolved: 'Resolved',
      temporary: 'Temporary'
    },
    
    // Feedback Type Options
    feedbackTypeOptions: {
      proactive: 'Proactive',
      customer: 'Customer Feedback'
    },
    
    // Event Type Options
    eventTypeOptions: {
      inspection: 'Inspection',
      alert: 'Alert',
      customerFeedback: 'Customer Feedback',
      proactiveCheck: 'Proactive Check'
    },
    
    // Escalate Options
    escalateOptions: {
      leader: 'Team Lead',
      hod: 'HOD',
      backend: 'Backend',
      frontend: 'Frontend',
      dba: 'DBA',
      network: 'Network',
      security: 'Security',
      devops: 'DevOps'
    },
    
    // Form
    form: {
      addTitle: 'Add Duty Record',
      editTitle: 'Edit Duty Record',
      viewTitle: 'Duty Record Details',
      
      // Basic Info
      basicInfo: 'Basic Information',
      dutyTime: 'Duty Time',
      dutyPerson: 'Duty Person',
      dutyPersonPlaceholder: 'Enter duty person name',
      project: 'Project',
      selectProject: 'Select a project',
      feedbackType: 'Feedback Type',
      eventType: 'Event Type',
      taskDesc: 'Task Description',
      taskDescPlaceholder: 'Describe the duty task...',
      problemDesc: 'Problem Description',
      problemDescPlaceholder: 'Describe the problem in detail...',
      
      // Handle Info
      handleInfo: 'Handle Information',
      handler: 'Handler',
      handlerPlaceholder: 'Person who handles this',
      status: 'Status',
      selectStatus: 'Select status',
      plannedFixTime: 'Planned Fix Time',
      plannedFixTimeEdited: 'This field has been edited. "Edit Planned Fix Time" permission is required.',
      plannedFixTimeFirstEdit: 'First edit, can be modified once',
      solution: 'Solution',
      solutionPlaceholder: 'Describe the solution or resolution steps',
      markOverdue: 'Mark as Overdue',
      overdueReason: 'Overdue Reason',
      overdueReasonPlaceholder: 'Explain the overdue reason',

      // Call Info
      callInfo: 'Call Information',
      firstCallTime: 'First Call Time',
      answerTime: 'Answer Time',
      callCount: 'Call Count',
      answered: 'Answered',
      responseTime: 'Response Time (min)',
      minutes: 'min',
      autoCalculated: 'Auto: Answer Time - First Call Time',
      
      // Escalate & Handover
      escalateAndHandover: 'Escalate & Handover',
      escalate: 'Escalate Issue',
      escalateTo: 'Escalate To',
      hasHandover: 'Work Handover',
      handoverPerson: 'Handover To',
      handoverPersonPlaceholder: 'Handover to whom',
      handoverContent: 'Handover Content',
      handoverContentPlaceholder: 'Handover details...',
      
      // Attachments
      attachments: 'Attachments',
      dropToUpload: 'Drop files here to upload',
      orClickToSelect: 'or click to select',
      clickToSelect: 'Click to select',
      dragOrPaste: ', drag or Ctrl+V to paste',
      multipleImages: 'Multiple images supported',
      uploadingFile: 'Uploading...',
      fileCount: '{count} files'
    },
    
    // Actions
    actions: {
      confirmDelete: 'Confirm Delete',
      confirmDeleteMsg: 'Are you sure you want to delete this duty record? This action cannot be undone.',
      confirmDeleteBtn: 'Delete',
      deleteSuccess: 'Deleted successfully',
      deleteFailed: 'Delete failed',
      saveSuccess: 'Saved successfully',
      saveFailed: 'Save failed',
      exportSuccess: 'Exported successfully',
      exportFailed: 'Export failed',
      editPlannedFixTime: 'Edit Planned Fix Time',
      viewDetails: 'View Details',
      createSuccess: 'Created successfully',
      updateSuccess: 'Updated successfully',
      loadFailed: 'Load failed',
      shareSuccess: 'Share link created',
      shareFailed: 'Create share failed',
      linkCopied: 'Link copied',
      loadStatsFailed: 'Load statistics failed',
      exportDeveloping: 'Export feature in development',
      plannedFixTimeUpdated: 'Planned fix time updated',
      updateFailed: 'Update failed',
      fillRequired: 'Please fill required fields (duty time, duty person, project)',
      selectStatus: 'Please select status',
      selectRecordsFirst: 'Please select records to modify first',
      batchModifyFailed: 'Batch modify failed',
      selectDeleteFirst: 'Please select records to delete first',
      batchDeleteFailed: 'Batch delete failed',
      selectImageFile: 'Please select image file',
      uploadFailed: 'Upload failed',
      noPermissionAdd: 'No permission to add project',
      noPermissionEdit: 'No permission to edit project',
      noPermissionDelete: 'No permission to delete project',
      fillProjectNameCode: 'Please fill project name and code'
    },
    
    // Detail Modal
    detail: {
      title: 'Duty Record Details',
      basicInfo: 'Basic Information',
      handleInfo: 'Handle Information',
      callInfo: 'Call Information',
      handoverInfo: 'Handover Information',
      attachments: 'Attachments'
    },
    
    // Project Modal
    projectModal: {
      title: 'Project Configuration',
      addProject: 'Add Project',
      editProject: 'Edit Project',
      name: 'Project Name',
      code: 'Project Code',
      description: 'Description',
      status: 'Status',
      sortOrder: 'Sort Order',
      active: 'Active',
      inactive: 'Inactive',
      confirmDelete: 'Confirm Delete',
      confirmDeleteMsg: 'Are you sure you want to delete project "{name}"?',
      confirmDeleteBtn: 'Delete',
      deleteSuccess: 'Deleted successfully',
      deleteFailed: 'Delete failed',
      saveSuccess: 'Saved successfully',
      saveFailed: 'Save failed'
    },
    
    // Planned Fix Modal
    plannedFixModal: {
      title: 'Edit Planned Fix Time',
      currentTime: 'Current Planned Fix Time',
      newTime: 'New Planned Fix Time',
      notSet: 'Not set'
    },
    
    // Batch Operations
    batch: {
      modifyStatus: 'Batch Modify Status',
      selectStatus: 'Select New Status',
      confirmModify: 'Confirm',
      modifySuccess: 'Batch modified successfully',
      modifyFailed: 'Batch modify failed',
      batchDelete: 'Batch Delete',
      cancelSelect: 'Cancel Selection'
    },

    // Stats Page
    statsPage: {
      title: 'Duty Statistics Analysis',
      exportReport: 'Export Report',
      filters: {
        project: 'Project',
        handler: 'Handler',
        dutyPerson: 'Duty Person',
        eventType: 'Event Type',
        startDate: 'Start Date',
        endDate: 'End Date',
        today: 'Today',
        thisWeek: 'This Week',
        thisMonth: 'This Month',
        thisQuarter: 'This Quarter',
        all: 'All',
        allProjects: 'All Projects',
        allHandlers: 'All Handlers',
        allDutyPersons: 'All Duty Persons',
        allTypes: 'All Types'
      },
      overview: {
        title: 'Overview',
        totalRecords: 'Total Records',
        avgResponseTime: 'Avg Response Time',
        minutes: 'min',
        resolvedRate: 'Resolved Rate',
        overdueRate: 'Overdue Rate'
      },
      charts: {
        byStatus: 'By Status',
        byProject: 'By Project',
        byHandler: 'By Handler',
        byEventType: 'By Event Type',
        byDutyPerson: 'By Duty Person',
        byFeedback: 'By Feedback Type',
        responseTime: 'Response Time Distribution',
        trend: 'Trend Analysis',
        callDistribution: 'Call Distribution',
        byEscalate: 'By Escalate Type',
        showPercent: 'Show Percent',
        showCount: 'Show Count'
      }
    },

    // Table cells
    table: {
      answered: 'Answered',
      notAnswered: 'Not Answered'
    },

    // Empty States
    empty: {
      noRecords: 'No duty records',
      noData: 'No data'
    },

    // Pagination
    pagination: {
      page: 'Page',
      of: 'of',
      total: 'Total',
      perPage: 'per page',
      items: 'items',
      first: 'First',
      prev: 'Prev',
      next: 'Next',
      last: 'Last'
    },

    // Time Related
    time: {
      justNow: 'Just now',
      minutesAgo: '{n} min ago',
      hoursAgo: '{n} hours ago',
      daysAgo: '{n} days ago'
    }
  },

  // Sidebar Menu
  menu: {
    groups: {
      system: 'System',
      resource: 'Resources',
      monitor: 'Monitoring',
      k8s: 'Kubernetes',
      ticket: 'Tickets',
      automation: 'Automation',
      aiops: 'AIOps',
      release: 'Releases',
      logs: 'Logs',
      security: 'Security',
      settings: 'Settings'
    },
    items: {
      welcome: 'Welcome',
      users: 'Users',
      roles: 'Roles',
      permissions: 'Permissions',
      audit: 'Audit Logs',
      api: 'API Management',
      schedule: 'Scheduling',
      taskpool: 'Task Pool',
      incidents: 'Incidents',
      duty: 'Duty Records',
      tableMaintenance: 'Table Maintenance',
      tableHierarchyConfig: 'Table Config',
      assets: 'Assets',
      domains: 'Domains',
      merchants: 'Merchants',
      network: 'Network',
      serviceconfig: 'Service Config',
      topology: 'Topology',
      metrics: 'Metrics',
      alerts: 'Alerts',
      alertRules: 'Alert Rules',
      alertNotify: 'Notifications',
      dashboardScreen: 'Dashboard',
      clusters: 'Clusters',
      workloads: 'Workloads',
      configmaps: 'ConfigMaps',
      storage: 'Storage',
      terminal: 'Terminal',
      tickets: 'Tickets',
      workflow: 'Workflows',
      sla: 'SLA',
      ticketTemplate: 'Templates',
      jobs: 'Jobs',
      crontab: 'Cron Jobs',
      inspection: 'Inspection',
      selfHealing: 'Self-healing',
      anomaly: 'Anomaly Detection',
      rootCause: 'Root Cause',
      predict: 'Prediction',
      smartAlert: 'Smart Alerts',
      capacity: 'Capacity',
      deploy: 'Deployments',
      change: 'Changes',
      rollback: 'Rollbacks',
      logQuery: 'Log Query',
      logAnalysis: 'Log Analysis',
      logAlert: 'Log Alerts',
      vault: 'Vault',
      secrets: 'Secrets',
      certificates: 'Certificates',
      datasources: 'Data Sources',
      sysParams: 'Parameters',
      settings: 'Settings',
      ssoConfig: 'SSO Config',
      appsManage: 'Apps'
    }
  },

  // Welcome Page
  welcome: {
    greeting: 'Welcome back, {name}',
    today: 'Today is {date}',
    ssoNotice: 'You have logged in via Single Sign-On (SSO)',
    quickEntry: 'Quick Access',
    quickLinks: {
      domains: 'Domains',
      domainsDesc: 'Manage domains & certs',
      merchants: 'Merchants',
      merchantsDesc: 'Manage merchant info',
      network: 'Network',
      networkDesc: 'Manage network nodes',
      users: 'Users',
      usersDesc: 'Manage system users',
      audit: 'Audit Logs',
      auditDesc: 'View operation logs',
      duty: 'Duty Records',
      dutyDesc: 'Manage duty records'
    },
    stats: {
      domains: 'Total Domains',
      expiring: 'Expiring Soon',
      merchants: 'Total Merchants',
      active: 'Active',
      users: 'Total Users',
      activeUsers: 'Active Users'
    }
  },

  // Table Maintenance Page
  tableMaintenance: {
    pageTitle: 'Table Maintenance Records',
    recordCount: '{count} records in total',
    hierarchyConfig: 'Hierarchy Config',
    addRecord: 'Add Record',
    pasteRecord: 'Paste Records',
    deleteSelected: 'Delete Selected',
    exportExcel: 'Export Excel',

    tabs: {
      list: 'Records List',
      stats: 'Statistics'
    },

    stats: {
      total: 'Total Records',
      today: 'Today',
      week: 'This Week',
      month: 'This Month'
    },

    filters: {
      searchPlaceholder: 'Search table/description/operator...',
      searchAllPlaceholder: 'Search table/round ID/operator/reason...',
      filterConditions: 'Filter Conditions',
      qcStatusLabel: 'QC Status',
      projectLabel: 'Project',
      durationLabel: 'Duration',
      operatorLabel: 'Operator',
      operationLabel: 'Operation',
      dateRangeLabel: 'Date Range',
      dateTo: 'to',
      allOperations: 'All Operations',
      tableStatusLabel: 'Table Status',
      allTableStatus: 'All Status',
      allQcStatus: 'All QC Status',
      allProjects: 'All Projects',
      allDurations: 'All Durations',
      allOperators: 'All Operators',
      roundIdPlaceholder: 'Search round ID...',
      normal: 'Normal',
      abnormal: 'Abnormal',
      notInspected: 'Not Inspected',
      search: 'Search',
      reset: 'Reset'
    },

    columns: {
      date: 'Date',
      startTime: 'Start Time',
      notifyStartScreenshot: 'Start Screenshot',
      startDuration: 'Start Duration',
      endTime: 'End Time',
      notifyEndScreenshot: 'End Screenshot',
      closeDuration: 'Close Duration',
      affectedProjects: 'Affected Projects',
      site: 'Site',
      table: 'Table',
      tableStatus: 'Table Status',
      gameType: 'Game Type',
      maintenanceType: 'Maintenance Type',
      reason: 'Reason',
      affectSettlement: 'Affect Settlement',
      affectedRoundIds: 'Affected Round IDs',
      operation: 'Operation',
      operator: 'Operator',
      inspector: 'Inspector',
      qcStatus: 'QC Status',
      remark: 'Remark',
      createdBy: 'Created By',
      createdAt: 'Created At',
      updatedAt: 'Updated At',
      actions: 'Actions'
    },

    options: {
      duration: {
        twoMin: 'Within 2 min',
        fiveMin: 'Within 5 min',
        tenMin: 'Within 10 min',
        overTenMin: 'Over 10 min'
      },
      maintenanceType: {
        none: 'None',
        emergency: 'Emergency',
        temporary: 'Temporary',
        routine: 'Routine'
      },
      operation: {
        maintenance: 'Maintenance',
        cancel: 'Cancel',
        recalculate: 'Recalculate',
        repayout: 'Re-payout',
        missed: 'Missed Op',
        missedScreenshot: 'Missed Screenshot'
      },
      yesNo: {
        yes: 'Yes',
        no: 'No'
      },
      qcStatus: {
        normal: 'Normal',
        abnormal: 'Abnormal'
      }
    },

    placeholders: {
      select: 'Please select {field}',
      remarkRequired: 'QC abnormal, please fill in reason (required)'
    },

    actions: {
      view: 'View',
      copy: 'Copy',
      edit: 'Edit',
      delete: 'Delete',
      loadFailed: 'Failed to load records',
      saveFailed: 'Failed to save',
      saveSuccess: 'Saved successfully',
      deleteFailed: 'Failed to delete',
      deleteSuccess: 'Deleted successfully',
      confirmDelete: 'Are you sure you want to delete this record?',
      confirmBatchDelete: 'Are you sure you want to delete {count} selected records?',
      exportSuccess: 'Export successful',
      exportFailed: 'Export failed',
      copySuccess: 'Copied! Click "Paste Records" button to add multiple records',
      pasteSuccess: 'Pasted successfully, {count} records added'
    },

    form: {
      addTitle: 'Add Maintenance Record',
      editTitle: 'Edit Maintenance Record',
      pasteTitle: 'Paste Records',
      pasteCount: 'Paste Count',
      pasteHint: 'Enter the number of records to paste (1-100)'
    },

    hints: {
      noGameTypesForTables: 'No game types for selected tables',
      selectTableFirst: 'Please select a table first',
      noSitesConfigured: 'No sites configured',
      goConfigure: 'Go Configure'
    },

    detail: {
      title: 'Record Details',
      basicInfo: 'Basic Information',
      attachments: 'Attachments'
    },

    statsPanel: {
      title: 'Statistics Analysis',
      dateRange: 'Date Range',
      searchBtn: 'Search',
      export: 'Export Stats',
      overview: {
        title: 'Overview',
        totalRecords: 'Total Records',
        maintenanceCount: 'Maintenance',
        cancelCount: 'Cancel',
        recalculateCount: 'Recalculate',
        repayoutCount: 'Re-payout'
      },
      byOperation: 'By Operation',
      byMaintenanceType: 'By Maintenance Type',
      byStartDuration: 'Start Duration',
      byCloseDuration: 'Close Duration',
      byTotalDuration: 'Total Duration',
      byOperator: 'By Operator',
      byInspector: 'By Inspector',
      byQcStatus: 'QC Status',
      byGameType: 'By Game Type',
      bySite: 'By Site',
      byTableSummary: 'Table Summary',
      byTable: 'Table Detail (By Project)',
      filterProject: 'Project',
      filterDuration: 'Duration',
      filterOperator: 'Operator',
      filterRoundId: 'Round ID',
      cancelDetail: 'Cancel Detail',
      recalcDetail: 'Recalculate Detail',
      repayoutDetail: 'Repayout Detail',
      missedDetail: 'Missed Operation Detail',
      missedScreenshotDetail: 'Missed Screenshot Detail',
      detailDate: 'Date',
      detailTable: 'Table',
      detailProject: 'Project',
      detailRoundId: 'Round ID',
      detailDuration: 'Duration',
      detailOperator: 'Operator',
      tableColumns: {
        tableNo: 'Table No.',
        gameType: 'Game Type',
        site: 'Site',
        projects: 'Projects',
        maintCount: 'Maint. Count',
        status: 'Status'
      },
      byAffectedProject: 'By Affected Project',
      notFilled: 'Not Filled',
      unknown: 'Unknown',
      qcPending: 'Pending',
      maintDurationDetail: 'Maintenance Duration Detail',
      detailMaintType: 'Maint. Type',
      detailStartDuration: 'Start Duration',
      detailCloseDuration: 'Close Duration',
      detailTotalDuration: 'Total Duration',
      tableTimes: 'times',
      involves: ', involving',
      csvDateRange: 'Date Range',
      csvTotalRecords: 'Total Records',
      csvTotalTables: 'Total Tables',
      csvNoLimit: 'No limit',
      csvAllTime: 'All time',
      csvByMaintType: 'By Maintenance Type (Duration Distribution)',
      csvMaintType: 'Maint. Type',
      csvTotal: 'Total',
      csvByOpType: 'By Operation Type (Start Duration Distribution)',
      csvOpType: 'Operation Type',
      csvMaintStartDuration: 'Maintenance - Start Duration',
      csvDuration: 'Duration',
      csvCount: 'Count',
      csvMaintCloseDuration: 'Maintenance - Close Duration',
      csvMaintTotalDuration: 'Maintenance - Total Duration (Max of Start & Close)',
      csvByOperator: 'By Operator',
      csvOperator: 'Operator',
      csvByInspector: 'By Inspector',
      csvInspector: 'Inspector',
      csvQcStatus: 'QC Status',
      csvStatus: 'Status',
      csvQcNormal: 'Normal',
      csvQcAbnormal: 'Abnormal',
      csvQcPending: 'Pending',
      csvByAffectedProject: 'By Affected Project',
      csvProject: 'Project',
      csvNoAffect: 'No Affect',
      csvByGameType: 'By Game Type',
      csvGameType: 'Game Type',
      csvBySite: 'By Site',
      csvSite: 'Site',
      csvTableSummary: 'Table Summary',
      csvTableNo: 'Table No.',
      csvMaintCount: 'Maint. Count',
      csvProjects: 'Projects',
      csvSites: 'Sites',
      csvGameTypes: 'Game Types',
      csvStatus: 'Status',
      csvTableDetail: 'Table Detail (By Project)',
      csvFileName: 'Table_Maintenance_Stats'
    },

    table: {
      loading: 'Loading...',
      empty: 'No data',
      pagination: {
        total: 'Total',
        records: 'records',
        perPage: 'Per page',
        items: 'items',
        page: 'Page',
        first: 'First',
        prev: 'Previous',
        next: 'Next',
        last: 'Last'
      }
    },

    upload: {
      click: 'Click or drag to upload',
      hint: 'Supports JPG, PNG, GIF, max 5MB'
    },

    selectAll: 'Select All / Deselect All'
  },

  // Table Config Page
  tableHierarchy: {
    pageTitle: 'Table Config',
    subtitle: 'Configure global project, site, game type and table relationships',
    recordCount: '{count} tables',

    stats: {
      projects: 'projects',
      sites: 'sites',
      tables: 'tables'
    },

    search: {
      placeholder: 'Search project, site, table...',
      searchBtn: 'Search',
      reset: 'Reset'
    },

    filters: {
      title: 'Filter',
      project: 'Project',
      site: 'Site',
      gameType: 'Game Type',
      tableNo: 'Table No.',
      allProjects: 'All Projects',
      allSites: 'All Sites',
      allGameTypes: 'All Game Types',
      tableNoPlaceholder: 'Enter table number...'
    },

    add: {
      button: 'Add',
      single: 'Single Add',
      batch: 'Batch Add',
      addProject: 'Add Project',
      addSite: 'Add Site',
      addTable: 'Add Table',
      addGameType: 'Add Game Type',
      batchProject: 'Batch Add Projects',
      batchSite: 'Batch Add Sites',
      batchGameType: 'Batch Add Game Types',
      batchTable: 'Batch Add Tables'
    },

    columns: {
      expand: '',
      project: 'Project',
      site: 'Site',
      table: 'Table',
      gameType: 'Game Type',
      status: 'Status',
      siteCount: 'Sites',
      tableCount: 'Tables',
      actions: 'Actions'
    },

    status: {
      enabled: 'Enabled',
      disabled: 'Disabled'
    },

    types: {
      project: 'Project',
      site: 'Site',
      table: 'Table'
    },

    form: {
      addProject: 'Add Project',
      editProject: 'Edit Project',
      addSite: 'Add Site',
      editSite: 'Edit Site',
      addTable: 'Add Table',
      addGameType: 'Add Game Type',
      editGameType: 'Edit Game Type',
      gameTypeNameZh: 'Chinese Name',
      gameTypeNameEn: 'English Name',
      gameTypeNameZhPlaceholder: 'e.g., 百家乐',
      gameTypeNameEnPlaceholder: 'e.g., Baccarat',
      gameTypeNameZhRequired: 'Chinese name is required',
      editTable: 'Edit Table',
      batchAddProject: 'Batch Add Projects',
      batchAddSite: 'Batch Add Sites',
      batchAddGameType: 'Batch Add Game Types',
      batchAddTable: 'Batch Add Tables',
      gameTypeName: 'Game Type Name',
      belongProject: 'Project',
      belongSite: 'Site',
      projectName: 'Project Name',
      projectNameZh: 'Chinese Name',
      projectNameEn: 'English Name',
      projectNameZhPlaceholder: 'e.g., 项目A',
      projectNameEnPlaceholder: 'e.g., Project A',
      projectNameZhRequired: 'Project Chinese name is required',
      siteName: 'Site Name',
      siteNameZh: 'Chinese Name',
      siteNameEn: 'English Name',
      siteNameZhPlaceholder: 'e.g., 1号现场',
      siteNameEnPlaceholder: 'e.g., Site 1',
      siteNameZhRequired: 'Chinese name is required',
      optional: '(Optional)',
      tableNumber: 'Table Number',
      gameType: 'Game Type',
      gameTypeHint: '(Multiple selection)',
      batchGameTypeHint: '(One per line, format: Chinese|English)',
      selectProject: 'Select project',
      selectSite: 'Select site',
      projectPlaceholder: 'e.g., Project A',
      sitePlaceholder: 'e.g., Site 1',
      tablePlaceholder: 'e.g., B01',
      tableStatus: 'Table Status',
      batchHint: '(One per line)',
      batchProjectPlaceholder: 'Project A|项目A\nProject B|项目B\nProject C|项目C',
      batchSitePlaceholder: 'Site 1|1号现场\nSite 2|2号现场\nVIP Site|VIP现场',
      batchGameTypePlaceholder: 'Baccarat|百家乐\nDragon Tiger|龙虎\nRoulette|轮盘',
      batchTablePlaceholder: 'A01\nA02\nA03'
    },

    actions: {
      addSite: 'Add Site',
      addTable: 'Add Table',
      edit: 'Edit',
      delete: 'Delete',
      save: 'Save',
      saving: 'Saving...',
      cancel: 'Cancel'
    },

    messages: {
      loadFailed: 'Failed to load config',
      saveSuccess: 'Config saved successfully',
      saveFailed: 'Failed to save',
      noPermission: 'No permission to save table hierarchy config',
      deleteSuccess: 'Deleted successfully',
      confirmDeleteProject: 'Are you sure you want to delete project "{name}"?',
      confirmDeleteSite: 'Are you sure you want to delete site "{name}"?',
      confirmDeleteTable: 'Are you sure you want to delete table "{name}"?',
      confirmDeleteGameType: 'Are you sure you want to delete game type "{name}"?',
      cascadeWarningProject: 'All sites and tables under this project will also be deleted',
      cascadeWarningSite: 'All tables under this site will also be deleted',
      hasTablesCannotDelete: 'Has associated tables, cannot delete'
    },

    configBtn: {
      project: 'Projects',
      site: 'Sites',
      gameType: 'Game Types',
      table: 'Tables'
    },

    gameTypeSection: {
      title: 'Game Types',
      badge: 'Game',
      empty: 'No game types, click "Add" button above to create'
    },

    projectSection: {
      title: 'Project Management',
      empty: 'No projects yet. Click "Add" button above to create.'
    },

    siteSection: {
      title: 'Site Management',
      empty: 'No sites yet. Click "Add" button above to create.'
    },

    pagination: {
      total: 'Total',
      records: 'tables',
      perPage: 'Per page',
      items: 'items'
    },

    empty: {
      noData: 'No config data',
      noMatch: 'No matching data found',
      noDataHint: 'Click "Add" button above to create',
      noMatchHint: 'Try other search keywords'
    },

    loading: 'Loading...',

    gameTypes: {
      baccarat: 'Baccarat',
      dragonTiger: 'Dragon Tiger',
      roulette: 'Roulette',
      sicBo: 'Sic Bo',
      other: 'Other'
    }
  }
}
