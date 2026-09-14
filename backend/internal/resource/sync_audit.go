// 本文件构建一次同步内的资源变化审计快照，不负责数据库写入。
package resource

import "sort"

const (
	syncChangeCreated  = "created"
	syncChangeUpdated  = "updated"
	syncChangeRestored = "restored"
	syncChangeLost     = "lost"
	syncChangeDeleted  = "deleted"
)

var syncChangeOrder = []string{
	syncChangeCreated, syncChangeUpdated, syncChangeRestored, syncChangeLost, syncChangeDeleted,
}

type syncAuditChanges map[string]map[string][]string

func newSyncAuditChanges() syncAuditChanges {
	changes := make(syncAuditChanges, len(syncChangeOrder))
	for _, action := range syncChangeOrder {
		changes[action] = map[string][]string{}
	}
	return changes
}

func (changes syncAuditChanges) add(action, resourceType, externalID string) {
	changes[action][resourceType] = append(changes[action][resourceType], externalID)
}

func (changes syncAuditChanges) snapshot() syncAuditChanges {
	result := newSyncAuditChanges()
	for _, action := range syncChangeOrder {
		for resourceType, ids := range changes[action] {
			result[action][resourceType] = append([]string(nil), ids...)
			sort.Strings(result[action][resourceType])
		}
	}
	return result
}
