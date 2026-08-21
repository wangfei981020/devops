package diag

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// 从**日志内容**判根因。
//
// # 为什么必须有这一层
//
// 原有 33 条规则全部按 **k8s 状态**匹配（CrashLoopBackOff / OOMKilled / ImagePullBackOff…），
// 日志只被塞进 Evidence，**不参与判定**。于是结论长这样：
//
//	root_cause: "容器启动后即崩溃（CrashLoopBackOff）"
//	solutions:  "查看上面日志末尾的报错，多为配置错误/依赖不可用/启动参数问题"
//
// 🔴 **CrashLoopBackOff 是现象，不是根因。** 真根因永远在日志里，
// 而上面那句 solution 等于把活推回给人 —— 那正是 CMDB 立项要消灭的东西。
//
// 2026-08-18 在 DEV/UAT 实测，两条烧得最久的故障都卡在这一跳：
//
//	cicd/gitlab-runner-dev          重启 5431 次
//	  日志：x509: certificate has expired ... is after 2026-06-26T03:18:25Z
//	  真根因：gitlab-g32uat.slileisure.com 的证书 6 月 26 日就过期了
//
//	kubesphere-system/ks-apiserver  重启 4977 次
//	  日志：no matches for kind "Cluster" in version "cluster.kubesphere.io/v1alpha1"
//	  真根因：CRD 缺失
//
// 两条的根因都**已经在 Evidence 里**，只差从证据到结论的这一跳。
//
// # 位置：必须排在 ruleCrashLoop / ruleNonZeroExit 之前
//
// 那两条是"它崩了"的兜底规则，一旦先命中，日志规则永远轮不到。
// ruleset 里 ruleDNSFailure 已经证明了这个插槽可行（它是原有唯一读日志内容的规则），
// 本规则紧跟其后。
//
// ⚠️ 反过来，**不能排到状态规则前面**：OOMKilled / ImagePullBackOff 这些
// 有确定性的 k8s 信号，比日志里的只言片语可靠。一个被 OOM 杀掉的容器，
// 它日志末尾很可能还留着 OOM 之前的 "connection refused" ——
// 那是噪音，不是根因。
//
// # 判据：信号优先级 > 行位置
//
// 对每个信号，取它在日志里**最后一次**出现的那行（最接近崩溃时刻）；
// 但信号之间按优先级比，不按行号比。
//
// 🔴 这一条是实测逼出来的。gitlab-runner 的日志末尾是：
//
//	...x509: certificate has expired...     ← 真根因，靠上
//	PANIC: Failed to verify the runner.     ← 泛化信号，靠下
//	Quit
//
// 若"谁靠后谁赢"，会选中 PANIC 那行，得到"应用 panic 了"这种废话结论。
// 具体信号即使出现得早，也比泛化信号有价值。

// logSignal 一条日志特征。
type logSignal struct {
	// key 进 Evidence，用来回溯"是哪条规则判的"
	key string
	// re 命中即判定。⚠️ 一律用小写匹配（下面统一 ToLower），所以这里的模式必须是小写
	re *regexp.Regexp
	// build 由命中行产出结论。返回 nil = 这行虽然像，但提取不到足够信息，放弃（继续找下一条）
	build func(line string, cc *ContainerCtx) *DiagnosisResult
}

