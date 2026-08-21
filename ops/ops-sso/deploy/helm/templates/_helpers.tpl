{{- define "sso.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "sso.fullname" -}}
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

{{- define "sso.frontend.fullname" -}}
{{- printf "%s-frontend" (include "sso.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "sso.backend.fullname" -}}
{{- printf "%s-backend" (include "sso.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
后端地址 —— 前端 nginx 反代的目标。

这个 helper 是单 chart 最主要的收益：地址由部署形态推导，不需要人填。
  backend.enabled=true  → 指向本 chart 部署的后端 Service（同 namespace，短名即可）
  backend.enabled=false → 指向 external 里配置的既有后端

手工同步地址是上一代反复出问题的地方，收口在这里之后就只有一处真相。
*/}}
{{- define "sso.backendAddr" -}}
{{- if .Values.backend.enabled -}}
{{ include "sso.backend.fullname" . }}:{{ .Values.backend.service.port }}
{{- else -}}
{{ required "backend.enabled=false 时必须给出 backend.external.host" .Values.backend.external.host }}:{{ .Values.backend.external.port }}
{{- end -}}
{{- end }}

{{- define "sso.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: {{ include "sso.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: opsplane
{{- end }}

{{- define "sso.frontend.selectorLabels" -}}
app.kubernetes.io/name: {{ include "sso.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: frontend
{{- end }}

{{- define "sso.backend.selectorLabels" -}}
app.kubernetes.io/name: {{ include "sso.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: backend
{{- end }}

{{/* 反亲和：把同一组件的副本尽量分散到不同节点 */}}
{{- define "sso.antiAffinity" -}}
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

{{/* 通用选择器标签。各组件在其后追加 app.kubernetes.io/component。 */}}
{{- define "sso.selectorLabels" -}}
app.kubernetes.io/name: {{ include "sso.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "sso.gateway.selectorLabels" -}}
{{ include "sso.selectorLabels" . }}
app.kubernetes.io/component: gateway
{{- end }}

{{/*
镜像地址：global.imageRegistry + 组件的 repository。
前后端各写一份完整地址的话，换仓库要改两处，而**只改一处不会报错**：
改了的正常拉，没改的还在拉旧仓库 —— 两个镜像都能起来，只是版本对不上。
*/}}
{{- define "sso.image" -}}
{{- $reg := .ctx.Values.global.imageRegistry | default "" -}}
{{- $tag := .img.tag | default .ctx.Chart.AppVersion -}}
{{- if $reg -}}
{{- printf "%s/%s:%s" (trimSuffix "/" $reg) .img.repository $tag -}}
{{- else -}}
{{- printf "%s:%s" .img.repository $tag -}}
{{- end -}}
{{- end -}}
