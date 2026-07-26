-- 成本阶段2：节点月成本手动覆盖（IDC 物理机/无机型时手填；GKE 走机型费率估算）。
ALTER TABLE k8s_nodes ADD COLUMN monthly_cost_override DECIMAL(12,2) NOT NULL DEFAULT 0 AFTER stuck;
