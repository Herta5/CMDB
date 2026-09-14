// 本文件提供三类资产表的统一路由和行模型，避免同步与生命周期逻辑重复。
package resource

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"strings"
)

// assetRow 覆盖三张表的字段超集，仅在资源核心内部用于统一读写。
type assetRow struct {
	AssetBase
	SourceName     string
	ProjectName    string
	InstanceType   string
	VCPU           sql.NullInt64 `gorm:"column:vcpu"`
	Memory         sql.NullInt64
	PrivateIPs     json.RawMessage `gorm:"type:json"`
	PublicIPs      json.RawMessage `gorm:"type:json"`
	Disks          json.RawMessage `gorm:"type:json"`
	Endpoints      json.RawMessage `gorm:"type:json"`
	Engine         string
	EngineVersion  string
	StorageType    string
	StorageSizeGiB sql.NullInt64 `gorm:"column:storage_size_gib"`
	NetworkType    string
}

// assetTableForType 将受支持的云产品路由到固定表名；旧 elb 不再作为新资源类型产生。
func assetTableForType(resourceType string) (string, error) {
	switch strings.ToLower(resourceType) {
	case "ecs", "ec2":
		return "resources_servers", nil
	case "rds":
		return "resources_databases", nil
	case "slb", "clb", "alb", "nlb", "gwlb":
		return "resources_load_balancers", nil
	default:
		return "", fmt.Errorf("不支持的资源类型：%s", resourceType)
	}
}

// endpointColumns 把采集端点转换为目标表的 JSON 字段。
func endpointColumns(table string, endpoints []EndpointSnapshot) map[string]any {
	if table != "resources_servers" {
		encoded, _ := json.Marshal(normalizedEndpoints(endpoints))
		return map[string]any{"endpoints": encoded}
	}
	privateIPs, publicIPs := make([]string, 0), make([]string, 0)
	for _, endpoint := range endpoints {
		if endpoint.Kind == "private" {
			privateIPs = append(privateIPs, endpoint.Address)
		} else if endpoint.Kind == "public" {
			publicIPs = append(publicIPs, endpoint.Address)
		}
	}
	privateIPs = normalizedStringSet(privateIPs)
	publicIPs = normalizedStringSet(publicIPs)
	privateJSON, _ := json.Marshal(privateIPs)
	publicJSON, _ := json.Marshal(publicIPs)
	return map[string]any{"private_ips": privateJSON, "public_ips": publicJSON}
}

