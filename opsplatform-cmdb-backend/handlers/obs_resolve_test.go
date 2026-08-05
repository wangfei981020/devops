package handlers

import (
	"database/sql"
	"database/sql/driver"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// resolveEndpointFull 同时承担两件相反的事，很容易改坏一个修好另一个：
//
//	Prometheus/Loki —— **严格隔离**：集群 A 的查询绝不能落到集群 B 的源上
//	                    （UAT 和 PROD 有大量同名 namespace，混了就是错数据）
//	夜莺告警        —— **不限定**：一套实例覆盖所有集群，接入时绑了具体集群，
//	                    但查告警时不该因此找不到它
//
// 生产上就是后者坏了：infra-n9e 绑了 集群3+PROD，而 alerts.go 传 clusterID=0，
// 被当成"我要一个没绑集群的源"，于是 continue 跳过 → 告警页永远"未接入"。
func mockEndpoints(t *testing.T, rows [][]driver.Value) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	r := sqlmock.NewRows([]string{"url", "token_enc", "env", "cluster_id", "cluster_label"})
	for _, row := range rows {
		r.AddRow(row...)
	}
	mock.ExpectQuery("SELECT url").WillReturnRows(r)
	return db, mock
}

func TestResolveEndpoint_夜莺绑了集群也要能找到(t *testing.T) {
	// 复刻生产：infra-n9e 绑 cluster_id=3 + env=PROD
	db, _ := mockEndpoints(t, [][]driver.Value{
		{"http://n9e.example.com", "", "PROD", 3, ""},
	})
	defer db.Close()

	// 修复前：传 (env="", clusterID=0) → 两个条件都 continue → 找不到
	// 修复后：传「不限定」→ 必须找到
	url, _, _, err := resolveEndpointFull(db, nil, "n9e", anyEnv, anyCluster)
	if err != nil {
		t.Fatalf("绑了集群/环境的夜莺应该也能找到，实际报错：%v（这就是生产那个「未接入」假象）", err)
	}
	if url != "http://n9e.example.com" {
		t.Errorf("url = %q", url)
	}
}

func TestResolveEndpoint_Prometheus的集群隔离不能被放开(t *testing.T) {
	// 两个源分属不同集群。查集群 1 时**绝不能**返回集群 2 的源。
	db, _ := mockEndpoints(t, [][]driver.Value{
		{"http://prom-cluster2:9090", "", "", 2, ""},
	})
	defer db.Close()

	_, _, _, err := resolveEndpointFull(db, nil, "prometheus", "", 1)
	if err == nil {
		t.Fatal("查集群 1 却拿到了集群 2 的 Prometheus——这会把别的集群的指标混进来，" +
			"且不会有任何报错。修夜莺时最容易顺手改坏的就是这条。")
	}
}

func TestResolveEndpoint_环境不匹配仍要排除(t *testing.T) {
	db, _ := mockEndpoints(t, [][]driver.Value{
		{"http://prom-uat:9090", "", "UAT", 0, ""},
	})
	defer db.Close()

	if _, _, _, err := resolveEndpointFull(db, nil, "prometheus", "PROD", 0); err == nil {
		t.Fatal("查 PROD 却拿到 UAT 的源，必须排除")
	}
}

func TestResolveEndpoint_不限定时通用源仍可用(t *testing.T) {
	// 不限定不等于"只要绑定的"——没绑的照样能用
	db, _ := mockEndpoints(t, [][]driver.Value{
		{"http://n9e-generic", "", "", 0, ""},
	})
	defer db.Close()

	url, _, _, err := resolveEndpointFull(db, nil, "n9e", anyEnv, anyCluster)
	if err != nil || url != "http://n9e-generic" {
		t.Fatalf("通用源也应命中，got url=%q err=%v", url, err)
	}
}
