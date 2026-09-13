// 本文件验证三类资产快照转换为稳定数据库字段时的规范化行为。
package resource

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestSnapshotBusinessColumnsUsesEmptyDiskArray 防止无云盘服务器写入 JSON null 并违反数组契约。
func TestSnapshotBusinessColumnsUsesEmptyDiskArray(t *testing.T) {
	columns := snapshotBusinessColumns("resources_servers", Snapshot{})
	disks, ok := columns["disks"].([]byte)
	if !ok || string(disks) != "[]" {
		t.Fatalf("无云盘服务器必须持久化空数组：%s", disks)
	}
}

// TestResourceJSONKeepsEmptyDiskArray 防止无云盘服务器的接口字段因空数组被省略。
func TestResourceJSONKeepsEmptyDiskArray(t *testing.T) {
	value, err := json.Marshal(Resource{Disks: []ServerDisk{}})
	if err != nil || !strings.Contains(string(value), `"disks":[]`) {
		t.Fatalf("统一资源接口必须稳定返回空磁盘数组：json=%s err=%v", value, err)
	}
}

// TestResourceJSONOmitsDatabaseStorageFromServer 防止数据库专属存储容量污染服务器统一视图。
func TestResourceJSONOmitsDatabaseStorageFromServer(t *testing.T) {
	value, err := json.Marshal(Resource{AssetBase: AssetBase{ResourceType: "ec2"}, Disks: []ServerDisk{}})
	if err != nil {
		t.Fatalf("序列化服务器资源失败：%v", err)
	}
	if strings.Contains(string(value), `"storage_size_gib"`) || strings.Contains(string(value), `"storage_type"`) {
		t.Fatalf("服务器资源不得混入数据库存储规格：%s", value)
	}
}

// TestServerDiskSetsTreatNullAsEmpty 防止无磁盘的旧空值被误判为服务器配置变化。
func TestServerDiskSetsTreatNullAsEmpty(t *testing.T) {
	if !jsonServerDiskSetsEqual(nil, []byte("[]")) || !jsonServerDiskSetsEqual([]byte("null"), []byte("[]")) {
		t.Fatal("NULL、JSON null 和空磁盘数组必须具有相同业务语义")
	}
}

// TestSnapshotBusinessColumnsNormalizesDisks 防止云盘顺序和重复项制造虚假更新。
func TestSnapshotBusinessColumnsNormalizesDisks(t *testing.T) {
	columns := snapshotBusinessColumns("resources_servers", Snapshot{Disks: []ServerDisk{
		{ID: "d-root", Kind: "system", SizeGiB: 40},
		{ID: "d-data", Kind: "data", SizeGiB: 100},
		{ID: "d-data", Kind: "data", SizeGiB: 100},
	}})
	disks := columns["disks"].([]byte)
	want := `[{"id":"d-data","kind":"data","type":"","size_gib":100,"device":"","encrypted":false},{"id":"d-root","kind":"system","type":"","size_gib":40,"device":"","encrypted":false}]`
	if string(disks) != want {
		t.Fatalf("磁盘必须按 ID 排序去重：got=%s want=%s", disks, want)
	}
}

// TestDatabaseSnapshotBusinessColumnsKeepsSpecification 防止 RDS 规格采集成功后在共享持久化层被丢弃。
func TestDatabaseSnapshotBusinessColumnsKeepsSpecification(t *testing.T) {
	columns := snapshotBusinessColumns("resources_databases", Snapshot{
		InstanceType:   "db.r6g.large",
		VCPU:           2,
		Memory:         16384,
		StorageType:    "gp3",
		StorageSizeGiB: 200,
	})
	want := map[string]any{
		"instance_type":    "db.r6g.large",
		"vcpu":             2,
		"memory":           int64(16384),
		"storage_type":     "gp3",
		"storage_size_gib": int64(200),
	}
	for column, expected := range want {
		if !reflect.DeepEqual(columns[column], expected) {
			t.Fatalf("RDS 规格字段必须进入持久化列：column=%s got=%v want=%v", column, columns[column], expected)
		}
	}
}

// TestDatabaseSnapshotBusinessColumnsUsesNullForUnknownCapacity 防止未知规格被持久化为具有误导性的零容量。
func TestDatabaseSnapshotBusinessColumnsUsesNullForUnknownCapacity(t *testing.T) {
	columns := snapshotBusinessColumns("resources_databases", Snapshot{InstanceType: "db.serverless"})
	for _, column := range []string{"vcpu", "memory", "storage_size_gib"} {
		if columns[column] != nil {
			t.Fatalf("未知 RDS 规格必须持久化为 NULL：column=%s got=%v", column, columns[column])
		}
	}
}

// TestResourceFromDatabaseRowKeepsStorageSpecification 防止数据库行转换为统一 API 视图时丢失存储规格。
func TestResourceFromDatabaseRowKeepsStorageSpecification(t *testing.T) {
	value := resourceFromRow(assetRow{
		AssetBase:      AssetBase{ResourceType: "rds"},
		StorageType:    "gp3",
		StorageSizeGiB: sql.NullInt64{Int64: 200, Valid: true},
	}, "resources_databases")
	if value.StorageType != "gp3" || value.StorageSizeGiB == nil || *value.StorageSizeGiB != 200 {
		t.Fatalf("统一资源视图必须返回 RDS 存储规格：%+v", value)
	}
}
