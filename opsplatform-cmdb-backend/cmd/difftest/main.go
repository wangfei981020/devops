// difftest 是本地验证工具：对同一个集群反复跑 SyncCluster，用来对照
// K8S_SYNC_MODE=replace（全删全插）与 diff（全量比对+增量写）两种模式的
// binlog 写入量与结果一致性。仅用于本地验证，不参与生产构建流程。
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/tools/clientcmd"

	"opsplatform-cmdb-backend/k8ssource"
)

func main() {
	rounds := flag.Int("rounds", 5, "采集轮数")
	cid := flag.Int("cluster", 2, "cluster_id")
	flag.Parse()

	dsn := os.Getenv("DSN")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "需要设置 DSN 环境变量")
		os.Exit(1)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "连库失败:", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		fmt.Fprintln(os.Stderr, "Ping 失败:", err)
		os.Exit(1)
	}

	kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读 kubeconfig 失败:", err)
		os.Exit(1)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "建 clientset 失败:", err)
		os.Exit(1)
	}
	dc, err := dynamic.NewForConfig(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "建 dynamic client 失败:", err)
		os.Exit(1)
	}
	mc, err := metadata.NewForConfig(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "建 metadata client 失败:", err)
		os.Exit(1)
	}

	mode := os.Getenv("K8S_SYNC_MODE")
	if mode == "" {
		mode = "diff(默认)"
	}
	fmt.Printf("模式=%s 集群=%d 轮数=%d\n", mode, *cid, *rounds)

	for i := 1; i <= *rounds; i++ {
		start := time.Now()
		res := k8ssource.SyncCluster(context.Background(), db, cs, dc, mc, *cid, "")
		total, failed := 0, 0
		for _, r := range res {
			total += r.Count
			if r.Err != nil {
				failed++
				fmt.Printf("  ! %s: %v\n", r.Resource, r.Err)
			}
		}
		fmt.Printf("第 %d 轮: %d 行, 失败 %d 类, 耗时 %v\n",
			i, total, failed, time.Since(start).Round(time.Millisecond))
	}
}
