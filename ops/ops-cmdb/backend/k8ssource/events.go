package k8ssource

import (
	"context"
	"database/sql"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// 事件采集。
//
// # 为什么要落库
//
// **事件在 etcd 里只保留 1 小时**（apiserver 默认 --event-ttl=1h）。
// 排障时最想看的恰恰是"刚才那一下"，而等人打开界面时它常常已经没了。
// 采下来存着，才谈得上回看。
//
// # 为什么只采 Warning
//
// Normal 事件量是 Warning 的几十倍（每次拉镜像、每次调度都有一条），
// 全采会把库撑爆，而排障时没人看 Normal。要看全量就去 kubectl —— 那是实时场景。

// eventPageLimit 单轮最多取多少条。
//
// 大集群一小时内的 Warning 可能上千条。取 2000 是为了不让一次采集把
// apiserver 和我们自己都拖住；超出的部分下一轮会补上（事件有 1 小时窗口，
// 而采集间隔远小于此），所以不会漏。
const eventPageLimit = 2000

// syncEvents 采集 Warning 事件。
//
// ⚠️ 幂等靠 **Event 对象自己的名字**（cluster, ns, event_name）：
// 同一条事件在多轮采集里会被反复看到，不去重的话每 5 分钟就多一行，
// 一天下来同一个 FailedScheduling 有 288 条 —— 界面上看起来像"炸了 288 次"。
//
// ⚠️ 别拿 (obj, reason, first_at) 当键：同一个 Pod 拉镜像失败会先后产生
// ErrImagePull 和 ImagePullBackOff 两条事件，reason 都是 Failed、首次时间相同，
// 于是它们会被合并成一行 —— 而丢掉的那条恰恰说明了失败是怎么演进的（见迁移 105）。
func syncEvents(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, clusterID int) (int, error) {
	list, err := cs.CoreV1().Events("").List(ctx, metav1.ListOptions{
		FieldSelector: "type=Warning",
		Limit:         eventPageLimit,
	})
	if err != nil {
		return 0, err
	}

	n := 0
	for i := range list.Items {
		e := &list.Items[i]
		first := eventFirst(e)
		last := eventLast(e)
		msg := e.Message
		if len(msg) > 1000 {
			// 截断而不是丢弃：超长消息通常是一大段 YAML diff，
			// 前 1000 字已经够定位，丢掉整条则是把这次故障从记录里抹掉
			msg = msg[:1000]
		}
		count := int(e.Count)
		if count == 0 {
			count = 1
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO k8s_events
			(cluster_id, namespace, event_name, kind, obj_name, reason, message, type, count, first_at, last_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)
			ON DUPLICATE KEY UPDATE message=VALUES(message), count=VALUES(count),
				last_at=VALUES(last_at), synced_at=NOW()`,
			clusterID, e.Namespace, e.Name, e.InvolvedObject.Kind, e.InvolvedObject.Name,
			e.Reason, msg, string(e.Type), count, first, last); err == nil {
			n++
		}
	}
	return n, nil
}

// eventFirst / eventLast 取事件的首末时刻。
//
// ⚠️ k8s 有两套事件时间字段：老的 FirstTimestamp/LastTimestamp 和新的
// EventTime/Series。新版 apiserver 上老字段可能是零值 —— 只读老字段的话，
// 所有事件的时间都会变成 0001-01-01，界面上按时间排序完全失效，
// 而它不报任何错。
func eventFirst(e *corev1.Event) time.Time {
	if !e.FirstTimestamp.IsZero() {
		return e.FirstTimestamp.Time
	}
	if !e.EventTime.IsZero() {
		return e.EventTime.Time
	}
	return e.CreationTimestamp.Time
}

func eventLast(e *corev1.Event) time.Time {
	if e.Series != nil && !e.Series.LastObservedTime.IsZero() {
		return e.Series.LastObservedTime.Time
	}
	if !e.LastTimestamp.IsZero() {
		return e.LastTimestamp.Time
	}
	return eventFirst(e)
}
