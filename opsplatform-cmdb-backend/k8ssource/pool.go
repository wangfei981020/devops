// Package k8ssource 管理多集群的只读 client-go 连接（移植自 k8sinsight k8s/pool.go）。
// 与 k8sinsight 的区别：kubeconfig 从 CMDB k8s_clusters 表读出后 AES 解密再用（不明文存）。
// 只读语义由 kubeconfig 背后的 view/只读 ServiceAccount 在集群侧 RBAC 保证；本进程只 get/list/watch。
package k8ssource

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"opsplatform-cmdb-backend/crypto"
)

// Pool 按 cluster id 缓存 clientset / dynamic client，避免每次请求重建连接。
type Pool struct {
	db      *sql.DB
	cipher  *crypto.Cipher
	mu      sync.Mutex
	cache   map[int]*kubernetes.Clientset
	dynCache map[int]dynamic.Interface
}

func NewPool(db *sql.DB, cipher *crypto.Cipher) *Pool {
	return &Pool{db: db, cipher: cipher, cache: map[int]*kubernetes.Clientset{}, dynCache: map[int]dynamic.Interface{}}
}

// restConfigFor 从 DB 读加密 kubeconfig（或 in-cluster）构建 rest.Config。
func (p *Pool) restConfigFor(id int) (*rest.Config, error) {
	var kcEnc sql.NullString
	var provider string
	var enabled int
	err := p.db.QueryRow(`SELECT provider, kubeconfig_enc, enabled FROM k8s_clusters WHERE id=?`, id).
		Scan(&provider, &kcEnc, &enabled)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("集群 %d 不存在", id)
	}
	if err != nil {
		return nil, fmt.Errorf("load cluster %d: %w", id, err)
	}
	if enabled == 0 {
		return nil, fmt.Errorf("集群 %d 已禁用", id)
	}
	var cfg *rest.Config
	if provider == "in-cluster" {
		cfg, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("in-cluster config: %w", err)
		}
	} else {
		if !kcEnc.Valid || kcEnc.String == "" {
			return nil, fmt.Errorf("集群 %d 未配置 kubeconfig", id)
		}
		kc, e := p.cipher.Decrypt(kcEnc.String)
		if e != nil {
			return nil, fmt.Errorf("解密 kubeconfig 失败: %w", e)
		}
		cfg, err = clientcmd.RESTConfigFromKubeConfig([]byte(kc))
		if err != nil {
			return nil, fmt.Errorf("解析 kubeconfig: %w", err)
		}
	}
	cfg.Timeout = 15 * time.Second // 只读兜底：卡死集群不拖垮采集
	return cfg, nil
}

// ClientFor 返回指定集群的 clientset（缓存）。
func (p *Pool) ClientFor(id int) (*kubernetes.Clientset, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.cache[id]; ok {
		return c, nil
	}
	cfg, err := p.restConfigFor(id)
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("构建 clientset: %w", err)
	}
	p.cache[id] = cs
	return cs, nil
}

// DynamicFor 返回指定集群的 dynamic client（缓存），用于 Gateway API 等 CRD。
func (p *Pool) DynamicFor(id int) (dynamic.Interface, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if d, ok := p.dynCache[id]; ok {
		return d, nil
	}
	cfg, err := p.restConfigFor(id)
	if err != nil {
		return nil, err
	}
	dc, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("构建 dynamic client: %w", err)
	}
	p.dynCache[id] = dc
	return dc, nil
}

// Invalidate 在集群 kubeconfig 变更/删除后清除缓存（下次重建）。
func (p *Pool) Invalidate(id int) {
	p.mu.Lock()
	delete(p.cache, id)
	delete(p.dynCache, id)
	p.mu.Unlock()
}
