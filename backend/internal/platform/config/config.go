// Package config 负责加载 CMDB 服务运行所需的环境配置。
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config 汇总服务启动阶段需要的配置，敏感字段只供内部依赖装配使用。
type Config struct {
	Database      Database
	JWTSecret     string
	EncryptionKey string
	Server        Server
	Sync          Sync
}

// Database 描述连接 PostgreSQL 所需的环境配置。
type Database struct {
	Host            string
	Port            string
	User            string
	Password        string
	Name            string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// Sync 限制整个 CMDB 进程中同时执行的同步数与单任务执行时长。
type Sync struct {
	MaxConcurrent int
	Timeout       time.Duration
}

// Server 描述 HTTP 服务监听配置；它不承载业务领域的路由定义。
type Server struct {
	Port string
	Mode string
}

// Load 从环境变量加载配置，并拒绝任何缺失的数据库或安全关键配置。
// 生产环境不得为凭证和密钥设置默认值，避免误启动时产生可预测的安全边界。
func Load() (Config, error) {
	config := Config{
		Database: Database{
			Host:     os.Getenv("DB_HOST"),
			Port:     os.Getenv("DB_PORT"),
			User:     os.Getenv("DB_USER"),
			Password: os.Getenv("DB_PASSWORD"),
			Name:     os.Getenv("DB_NAME"),
		},
		JWTSecret:     os.Getenv("JWT_SECRET"),
		EncryptionKey: os.Getenv("CMDB_ENCRYPTION_KEY"),
		Server: Server{
			Port: valueOrDefault("SERVER_PORT", "8080"),
			Mode: valueOrDefault("GIN_MODE", "debug"),
		},
	}

	for _, requirement := range []struct {
		name  string
		value string
	}{
		{name: "DB_HOST", value: config.Database.Host},
		{name: "DB_PORT", value: config.Database.Port},
		{name: "DB_USER", value: config.Database.User},
		{name: "DB_PASSWORD", value: config.Database.Password},
		{name: "DB_NAME", value: config.Database.Name},
		{name: "JWT_SECRET", value: config.JWTSecret},
		{name: "CMDB_ENCRYPTION_KEY", value: config.EncryptionKey},
	} {
		if requirement.value == "" {
			return Config{}, fmt.Errorf("缺少必需环境变量 %s", requirement.name)
		}
	}

	values := []struct {
		name               string
		fallback, min, max int
		target             *int
	}{
		{"CMDB_SYNC_MAX_CONCURRENT", 4, 1, 64, &config.Sync.MaxConcurrent},
		{"DB_MAX_OPEN_CONNS", 20, 1, 1000, &config.Database.MaxOpenConns},
		{"DB_MAX_IDLE_CONNS", 5, 0, 1000, &config.Database.MaxIdleConns},
	}
	for _, item := range values {
		value, err := boundedInteger(item.name, item.fallback, item.min, item.max)
		if err != nil {
			return Config{}, err
		}
		*item.target = value
	}
	if config.Database.MaxIdleConns > config.Database.MaxOpenConns {
		return Config{}, fmt.Errorf("DB_MAX_IDLE_CONNS 不得超过 DB_MAX_OPEN_CONNS")
	}
	timeout, err := boundedInteger("CMDB_SYNC_TIMEOUT_SECONDS", 900, 1, 86400)
	if err != nil {
		return Config{}, err
	}
	config.Sync.Timeout = time.Duration(timeout) * time.Second
	lifetime, err := boundedInteger("DB_CONN_MAX_LIFETIME_SECONDS", 1800, 1, 86400)
	if err != nil {
		return Config{}, err
	}
	config.Database.ConnMaxLifetime = time.Duration(lifetime) * time.Second

	return config, nil
}

// valueOrDefault 为非敏感的运行参数提供开发环境可用的默认值。
func valueOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// boundedInteger 只报告参数名，禁止把来自环境的任意输入回显到诊断输出。
func boundedInteger(name string, fallback, min, max int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return 0, fmt.Errorf("运行参数 %s 无效", name)
	}
	return value, nil
}
