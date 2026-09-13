// 本文件验证三类资产快照转换为稳定数据库字段时的规范化行为。
package resource

import (
	"encoding/json"
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