// 提取参数用的辅助模式。
var (
	reURLHost   = regexp.MustCompile(`https?://([a-zA-Z0-9._\-]+(?::\d+)?)`)
	reCertAfter = regexp.MustCompile(`is after ([0-9T:\-.Z+]+)`)
	// ⚠️ 引号可能是**转义**的：k8s 组件常把整段错误裹在 `err="..."` 里，
	//	真实日志长这样 —— no matches for kind \"Cluster\" in version \"cluster.kubesphere.io/v1alpha1\"
	//	只认裸引号会整条漏掉（单测用真实日志当场抓到）
	reCRDKind  = regexp.MustCompile(`no matches for kind \\?"([^"\\]+)\\?" in version \\?"([^"\\]+)\\?"`)
	reDialAddr = regexp.MustCompile(`dial (?:tcp|udp) ([0-9a-zA-Z._\-\[\]]+:\d+)`)
	reCertFor  = regexp.MustCompile(`certificate is valid for ([^,]+(?:, [^,]+)*), not ([a-zA-Z0-9._\-]+)`)
	// 🔴 提取用的正则必须带 (?i)：命中判定是在**小写副本**上做的，
	//	而提取是拿**原文**做的。两边大小写口径不一致 →
	//	`Caused by: java.lang.IllegalStateException` 命中了信号、却提取不到内容，
	//	build 返回 nil 被跳过 —— 整条信号**静默失效**。
	//	实测 public/nacos-v1（重启 8528 次）就是这么漏掉的：
	//	日志里明写着 "Caused by: java.lang.IllegalStateException: No DataSource set"，
	//	结果落回了通用的「容器启动后即崩溃」。
	//	⚠️ 单测当时没抓到，因为 JVM OOM 那条即使提取失败也会返回结果，把问题盖住了。
	reJVMOOM    = regexp.MustCompile(`(?i)java\.lang\.outofmemoryerror:?\s*([^\n]*)`)
	reGoPanic   = regexp.MustCompile(`(?i)panic:\s*([^\n]+)`)
	reJavaCause = regexp.MustCompile(`(?i)caused by:\s*([^\n]+)`)
	reNoSuchDir = regexp.MustCompile(`(?:open|stat|read|no such file or directory)[^\n]*?([/][\w./\-]+)[^\n]*no such file or directory`)
	// nginx: [emerg] invalid number of arguments in "proxy_pass" directive in /etc/nginx/conf.d/frontend.conf:42
	reNginxEmerg = regexp.MustCompile(`\[emerg\]\s*(?:\d+#\d+:\s*)?(.+)`)
	reFileLine   = regexp.MustCompile(`(/[\w./\-]+):(\d+)`)
	// ./app: error while loading shared libraries: libsignature.so: cannot open shared object file
	reMissingSO = regexp.MustCompile(`(?i)error while loading shared libraries:\s*([^\s:]+)`)
	// x509: cannot validate certificate for 10.170.80.6 because it doesn't contain any IP SANs
	reCertForHost = regexp.MustCompile(`(?i)cannot validate certificate for\s+([^\s]+)`)
	// FATAL/fatal error 那一行的正文
	reFatalLine  = regexp.MustCompile(`(?i)\bfatal(?:\s+error)?\b[:\s]+(.+)`)
	reMissingMod = regexp.MustCompile(`(?i)(?:cannot find module|modulenotfounderror: no module named|classnotfoundexception:|noclassdeffounderror:)\s*['"]?([\w./@\-]+)['"]?`)
)

