package diag

import (
	"fmt"
	"regexp"
	"strings"
)

// 拉镜像失败的**具体**原因，从事件内容判。
//
// # 为什么要拆开
//
// 原来这条规则只说「镜像拉取失败（ImagePullBackOff）」，附三条通用建议：
// 核对镜像名 / 检查 imagePullSecret / 确认网络。三条各指一个方向，
// 而人每次都得自己重新走一遍排除法 —— 明明事件里已经写清楚是哪一种了。
//
// 2026-08-18 DEV 实测，两个 Pod 的事件长这样：
//
//	g50-uat  FailedToRetrieveImagePullSecret ×19647
//	         "Unable to retrieve some image pull secrets (harbor-id)"
//	         → 根因是命名空间里没有 harbor-id 这个拉取密钥，和镜像存不存在无关
//
//	g32-dev  BackOff ×255314  "Back-off pulling image \"…\""
//	         → 只剩 BackOff。带原因的那条事件早过 TTL 了，真的判不出来
//
// 前者能确定性判到密钥名；后者**必须诚实说「判不出来」**，
// 而不是继续给那三条通用建议假装分析过了。
//
// # 🔴 不接镜像仓库是有意的
//
// 一度打算把 Harbor 接进来做"镜像在不在"的确定性判定。放弃了，因为：
//
//   - 实测那批 ImagePullBackOff，32 个 Pod 的根因是**缺拉取密钥**，
//     查仓库对它们一点用没有
//   - 真正需要查仓库的只有"事件已过期"那一类，而那类的处置本来就是重新构建重推，
//     查不查仓库不改变方案
//
// 所以这里的做法是：**把完整镜像地址原样摆出来 + 给出手动验证的命令**，
// 让人自己去确认。少一个自动交叉验证，换掉一份仓库凭据。
//
// ⚠️ 同理，Secret 名录（allow_secret_inventory）在 UAT/生产一律不开：
// K8s 的 `list secrets` 会连 data 一并返回，那是集群 RBAC 层给出去的能力，
// CMDB 自己约束不了。现状靠 KSM 的 kube_secret_info（只有元数据）
// 已覆盖 DEV 91 个命名空间里的 75 个，且零额外权限。

var (
	// "Unable to retrieve some image pull secrets (harbor-id); attempting…"
	rePullSecretName = regexp.MustCompile(`image pull secrets? \(([^)]+)\)`)
)

// imagePullVerdict 一种拉取失败的具体形态。
type imagePullVerdict struct {
	key string
	// hit 在事件的 reason+message（均已转小写）上判断
	hit func(reason, msg string) bool
	// build 产出根因与方案；image 是完整镜像地址，ns 是命名空间
	build func(w *EventCtx, image, ns string) (root string, sol []Solution)
}

// 从具体到泛化。第一个命中即用。
func imagePullVerdicts() []imagePullVerdict {
	return []imagePullVerdict{
		{
			key: "pull-secret-missing",
			hit: func(reason, msg string) bool {
				return strings.Contains(reason, "failedtoretrieveimagepullsecret") ||
					strings.Contains(msg, "unable to retrieve some image pull secrets")
			},
			build: func(w *EventCtx, image, ns string) (string, []Solution) {
				name := "（事件里没给出名字）"
				if m := rePullSecretName.FindStringSubmatch(w.Message); m != nil {
					name = m[1]
				}
				return fmt.Sprintf("命名空间 %s 下的镜像拉取密钥 %s 不存在，镜像根本没开始拉", ns, name),
					[]Solution{
						{Text: fmt.Sprintf("确认它是否真的不在：kubectl -n %s get secret %s", ns, name)},
						{Text: fmt.Sprintf("多为新建命名空间时漏了拷贝拉取密钥 —— 从同仓库的其它命名空间复制一份到 %s", ns)},
						{Text: "⚠️ 这和「镜像不存在」无关，别去查仓库：镜像连拉都还没拉"},
					}
			},
		},
		{
			key: "image-not-found",
			hit: func(reason, msg string) bool {
				// 🔴 措辞必须照抄运行时实际吐的，别凭印象写。
				//	本地实测 containerd 的原话是：
				//	  rpc error: code = NotFound desc = failed to pull and unpack image
				//	  "…": failed to resolve reference "…": …:this-tag-does-not-exist: not found
				//	我第一版按 registry 协议的措辞写了 manifest unknown / manifest for…not found，
				//	四条判据**一条都没对上**，当场落到"判不出来"的兜底。
				//	不同运行时（containerd / docker / cri-o）和不同仓库措辞都不一样，
				//	所以这里既收结构化错误码，也收各家的文本形态。
				if strings.Contains(msg, "code = notfound") ||
					strings.Contains(msg, "manifest unknown") ||
					strings.Contains(msg, "repository does not exist") ||
					strings.Contains(msg, "not found: manifest") ||
					(strings.Contains(msg, "manifest for") && strings.Contains(msg, "not found")) {
					return true
				}
				// 兜底：拉镜像的事件里出现 "not found" 基本就是镜像/tag 不存在。
				// ⚠️ 但要排掉 "secret … not found" —— 那是缺拉取密钥，
				//	排查方向完全相反（一个去仓库查，一个去命名空间建密钥）
				return strings.Contains(msg, "not found") && !strings.Contains(msg, "secret")
			},
			build: func(w *EventCtx, image, ns string) (string, []Solution) {
				return fmt.Sprintf("仓库里没有这个镜像或 tag：%s", image),
					[]Solution{
						{Text: "多为构建没成功、或成功了但没推上去 —— 去「构建流水线」页看这个服务最近一次构建"},
						{Text: "也可能是 tag 写错（部署清单里的 tag 与实际推上去的对不上）"},
						{Text: manualCheckHint(image)},
					}
			},
		},
		{
			key: "pull-unauthorized",
			hit: func(reason, msg string) bool {
				return strings.Contains(msg, "unauthorized") ||
					strings.Contains(msg, "authentication required") ||
					strings.Contains(msg, "requested access to the resource is denied") ||
					strings.Contains(msg, "denied: ")
			},
			build: func(w *EventCtx, image, ns string) (string, []Solution) {
				return fmt.Sprintf("仓库拒绝了这次拉取（凭据无效或无权限）：%s", image),
					[]Solution{
						{Text: fmt.Sprintf("拉取密钥存在但不管用 —— 确认 %s 下那个 secret 里的账号还有效、密码没被改过", ns)},
						{Text: "也可能是账号没有这个项目的权限（换了仓库项目时最常见）"},
						{Text: manualCheckHint(image)},
					}
			},
		},
		{
			key: "registry-unreachable",
			hit: func(reason, msg string) bool {
				return strings.Contains(msg, "no such host") ||
					strings.Contains(msg, "i/o timeout") ||
					strings.Contains(msg, "connection refused") ||
					strings.Contains(msg, "dial tcp") ||
					strings.Contains(msg, "certificate")
			},
			build: func(w *EventCtx, image, ns string) (string, []Solution) {
				return fmt.Sprintf("节点连不上镜像仓库：%s", image),
					[]Solution{
						{Text: "⚠️ 这是**节点**到仓库的连通性，不是 CMDB 的 —— 要在出问题的那个节点上验"},
						{Text: "常见成因：仓库域名解析不了、出网策略变了、仓库自签证书没装进节点信任库"},
						{Text: manualCheckHint(image)},
					}
			},
		},
	}
}

