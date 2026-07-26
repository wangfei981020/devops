-- Pod 失败/异常原因(容器 waiting.reason / OOMKilled / Pending 未调度原因)，供命名空间概览+Pod页展示。
ALTER TABLE k8s_pods ADD COLUMN reason VARCHAR(255) NOT NULL DEFAULT '' AFTER phase;
