package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 是 agent 的运行时配置，从 YAML 文件加载
//
//	所有字段尽量给合理默认；token 必填，缺失启动报错（防止误开放）。
type Config struct {
	// HTTP server
	Listen  string `yaml:"listen"`   // ":8443"
	TLSCert string `yaml:"tls_cert"` // PEM 证书路径；空表示走 HTTP（仅本地测试）
	TLSKey  string `yaml:"tls_key"`  // PEM 私钥路径

	// 鉴权
	Token string `yaml:"token"` // 必填；client 用 Authorization: Bearer <token>

	// ansible 仓库根目录（agent 在这里 git pull）
	AnsibleRoot string `yaml:"ansible_root"` // /etc/ansible

	// 版本根目录（rsync 落地处，扫服务版本用）
	VersionRoot string `yaml:"version_root"` // /data/vcs

	// 并发控制
	MaxConcurrent int `yaml:"max_concurrent"` // 同时跑多少个 ansible 任务，默认 10

	// 任务保留（内存里）
	TaskRetention int `yaml:"task_retention_hours"` // 完成任务保留多少小时；默认 24h

	// 日志
	LogLevel string `yaml:"log_level"` // info / debug
}

// Load 从 path 读取 YAML 配置；填默认值；最后校验必填项
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = ":8443"
	}
	if c.AnsibleRoot == "" {
		c.AnsibleRoot = "/etc/ansible"
	}
	if c.VersionRoot == "" {
		c.VersionRoot = "/data/vcs"
	}
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = 10
	}
	if c.TaskRetention <= 0 {
		c.TaskRetention = 24
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
}

func (c *Config) validate() error {
	if c.Token == "" {
		return errors.New("config.token is required (run install.sh to generate one)")
	}
	if (c.TLSCert != "" && c.TLSKey == "") || (c.TLSKey != "" && c.TLSCert == "") {
		return errors.New("tls_cert and tls_key must both be set or both empty")
	}
	return nil
}

// IsTLS 是否启用 TLS（cert + key 都配了）
func (c *Config) IsTLS() bool {
	return c.TLSCert != "" && c.TLSKey != ""
}
