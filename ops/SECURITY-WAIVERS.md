# 漏洞豁免台账（ops/ 全线）

`CONVENTIONS.md` 规定：**推镜像前 Trivy 必须 0 漏洞（含 medium/low），前端另要 `npm audit` 清零。**

这份文件记的是这条规矩的**例外**——上游还没有修复版本、我们改不动的那些。

## 🔴 为什么必须有这份台账

一条被忽略但**没有记录**的漏洞，和一条不存在的漏洞在界面上长得一模一样：
下次扫描出来照样是那一条，人会重新查一遍"这个能不能修"，
查到一半发现上次已经查过了 —— 而结论没人写下来。

更糟的是反过来：上游发了修复版本，而没人知道该回来看一眼，
于是一条**本来已经能修**的漏洞被"历史上决定忽略"这个理由一直挡着。

所以每一条都必须写清三件事：

1. **忽略的是哪一条**（ID + 包 + 版本，精确到能重新验证）
2. **为什么可以忽略**（判据，不是"看起来不严重"）
3. **什么条件下回来解决**（可检查的条件，不是"以后再说"）

⚠️ 不写第 3 条的豁免等于永久豁免。

---

## 当前豁免

### W-001 · `golang.org/x/crypto` GO-2026-5932（openpgp）

| | |
|---|---|
| 漏洞 | `GO-2026-5932` — golang.org/x/crypto/openpgp |
| 影响包 | `golang.org/x/crypto` v0.55.0 |
| 严重度 | **UNKNOWN**（Go 漏洞库未定级） |
| 涉及产品 | `ops-cmdb` backend（其他 ops/ 产品同依赖时同样适用） |
| 登记 | 2026-08-18 |
| 状态 | **忽略中** |

**为什么可以忽略：漏洞所在的包根本没有被编译进二进制。**

判据（可复现）：

```
$ go mod why golang.org/x/crypto/openpgp
(main module does not need package golang.org/x/crypto/openpgp)

$ go list -deps ./... | grep -c "golang.org/x/crypto/openpgp"
0
```

实际编进去的只有 TLS 相关的几个子包：

```
golang.org/x/crypto/cryptobyte
golang.org/x/crypto/cryptobyte/asn1
golang.org/x/crypto/chacha20
golang.org/x/crypto/internal/poly1305
golang.org/x/crypto/internal/alias
```

⚠️ Trivy 报的是**模块级**（整个 `golang.org/x/crypto`），
而 Go 的漏洞是**包级**的 —— 这个差异是这条豁免成立的全部理由。
换句话说：不是"风险低所以放过"，是**那段代码不在镜像里**。

**上游现状**：没有 fixed version（Trivy 的 `FixedVersion` 为空）。
`openpgp` 包本身已被 Go 官方标为 deprecated 且不再维护，
所以大概率**不会有修复版本**，只会等依赖链上有人把它摘掉。

**什么条件下回来解决**（满足任一即可动手）：

- [ ] `golang.org/x/crypto` 发布了带 fix 的版本 → 直接 `go get -u` 升上去
- [ ] 依赖链上不再引入 `openpgp`（`go mod why` 变成"不需要"以外的输出）→ 重扫确认自然消失
- [ ] Trivy 支持按**包**而非按模块判定 → 这条会自动不再上报

**复查节奏**：每次大版本发布前重扫一次即可，不必单独盯。
重扫命令见下方「怎么复查」。

---

## 怎么复查

```bash
# 后端
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$HOME/.cache/trivy:/root/.cache/trivy" aquasec/trivy:latest image \
  --scanners vuln --severity UNKNOWN,LOW,MEDIUM,HIGH,CRITICAL \
  localhost:8070/opsplatform-dev/ops-cmdb-backend:<版本>

# 前端（同上，换镜像名）
```

⚠️ **必须带 `--severity` 且把 `UNKNOWN` 写进去。**
Trivy 默认不报 UNKNOWN —— 不写的话这一条根本不会出现，
于是"扫描通过"变成一句没有内容的话。W-001 就是 UNKNOWN 级。

## 记录一条新豁免时

按 W-001 的格式抄一份，三件事一件都不能少。
**判据必须是可复现的命令**，不是"我判断影响不大"。