// 信号表：**从具体到泛化**排列，第一个命中即返回。
func logSignals() []logSignal {
	return []logSignal{
		// ── 证书三态：过期 / CA 不受信 / 域名不匹配 ──
		// 三种的修法完全不同，绝不能合成一条"证书有问题"
		{
			key: "tls-cert-expired",
			re:  regexp.MustCompile(`certificate has expired or is not yet valid`),
			build: func(line string, cc *ContainerCtx) *DiagnosisResult {
				host := firstHost(line)
				cause := "对端 TLS 证书已过期"
				if host != "" {
					cause += "：" + host
				}
				if m := reCertAfter.FindStringSubmatch(line); m != nil {
					cause += fmt.Sprintf("（证书有效期止于 %s）", m[1])
				}
				sol := []Solution{}
				if host != "" {
					// 🔴 CMDB 自己就有证书台账 —— 直接把人送到那个域名上，
					//	而不是让他自己去想"该去哪查这张证书"
					sol = append(sol, Solution{
						Text: fmt.Sprintf("到「证书」页查 %s 这张证书并续期；若是自签/内部签发，重新签发后更新对应 Secret", host),
						Link: "/resources/certs?q=" + url.QueryEscape(hostOnly(host)),
					})
				} else {
					sol = append(sol, Solution{Text: "到「证书」页找到该域名的证书并续期"})
				}
				sol = append(sol,
					Solution{Text: "证书换新后，本容器需要重启才会重新握手（Deployment 滚动重启即可）"},
					Solution{Text: "⚠️ 别用「跳过证书校验」绕过 —— 那会把这次的过期变成永久的中间人风险"},
				)
				return &DiagnosisResult{
					Matched: true, Confidence: "high",
					RootCause: cause,
					Solutions: sol,
				}
			},
		},
		{
			key: "tls-unknown-authority",
			re:  regexp.MustCompile(`certificate signed by unknown authority`),
			build: func(line string, cc *ContainerCtx) *DiagnosisResult {
				host := firstHost(line)
				cause := "对端证书的签发 CA 不在容器信任链里"
				if host != "" {
					cause += "：" + host
				}
				return &DiagnosisResult{
					Matched: true, Confidence: "high",
					RootCause: cause,
					Solutions: []Solution{
						{Text: "把签发它的 CA 根证书挂进容器信任库（挂 ConfigMap 到 /etc/ssl/certs 或镜像内 update-ca-certificates）"},
						{Text: "确认对端发的是**完整证书链**（少中间证书时，浏览器能开但程序会报这个错）"},
					},
				}
			},
		},
		{
			key: "tls-hostname-mismatch",
			// 🔴 措辞有三种，实测撞到过两种：
			//	Go 标准库      certificate is valid for a.com, not b.com
			//	直接连 IP 时   cannot validate certificate for 10.170.80.6 because it doesn't contain any IP SANs
			//	通用兜底       x509: ...（前面几条更具体的证书信号没命中时才轮到这里）
			//	只写第一种的话，g66-dev/g66-baccarat-resource-backend（重启 3311 次、
			//	根因是连 Redis 时证书没有 IP SAN）整条漏掉 —— 实测就是这么漏的。
			re: regexp.MustCompile(`certificate is valid for .+, not |` +
				`cannot validate certificate for|doesn't contain any ip sans|` +
				`certificate relies on legacy common name field`),
			build: func(line string, cc *ContainerCtx) *DiagnosisResult {
				cause := "证书域名不匹配（SAN 里没有正在访问的这个域名）"
				switch {
				case reCertFor.MatchString(line):
					m := reCertFor.FindStringSubmatch(line)
					cause = fmt.Sprintf("证书域名不匹配：证书签给了 %s，而正在访问的是 %s", m[1], m[2])
				case strings.Contains(strings.ToLower(line), "ip sans"):
					host := ""
					if m := reCertForHost.FindStringSubmatch(line); m != nil {
						host = m[1]
					}
					cause = "证书里没有 IP SAN，而这里是**直接用 IP 连**的"
					if host != "" {
						cause += "：" + host
					}
				}
				return &DiagnosisResult{
					Matched: true, Confidence: "high",
					RootCause: cause,
					Solutions: []Solution{
						{Text: "改用证书 SAN 里已有的**域名**访问（最常见的正解：把配置里的 IP 换成 Service 域名）"},
						{Text: "或重新签发一张把该 IP 写进 IP SAN 的证书"},
						{Text: "⚠️ 别改成跳过证书校验 —— 那是把一次配置问题换成长期的中间人风险"},
					},
				}
			},
		},

		// ── CRD 缺失 ──
		{
			key: "crd-missing",
			re:  regexp.MustCompile(`no matches for kind \\?"[^"\\]+\\?" in version `),
			build: func(line string, cc *ContainerCtx) *DiagnosisResult {
				kind, ver := "", ""
				if m := reCRDKind.FindStringSubmatch(line); m != nil {
					kind, ver = m[1], m[2]
				}
				cause := "集群里缺这个 CRD，组件启动时找不到它要的资源类型"
				if kind != "" {
					cause = fmt.Sprintf("集群里没有 %s（%s）这个资源类型 —— CRD 未安装或版本不匹配", kind, ver)
				}
				return &DiagnosisResult{
					Matched: true, Confidence: "high",
					RootCause: cause,
					Solutions: []Solution{
						{Text: fmt.Sprintf("确认该 CRD 是否存在：kubectl get crd | grep -i %s", crdGrepHint(kind))},
						{Text: "多为组件升级时只换了镜像、CRD 没跟着装（Helm 默认不升级 CRD）——补装对应版本的 CRD"},
						{Text: "也可能是有人删了 CRD，那会连带删掉它的全部自定义资源，恢复前先确认数据"},
					},
				}
			},
		},

		// ── 认证/授权失败 ──
		{
			key: "auth-failed",
			re: regexp.MustCompile(`access denied for user|authentication failed|password authentication failed|` +
				`auth failed|invalid credentials|permission denied \(publickey|401 unauthorized|unauthorized: authentication required`),
			build: func(line string, cc *ContainerCtx) *DiagnosisResult {
				// 🔴 `Access denied ... to database 'X'` 有**两种**成因：
				//
				//	① 用户确实没有这个库的权限     → 去核对 Secret（下面那条）
				//	② **库名本身写错了**（含不可见字符）→ 核对 Secret 永远查不出来
				//
				//	实测：`TIDB_DATABASE` 的值末尾多了一个换行符，报错是
				//	    Access denied for user 'x'@'%' to database 'g33_niuniu_dev
				//	    '
				//	右引号跑到了下一行 —— 而工具当时判成"凭据不对"，
				//	按那个建议查会一直查不出来（账号密码本来就是对的，OPSCMDB-064）。
				//
				// ⚠️ 线索强度很高且好识别：**被引号包住的标识符里出现换行/制表符**，
				//	基本可以断定是配置值混进了不可见字符。
				ident, ch := quotedIdentWithInvisible(line)
				if ident == "" {
					ident, ch = unterminatedQuotedIdent(line)
				}
				if ident != "" {
					return &DiagnosisResult{
						Matched: true, Confidence: "high",
						RootCause: fmt.Sprintf("配置值里混进了不可见字符（%s）——报错里的标识符 %q 末尾带着它，"+
							"所以对方按一个不存在的名字去找", ch, ident),
						Solutions: []Solution{
							{Text: "账号密码多半是对的：真正要改的是那个环境变量/配置项的**值**，把末尾的换行或空格去掉"},
							{Text: "查是哪一项：在同一份日志里搜这个标识符，配置解析行（parseConfLine / var=... realVal=...）会指出变量名"},
							{Text: "⚠️ 用 kubectl get cm/secret -o yaml 看时不容易发现——YAML 的块标量（|）会在末尾自动补换行，改用 `... | od -c` 确认"},
						},
					}
				}
				return &DiagnosisResult{
					Matched: true, Confidence: "high",
					RootCause: "凭据不对，向依赖方认证失败",
					Solutions: []Solution{
						{Text: "核对该容器引用的 Secret 里的账号/密码/令牌是否与依赖方当前的一致"},
						{Text: "⚠️ 常见成因是依赖方改了密码但没同步这里；改 Secret 后容器必须重启才会重新读取"},
					},
				}
			},
		},

		// ── JVM 堆内 OOM（与容器被 OOMKilled 是两回事）──
		{
			key: "jvm-oom",
			re:  regexp.MustCompile(`java\.lang\.outofmemoryerror`),
			build: func(line string, cc *ContainerCtx) *DiagnosisResult {
				detail := ""
				if m := reJVMOOM.FindStringSubmatch(line); m != nil && strings.TrimSpace(m[1]) != "" {
					detail = "：" + strings.TrimSpace(m[1])
				}
				return &DiagnosisResult{
					Matched: true, Confidence: "high",
					// ⚠️ 这一条和 ruleOOM（容器被内核 OOMKilled）必须分开说。
					//	调容器 limit 对堆内 OOM 是**无效**的 —— JVM 自己的 -Xmx 才是上限
					RootCause: "JVM 堆内存耗尽（应用层 OOM，不是容器被内核杀掉）" + detail,
					Solutions: []Solution{
						{Text: "先看是不是内存泄漏：连续几次重启的间隔是否越来越短"},
						{Text: "调 -Xmx / -XX:MaxRAMPercentage；⚠️ 只调容器 memory limit 对堆内 OOM 无效"},
						{Text: "确认容器 limit ≥ 堆上限 + 堆外开销，否则调大 -Xmx 会变成被内核 OOMKilled"},
					},
				}
			},
		},

		// ── 磁盘满 ──
		{
			key: "disk-full",
			re:  regexp.MustCompile(`no space left on device`),
			build: func(line string, cc *ContainerCtx) *DiagnosisResult {
				return &DiagnosisResult{
					Matched: true, Confidence: "high",
					RootCause: "写入失败：磁盘已满",
					Solutions: []Solution{
						{Text: "分清是**节点盘**还是**挂载的 PVC** 满了 —— 前者去「节点」页看磁盘水位，后者去「存储」页看该 PVC 使用率"},
						{Text: "节点盘满多为镜像/日志堆积；PVC 满则要扩容或清理数据"},
					},
				}
			},
		},

		// ── 端口占用 ──
		{
			key: "port-in-use",
			re:  regexp.MustCompile(`address already in use`),
			build: func(line string, cc *ContainerCtx) *DiagnosisResult {
				return &DiagnosisResult{
					Matched: true, Confidence: "high",
					RootCause: "监听端口已被占用，进程起不来",
					Solutions: []Solution{
						{Text: "同一 Pod 内两个容器抢同一端口？检查各容器的监听配置"},
						{Text: "用了 hostPort/hostNetwork 时会和**节点上**别的 Pod 抢端口 —— 改用 Service 暴露，或换端口"},
					},
				}
			},
		},

		// ── 文件系统权限 ──
		{
			key: "fs-permission",
			re:  regexp.MustCompile(`permission denied|operation not permitted`),
			build: func(line string, cc *ContainerCtx) *DiagnosisResult {
				return &DiagnosisResult{
					Matched: true, Confidence: "medium",
					RootCause: "文件或设备权限不足",
					Solutions: []Solution{
						{Text: "多为容器以非 root 运行，而挂载卷属主是 root：给 Pod 设 securityContext.fsGroup"},
						{Text: "若要求的是特权操作（挂载/网络/内核参数），确认是否真的需要，能避则避"},
					},
				}
			},
		},

		// ── 依赖不可用导致启动失败 ──
		//
		// 🔴 必须排在 conn-refused **之前**。
		//	实测 g32-dev/bi-gateway-backend：日志里既有 `java.net.ConnectException: Connection refused`
		//	又有 `Could not open JDBC Connection for transaction`。
		//	conn-refused 先命中的话，结论只有"某个端口没人听"——那是**症状**；
		//	而"启动阶段连不上数据库、应用没起来"才是人要的那句话。
		//	两条都能匹配时，永远选信息量更大的那条。
		// 🔴 实测两例，都是"应用起不来"而不是"应用崩了"：
		//	g32-dev/bi-gateway-backend  MySQL 连不上 → Spring 上下文初始化失败（重启 12884 次）
		//	public/nacos-v1             No DataSource set → 启动中止（重启 8528 次）
		//	这类的处置是**去修依赖**，不是查应用本身
		{
			key: "startup-dependency-failed",
			re: regexp.MustCompile(`no datasource set|could not open jdbc connection|` +
				`unsatisfieddependencyexception|error creating bean with name|` +
				`cannot create poolableconnectionfactory|communications link failure|` +
				`application run failed`),
			build: func(line string, cc *ContainerCtx) *DiagnosisResult {
				what := "依赖不可用"
				low := strings.ToLower(line)
				switch {
				case strings.Contains(low, "no datasource set"):
					what = "没有配置数据源（No DataSource set）"
				case strings.Contains(low, "could not open jdbc connection"),
					strings.Contains(low, "communications link failure"),
					strings.Contains(low, "cannot create poolableconnectionfactory"):
					what = "数据库连不上"
				}
				return &DiagnosisResult{
					Matched: true, Confidence: "high",
					RootCause: "启动阶段依赖不可用，应用没能起来：" + what,
					Solutions: []Solution{
						{Text: "🔴 先修**依赖**，不是查这个应用 —— 它只是起不来，本身没问题"},
						{Text: "数据库类：确认对应的 Service/实例还在、账号密码没过期、网络策略没变"},
						{Text: "配置类（No DataSource set 等）：确认配置项真的注入进来了 —— 多为 ConfigMap/Secret 改名或漏挂"},
						{Text: "⚠️ 这类反复重启会持续到依赖恢复为止，重启次数再高也说明不了应用有问题"},
					},
				}
			},
		},

		// ── 配置文件语法错（进程根本没起来）──
		//
		// ⚠️ 这一组和上面的依赖类一样，都要排在 conn-refused/conn-timeout **之前**。
		//	排序原则：**确定性的配置/依赖根因 > 网络症状 > 泛化兜底**。
		//	nginx 报 [emerg] 精确到行号，而 conn-refused 只说"某端口没人听"——
		//	后者先命中就把前者盖掉了。
		// 🔴 实测：g50-test 那一族 5 个前端，重启数都卡在 11450 上下，
		//	日志里明写着 nginx: [emerg] invalid number of arguments in "proxy_pass"
		//	directive in /etc/nginx/conf.d/frontend.conf:42
		//	—— 根因精确到行号，而改这一版之前只给出「容器启动后即崩溃」
		{
			key: "nginx-config-error",
			re:  regexp.MustCompile(`nginx:\s*\[emerg\]|\[emerg\]`),
			build: func(line string, cc *ContainerCtx) *DiagnosisResult {
				detail := strings.TrimSpace(line)
				if m := reNginxEmerg.FindStringSubmatch(line); m != nil {
					detail = strings.TrimSpace(m[1])
				}
				where := ""
				if m := reFileLine.FindStringSubmatch(line); m != nil {
					where = fmt.Sprintf("（%s 第 %s 行）", m[1], m[2])
				}
				return &DiagnosisResult{
					Matched: true, Confidence: "high",
					RootCause: "nginx 配置有误，进程起不来" + where + "：" + trimTo(detail, 200),
					Solutions: []Solution{
						{Text: "按上面的文件与行号改配置；改完可先 nginx -t 验一遍再发"},
						{Text: "⚠️ 配置多来自 ConfigMap 或镜像内模板 —— 改对了地方才生效，" +
							"改完 ConfigMap 要重启 Pod 才会重新挂载"},
						{Text: "常见成因：模板变量没被替换（envsubst 少了变量），渲染出空值导致指令参数个数不对"},
					},
				}
			},
		},
		{
			key: "config-parse-error",
			re: regexp.MustCompile(`syntax error|failed to parse|invalid configuration|` +
				`cannot unmarshal|error unmarshaling|yaml: line`),
			build: func(line string, cc *ContainerCtx) *DiagnosisResult {
				where := ""
				if m := reFileLine.FindStringSubmatch(line); m != nil {
					where = fmt.Sprintf("（%s 第 %s 行）", m[1], m[2])
				}
				return &DiagnosisResult{
					Matched: true, Confidence: "medium",
					RootCause: "配置解析失败，进程起不来" + where + "：" + trimTo(strings.TrimSpace(line), 200),
					Solutions: []Solution{
						{Text: "按上面的报错定位配置项；多为 ConfigMap 里的格式写坏了"},
						{Text: "⚠️ 若配置由模板渲染，先确认变量都被替换掉了 —— 空值最容易渲染出非法语法"},
					},
				}
			},
		},
		{
			key: "missing-module",
			re: regexp.MustCompile(`cannot find module|modulenotfounderror: no module named|` +
				`classnotfoundexception|noclassdeffounderror`),
			build: func(line string, cc *ContainerCtx) *DiagnosisResult {
				name := ""
				if m := reMissingMod.FindStringSubmatch(line); m != nil {
					name = m[1]
				}
				root := "启动时找不到依赖（模块/类）"
				if name != "" {
					root += "：" + name
				}
				return &DiagnosisResult{
					Matched: true, Confidence: "high",
					RootCause: root,
					Solutions: []Solution{
						{Text: "镜像里没打进这个依赖 —— 多为构建阶段装依赖失败但没让构建失败"},
						{Text: "去「构建流水线」页看这个服务最近一次构建的日志，确认依赖安装那一步"},
						{Text: "⚠️ 这类问题重启无效：镜像里没有的东西，重启多少次也不会出现"},
					},
				}
			},
		},

		{
			key: "missing-shared-library",
			// 🔴 实测 g50-test/g50-link-baccarat-game-server-backend（重启 9016 次）：
			//	./app: error while loading shared libraries: libsignature.so: cannot open shared object file
			//	原来的 missing-module 只覆盖 Node/Python/Java，C/Go 动态链接这一类整个漏掉。
			re: regexp.MustCompile(`error while loading shared libraries|cannot open shared object file`),
			build: func(line string, cc *ContainerCtx) *DiagnosisResult {
				name := ""
				if m := reMissingSO.FindStringSubmatch(line); m != nil {
					name = m[1]
				}
				root := "镜像里缺动态链接库，二进制根本起不来"
				if name != "" {
					root += "：" + name
				}
				return &DiagnosisResult{
					Matched: true, Confidence: "high",
					RootCause: root,
					Solutions: []Solution{
						{Text: "构建阶段漏打了这个 .so —— 多为多阶段构建时只 COPY 了二进制、没带它依赖的库"},
						{Text: "去「构建流水线」页看这个服务最近一次构建；确认运行阶段的基础镜像里有这个库"},
						{Text: "⚠️ 重启无效：镜像里没有的文件，重启多少次也不会出现"},
					},
				}
			},
		},

		// ── 连不上依赖 ──
		{
			key: "conn-refused",
			re:  regexp.MustCompile(`connection refused`),
			build: func(line string, cc *ContainerCtx) *DiagnosisResult {
				addr := ""
				if m := reDialAddr.FindStringSubmatch(line); m != nil {
					addr = m[1]
				} else if h := firstHost(line); h != "" {
					addr = h
				}
				cause := "连接被拒绝：目标端口上没有进程在监听"
				sol := []Solution{}
				if addr != "" {
					cause = fmt.Sprintf("连接被拒绝：%s 上没有进程在监听", addr)
					sol = append(sol, Solution{
						Text: fmt.Sprintf("到「服务与入口」页查 %s 对应的 Service 有没有就绪的后端 Pod", hostOnly(addr)),
						Link: "/k8s/services?q=" + url.QueryEscape(svcNameHint(addr)),
					})
				}
				sol = append(sol,
					Solution{Text: "⚠️ 「连接被拒绝」说明网络是通的、只是没人监听 —— 和防火墙拦截（表现为超时）是两回事，别往网络策略上查"},
					Solution{Text: "确认依赖方是否也在重启中：依赖挂了会让这个服务跟着 CrashLoop"},
				)
				return &DiagnosisResult{
					Matched: true, Confidence: "high",
					RootCause: cause,
					Solutions: sol,
				}
			},
		},
		{
			key: "conn-timeout",
			re:  regexp.MustCompile(`i/o timeout|dial tcp[^\n]*timeout|connect: connection timed out`),
			build: func(line string, cc *ContainerCtx) *DiagnosisResult {
				addr := ""
				if m := reDialAddr.FindStringSubmatch(line); m != nil {
					addr = m[1]
				}
				cause := "连接超时：包发出去了但没有回应"
				if addr != "" {
					cause = fmt.Sprintf("连接 %s 超时：包发出去了但没有回应", addr)
				}
				return &DiagnosisResult{
					Matched: true, Confidence: "high",
					RootCause: cause,
					Solutions: []Solution{
						{Text: "⚠️ 超时而非「拒绝」，优先查**拦截**：NetworkPolicy、云防火墙、安全组"},
						{Text: "目标在集群外时，确认出网路径（NAT/代理）是否放行"},
					},
				}
			},
		},

		// ── 泛化兜底：至少把崩溃的第一行抽出来当根因 ──
		// ⚠️ 这两条排在最后。它们给的是"应用自己报的错"，比"它崩了"强，
		//	但比上面任何一条具体信号弱
		{
			key: "fatal-line",
			// 应用自己打了 FATAL —— 那一行就是它对"为什么活不下去"的自述。
			// 🔴 实测 UAT trivy-system/scan-vulnerabilityreport：
			//	FATAL Fatal error image scan error: ... UNAUTHORIZED: project bitnami not found
			//	（真根因是 Harbor 上没有 bitnami 这个项目）
			//	前面的具体信号都没覆盖这种组合，而"容器异常退出（退出码 1）"等于什么都没说。
			// ⚠️ 排在泛化的 java-exception / panic 之前、所有具体信号之后。
			re: regexp.MustCompile(`\bfatal\b`),
			build: func(line string, cc *ContainerCtx) *DiagnosisResult {
				m := reFatalLine.FindStringSubmatch(line)
				if m == nil || strings.TrimSpace(m[1]) == "" {
					return nil
				}
				return &DiagnosisResult{
					Matched: true, Confidence: "medium",
					RootCause: "应用报了致命错误后退出：" + trimTo(strings.TrimSpace(m[1]), 240),
					Solutions: []Solution{
						{Text: "上面这行是应用自己给出的原因，按它排查"},
						{Text: "⚠️ 若这行里提到某个镜像/仓库/地址不存在或无权限，先去核对那个对象是否真的存在"},
					},
				}
			},
		},
		{
			key: "java-exception",
			re:  regexp.MustCompile(`caused by:\s*\S`),
			build: func(line string, cc *ContainerCtx) *DiagnosisResult {
				m := reJavaCause.FindStringSubmatch(line)
				if m == nil {
					return nil
				}
				return &DiagnosisResult{
					Matched: true, Confidence: "medium",
					RootCause: "应用启动异常，根异常：" + trimTo(strings.TrimSpace(m[1]), 200),
					Solutions: []Solution{
						{Text: "上面这行是异常链最底层的原因，按它排查比看栈顶有用"},
						{Text: "确认是否为近期发版引入（见变更关联），是则先回滚止血"},
					},
				}
			},
		},
		{
			key: "process-panic",
			re:  regexp.MustCompile(`panic:\s*\S`),
			build: func(line string, cc *ContainerCtx) *DiagnosisResult {
				m := reGoPanic.FindStringSubmatch(line)
				if m == nil {
					return nil
				}
				return &DiagnosisResult{
					Matched: true, Confidence: "medium",
					RootCause: "进程 panic 退出：" + trimTo(strings.TrimSpace(m[1]), 200),
					Solutions: []Solution{
						{Text: "panic 后面那句就是直接原因，多为配置缺失、依赖不可用或空指针"},
						{Text: "确认是否为近期发版引入（见变更关联），是则先回滚止血"},
					},
				}
			},
		},
	}
}

