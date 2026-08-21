# ops/

OpsPlane 产品线的新架构目录。React 19 + Vite + Tailwind v4 + shadcn 风格组件。

> ## 先读 [CONVENTIONS.md](./CONVENTIONS.md)
>
> **新系统进 `ops/` 之前必读**。里面是这套系统所有的工程约定
> （前端四态、后端分页、数据语义、部署、构建防线），每条都写了「为什么」。
> 照着它做，不用重新拍一遍规范。
>
> 本文件只管「怎么跑起来」，规范在那边。

工程约定：[CONVENTIONS.md](CONVENTIONS.md)　授权规范：[LICENSING.md](LICENSING.md)
设计规范：[docs/design/design-system.md](../docs/design/design-system.md)
功能与目录：[docs/design/structure.md](../docs/design/structure.md)
重写计划：[docs/plans/opsplane-rewrite-plan.md](../docs/plans/opsplane-rewrite-plan.md)

> **旧代码一个都没删。**
> `opsplatform-cmdb-*`（生产在跑）和 `enterprise/*`（现有 Vue 前端 + 后端）
> 原样保留。这里是增量新建，验收通过前旧版一直是唯一在跑的版本。

## 结构

```
ops/
├── packages/            跨产品共享（前端基建）
│   ├── design/          设计 tokens、明暗主题、首屏防闪、白标推导与对比度校验
│   ├── i18n/            语言包、格式化（时区/货币/字节/相对时间）
│   └── ui/              基础组件、四态强制分流、DataTable、NoValue
│
├── ops-kit/             跨产品共享的 Go 包
│   └── licensekit/      授权内核（Ed25519 签名、安装指纹、分档门控）
│
├── ops-cmdb/            一个产品一个目录，前后端在一起
│   ├── frontend/        React 应用
│   ├── backend/         Go 服务（已从 enterprise/ops-data-plane 搬入）
│   └── deploy/helm/     单 chart 含前后端，一次发布一次回滚
│
└── tooling/scripts/     构建与检查脚本（CI 与本地共用）
```

共享包为什么不放进某个产品目录：五个产品要共用同一套设计系统与 i18n，
放在任一产品下都会让其他产品反向依赖它，依赖图立刻成环。

## 常用命令

```bash
pnpm install                  # 根目录执行
pnpm dev:cmdb                 # 起 CMDB 前端 → http://localhost:5273
pnpm build                    # 全量构建（共享包 → 应用）
pnpm vitest run               # 单测
pnpm check:i18n               # 语言包一致性（含复数形式）
pnpm check:tokens             # 硬编码颜色检查

# 构建镜像 —— 版本号收口在这里，不要直接 docker build
# 版本号规范见 VERSIONING.md（语义化版本，预发布只允许 alpha/beta/rc）
tooling/scripts/build-image.sh ops-cmdb v0.1.0
tooling/scripts/build-image.sh ops-cmdb v0.1.0 --push          # 推本地 Harbor
tooling/scripts/build-image.sh ops-cmdb v1.0.0 --push-remote   # 多架构推 marks26
```

`pnpm build` 会先跑五个检查脚本再编译，任一失败都不出产物。

## 构建期防线

它们的共同点是：**出问题时不会报错，只会静默地做错**。详见 [CONVENTIONS.md §5.1](./CONVENTIONS.md)。

| 防线 | 拦的问题 |
|---|---|
| `check-hardcoded-colors.mjs` | 组件里写死 `bg-blue-500` / `#6E6CEF`，白标换色后那一处不跟着变 |
| `check-i18n.mjs` | 漏翻导致英文界面零星漏中文；复数形式按各语言 CLDR 规则校验（英文要 one/other，中文只要 other） |
| `check-error-keys.mjs` | 后端错误码在前端语言包里没有文案，用户看到的是生的 key |
| `check-perm-codes.mjs` | 菜单的权限码和后端 `perm.go` 不同名，该菜单对所有非管理员静默消失 |
| `check-license-keys.mjs` | 授权状态没有对应文案（`lapsed` 要欠费满 30 天才出现，漏了没人会发现） |
| `build-image.sh` | 漏传 `--build-arg VERSION`，线上版本号落成 `dev`（不在 `pnpm build` 里，是构建镜像的唯一入口） |

## 调试三态

错误态和空态在正常环境里很难复现，靠 URL 参数强制：

```
http://localhost:5273/?state=error    # 504 + request id + 重试
http://localhost:5273/?state=empty    # 空态：原因 + 下一步动作
```

## 本地验证入口

固定端口，不要用临时 port-forward：

- 前端 dev server：`http://localhost:5273`
- k8s 部署后：`http://localhost:30833`（经 k8s-proxy，见 `k8s-proxy/README.md`）
- 后端：port-forward 到 `18080`，由 vite proxy 转发 `/api`

```bash
helm upgrade --install cmdb ops-cmdb/deploy/helm -n ops-cmdb --create-namespace \
  -f ops-cmdb/deploy/helm/values-local.yaml --set frontend.image.tag=v0.1.0
```
