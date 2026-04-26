package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
)

// =========================================================================
//   失败 pod 日志归档（MinIO）
//
// 流程：
//   1. PollUntilStable 进入失败终态时，异步起 goroutine 拉「上一次崩溃前」日志
//   2. 上传到 MinIO bucket，路径 deploy-logs/{depID}/{argocdApp}/{podName}.txt
//   3. 写一行到 deployment_pod_logs 表（depID + app + pod + object_key + size + at）
//
// MinIO 凭证由 admin 在「系统设置」配置；未配置时归档功能完全跳过（优雅降级）。
//
// Bucket lifecycle 自动设：N 天后过期（admin 在系统设置改保留天数）
// =========================================================================

// MinIOConfig 结构 ——  与 global_config 表 minio_* 字段对应
type MinIOConfig struct {
	Endpoint       string // 完整 URL，如 http://minio.svc:9000 或 https://minio.example.com
	Bucket         string
	AccessKey      string
	SecretKey      string // 调用方拿到时已是明文（DB 里 AES 加密）
	Region         string
	RetentionDays  int
}

// MinIOClient 包了一层，带 endpoint URL 解析和 lifecycle helper
type MinIOClient struct {
	cli    *minio.Client
	bucket string
	cfg    MinIOConfig
}

// NewMinIOClient 解析 Endpoint URL，组装 minio-go client
func NewMinIOClient(cfg MinIOConfig) (*MinIOClient, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("minio not configured")
	}
	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse minio endpoint: %w", err)
	}
	host := u.Host
	if host == "" {
		// 用户可能直接填 host:port 没带 scheme
		host = strings.TrimPrefix(strings.TrimPrefix(cfg.Endpoint, "http://"), "https://")
	}
	secure := u.Scheme == "https"
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	cli, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
		Region: region,
	})
	if err != nil {
		return nil, err
	}
	return &MinIOClient{cli: cli, bucket: cfg.Bucket, cfg: cfg}, nil
}

// EnsureBucket 确保 bucket 存在且 lifecycle 策略已设置
//
//	幂等：bucket 已存在跳过创建；lifecycle 设置每次都覆盖（admin 改保留天数后立即生效）。
func (m *MinIOClient) EnsureBucket(ctx context.Context) error {
	exists, err := m.cli.BucketExists(ctx, m.bucket)
	if err != nil {
		return fmt.Errorf("check bucket: %w", err)
	}
	if !exists {
		if err := m.cli.MakeBucket(ctx, m.bucket, minio.MakeBucketOptions{Region: m.cfg.Region}); err != nil {
			return fmt.Errorf("make bucket: %w", err)
		}
	}
	return m.applyLifecycle(ctx)
}

// applyLifecycle 设置 bucket 自动过期规则：deploy-logs/ 前缀下所有对象 N 天后过期
func (m *MinIOClient) applyLifecycle(ctx context.Context) error {
	days := m.cfg.RetentionDays
	if days <= 0 {
		days = 90
	}
	cfg := lifecycle.NewConfiguration()
	cfg.Rules = []lifecycle.Rule{
		{
			ID:     "expire-deploy-logs",
			Status: "Enabled",
			RuleFilter: lifecycle.Filter{
				Prefix: "deploy-logs/",
			},
			Expiration: lifecycle.Expiration{
				Days: lifecycle.ExpirationDays(days),
			},
		},
	}
	return m.cli.SetBucketLifecycle(ctx, m.bucket, cfg)
}

// PutLog 上传日志文本到指定 object key，返回大小（字节）
func (m *MinIOClient) PutLog(ctx context.Context, objectKey, content string) (int64, error) {
	if content == "" {
		return 0, errors.New("empty log content")
	}
	r := bytes.NewReader([]byte(content))
	info, err := m.cli.PutObject(ctx, m.bucket, objectKey, r, int64(len(content)),
		minio.PutObjectOptions{ContentType: "text/plain; charset=utf-8"})
	if err != nil {
		return 0, err
	}
	return info.Size, nil
}

// GetLog 读取指定 object 内容（最大 5MB 防御性上限）
func (m *MinIOClient) GetLog(ctx context.Context, objectKey string) (string, error) {
	obj, err := m.cli.GetObject(ctx, m.bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return "", err
	}
	defer obj.Close()
	const maxSize = 5 * 1024 * 1024
	buf := make([]byte, 0, 64*1024)
	tmp := make([]byte, 32*1024)
	for len(buf) < maxSize {
		n, rerr := obj.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if rerr != nil {
			break
		}
	}
	return string(buf), nil
}

// TestConnection 测试连接 + bucket 存在性，给「测试连接」按钮用
func (m *MinIOClient) TestConnection(ctx context.Context) error {
	tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := m.cli.ListBuckets(tctx)
	return err
}

// LogObjectKey 统一对象命名：deploy-logs/{depID}/{argocdApp}/{podName}.txt
func LogObjectKey(deploymentID int64, argocdApp, podName string) string {
	return fmt.Sprintf("deploy-logs/%d/%s/%s.txt", deploymentID, argocdApp, podName)
}
