package k8ssource

import (
	"context"
	"database/sql"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/metadata"

	"opsplatform-cmdb-backend/logx"
)

// Secret 名录采集（默认关闭，按集群开关）。
//
// 为什么此前不做：K8s 的 list secrets 会连 data 一并返回，给 CMDB 这个权限
// 等于让它能读全集群所有密码。代价是「Pod 起不来缺哪个 Secret」只能靠事件佐证，
// 而从未启动过的 Pod（事件已过 TTL）就查不出来。
//
// 现在的取舍：**DEV 放开，UAT/生产不放**，靠三层控制：
//  1. 集群 RBAC —— 只给 DEV 的只读 SA 加 secrets:[list]（最硬的一层，CMDB 无法绕过）
//  2. allow_secret_inventory 开关 —— 即使有权限，没开也不采
//  3. 本文件用 metadata 客户端 —— 请求的是 PartialObjectMetadata，
//     **APIServer 根本不返回 data**，进程内从不出现 Secret 内容
//
// 因此表里只有 namespace/name/type：metadata 里本来就没有键名，更没有值。
// 对 Secret 只能判「存不存在」，判不了「键对不对」——这是有意为之。

var secretGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}

// syncSecretNames 采 Secret 名录。集群未开启开关时直接清空并返回，
// 不留上一次的残留数据——开关关掉后还能查到名录，等于开关没关。
func syncSecretNames(ctx context.Context, db *sql.DB, mc metadata.Interface, cid int, allowed bool) (int, error) {
	if !allowed {
		return 0, clearAll(db, "k8s_secrets", cid)
	}
	list, err := mc.Resource(secretGVR).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil {
		// 开了开关却没权限是最容易误判的一种状态：名录为空会被读成「Secret 都不存在」，
		// 而真相是压根没采到。日志必须说清该去加什么权限。
		logx.J("k8s", "secret_inventory_denied", map[string]any{
			"cluster_id": cid, "err": err.Error(),
			"warn": "已开启 Secret 名录但采集失败；若为 403，该集群只读 ClusterRole 需要 secrets:[list]。" +
				"名录为空不代表 Secret 不存在",
		})
		return 0, err
	}
	rows := make([][]any, 0, len(list.Items))
	for i := range list.Items {
		m := &list.Items[i]
		// Secret 的 type 是对象顶层字段、不在 metadata 里，metadata-only 拿不到，留空。
		// 判「存不存在」不需要它，而为了拿它去请求完整对象就等于把 data 也取回来了。
		rows = append(rows, []any{cid, m.Namespace, m.Name, ""})
	}
	n, err := writeRows(db, "k8s_secrets",
		[]string{"cluster_id", "namespace", "name", "type"}, cid, rows, "cluster_id", "namespace", "name")
	if err == nil {
		logx.J("k8s", "secret_inventory", map[string]any{
			"cluster_id": cid, "count": n,
			"note": "仅采集名字/命名空间，未请求也未存储 Secret 内容",
		})
	}
	return n, err
}
