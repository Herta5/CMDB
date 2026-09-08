-- Run against an existing CMDB database before importing/re-importing seeds.
-- The server also performs this compatibility migration at startup.
DELIMITER //
DROP PROCEDURE IF EXISTS migrate_relation_rule_unique//
CREATE PROCEDURE migrate_relation_rule_unique()
BEGIN
  DECLARE legacy_index VARCHAR(128);
  IF EXISTS (SELECT 1 FROM ci_relation_rule GROUP BY name, source_type_id, target_type_id HAVING COUNT(*) > 1) THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Duplicate relation rule (name, source_type_id, target_type_id); resolve duplicates before migrating';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'ci_relation_rule' AND index_name = 'uk_relation_rule_types') THEN
    ALTER TABLE ci_relation_rule ADD UNIQUE INDEX uk_relation_rule_types (name, source_type_id, target_type_id);
  END IF;
  SELECT MIN(index_name) INTO legacy_index FROM (
    SELECT index_name FROM information_schema.statistics
    WHERE table_schema = DATABASE() AND table_name = 'ci_relation_rule' AND non_unique = 0 AND index_name <> 'PRIMARY'
    GROUP BY index_name HAVING COUNT(*) = 1 AND MAX(column_name) = 'name'
  ) AS legacy;
  WHILE legacy_index IS NOT NULL DO
    SET @drop_relation_index = CONCAT('ALTER TABLE ci_relation_rule DROP INDEX `', REPLACE(legacy_index, '`', '``'), '`');
    PREPARE drop_relation_index FROM @drop_relation_index;
    EXECUTE drop_relation_index;
    DEALLOCATE PREPARE drop_relation_index;
    SELECT MIN(index_name) INTO legacy_index FROM (
      SELECT index_name FROM information_schema.statistics
      WHERE table_schema = DATABASE() AND table_name = 'ci_relation_rule' AND non_unique = 0 AND index_name <> 'PRIMARY'
      GROUP BY index_name HAVING COUNT(*) = 1 AND MAX(column_name) = 'name'
    ) AS legacy;
  END WHILE;
END//
CALL migrate_relation_rule_unique()//
DROP PROCEDURE migrate_relation_rule_unique//
DELIMITER ;
