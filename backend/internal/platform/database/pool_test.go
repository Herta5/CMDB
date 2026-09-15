// 本文件验证数据库连接池真实连接数量受到运行参数限制。
package database

import (
	"cmdb/internal/platform/config"
	"context"
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"testing"
	"time"
)

func TestConnectionPoolLimitsActiveAndIdleConnections(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("创建连接池测试失败")
	}
	defer db.Close()
	configurePool(db, config.Database{MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Minute})
	first, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("获取首连接失败")
	}
	second, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("获取第二连接失败")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	extra, err := db.Conn(ctx)
	if err == nil {
		extra.Close()
		t.Fatal("超过连接池上限的请求不得取得连接")
	}
	first.Close()
	second.Close()
	stats := db.Stats()
	if stats.MaxOpenConnections != 2 || stats.Idle > 1 {
		t.Fatal("连接池未应用连接上限或空闲上限")
	}
}
