package model

import (
	"fmt"
	"gorm.io/gorm"
)

// MigrateRelationRuleUnique replaces legacy name-only uniqueness without
// deleting any rule. The new constraint is added before the old one is removed.
func MigrateRelationRuleUnique(db *gorm.DB) error {
	m := db.Migrator()
	if !m.HasTable(&CIRelationRule{}) {
		return nil
	}
	var duplicates []struct {
		Name         string
		SourceTypeID uint64
		TargetTypeID uint64
	}
	if err := db.Model(&CIRelationRule{}).Select("name, source_type_id, target_type_id").Group("name, source_type_id, target_type_id").Having("COUNT(*) > 1").Limit(1).Find(&duplicates).Error; err != nil {
		return err
	}
	if len(duplicates) > 0 {
		return fmt.Errorf("duplicate relation rule (%s, %d, %d); resolve duplicates before migrating", duplicates[0].Name, duplicates[0].SourceTypeID, duplicates[0].TargetTypeID)
	}
	if !m.HasIndex(&CIRelationRule{}, "uk_relation_rule_types") {
		if err := m.CreateIndex(&CIRelationRule{}, "uk_relation_rule_types"); err != nil {
			return err
		}
	}
	indexes, err := m.GetIndexes(&CIRelationRule{})
	if err != nil {
		return err
	}
	for _, index := range indexes {
		unique, _ := index.Unique()
		columns := index.Columns()
		if unique && len(columns) == 1 && columns[0] == "name" {
			if err := m.DropIndex(&CIRelationRule{}, index.Name()); err != nil {
				return err
			}
		}
	}
	return nil
}
