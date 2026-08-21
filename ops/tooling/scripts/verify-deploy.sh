#!/usr/bin/env bash
#
# 部署后校验：**跑起来的那个镜像，是不是你刚推的那个**。
#
# 为什么需要这一步（真实事故）：
#
#   推了 ops-alert-frontend:v0.2.0 → helm upgrade → pod Running、版本页也显示 v0.2.0
#   —— 但界面上新功能一个都没有。
#
#   原因是节点上早就缓存了一个**同名 tag 的不同镜像**（tag 被覆盖过，
#   多个会话/流水线都会往同一个 tag 推），而 imagePullPolicy 是 IfNotPresent，
#   于是 kubelet 根本没去 registry 取新的。
#
#   这类故障没有任何报错：Pod 健康、探针通过、版本号"正确"（因为版本号是
#   构建期写进镜像的，旧镜像里写的也是它自己的版本）。唯一能证伪的是 digest。
#
# 用法：
#   tooling/scripts/verify-deploy.sh ops-alert ops-alert v0.2.0
#
# 不一致时的处理：
#   docker exec desktop-control-plane crictl rmi <镜像:tag>   # 删掉节点上的旧的
#   kubectl rollout restart deploy/<deploy> -n <ns>

set -euo pipefail

PRODUCT="${1:-}"
NAMESPACE="${2:-}"
VERSION="${3:-}"
REGISTRY="${REGISTRY:-localhost:8070/opsplatform-dev}"
HARBOR_URL="${HARBOR_URL:-http://localhost:8070}"
HARBOR_AUTH="${HARBOR_AUTH:-admin:Harbor12345}"

if [[ -z "$PRODUCT" || -z "$NAMESPACE" || -z "$VERSION" ]]; then
  echo "用法: $0 <产品> <命名空间> <版本>" >&2
  echo "例:   $0 ops-alert ops-alert v0.2.0" >&2
  exit 1
fi

fail=0

for COMPONENT in backend frontend; do
  REPO="${REGISTRY#*/}/${PRODUCT}-${COMPONENT}"   # 去掉主机名，留 项目/镜像名

  # registry 上这个 tag 现在指向谁
  want="$(curl -fsS -u "$HARBOR_AUTH" \
        -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' \
        -o /dev/null -D - "${HARBOR_URL}/v2/${REPO}/manifests/${VERSION}" 2>/dev/null \
        | tr -d '\r' | awk -F': ' '/[Dd]ocker-[Cc]ontent-[Dd]igest/{print $2}')"

  if [[ -z "$want" ]]; then
    echo "✗ ${COMPONENT}: registry 上找不到 ${REPO}:${VERSION}"
    fail=1
    continue
  fi

  # 集群里实际跑的是谁。imageID 才是真相 —— .spec.containers[].image 只是
  # 你**要求**的 tag，它永远等于你写的那个，证明不了任何事
  # ⚠️ 用 while read 而不是 mapfile：macOS 自带 bash 3.2，没有 mapfile，
  # 报的是 "mapfile: command not found" —— 看着像少装了工具，其实是版本太老。
  # ⚠️ 必须排除**正在终止**的 Pod（metadata.deletionTimestamp 非空）。
  # kubectl rollout status 返回成功时，旧 Pod 往往还在优雅终止期里，
  # 把它们算进来会报"跑的不是刚推的镜像"——一个假警报。
  # 假警报比漏报更伤：报几次狼来了，以后真出问题也没人看了。
  got=()
  while IFS= read -r line; do
    [[ -n "$line" ]] && got+=("$line")
  # ⚠️ 用 go-template 而不是 jsonpath：kubectl 的 jsonpath 不支持 `!`，
  # 写 [?(!@.metadata.deletionTimestamp)] 会报 unrecognized character U+0021。
  done < <(kubectl get pod -n "$NAMESPACE" \
      -l "app.kubernetes.io/component=${COMPONENT}" \
      -o go-template='{{range .items}}{{if not .metadata.deletionTimestamp}}{{range .status.containerStatuses}}{{.imageID}}{{"\n"}}{{end}}{{end}}{{end}}' \
      2>/dev/null | sed 's/.*@//' || true)

  if [[ ${#got[@]} -eq 0 ]]; then
    echo "✗ ${COMPONENT}: 命名空间 ${NAMESPACE} 里没有找到 Pod"
    fail=1
    continue
  fi

  bad=0
  for d in "${got[@]}"; do
    [[ "$d" == "$want" ]] || bad=1
  done

  if [[ $bad -eq 0 ]]; then
    echo "✓ ${COMPONENT}: ${#got[@]} 个副本都跑在 ${want:0:19}…"
  else
    echo "✗ ${COMPONENT}: 跑的不是刚推的那个镜像"
    echo "    registry ${VERSION} → ${want}"
    for d in "${got[@]}"; do echo "    Pod 实际         → ${d}"; done
    echo "    多半是节点缓存了同名 tag 的旧镜像（tag 被覆盖过 + IfNotPresent）。修："
    echo "      docker exec desktop-control-plane crictl rmi ${REGISTRY}/${PRODUCT}-${COMPONENT}:${VERSION}"
    echo "      kubectl rollout restart deploy/<deploy> -n ${NAMESPACE}"
    fail=1
  fi
done

# 前后端必须是同一个版本：它们是一个产品的两半，一次发布、一次回滚。
# chart 里的 alert.checkVersions 已经在渲染期拦了一道，这里是运行期的复核。
echo
if [[ $fail -eq 0 ]]; then
  echo "✓ ${PRODUCT} ${VERSION} 部署校验通过（前后端同版本，digest 与 registry 一致）"
else
  echo "✗ ${PRODUCT} ${VERSION} 部署校验未通过，见上面的处理办法" >&2
  exit 1
fi
