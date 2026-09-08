package service

import (
	"strings"
	"testing"

	"github-cmdb/internal/model"
)

func TestNormalizeCISourcePreservesAllowedValuesAndDefaultsEmpty(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "import", source: "import", want: "import"},
		{name: "auto discovery", source: "auto_discovery", want: "auto_discovery"},
		{name: "empty defaults to manual", source: "", want: "manual"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeCISource(tt.source); got != tt.want {
				t.Fatalf("NormalizeCISource(%q) = %q, want %q", tt.source, got, tt.want)
			}
		})
	}
}

func TestCIInstanceCreateRejectsUnsupportedSource(t *testing.T) {
	svc := NewCIInstanceSvc(nil, nil, nil)

	err := svc.Create(&model.CIInstance{Source: "spreadsheet"})

	if err == nil {
		t.Fatal("Create() accepted an unsupported source")
	}
	if !strings.Contains(err.Error(), "unsupported ci source") {
		t.Fatalf("Create() error = %q, want unsupported source error", err)
	}
}
