{{- define "cmdb.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "cmdb.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "cmdb.frontend.fullname" -}}
{{- printf "%s-frontend" (include "cmdb.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "cmdb.backend.fullname" -}}
{{- printf "%s-backend" (include "cmdb.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
后端地址 —— 前端 nginx 反代的目标。

这个 helper 是单 chart 最主要的收益：地址由 chart 自己推导，不需要人填。
手工同步地址是上一代反复出问题的地方（env / Secret / ConfigMap 三处，第三处最常漏），
收口在这里之后就只有一处真相。

⚠️ 曾经有个 backend.enabled=false 分支，用来反代到 chart 之外的既有后端
（迁移期指向 enterprise/ops-data-plane）。后端搬进本仓库后那条路径就没有用了，
留着反而危险：那是**另一个产品**的后端，接上去之后新前端要的字段它没有，
而缺字段在界面上只表现为"这块永远是空的" —— 不报错、不 404、监控看不见。
已删除，后端一律由本 chart 部署。
*/}}
{{- define "cmdb.backendAddr" -}}
{{ include "cmdb.backend.fullname" . }}:{{ .Values.backend.service.port }}
{{- end }}

{{- define "cmdb.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: {{ include "cmdb.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: opsplane
{{- end }}

{{- define "cmdb.frontend.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cmdb.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: frontend
{{- end }}

{{- define "cmdb.backend.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cmdb.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: backend
{{- end }}

{{/* 反亲和：把同一组件的副本尽量分散到不同节点 */}}
{{- define "cmdb.antiAffinity" -}}
{{- $ctx := index . 0 -}}
{{- $selector := index . 1 -}}
{{- if $ctx.Values.podAntiAffinity.enabled }}
affinity:
  podAntiAffinity:
    {{- if $ctx.Values.podAntiAffinity.required }}
    requiredDuringSchedulingIgnoredDuringExecution:
      - topologyKey: kubernetes.io/hostname
        labelSelector:
          matchLabels:
{{ $selector | indent 12 }}
    {{- else }}
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          topologyKey: kubernetes.io/hostname
          labelSelector:
            matchLabels:
{{ $selector | indent 14 }}
    {{- end }}
{{- end }}
{{- end }}

{{/*
镜像地址：global.imageRegistry + 组件的 repository。

# 为什么要有全局前缀

前后端各写一份完整地址的话，换仓库要改两处 —— 而**只改一处不会报错**：
改了的那个正常拉，没改的那个还在拉旧仓库。两个镜像都能起来，
只是版本对不上，表现为"前端升了后端没升"这类最难复现的错配。

registry 为空时用组件自己的 repository 原样（兼容写全路径的老配置）。
*/}}
{{/*
统一 tag。优先级：组件级 image.tag > global.tag > Chart.AppVersion。

# 为什么要有 global.tag

前后端**必须是同一个版本**：同一个 chart 一次发布、一次回滚。
让人在两个地方各填一遍，迟早只改一处 —— 而只改一处**不会报错**：
两个镜像都拉得到、都能起来，只是前端新后端旧，
表现为某个接口 404 或字段缺失，排查时谁都想不到是版本错配。

⚠️ 2026-08-18 之前 values 里写 global.tag 是**不生效的**（helper 只读组件级），
设了没反应且不报错。现在把它做成真的，组件级仍然优先，原有 --set 用法不受影响。
*/}}
{{- define "cmdb.tag" -}}
{{- $g := (.ctx.Values.global | default dict).tag | default "" -}}
{{- .img.tag | default $g | default .ctx.Chart.AppVersion -}}
{{- end -}}

{{/*
前后端 tag 一致性闸。挂在 requireEnv 里，每个模板都会经过。

光靠约定保证不了「一个 tag」—— 必须让不一致**装不上**。
*/}}
{{- define "cmdb.requireSameTag" -}}
{{- $g := (.Values.global | default dict).tag | default "" -}}
{{- $b := .Values.backend.image.tag | default $g | default .Chart.AppVersion -}}
{{- $f := .Values.frontend.image.tag | default $g | default .Chart.AppVersion -}}
{{- if ne $b $f -}}
{{- fail (printf "前后端 tag 不一致：backend=%s frontend=%s。同一个 chart 必须一次发布、一次回滚 —— 版本错配不会报错，只表现为某个接口 404 或字段缺失，最难排查。请用 global.tag 统一设置，或把两个组件的 image.tag 设成同一个值。" $b $f) -}}
{{- end -}}
{{- end -}}

{{- define "cmdb.image" -}}
{{- $reg := .ctx.Values.global.imageRegistry | default "" -}}
{{- $repo := .img.repository -}}
{{- $tag := include "cmdb.tag" . -}}
{{- if $reg -}}
{{- printf "%s/%s:%s" (trimSuffix "/" $reg) $repo $tag -}}
{{- else -}}
{{- printf "%s:%s" $repo $tag -}}
{{- end -}}
{{- end -}}

{{/*
安全上下文。前后端**完全一致**，所以只有一份。

⚠️ 不要为了"以后可能不一样"提前拆成两份：
两份一模一样的配置，改的时候必然只改一处，而分叉之后两边都能跑，
没人会发现 —— 直到某个按属主授权的地方失败，报错却只说 permission denied。
真出现了组件间差异，再在那时拆，并写清楚为什么不同。
*/}}
{{- define "cmdb.securityContext" -}}
runAsNonRoot: true
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
capabilities:
  drop: ["ALL"]
seccompProfile:
  type: RuntimeDefault
{{- end -}}

{{/*
没传环境 values 文件时直接失败。
拿一份空配置渲染出来的 release 是能装的 —— 镜像地址、入口、凭据全是空，
而失败点会推迟到 pod 起不来，那时排查的人看到的是 ImagePullBackOff，
不会想到根因是「helm 命令少了一个 -f」。
*/}}
{{- define "cmdb.requireEnv" -}}
{{- include "cmdb.requireSameTag" . -}}
{{- if .Values.requireEnvValues -}}
{{- fail "必须指定环境 values 文件：生产用 -f values-prod.yaml，本地用 -f values-local.yaml。chart 自带的 values.yaml 只是默认结构（镜像仓库为空、入口全关），装出来跑不起来。" -}}
{{- end -}}
{{- end -}}
