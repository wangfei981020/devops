-- 节点全部 conditions（JSON）用于详情弹窗；conditions 列改为只存"真压力"摘要，SysctlChanged 等信息 condition 不再算异常。
ALTER TABLE k8s_nodes ADD COLUMN conditions_json MEDIUMTEXT AFTER conditions;
