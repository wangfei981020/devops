package services

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/url"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"opsplatform-probe-backend/config"
)

var (
	mc     *minio.Client
	bucket string
)

// InitMinIO initializes MinIO client. Endpoint can be like "http://host:9000" or "host:9000".
func InitMinIO(cfg *config.Config) error {
	if cfg.MinIOEndpoint == "" {
		log.Println("[MinIO] endpoint not configured, version upload disabled")
		return nil
	}
	endpoint := cfg.MinIOEndpoint
	useSSL := cfg.MinIOUseSSL
	if u, err := url.Parse(cfg.MinIOEndpoint); err == nil && u.Host != "" {
		endpoint = u.Host
		if u.Scheme == "https" {
			useSSL = true
		} else if u.Scheme == "http" {
			useSSL = false
		}
	}

	cli, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinIOAccessKey, cfg.MinIOSecretKey, ""),
		Secure: useSSL,
		Region: cfg.MinIORegion,
	})
	if err != nil {
		return fmt.Errorf("minio new: %w", err)
	}
	mc = cli
	bucket = cfg.MinIOBucket

	ctx := context.Background()
	exists, err := cli.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("bucket exists check: %w", err)
	}
	if !exists {
		if err := cli.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: cfg.MinIORegion}); err != nil {
			return fmt.Errorf("make bucket: %w", err)
		}
		log.Printf("[MinIO] created bucket %s", bucket)
	}
	log.Printf("[MinIO] connected: endpoint=%s bucket=%s", endpoint, bucket)
	return nil
}

func MinIOReady() bool { return mc != nil }

// PutObjectBytes uploads a byte slice to MinIO.
func PutObjectBytes(ctx context.Context, key string, data []byte, contentType string) error {
	if mc == nil {
		return fmt.Errorf("minio not configured")
	}
	_, err := mc.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	return err
}

// GetObjectStream returns a streaming reader for the object.
func GetObjectStream(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if mc == nil {
		return nil, 0, fmt.Errorf("minio not configured")
	}
	obj, err := mc.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, err
	}
	stat, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, 0, err
	}
	return obj, stat.Size, nil
}

// DeleteObject removes an object.
func DeleteObject(ctx context.Context, key string) error {
	if mc == nil {
		return fmt.Errorf("minio not configured")
	}
	return mc.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})
}
