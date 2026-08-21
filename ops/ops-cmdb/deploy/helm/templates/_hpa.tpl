{{/*
HPA 片段，前后端共用。

⚠️ 抽成一份是刻意的：前后端各写一份的话，改扩容策略时必然只改一处，
而"另一处没跟上"在集群里完全看不出来 —— 两个 HPA 都存在、都健康，
只是行为不一样，要到某次扩容没按预期发生才会有人发现。

参数：ctx=根上下文  name=目标 Deployment 名  cfg=autoscaling 配置  component=标签用

⚠️ 调用处必须写 `{{ include ... }}`，**不能写 `{{- include ... }}`**。
带 `-` 会把前一个文档末尾的换行吃掉，本模板开头的 `---` 于是粘到上一行尾部：

    app.kubernetes.io/component: backend---
    apiVersion: autoscaling/v2

两个对象变成一个非法文档。后果极其隐蔽：`helm template | grep -c "kind:"`
数出来还是对的（两行 kind 都在），helm 也不报错，
但 apply 时前一个对象（PDB）根本不会被创建 —— 而 `helm get manifest`
里它明明在。真的靠 kubectl get 才发现集群里一个 PDB 都没有。
*/}}
{{- define "cmdb.hpa" -}}
{{- $ctx := .ctx -}}
{{- $cfg := .cfg -}}
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: {{ .name }}
  labels:
    {{- include "cmdb.labels" $ctx | nindent 4 }}
    app.kubernetes.io/component: {{ .component }}
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: {{ .name }}
  minReplicas: {{ $cfg.minReplicas }}
  maxReplicas: {{ $cfg.maxReplicas }}
  metrics:
    {{- /*
      CPU 与内存**任一**超阈值就扩容（HPA 取各指标算出的副本数的最大值），
      而缩容要**全部**低于阈值才会发生。这是 HPA 的既定语义，不是配置项。
    */}}
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: {{ $cfg.targetCPUUtilizationPercentage }}
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: {{ $cfg.targetMemoryUtilizationPercentage }}
  behavior:
    scaleDown:
      # 缩容给长稳定窗口：流量是尖峰型的，缩太快会在下一个尖峰立刻又扩，来回抖动。
      #
      # ⚠️ 内存指标基本上是**单向**的：Go 运行时和 nginx 都不怎么把内存还给 OS，
      # RSS 涨上去很难落回来。所以内存触发的扩容多半不会自己缩回去 ——
      # 这不是配置错了，是内存这个指标的性质。真要缩回去通常得靠重启。
      # 把它当"涨上去就别再涨"的保护，不要指望它像 CPU 那样自动收敛。
      stabilizationWindowSeconds: 300
{{- end -}}
