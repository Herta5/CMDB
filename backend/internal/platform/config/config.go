// Package config 负责加载 CMDB 服务运行所需的环境配置。
package config

import (
	"fmt"
	"os"
)

// Config 汇总服务启动阶段需要的配置，敏感字段只供内部依赖装配使用。
type Config struct {
	Database      Database
	JWTSecret     string
	EncryptionKey string
	Server        Server
}

// Database 描述连接 MySQL 所需的环境配置。
type Database struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
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

	return config, nil
}

// valueOrDefault 为非敏感的运行参数提供开发环境可用的默认值。
func valueOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
