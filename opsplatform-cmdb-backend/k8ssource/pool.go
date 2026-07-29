// Package k8ssource 管理多集群的只读 client-go 连接（移植自 k8sinsight k8s/pool.go）。
// 与 k8sinsight 的区别：kubeconfig 从 CMDB k8s_clusters 表读出后 AES 解密再用（不明文存）。
// 只读语义由 kubeconfig 背后的 view/只读 ServiceAccount 在集群侧 RBAC 保证；本进程只 get/list/watch。
package k8ssource

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"opsplatform-cmdb-backend/crypto"
)

// Pool 按 cluster id 缓存 clientset / dynamic client，避免每次请求重建连接。
type Pool struct {
	db       *sql.DB
	cipher   *crypto.Cipher
	mu       sync.Mutex
	cache    map[int]*kubernetes.Clientset
	dynCache map[int]dynamic.Interface
}

func NewPool(db *sql.DB, cipher *crypto.Cipher) *Pool {
	return &Pool{db: db, cipher: cipher, cache: map[int]*kubernetes.Clientset{}, dynCache: map[int]dynamic.Interface{}}
}

// restConfigFor 构建 rest.Config：in-cluster / kubeconfig / GKE-SA(用云账号SA key换token) 三种。
func (p *Pool) restConfigFor(id int) (*rest.Config, error) {
	var kcEnc, caData sql.NullString
	var provider, projectID, endpoint string
	var enabled, cloudAcct int
	err := p.db.QueryRow(`SELECT provider, kubeconfig_enc, enabled, cloud_account_id, project_id, endpoint, ca_data
		FROM k8s_clusters WHERE id=?`, id).
		Scan(&provider, &kcEnc, &enabled, &cloudAcct, &projectID, &endpoint, &caData)
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
	switch {
	case provider == "in-cluster":
		cfg, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("in-cluster config: %w", err)
		}
	case kcEnc.Valid && kcEnc.String != "":
		kc, e := p.cipher.Decrypt(kcEnc.String)
		if e != nil {
			return nil, fmt.Errorf("解密 kubeconfig 失败: %w", e)
		}
		cfg, err = clientcmd.RESTConfigFromKubeConfig([]byte(kc))
		if err != nil {
			return nil, fmt.Errorf("解析 kubeconfig: %w", err)
		}
	case provider == "gke" && cloudAcct > 0 && endpoint != "" && caData.Valid && caData.String != "":
		// GKE 经云账号 SA key 连：取该 project 的 SA key → 换 OAuth token
		saJSON, e := p.projectSA(cloudAcct, projectID)
		if e != nil {
			return nil, e
		}
		return gkeRestConfig(context.Background(), saJSON, endpoint, caData.String)
	default:
		return nil, fmt.Errorf("集群 %d 未配置连接方式（kubeconfig 或 GKE 云账号）", id)
	}
	cfg.Timeout = 15 * time.Second // 只读兜底：卡死集群不拖垮采集
	return cfg, nil
}

// projectSA 取某云账号项目的 SA key JSON（解密）。
func (p *Pool) projectSA(accountID int, projectID string) ([]byte, error) {
	var enc sql.NullString
	e := p.db.QueryRow(`SELECT cred_enc FROM cloud_account_projects WHERE account_id=? AND project_id=?`, accountID, projectID).Scan(&enc)
	if e != nil {
		return nil, fmt.Errorf("找不到云账号项目 %d/%s 的凭据", accountID, projectID)
	}
	if !enc.Valid || enc.String == "" {
		return nil, fmt.Errorf("云账号项目 %s 未配 SA key", projectID)
	}
	sa, e := p.cipher.Decrypt(enc.String)
	if e != nil {
		return nil, fmt.Errorf("解密 SA key 失败: %w", e)
	}
	return []byte(sa), nil
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

// MetadataFor 返回只取对象 metadata 的客户端。
//
// 专为 Secret 名录准备：普通 clientset 的 list secrets 会把 data（也就是密码本身）
// 一并取回来，而 metadata 客户端请求的是 PartialObjectMetadata，
// **APIServer 根本不会把 data 发过来**。所以 CMDB 进程从不接触 Secret 内容，
// 即使这个集群给了 secrets:list 权限。
func (p *Pool) MetadataFor(id int) (metadata.Interface, error) {
	cfg, err := p.restConfigFor(id)
	if err != nil {
		return nil, err
	}
	mc, err := metadata.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("构建 metadata client: %w", err)
	}
	return mc, nil
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