// manualCheckHint 手动确认镜像在不在仓库的办法。
//
// 🔴 故意给命令而不是自动查：接仓库要一份长期凭据，而实测它只能多解一类问题
// （事件已过期那类），处置方案还不受影响。这笔交换不划算。
func manualCheckHint(image string) string {
	return "手动确认这个镜像在不在仓库（在能访问该仓库的机器上跑，只查不拉）：" +
		"crane manifest " + image + "　或　docker manifest inspect " + image
}

// ruleImagePullDetailed 拉镜像失败的细分判定，取代原来那条只说"拉取失败"的规则。
func ruleImagePullDetailed(c *DiagnosisContext) *DiagnosisResult {
	cc := waitingContainer(c, "ImagePullBackOff", "ErrImagePull", "InvalidImageName", "RegistryUnavailable")
	if cc == nil {
		return nil
	}
	base := fmt.Sprintf("容器 %s 处于 %s，镜像 %s", cc.Name, cc.StateReason, cc.Image)

	// ⚠️ 必须扫**全部**事件，不能只扫 Warning。
	//	kubelet 的 `BackOff`（"Back-off pulling image …"）是 **Normal** 类型 ——
	//	原来的 eventContaining 只看 Warning，于是那条 25 万次的事件一直没被读到，
	//	证据里连它都没有。
	for _, v := range imagePullVerdicts() {
		for i := range c.Events {
			w := &c.Events[i]
			if !v.hit(strings.ToLower(w.Reason), strings.ToLower(w.Message)) {
				continue
			}
			root, sol := v.build(w, cc.Image, c.Namespace)
			ev := []string{base}
			// 🔴 历史事件必须标出来，且**排在证据最前面**。
			//	一条三天前的报错和此刻的状态是两回事，不说清就会被照着去处置。
			conf := "high"
			if w.Historical {
				conf = "medium" // 说的是「当时」，此刻未必还是这个原因
				ev = append(ev, "⚠️ 下面这条来自 **CMDB 采集的历史事件**，不是集群上的实时事件")
				root += "（依据的是历史事件，此刻是否仍如此需确认）"
			}
			ev = append(ev, fmt.Sprintf("事件 %s（累计 %d 次，最后出现 %s）: %s",
				w.Reason, w.Count, w.LastSeen.Format("2006-01-02 15:04"), w.Message))
			if c.EventNote != "" {
				ev = append(ev, c.EventNote)
			}
			return &DiagnosisResult{
				Matched: true, Confidence: conf,
				RootCause: root,
				Evidence:  ev,
				Solutions: sol,
			}
		}
	}

	// 一条具体事件都没有 = 带原因的那条已过 TTL，只剩 BackOff。
	//
	// 🔴 这里必须**明说判不出来**，不能继续给"核对镜像名/检查密钥/确认网络"三件套。
	//	那三条看着像分析，实际是把排除法原样退还给人，
	//	而且会让人以为工具已经看过证据了。
	return &DiagnosisResult{
		Matched: true, Confidence: "low", Generic: true,
		RootCause: fmt.Sprintf("镜像拉不下来（%s），但**具体原因判不出来**：带原因的那条事件已过期，只剩重试记录", cc.StateReason),
		Evidence: []string{
			base,
			"⚠️ 事件里只剩 Back-off 重试，没有首次失败的原因。K8s 事件默认只留 1 小时，" +
				"这个 Pod 已经失败很久了 —— 原始报错查不到了，不是没有报错",
		},
		Solutions: []Solution{
			{Text: manualCheckHint(cc.Image)},
			{Text: fmt.Sprintf("确认拉取密钥在不在：kubectl -n %s get secret", c.Namespace)},
			{Text: "想拿到新的报错事件：删掉这个 Pod 让它重建，失败原因会重新写进事件（⚠️ 生产上先确认可以重建）"},
		},
	}
}
