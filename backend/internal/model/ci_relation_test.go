package model

import (
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestRelationRuleUniqueConstraintAcceptsSeedTypeCombinations(t *testing.T) {
	s, err := schema.Parse(&CIRelationRule{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	var uniqueFields []string
	for _, index := range s.ParseIndexes() {
		if index.Class != "UNIQUE" {
			continue
		}
		for _, field := range index.Fields {
			uniqueFields = append(uniqueFields, field.DBName)
		}
	}
	if got := strings.Join(uniqueFields, ","); got != "name,source_type_id,target_type_id" {
		t.Fatalf("unique constraint = %s; same names must be accepted for different source/target types", got)
	}
	// Check actual seed rows against the model's uniqueness contract.
	seed, err := os.ReadFile("../../migrations/001_seed.sql")
	if err != nil {
		t.Fatal(err)
	}
	relationSeed := strings.SplitN(string(seed), "INSERT INTO ci_relation_rule", 2)[1]
	rows := regexp.MustCompile(`(?m)^\(\d+,\s*'([^']+)',\s*'[^']+',\s*'[^']+',\s*(\d+),\s*(\d+),`).FindAllStringSubmatch(relationSeed, -1)
	if len(rows) == 0 {
		t.Fatal("no relation seed rows found")
	}
	seen := map[string]bool{}
	for _, row := range rows {
		key := strings.Join(row[1:4], ":")
		if seen[key] {
			t.Fatalf("seed duplicates composite relation key %s", key)
		}
		seen[key] = true
	}
	if !seen["runs_on:12:11"] || !seen["runs_on:13:11"] {
		t.Fatal("expected both runs_on seed combinations")
	}
}
