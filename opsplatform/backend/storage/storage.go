package storage

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type StorageType string

const (
	StorageTypeLocal StorageType = "local"
	StorageTypeMinio StorageType = "minio"
	StorageTypeS3    StorageType = "s3"
	StorageTypeGCS   StorageType = "gcs"
)

type StorageConfig struct {
	Type        StorageType
	Endpoint    string
	AccessKey   string
	SecretKey   string
	Bucket      string
	Region      string
	UseSSL      bool
	LocalPath   string
	PublicURL   string
	PresignHost string
}

type Storage interface {
	Upload(ctx context.Context, objectName string, reader io.Reader, size int64, contentType string) (string, error)
	Delete(ctx context.Context, objectName string) error
	GetURL(objectName string) string
	GetPresignedURL(ctx context.Context, objectName string, expires time.Duration) (string, error)
	GetObjectName(publicURL string) string
	// v739: 响应记录用独立 bucket
	EnsureBucket(ctx context.Context, bucket string) error
	UploadTo(ctx context.Context, bucket, objectName string, reader io.Reader, size int64, contentType string) (string, error)
}

var defaultStorage Storage

func Init() error {
	storageType := StorageType(os.Getenv("STORAGE_TYPE"))
	if storageType == "" {
		storageType = StorageTypeMinio
	}

	config := StorageConfig{
		Type:        storageType,
		Endpoint:    getEnvOrDefault("STORAGE_ENDPOINT", "minio:9000"),
		AccessKey:   getEnvOrDefault("STORAGE_ACCESS_KEY", "minioadmin"),
		SecretKey:   getEnvOrDefault("STORAGE_SECRET_KEY", "minioadmin123"),
		Bucket:      getEnvOrDefault("STORAGE_BUCKET", "opsplatform"),
		Region:      getEnvOrDefault("STORAGE_REGION", "us-east-1"),
		UseSSL:      os.Getenv("STORAGE_USE_SSL") == "true",
		LocalPath:   getEnvOrDefault("STORAGE_LOCAL_PATH", "./uploads"),
		PublicURL:   os.Getenv("STORAGE_PUBLIC_URL"),
		PresignHost: os.Getenv("STORAGE_PRESIGN_HOST"),
	}

	var err error
	switch config.Type {
	case StorageTypeLocal:
		defaultStorage, err = NewLocalStorage(config)
	case StorageTypeMinio, StorageTypeS3, StorageTypeGCS:
		defaultStorage, err = NewMinioStorage(config)
	default:
		defaultStorage, err = NewMinioStorage(config)
	}

	if err != nil {
		log.Printf("[Storage] 初始化失败，回退到本地存储: %v", err)
		config.Type = StorageTypeLocal
		defaultStorage, err = NewLocalStorage(config)
	}

	log.Printf("[Storage] 使用存储类型: %s", config.Type)
	return err
}

