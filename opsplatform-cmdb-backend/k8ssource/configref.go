package k8ssource

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// 配置引用采集：回答「Pod 起不来时到底缺哪个 ConfigMap/Secret」。
//
// 此前 CMDB 能看到 CreateContainerConfigError，但缺哪个配置必须登集群查——
// 这是「看得到卡住、查不到原因」的典型缺口。
//
// 两条数据来源，权限代价完全不同：
//   - ConfigMap 名录：只读 RBAC 里已有 configmaps 权限，可以直接 List。但**只取键名**，
//     value 里常有连接串/密码，落库等于把敏感配置复制进 CMDB。
//   - 引用关系：全部从 pod spec 里读，复用采 Pod 那一次 List，**不需要任何新权限**、
//     也不额外请求 APIServer。
//
// Secret 名录刻意不做：K8s 的 list secrets 会连 data 一起返回，
// 给 CMDB 这个权限等于让它能读全集群所有密码。缺失判定改走事件佐证（见 config_audit.go）。

// configRef 一条配置引用。独立于 SQL 行是为了能单测——
// 从 pod spec 抽引用的分支很多（env/envFrom/卷/投射卷/拉取密钥/初始化容器），漏一类就少一类根因。
type configRef struct {
	Container string
	Kind      string // configmap | secret
	Name      string
	Key       string // 空=整体引用
	Source    string // env | envFrom | volume | imagePullSecret
	Optional  bool
}

const (
	kindCM  = "configmap"
	kindSec = "secret"
)

// optBool 解引用 K8s 的 *bool（optional 字段），nil 视为 false（即必需）。
func optBool(b *bool) bool { return b != nil && *b }

// podConfigRefs 从 pod spec 抽出全部配置引用。
//
// 覆盖 initContainers 与 containers 两类容器：初始化容器缺配置同样会让 Pod 卡住，
// 而且它先跑，往往才是真正的卡点。
func podConfigRefs(p *corev1.Pod) []configRef {
	refs := make([]configRef, 0, 8)

	// 镜像拉取密钥：没有 optional 语义——缺了必然 ImagePullBackOff。
	// DEV-002（缺 harbor-id）就是这一类，此前完全查不到。
	for _, ips := range p.Spec.ImagePullSecrets {
		if ips.Name != "" {
			refs = append(refs, configRef{Kind: kindSec, Name: ips.Name, Source: "imagePullSecret"})
		}
	}

	containers := make([]corev1.Container, 0, len(p.Spec.InitContainers)+len(p.Spec.Containers))
	containers = append(containers, p.Spec.InitContainers...)
	containers = append(containers, p.Spec.Containers...)
	for _, ct := range containers {
		for _, e := range ct.Env {
			if e.ValueFrom == nil {
				continue
			}
			if r := e.ValueFrom.ConfigMapKeyRef; r != nil {
				refs = append(refs, configRef{ct.Name, kindCM, r.Name, r.Key, "env", optBool(r.Optional)})
			}
			if r := e.ValueFrom.SecretKeyRef; r != nil {
				refs = append(refs, configRef{ct.Name, kindSec, r.Name, r.Key, "env", optBool(r.Optional)})
			}
		}
		for _, ef := range ct.EnvFrom {
			if r := ef.ConfigMapRef; r != nil {
				refs = append(refs, configRef{ct.Name, kindCM, r.Name, "", "envFrom", optBool(r.Optional)})
			}
			if r := ef.SecretRef; r != nil {
				refs = append(refs, configRef{ct.Name, kindSec, r.Name, "", "envFrom", optBool(r.Optional)})
			}
		}
	}

	// 卷是 Pod 级的，容器名留空。
	for _, v := range p.Spec.Volumes {
		switch {
		case v.ConfigMap != nil:
			refs = append(refs, configRef{Kind: kindCM, Name: v.ConfigMap.Name, Source: "volume", Optional: optBool(v.ConfigMap.Optional)})
		case v.Secret != nil:
			refs = append(refs, configRef{Kind: kindSec, Name: v.Secret.SecretName, Source: "volume", Optional: optBool(v.Secret.Optional)})
		case v.Projected != nil:
			// 投射卷把多个源合成一个目录挂载，证书/token 经常这么挂。
			// 不展开的话这类引用会整片漏掉。
			for _, s := range v.Projected.Sources {
				if r := s.ConfigMap; r != nil {
					refs = append(refs, configRef{Kind: kindCM, Name: r.Name, Source: "volume", Optional: optBool(r.Optional)})
				}
				if r := s.Secret; r != nil {
					refs = append(refs, configRef{Kind: kindSec, Name: r.Name, Source: "volume", Optional: optBool(r.Optional)})
				}
			}
		}
	}

	// 同一 Pod 内的重复引用（多容器引同一个 envFrom）去重，否则影响面统计会虚高。
	seen := make(map[configRef]bool, len(refs))
	out := refs[:0]
	for _, r := range refs {
		if r.Name == "" || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}

// syncPodConfigRefs 落库 Pod 配置引用。由 syncPods 复用同一次 List 调用，不额外请求 APIServer。
func syncPodConfigRefs(db *sql.DB, cid int, pods []corev1.Pod) error {
	rows := make([][]any, 0, len(pods)*4)
	for i := range pods {
		p := &pods[i]
		for _, r := range podConfigRefs(p) {
			opt := 0
			if r.Optional {
				opt = 1
			}
			rows = append(rows, []any{cid, p.Namespace, p.Name, r.Container, r.Kind, r.Name, r.Key, r.Source, opt})
		}
	}
	_, err := replaceAll(db, "k8s_pod_config_refs", []string{
		"cluster_id", "namespace", "pod_name", "container",
		"ref_kind", "ref_name", "ref_key", "source", "optional",
	}, cid, rows)
	return err
}

// syncConfigMaps 采 ConfigMap 名录。
//
// 只取键名不取 value：value 里常有数据库连接串、第三方密钥等，
// 落库等于把它们复制到 CMDB 里再存一份。名录的用途是判断「引用的东西存不存在」，
// 键名足够，内容一概不需要。
func syncConfigMaps(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, cid int) (int, error) {
	list, err := cs.CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	rows := make([][]any, 0, len(list.Items))
	for i := range list.Items {
		cm := &list.Items[i]
		keys := make([]string, 0, len(cm.Data)+len(cm.BinaryData))
		for k := range cm.Data {
			keys = append(keys, k)
		}
		for k := range cm.BinaryData {
			keys = append(keys, k)
		}
		// map 遍历顺序随机，不排序的话每轮采集结果都不一样，diff 全是噪音。
		sort.Strings(keys)
		rows = append(rows, []any{cid, cm.Namespace, cm.Name, strings.Join(keys, ","), len(keys)})
	}
	return replaceAll(db, "k8s_configmaps",
		[]string{"cluster_id", "namespace", "name", "key_names", "key_count"}, cid, rows)
}