// normalizedEndpoints 将业务上无序的端点和解析 IP 转为稳定顺序，避免云 API 返回次序制造伪变化。
func normalizedEndpoints(endpoints []EndpointSnapshot) []EndpointSnapshot {
	values := make([]EndpointSnapshot, 0, len(endpoints))
	for _, endpoint := range endpoints {
		endpoint.ResolvedIPs = normalizedStringSet(endpoint.ResolvedIPs)
		values = append(values, endpoint)
	}
	sort.Slice(values, func(i, j int) bool {
		left, right := values[i], values[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Address != right.Address {
			return left.Address < right.Address
		}
		if left.Port != right.Port {
			return left.Port < right.Port
		}
		if left.Protocol != right.Protocol {
			return left.Protocol < right.Protocol
		}
		return strings.Join(left.ResolvedIPs, "\x00") < strings.Join(right.ResolvedIPs, "\x00")
	})
	unique := values[:0]
	for _, endpoint := range values {
		if len(unique) == 0 || !reflect.DeepEqual(unique[len(unique)-1], endpoint) {
			unique = append(unique, endpoint)
		}
	}
	return unique
}

// normalizedStringSet 对 IP 等无序字符串集合排序去重，并统一使用非空切片表达空集合。
func normalizedStringSet(values []string) []string {
	result := append([]string{}, values...)
	sort.Strings(result)
	unique := result[:0]
	for _, value := range result {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}
	return unique
}

// normalizedServerDisks 按稳定磁盘 ID 排序去重，避免云 API 返回顺序制造配置变化。
func normalizedServerDisks(values []ServerDisk) []ServerDisk {
	result := append([]ServerDisk{}, values...)
	sort.Slice(result, func(left, right int) bool {
		if result[left].ID != result[right].ID {
			return result[left].ID < result[right].ID
		}
		if result[left].Kind != result[right].Kind {
			return result[left].Kind < result[right].Kind
		}
		if result[left].Type != result[right].Type {
			return result[left].Type < result[right].Type
		}
		if result[left].SizeGiB != result[right].SizeGiB {
			return result[left].SizeGiB < result[right].SizeGiB
		}
		if result[left].Device != result[right].Device {
			return result[left].Device < result[right].Device
		}
		return !result[left].Encrypted && result[right].Encrypted
	})
	unique := result[:0]
	for _, disk := range result {
		if disk.ID != "" && (len(unique) == 0 || unique[len(unique)-1].ID != disk.ID) {
			unique = append(unique, disk)
		}
	}
	return unique
}

// snapshotBusinessColumns 将云端快照转换为资源表中的业务字段，不包含同步心跳和 CMDB 生命周期字段。
func snapshotBusinessColumns(table string, snapshot Snapshot) map[string]any {
	columns := map[string]any{
		"name":           snapshot.Name,
		"region":         snapshot.Region,
		"zone":           snapshot.Zone,
		"cloud_status":   snapshot.CloudStatus,
		"raw_attributes": snapshot.RawAttributes,
	}
	for key, value := range endpointColumns(table, snapshot.Endpoints) {
		columns[key] = value
	}
	if table == "resources_servers" {
		disks, _ := json.Marshal(normalizedServerDisks(snapshot.Disks))
		columns["instance_type"] = snapshot.InstanceType
		columns["vcpu"] = snapshot.VCPU
		columns["memory"] = snapshot.Memory
		columns["disks"] = disks
	}
	if table == "resources_databases" {
		columns["engine"] = snapshot.Engine
		columns["engine_version"] = snapshot.EngineVersion
		columns["instance_type"] = snapshot.InstanceType
		columns["vcpu"] = nullablePositiveInt(snapshot.VCPU)
		columns["memory"] = nullablePositiveInt64(snapshot.Memory)
		columns["storage_type"] = snapshot.StorageType
		columns["storage_size_gib"] = nullablePositiveInt64(snapshot.StorageSizeGiB)
	}
	if table == "resources_load_balancers" {
		columns["network_type"] = snapshot.NetworkType
	}
	return columns
}

// nullablePositiveInt 将采集契约中的零值转换为数据库 NULL，避免把未知规格解释为真实的零容量。
func nullablePositiveInt(value int) any {
	if value <= 0 {
		return nil
	}
	return value
}

// nullablePositiveInt64 为内存和存储容量提供相同的未知值语义。
func nullablePositiveInt64(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

// changedBusinessColumns 只返回与持久化资源不同的云端业务字段，避免重复快照重写宽表。
func changedBusinessColumns(existing assetRow, table string, snapshot Snapshot) map[string]any {
	desired := snapshotBusinessColumns(table, snapshot)
	changes := make(map[string]any)
	for key, value := range desired {
		changed := false
		switch key {
		case "name":
			changed = existing.Name != value
		case "region":
			changed = existing.Region != value
		case "zone":
			changed = existing.Zone != value
		case "cloud_status":
			changed = existing.CloudStatus != value
		case "instance_type":
			changed = existing.InstanceType != value
		case "vcpu":
			changed = !nullInt64Equals(existing.VCPU, value)
		case "memory":
			changed = !nullInt64Equals(existing.Memory, value)
		case "engine":
			changed = existing.Engine != value
		case "engine_version":
			changed = existing.EngineVersion != value
		case "storage_type":
			changed = existing.StorageType != value
		case "storage_size_gib":
			changed = !nullInt64Equals(existing.StorageSizeGiB, value)
		case "network_type":
			changed = existing.NetworkType != value
		case "raw_attributes":
			changed = !jsonValuesEqualForChanges(existing.RawAttributes, value.([]byte), snapshot.VolatileRawAttributeKeys)
		case "private_ips":
			changed = !jsonStringSetsEqual(existing.PrivateIPs, value.([]byte))
		case "public_ips":
			changed = !jsonStringSetsEqual(existing.PublicIPs, value.([]byte))
		case "disks":
			changed = !jsonServerDiskSetsEqual(existing.Disks, value.([]byte))
		case "endpoints":
			changed = !jsonEndpointSetsEqual(existing.Endpoints, value.([]byte))
		}
		if changed {
			changes[key] = value
		}
	}
	return changes
}

// nullInt64Equals 同时比较服务器的必填整数和数据库资产的可空容量字段。
func nullInt64Equals(existing sql.NullInt64, desired any) bool {
	if desired == nil {
		return !existing.Valid
	}
	var value int64
	switch typed := desired.(type) {
	case int:
		value = int64(typed)
	case int64:
		value = typed
	default:
		return false
	}
	return existing.Valid && existing.Int64 == value
}

// jsonValuesEqualForChanges 忽略采集器声明的顶层观测键，只用剩余原始属性判断是否发生业务配置变化。
func jsonValuesEqualForChanges(left, right []byte, volatileKeys []string) bool {
	if len(volatileKeys) == 0 {
		return jsonValuesEqual(left, right)
	}
	leftValue, leftOK := decodeJSONValue(left)
	rightValue, rightOK := decodeJSONValue(right)
	if !leftOK || !rightOK {
		return false
	}
	leftObject, leftIsObject := leftValue.(map[string]any)
	rightObject, rightIsObject := rightValue.(map[string]any)
	if !leftIsObject || !rightIsObject {
		return jsonDecodedValuesEqual(leftValue, rightValue)
	}
	for _, key := range volatileKeys {
		delete(leftObject, key)
		delete(rightObject, key)
	}
	return jsonDecodedValuesEqual(leftObject, rightObject)
}

// jsonStringSetsEqual 将旧版可能无序或为 null 的 IP 数组按集合语义比较，避免升级时产生一次性伪更新。
func jsonStringSetsEqual(left, right []byte) bool {
	leftValues, leftOK := decodeStringSet(left)
	rightValues, rightOK := decodeStringSet(right)
	return leftOK && rightOK && reflect.DeepEqual(leftValues, rightValues)
}

// decodeStringSet 解析并规范化 JSON 字符串集合，空值和 null 都视为业务空集合。
func decodeStringSet(value []byte) ([]string, bool) {
	value = bytes.TrimSpace(value)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return []string{}, true
	}
	var values []string
	if json.Unmarshal(value, &values) != nil {
		return nil, false
	}
	return normalizedStringSet(values), true
}

// jsonEndpointSetsEqual 同时规范化存量端点和新快照，兼容旧版本按云端原始顺序保存的数据。
func jsonEndpointSetsEqual(left, right []byte) bool {
	leftValues, leftOK := decodeEndpointSet(left)
	rightValues, rightOK := decodeEndpointSet(right)
	return leftOK && rightOK && reflect.DeepEqual(leftValues, rightValues)
}

// decodeEndpointSet 解析并规范化 JSON 端点集合，空值和 null 都视为业务空集合。
func decodeEndpointSet(value []byte) ([]EndpointSnapshot, bool) {
	value = bytes.TrimSpace(value)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return []EndpointSnapshot{}, true
	}
	var values []EndpointSnapshot
	if json.Unmarshal(value, &values) != nil {
		return nil, false
	}
	return normalizedEndpoints(values), true
}

