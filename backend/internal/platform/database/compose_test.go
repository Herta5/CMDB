// Package database 验证 CMDB Docker Compose 部署使用 PostgreSQL 17 的运行边界。
package database

import (
	"os"
	"strings"
	"testing"
)

const composeDefinitionPath = "../../../../docker-compose.yml"

// TestPostgreSQLComposeDefinesIsolatedDeployment 防止部署配置回退为 MySQL，或绕过受限应用账号初始化。
func TestPostgreSQLComposeDefinesIsolatedDeployment(t *testing.T) {
	compose := string(readComposeDefinition(t))

	for _, fragment := range []string{
		"postgresql:\n    image: postgres:17",
		`- "5432:5432"`,
		"POSTGRES_USER: postgres",
		"POSTGRES_PASSWORD: ${POSTGRES_ADMIN_PASSWORD:?请设置 POSTGRES_ADMIN_PASSWORD}",
		"POSTGRES_DB: cmdb\n      DB_PASSWORD: ${DB_PASSWORD:?请设置 DB_PASSWORD}",
		"cmdb-postgresql-data:/var/lib/postgresql/data",
		`test: ["CMD-SHELL", "pg_isready -U postgres -d cmdb"]`,
		"DB_HOST: postgresql",
		`DB_PORT: "5432"`,
		"postgresql:\n        condition: service_healthy",
	} {
		if !strings.Contains(compose, fragment) {
			t.Errorf("PostgreSQL Compose 部署缺少必要约束：%s", fragment)
		}
	}

	firstInitialization := strings.Index(compose, "./backend/database/init/001_create_app_role.sh:/docker-entrypoint-initdb.d/001_create_app_role.sh:ro")
	secondInitialization := strings.Index(compose, "./backend/database/init/002_schema.sql:/docker-entrypoint-initdb.d/002_schema.sql:ro")
	if firstInitialization < 0 || secondInitialization < 0 || firstInitialization > secondInitialization {
		t.Error("PostgreSQL 初始化脚本必须以只读方式按账号创建、结构初始化的顺序挂载")
	}

	for _, forbidden := range []string{"mysql:", "mysql:8.4", "3306", "cmdb-mysql-data", "./backend/migrations/"} {
		if strings.Contains(compose, forbidden) {
			t.Errorf("PostgreSQL Compose 部署不得保留 MySQL 遗留配置：%s", forbidden)
		}
	}
}

// readComposeDefinition 统一读取仓库根目录的部署配置，并保留中文失败上下文。
func readComposeDefinition(t *testing.T) []byte {
	t.Helper()
	content, err := os.ReadFile(composeDefinitionPath)
	if err != nil {
		t.Fatalf("读取 Docker Compose 部署配置失败：%v", err)
	}
	return content
}