// ruleLogSignals 按日志内容判根因。
//
// ⚠️ 遍历顺序必须走 c.Containers，**不能走 c.LogTails**（map 迭代顺序随机）——
// 多容器 Pod 会因此在两次调用间给出不同的结论，而"同一个 Pod 每次诊断结果不一样"
// 是最难被相信的一种缺陷。
func ruleLogSignals(c *DiagnosisContext) *DiagnosisResult {
	for _, sig := range logSignals() {
		for i := range c.Containers {
			cc := &c.Containers[i]
			tail := c.LogTails[cc.Name]
			if tail == "" {
				continue
			}
			line := lastMatchingLine(tail, sig.re)
			if line == "" {
				continue
			}
			res := sig.build(line, cc)
			if res == nil {
				continue // 像但提取不到关键信息，让给后面的信号
			}
			res.Evidence = append([]string{
				fmt.Sprintf("容器 %s 日志命中「%s」：", cc.Name, sig.key),
				trimTo(strings.TrimSpace(line), 500),
			}, res.Evidence...)
			res.Evidence = append(res.Evidence, "日志末尾:\n"+lastLines(tail, 8))
			return res
		}
	}
	return nil
}

// lastMatchingLine 返回**最后一条**命中的日志行（最接近崩溃时刻的那次）。
// 匹配一律在小写副本上做，所以信号的正则必须写成小写。
func lastMatchingLine(tail string, re *regexp.Regexp) string {
	lines := strings.Split(tail, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if re.MatchString(strings.ToLower(lines[i])) {
			return lines[i]
		}
	}
	return ""
}