// jsonServerDiskSetsEqual 将空值和磁盘顺序规范化后比较，防止空集合或返回顺序制造伪更新。
func jsonServerDiskSetsEqual(left, right []byte) bool {
	leftValues, leftOK := decodeServerDiskSet(left)
	rightValues, rightOK := decodeServerDiskSet(right)
	return leftOK && rightOK && reflect.DeepEqual(leftValues, rightValues)
}

// decodeServerDiskSet 解析服务器磁盘数组，空值和 null 都视为业务空集合。
func decodeServerDiskSet(value []byte) ([]ServerDisk, bool) {
	value = bytes.TrimSpace(value)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return []ServerDisk{}, true
	}
	var values []ServerDisk
	if json.Unmarshal(value, &values) != nil {
		return nil, false
	}
	return normalizedServerDisks(values), true
}

// jsonValuesEqual 按 JSON 值比较原始属性，忽略对象键顺序和无意义空白造成的伪变化。
func jsonValuesEqual(left, right []byte) bool {
	left = bytes.TrimSpace(left)
	right = bytes.TrimSpace(right)
	if bytes.Equal(left, right) {
		return true
	}
	leftValue, leftOK := decodeJSONValue(left)
	rightValue, rightOK := decodeJSONValue(right)
	if !leftOK || !rightOK {
		return false
	}
	return jsonDecodedValuesEqual(leftValue, rightValue)
}

