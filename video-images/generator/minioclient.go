package main

import (
	"context"
	"log"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// NewMinioClient 初始化 MinIO 客户端(使用最小权限的 writer 账号)。
// 桶的创建 + 双账号/策略由 Helm 的 video-images-bucket-init Job(用 root)完成,
// 这里只做可读性检查,不再自己建桶(scoped writer 无建桶权限,失败不致命)。
func NewMinioClient(cfg Config) (*minio.Client, error) {
	c, err := minio.New(cfg.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""),
		Secure: cfg.MinioUseSSL,
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if exists, err := c.BucketExists(ctx, cfg.MinioBucket); err != nil {
		// scoped 账号可能无 HeadBucket 权限,只告警不致命;真正上传时若失败会另有日志
		log.Printf("WARN 检查桶 %s 失败(可能权限受限,由 init Job 负责建桶): %v", cfg.MinioBucket, err)
	} else if !exists {
		log.Printf("WARN 桶 %s 不存在,等待 init Job 创建", cfg.MinioBucket)
	}
	return c, nil
}