// firstHost 从一行里抓第一个 URL 的 host（含端口）。
func firstHost(line string) string {
	if m := reURLHost.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	return ""
}

// hostOnly 去掉端口，只留主机名 —— 拿去证书/服务页搜索时端口是噪音。
func hostOnly(hostPort string) string {
	if i := strings.LastIndex(hostPort, ":"); i > 0 && !strings.Contains(hostPort[i:], "]") {
		return hostPort[:i]
	}
	return hostPort
}

// svcNameHint 从 `svc.ns.svc.cluster.local:8080` 这类地址里取出 Service 名，
// 拿它去「服务与入口」页搜。⚠️ 纯 IP 的地址取不出名字，原样返回让人自己看。
func svcNameHint(addr string) string {
	h := hostOnly(addr)
	if i := strings.Index(h, "."); i > 0 {
		return h[:i]
	}
	return h
}

// crdGrepHint 给 kubectl get crd 用的过滤词。kind 是驼峰，CRD 名是小写复数，
// 取小写的 kind 做子串足够定位。
func crdGrepHint(kind string) string {
	if kind == "" {
		return "<资源类型>"
	}
	return strings.ToLower(kind)
}

// trimTo 截断超长内容，避免一行日志把结论淹掉。
func trimTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…（已截断）"
}

