// Package database 验证 CMDB Docker Compose 部署使用 PostgreSQL 17 的运行边界。
package database

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const composeDefinitionPath = "../../../../docker-compose.yml"

const postgreSQLEntrypointPath = "../../../database/entrypoint/validate_before_init.sh"

// TestPostgreSQLComposeDefinesIsolatedDeployment 防止部署配置回退为 MySQL，或绕过受限应用账号初始化。
func TestPostgreSQLComposeDefinesIsolatedDeployment(t *testing.T) {
	compose := string(readComposeDefinition(t))

	for _, fragment := range []string{
		"postgresql:\n    image: postgres:17",
		`- "5432:5432"`,
		"POSTGRES_USER: postgres",
		"POSTGRES_PASSWORD: ${POSTGRES_ADMIN_PASSWORD:?请设置 POSTGRES_ADMIN_PASSWORD}",
		"POSTGRES_DB: cmdb\n      DB_PASSWORD: ${DB_PASSWORD:?请设置 DB_PASSWORD}",
		`entrypoint: ["/bin/sh", "/usr/local/bin/cmdb-postgresql-entrypoint.sh"]`,
		"./backend/database/entrypoint/validate_before_init.sh:/usr/local/bin/cmdb-postgresql-entrypoint.sh:ro",
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

// TestPostgreSQLEntrypointRejectsUnsafePasswordsBeforeInitializingDataDirectory 验证入口前校验不会留下半初始化数据目录或泄露凭证。
func TestPostgreSQLEntrypointRejectsUnsafePasswordsBeforeInitializingDataDirectory(t *testing.T) {
	const password = "仅用于测试的管理员密码"
	dataDirectory := filepath.Join(t.TempDir(), "postgresql-data")
	command := exec.Command("sh", postgreSQLEntrypointPath, "postgres")
	command.Env = append(os.Environ(), "PGDATA="+dataDirectory, "POSTGRES_PASSWORD="+password, "DB_PASSWORD="+password)

	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("管理员密码与应用密码相同时，PostgreSQL 入口必须拒绝启动")
	}
	if !strings.Contains(string(output), "POSTGRES_PASSWORD 与 DB_PASSWORD 不得相同") {
		t.Fatalf("PostgreSQL 入口必须明确拒绝相同密码，实际输出：%s", output)
	}
	if strings.Contains(string(output), password) {
		t.Fatal("PostgreSQL 入口拒绝不安全密码时不得回显凭证")
	}
	if _, statErr := os.Stat(dataDirectory); !os.IsNotExist(statErr) {
		t.Fatalf("PostgreSQL 入口拒绝不安全密码时不得初始化 PGDATA：%v", statErr)
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