func GetStorage() Storage {
	return defaultStorage
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

type MinioStorage struct {
	client      *minio.Client
	bucket      string
	publicURL   string
	presignHost string
	endpoint    string
	useSSL      bool
}

func NewMinioStorage(config StorageConfig) (*MinioStorage, error) {
	var creds *credentials.Credentials

	switch config.Type {
	case StorageTypeGCS:
		creds = credentials.NewStaticV2(config.AccessKey, config.SecretKey, "")
	default:
		creds = credentials.NewStaticV4(config.AccessKey, config.SecretKey, "")
	}

	client, err := minio.New(config.Endpoint, &minio.Options{
		Creds:  creds,
		Secure: config.UseSSL,
		Region: config.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("创建MinIO客户端失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exists, err := client.BucketExists(ctx, config.Bucket)
	if err != nil {
		return nil, fmt.Errorf("检查bucket失败: %v", err)
	}

	if !exists {
		err = client.MakeBucket(ctx, config.Bucket, minio.MakeBucketOptions{Region: config.Region})
		if err != nil {
			return nil, fmt.Errorf("创建bucket失败: %v", err)
		}

		policy := fmt.Sprintf(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Effect": "Allow",
				"Principal": {"AWS": ["*"]},
				"Action": ["s3:GetObject"],
				"Resource": ["arn:aws:s3:::%s/*"]
			}]
		}`, config.Bucket)
		err = client.SetBucketPolicy(ctx, config.Bucket, policy)
		if err != nil {
			log.Printf("[Storage] 设置bucket公开读策略失败: %v", err)
		}
		log.Printf("[Storage] 创建bucket: %s", config.Bucket)
	}

	publicURL := config.PublicURL
	if publicURL == "" {
		publicURL = fmt.Sprintf("/storage/%s", config.Bucket)
	}

	log.Printf("[Storage] MinIO连接成功: %s, bucket: %s", config.Endpoint, config.Bucket)
	return &MinioStorage{
		client:      client,
		bucket:      config.Bucket,
		publicURL:   publicURL,
		presignHost: config.PresignHost,
		endpoint:    config.Endpoint,
		useSSL:      config.UseSSL,
	}, nil
}

func (s *MinioStorage) Upload(ctx context.Context, objectName string, reader io.Reader, size int64, contentType string) (string, error) {
	_, err := s.client.PutObject(ctx, s.bucket, objectName, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("上传文件失败: %v", err)
	}
	return s.GetURL(objectName), nil
}

// EnsureBucket 确保给定 bucket 存在；不存在则创建并设公开读策略（v739：响应记录用独立 bucket）
func (s *MinioStorage) EnsureBucket(ctx context.Context, bucket string) error {
	exists, err := s.client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("检查 bucket %s 失败: %v", bucket, err)
	}
	if exists {
		return nil
	}
	if err := s.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("创建 bucket %s 失败: %v", bucket, err)
	}
	policy := fmt.Sprintf(`{
		"Version": "2012-10-17",
		"Statement": [{
			"Effect": "Allow",
			"Principal": {"AWS": ["*"]},
			"Action": ["s3:GetObject"],
			"Resource": ["arn:aws:s3:::%s/*"]
		}]
	}`, bucket)
	if err := s.client.SetBucketPolicy(ctx, bucket, policy); err != nil {
		log.Printf("[Storage] 设置 bucket %s 公开读策略失败: %v", bucket, err)
	}
	log.Printf("[Storage] 创建 bucket: %s", bucket)
	return nil
}

// UploadTo 上传到指定 bucket，返回相对 URL `/storage/<bucket>/<objectName>`
func (s *MinioStorage) UploadTo(ctx context.Context, bucket, objectName string, reader io.Reader, size int64, contentType string) (string, error) {
	_, err := s.client.PutObject(ctx, bucket, objectName, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("上传到 bucket %s 失败: %v", bucket, err)
	}
	return fmt.Sprintf("/storage/%s/%s", bucket, objectName), nil
}

func (s *MinioStorage) Delete(ctx context.Context, objectName string) error {
	return s.client.RemoveObject(ctx, s.bucket, objectName, minio.RemoveObjectOptions{})
}

func (s *MinioStorage) GetURL(objectName string) string {
	return fmt.Sprintf("%s/%s", strings.TrimSuffix(s.publicURL, "/"), objectName)
}

func (s *MinioStorage) GetPresignedURL(ctx context.Context, objectName string, expires time.Duration) (string, error) {
	presignedURL, err := s.client.PresignedGetObject(ctx, s.bucket, objectName, expires, nil)
	if err != nil {
		return "", fmt.Errorf("生成预签名URL失败: %v", err)
	}

	parsed := presignedURL
	relativeURL := fmt.Sprintf("/storage/%s/%s?%s", s.bucket, objectName, parsed.RawQuery)

	return relativeURL, nil
}

func (s *MinioStorage) GetObjectName(publicURL string) string {
	prefix := strings.TrimSuffix(s.publicURL, "/") + "/"
	if strings.HasPrefix(publicURL, prefix) {
		return strings.TrimPrefix(publicURL, prefix)
	}
	return publicURL
}

type LocalStorage struct {
	basePath  string
	publicURL string
}

func NewLocalStorage(config StorageConfig) (*LocalStorage, error) {
	if err := os.MkdirAll(config.LocalPath, 0755); err != nil {
		return nil, fmt.Errorf("创建本地存储目录失败: %v", err)
	}

	publicURL := config.PublicURL
	if publicURL == "" {
		publicURL = "/uploads"
	}

	log.Printf("[Storage] 使用本地存储: %s", config.LocalPath)
	return &LocalStorage{
		basePath:  config.LocalPath,
		publicURL: publicURL,
	}, nil
}

func (s *LocalStorage) Upload(ctx context.Context, objectName string, reader io.Reader, size int64, contentType string) (string, error) {
	dir := filepath.Dir(filepath.Join(s.basePath, objectName))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("创建目录失败: %v", err)
	}

	filePath := filepath.Join(s.basePath, objectName)
	file, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("创建文件失败: %v", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, reader); err != nil {
		return "", fmt.Errorf("写入文件失败: %v", err)
	}

	return s.GetURL(objectName), nil
}

func (s *LocalStorage) Delete(ctx context.Context, objectName string) error {
	filePath := filepath.Join(s.basePath, objectName)
	return os.Remove(filePath)
}

func (s *LocalStorage) GetURL(objectName string) string {
	return fmt.Sprintf("%s/%s", strings.TrimSuffix(s.publicURL, "/"), url.PathEscape(objectName))
}

func (s *LocalStorage) GetPresignedURL(ctx context.Context, objectName string, expires time.Duration) (string, error) {
	return s.GetURL(objectName), nil
}

func (s *LocalStorage) GetObjectName(publicURL string) string {
	prefix := strings.TrimSuffix(s.publicURL, "/") + "/"
	if strings.HasPrefix(publicURL, prefix) {
		return strings.TrimPrefix(publicURL, prefix)
	}
	return publicURL
}

// EnsureBucket 本地存储：bucket 即子目录，确保存在即可（v739 兼容 Storage 接口）
func (s *LocalStorage) EnsureBucket(ctx context.Context, bucket string) error {
	return os.MkdirAll(filepath.Join(s.basePath, bucket), 0o755)
}

// UploadTo 本地存储：把 objectName 写入 <basePath>/<bucket>/ 目录
func (s *LocalStorage) UploadTo(ctx context.Context, bucket, objectName string, reader io.Reader, size int64, contentType string) (string, error) {
	dir := filepath.Join(s.basePath, bucket)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, objectName)
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, reader); err != nil {
		return "", err
	}
	return fmt.Sprintf("/storage/%s/%s", bucket, objectName), nil
}