// reQuotedIdent 报错消息里被单引号/反引号/双引号包住的标识符。
//
// ⚠️ 用 [^'] 而不是 .*? —— 后者遇到一行里多个引号会跨对匹配，
//
//	把两个标识符之间的内容当成一个标识符。
var reQuotedIdent = regexp.MustCompile("'([^']*)'|`([^`]*)`|\"([^\"]*)\"")

// quotedIdentWithInvisible 找出报错里**含不可见字符**的引号标识符。
//
// 返回 (标识符原文, 不可见字符的人话名称)。没找到时返回空串。
//
// 🔴 只认换行/回车/制表 —— 不认普通空格。
//
//	库名、用户名里带空格虽然也可疑，但**合法**（有人真的这么建表），
//	报出来会是误判；而换行和制表符几乎不可能是有意的，
//	它们只会来自复制粘贴或 YAML 块标量的自动补行。
//	⚠️ 判据宁可窄一点：这条规则一旦误报，人会被引去改一个本来正确的配置。
func quotedIdentWithInvisible(line string) (ident, charName string) {
	for _, m := range reQuotedIdent.FindAllStringSubmatch(line, -1) {
		for _, g := range m[1:] {
			if g == "" {
				continue
			}
			switch {
			case strings.ContainsRune(g, '\n'):
				return g, "换行符 \\n"
			case strings.ContainsRune(g, '\r'):
				return g, "回车符 \\r"
			case strings.ContainsRune(g, '\t'):
				return g, "制表符 \\t"
			}
		}
	}
	return "", ""
}

