# 一个 Dockerfile 出两个二进制：控制面与网关必须同版本号 ——
# 允许版本漂移就等于允许「网关 v9 / 控制面 v12」这种排障时对不上的状态。
FROM golang:1.25-alpine AS build
ARG VERSION=dev
ARG GITSHA=unknown
ARG EDITION=ce
# ⚠️ 构建上下文是 **ops/**（各产品所在的那一层），不是 backend/。
#
#	后端通过 `replace ops-kit/licensekit => ../../ops-kit/licensekit`
#	依赖同仓的共享授权内核。上下文若只给 backend/，那个目录不在里面，
#	go mod download 会报「open /ops-kit/licensekit/go.mod: no such file」。
#	反过来上下文给 ops/ 而这里写 `COPY go.mod go.sum ./`，则报「/go.sum: not found」。
#	两种报错都只说文件不存在，看不出真因是上下文给宽了还是给窄了。
#
#	tooling/scripts/build-image.sh 会按 go.mod 里有没有 replace 自动选上下文，
#	**这个文件必须和它的选择一致**。（本文件曾落后于那次改动：
#	另一处把 licensekit 并进来加了 replace，脚本随之改用 ops/ 作上下文，
#	而这里还是窄上下文的写法，构建当场失败。）
WORKDIR /src

# 先只拷 go.mod/go.sum，让依赖层可缓存 —— 改业务代码不必重下依赖
COPY ops-kit/licensekit/go.mod ops-kit/licensekit/go.sum* ./ops-kit/licensekit/
COPY ops-sso/backend/go.mod ops-sso/backend/go.sum ./ops-sso/backend/
WORKDIR /src/ops-sso/backend
RUN go mod download

WORKDIR /src
COPY ops-kit/licensekit ./ops-kit/licensekit
COPY ops-sso/backend ./ops-sso/backend
WORKDIR /src/ops-sso/backend
RUN CGO_ENABLED=0 go build $([ "$EDITION" = "ee" ] && echo "-tags ee") \
      -ldflags "-s -w -X main.version=${VERSION} -X main.gitsha=${GITSHA} -X main.edition=${EDITION}" \
      -o /out/server ./cmd/server && \
    CGO_ENABLED=0 go build \
      -ldflags "-s -w -X main.version=${VERSION} -X main.gitsha=${GITSHA}" \
      -o /out/gateway ./cmd/gateway

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 oap
USER 10001
COPY --from=build /out/server /usr/local/bin/server
COPY --from=build /out/gateway /usr/local/bin/gateway
ENV TZ=Asia/Shanghai
EXPOSE 8080 8081
ENTRYPOINT ["/usr/local/bin/server"]