// decodeJSONValue 使用任意精度数字解码 JSON，供完整比较和忽略易变键后的配置比较共用。
func decodeJSONValue(value []byte) (any, bool) {
	value = bytes.TrimSpace(value)
	if len(value) == 0 || !json.Valid(value) {
		return nil, false
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	if decoder.Decode(&decoded) != nil {
		return nil, false
	}
	return decoded, true
}

// jsonDecodedValuesEqual 递归比较 JSON 值，并用任意精度有理数避免大整数转换为 float64 后丢失变化。
func jsonDecodedValuesEqual(left, right any) bool {
	switch leftValue := left.(type) {
	case map[string]any:
		rightValue, ok := right.(map[string]any)
		if !ok || len(leftValue) != len(rightValue) {
			return false
		}
		for key, value := range leftValue {
			other, exists := rightValue[key]
			if !exists || !jsonDecodedValuesEqual(value, other) {
				return false
			}
		}
		return true
	case []any:
		rightValue, ok := right.([]any)
		if !ok || len(leftValue) != len(rightValue) {
			return false
		}
		for index := range leftValue {
			if !jsonDecodedValuesEqual(leftValue[index], rightValue[index]) {
				return false
			}
		}
		return true
	case json.Number:
		rightValue, ok := right.(json.Number)
		if !ok {
			return false
		}
		leftNumber, leftOK := new(big.Rat).SetString(leftValue.String())
		rightNumber, rightOK := new(big.Rat).SetString(rightValue.String())
		return leftOK && rightOK && leftNumber.Cmp(rightNumber) == 0
	default:
		return reflect.DeepEqual(left, right)
	}
}

// resourceFromRow 将各表的地址字段还原为稳定的统一 API 视图。
func resourceFromRow(row assetRow, table string) Resource {
	endpoints := make([]EndpointSnapshot, 0)
	disks := make([]ServerDisk, 0)
	if table == "resources_servers" {
		var privateIPs, publicIPs []string
		_ = json.Unmarshal(row.PrivateIPs, &privateIPs)
		_ = json.Unmarshal(row.PublicIPs, &publicIPs)
		for _, address := range privateIPs {
			endpoints = append(endpoints, EndpointSnapshot{Kind: "private", Address: address})
		}
		for _, address := range publicIPs {
			endpoints = append(endpoints, EndpointSnapshot{Kind: "public", Address: address})
		}
		_ = json.Unmarshal(row.Disks, &disks)
	} else if len(row.Endpoints) > 0 {
		_ = json.Unmarshal(row.Endpoints, &endpoints)
	}
	value := Resource{AssetBase: row.AssetBase, InstanceType: row.InstanceType, StorageType: row.StorageType, Endpoints: endpoints, Disks: disks}
	if row.VCPU.Valid {
		value.VCPU = int(row.VCPU.Int64)
	}
	if row.Memory.Valid {
		value.Memory = row.Memory.Int64
	}
	if row.StorageSizeGiB.Valid {
		storageSize := row.StorageSizeGiB.Int64
		value.StorageSizeGiB = &storageSize
	}
	return value
}