// reUnterminatedIdent 一行末尾**没有闭合**的引号标识符。
//
//	'g33_niuniu_dev<EOL>
//	 ^开引号        ^没有右引号就到行尾了
//
// ⚠️ 标识符里不许有空格 —— 否则英文里的所有格（`the user's password`）
// 会把整个后半句当成"未闭合标识符"，一报一大片。
var reUnterminatedIdent = regexp.MustCompile("['\"`]([^\\s'\"`]+)\\s*$")

// unterminatedQuotedIdent 判"右引号跑到了下一行"。
//
// 🔴 这条是给**换行符**用的，而换行符按行看是看不见的：
//
//	诊断是逐行做的（lastMatchingLine 已经按 \n 切过），
//	等规则拿到这一行时那个换行符已经被切掉了 ——
//	所以不能像制表符那样"在标识符里找不可见字符"，只能靠**引号没闭合**反推。
//
//	    Access denied for user 'x'@'%' to database 'g33_niuniu_dev
//	    '
//	                                                             ↑右引号在下一行
//
// ⚠️ 先要求这一行的引号总数为**奇数**：偶数说明每个引号都配上了对，
//
//	此时行尾那个只是正常的收尾引号（`(using password: YES)` 那种）。
func unterminatedQuotedIdent(line string) (ident, charName string) {
	l := strings.TrimRight(line, "\r")
	n := strings.Count(l, "'") + strings.Count(l, `"`) + strings.Count(l, "`")
	if n%2 == 0 {
		return "", ""
	}
	m := reUnterminatedIdent.FindStringSubmatch(l)
	if m == nil {
		return "", ""
	}
	return m[1], `换行符 \n`
}
