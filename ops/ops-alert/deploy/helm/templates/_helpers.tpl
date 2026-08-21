{{- define "alert.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "alert.fullname" -}}
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

{{- define "alert.frontend.fullname" -}}
{{- printf "%s-frontend" (include "alert.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "alert.backend.fullname" -}}
{{- printf "%s-backend" (include "alert.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
后端地址。恒为本 chart 部署的后端 Service（同 namespace，短名即可）。

⚠️ 这里曾支持 backend.enabled=false + external.host，把前端指向另行部署的后端。
   已删除：后端搬进 ops/ops-alert/backend 之后，那条路只剩坏处 ——
   前端可能指着**另一个产品**的后端，而它缺字段的表现只是"这块永远是空的"，
   不报错也不 404，排查时会一路怀疑前端。
*/}}
{{- define "alert.backendAddr" -}}
{{ include "alert.fullname" . }}-backend:{{ .Values.backend.service.port }}
{{- end -}}

{{- define "alert.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: {{ include "alert.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: opsplane
{{- end }}

{{- define "alert.frontend.selectorLabels" -}}
app.kubernetes.io/name: {{ include "alert.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: frontend
{{- end }}

{{- define "alert.backend.selectorLabels" -}}
app.kubernetes.io/name: {{ include "alert.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: backend
{{- end }}

{{/* 反亲和：把同一组件的副本尽量分散到不同节点 */}}
{{- define "alert.antiAffinity" -}}
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
前后端各写一份完整地址的话，换仓库要改两处，而**只改一处不会报错**：
改了的正常拉，没改的还在拉旧仓库 —— 两个镜像都能起来，只是版本对不上。
*/}}
{{- define "alert.image" -}}
{{- $reg := .ctx.Values.global.imageRegistry | default "" -}}
{{- $tag := .img.tag | default .ctx.Values.global.tag | default .ctx.Chart.AppVersion -}}
{{- if $reg -}}
{{- printf "%s/%s:%s" (trimSuffix "/" $reg) .img.repository $tag -}}
{{- else -}}
{{- printf "%s:%s" .img.repository $tag -}}
{{- end -}}
{{- end -}}

{{/*
版本一致性校验。

# 为什么要有

前后端是**同一个产品的两半**：同一个 chart、一次发布、一次回滚。
版本分叉的后果不是报错，而是"前端调了后端还没有的接口"——
界面上表现为某块永远是空的、或者点了没反应，不 404 也不报错。

线上真出现过：backend v0.1.4 配 frontend v0.1.7，
因为部署命令是 `--set frontend.image.tag=X --set backend.image.tag=Y`，
两个值靠人记得填一样。靠记忆的约定迟早会破。

现在的用法是**一个版本号**：
  --set global.tag=v0.2.0

组件级 tag 仍然保留（应急时单独回滚某一半），但一旦两边解析结果不同，
这里直接让 helm 失败并说明原因，而不是安静地装出去。
*/}}
{{- define "alert.checkVersions" -}}
{{/*
🔴 旧字段名必须显式报错，**不能静默忽略**。

这个 chart 早期用的是 global.imageTag，而 ops/ 家族的其余产品
（ops-version / ops-cmdb）一律是 global.tag。照着它们的部署命令写
`--set global.tag=...` 时，旧名字下 helm 会**安静地忽略这个值**，
回落到 Chart.AppVersion——渲染出来是 0.2.0 这种根本没构建过的 tag。
运气好是 ImagePullBackOff（吵，能发现），运气不好正好有个同名旧镜像，
于是你以为发了新版本，实际跑的是几个月前那份。
所以这里宁可让 helm 失败。
*/}}
{{- if (.Values.global | default dict).imageTag -}}
{{- fail "global.imageTag 已改名为 global.tag（与 ops-version / ops-cmdb 对齐）。请改用 --set global.tag=<版本>。保留旧名字会让它被静默忽略并回落到 Chart.AppVersion，装出一个你没构建过的版本。" -}}
{{- end -}}
{{- $fe := .Values.frontend.image.tag | default .Values.global.tag | default .Chart.AppVersion -}}
{{- $be := .Values.backend.image.tag | default .Values.global.tag | default .Chart.AppVersion -}}
{{- if ne $fe $be -}}
{{- fail (printf "前后端版本不一致：frontend=%s backend=%s。它们必须是同一个版本——用 --set global.tag=<版本> 一次设置两边。确实要分开发布时，才显式设置 frontend.image.tag / backend.image.tag。" $fe $be) -}}
{{- end -}}
{{- end -}}

{{/*
必须指定环境 values 文件。

chart 自带的 values.yaml 只是默认结构（镜像仓库为空、入口全关），
装出来跑不起来 —— 与其让人拿到一个"看起来装上了、实际连不上任何东西"的实例，
不如在渲染期就失败并说清该传哪个文件。
*/}}
{{- define "alert.requireEnv" -}}
{{- if .Values.requireEnvValues -}}
{{- fail "必须指定环境 values 文件：生产用 -f values-prod.yaml，本地用 -f values-local.yaml。chart 自带的 values.yaml 只是默认结构（镜像仓库为空、入口全关），装出来跑不起来。" -}}
{{- end -}}
{{- end -}}

{{/*
后端 Secret 的名字。

create=true  → 本 chart 生成的那个
create=false → existingSecret 指定的既有 Secret

⚠️ 让人在 values 里手填 secretRef 名字，就是"地址靠手工同步"的同一个坑：
   填错不会报错，pod 起不来只说 secret not found，而人往往先怀疑 RBAC。
*/}}
{{- define "alert.secretName" -}}
{{- if .Values.backend.secret.create -}}
{{ include "alert.backend.fullname" . }}-secret
{{- else -}}
{{ required "backend.secret.create=false 时必须给出 backend.secret.existingSecret" .Values.backend.secret.existingSecret }}
{{- end -}}
{{- end -}}
