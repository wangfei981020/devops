package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 用真实的 g32-prod（GKE COS）标签形状造数据：
// 同一块 /dev/sda1 挂在 12 个点上，其中 /home/containerd 是只读；
// `/` 是另一块只读的 1.93 GB 启动镜像；/dev/sdb 是 PVC 的 globalmount。
func fakeProm(t *testing.T) *httptest.Server {
	t.Helper()
	type ser struct {
		Metric map[string]string `json:"metric"`
		Value  [2]any            `json:"value"`
	}
	// mountpoint -> {device, size, avail, readonly}
	type fs struct {
		dev   string
		size  float64
		avail float64
		ro    string
	}
	const gb = 1073741824.0
	disks := map[string]fs{
		// COS 只读启动镜像：1.93 GB、常年 74%，就是它把判定骗了
		"/": {"/dev/root", 1.93 * gb, 0.5 * gb, "1"},
		// 真正会满的那块：980 GB 用了 490 GB = 50%
		"/var/lib/kubelet":        {"/dev/sda1", 980 * gb, 490 * gb, "0"},
		"/mnt/stateful_partition": {"/dev/sda1", 980 * gb, 490 * gb, "0"},
		"/var":                    {"/dev/sda1", 980 * gb, 490 * gb, "0"},
		// imagefs 单独挂一块盘、而且更满 —— 每个节点必须取**最满的**那个，
		// 不是"扫到的最后一个"。⚠️ 第一版测试数据里所有挂载点水位都一样，
		//	于是"取最后一个"这个变异**没被抓到**，测试只是恰好通过。
		"/var/lib/containerd": {"/dev/sdc", 200 * gb, 30 * gb, "0"},
		// 同一块盘的只读绑定挂载 —— 按设备排除会把整块盘丢掉（坑 1）
		"/home/containerd": {"/dev/sda1", 980 * gb, 490 * gb, "1"},
		// PVC 的 globalmount，99% 满。它不是节点磁盘（坑 2），不该被算进来
		"/home/kubernetes/containerized_mounter/rootfs/var/lib/kubelet/plugins/kubernetes.io/csi/pd.csi.storage.gke.io/x/globalmount": {"/dev/sdb", 100 * gb, 1 * gb, "0"},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		var metric string
		switch {
		case strings.Contains(q, "node_filesystem_size_bytes"):
			metric = "size"
		case strings.Contains(q, "node_filesystem_avail_bytes"):
			metric = "avail"
		default:
			t.Fatalf("没料到的查询: %s", q)
		}
		// 极简地模拟 `unless node_filesystem_readonly{...} == 1`
		dropRO := strings.Contains(q, "node_filesystem_readonly")
		// 极简地模拟挂载点白名单
		wl := map[string]bool{}
		if i := strings.Index(q, `mountpoint=~"`); i >= 0 {
			rest := q[i+len(`mountpoint=~"`):]
			for _, mp := range strings.Split(rest[:strings.Index(rest, `"`)], "|") {
				wl[mp] = true
			}
		}
		out := []ser{}
		for mp, d := range disks {
			if len(wl) > 0 && !wl[mp] {
				continue
			}
			if dropRO && d.ro == "1" {
				continue
			}
			v := d.size
			if metric == "avail" {
				v = d.avail
			}
			out = append(out, ser{
				Metric: map[string]string{"node": "node-a", "mountpoint": mp, "device": d.dev},
				Value:  [2]any{0, formatFloat(v)},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data":   map[string]any{"resultType": "vector", "result": out},
		})
	}))
}

func formatFloat(v float64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestNodeFsUsage(t *testing.T) {
	srv := fakeProm(t)
	defer srv.Close()

	got, err := nodeFsUsage(srv.URL, "", "")
	if err != nil {
		t.Fatalf("nodeFsUsage: %v", err)
	}
	v, ok := got["node-a"]
	if !ok {
		t.Fatalf("没有 node-a，拿到 %+v", got)
	}

	// 坑 0：不能量到只读的 `/`。它是 1.93 GB 的启动镜像，
	// 量它会得到一个永远不变的数字（生产上是 74.0656%）
	if v.Mountpoint == "/" {
		t.Fatalf("量到了只读根分区 —— 这正是生产上失效了很久的那个 bug")
	}

	// 坑 1：只读的绑定挂载 /home/containerd 和 /var/lib/kubelet 是同一块 /dev/sda1。
	// 按设备排除会把整块盘丢掉、返回空
	if len(got) == 0 {
		t.Fatalf("返回空 —— 大概率又按设备排除只读了")
	}

	// 坑 2：/dev/sdb 那个 PVC globalmount 是 99% 满的。
	// 如果它被算进来，Pct 会是 99 而不是 50
	if v.Pct > 90 {
		t.Fatalf("Pct=%.2f —— 把 PVC 的 globalmount 当成节点磁盘了", v.Pct)
	}

	// 必须选中最满的那个挂载点（85% 的 imagefs），而不是 50% 的那三个
	if v.Mountpoint != "/var/lib/containerd" {
		t.Fatalf("选中 %s（%.2f%%），期望最满的 /var/lib/containerd —— 没有取 max", v.Mountpoint, v.Pct)
	}
	if v.Pct < 84.9 || v.Pct > 85.1 {
		t.Fatalf("Pct=%.2f，期望 85", v.Pct)
	}

	// 坑 3：白名单里 /dev/sda1 出现了 3 次。做 sum 的话总量会变成 3140 GB
	const gb = 1073741824.0
	if tot := v.TotalBytes / gb; tot < 199 || tot > 201 {
		t.Fatalf("总量 %.0f GB，期望 200 —— 多半是对多个挂载点做了 sum", tot)
	}
	// 三个数必须自洽：同一个挂载点上 用了 170 + 剩 30 = 共 200
	if used := v.UsedBytes / gb; used < 169 || used > 171 {
		t.Fatalf("已用 %.0f GB，期望 170 —— 三个数取自不同挂载点就会这样", used)
	}
}
