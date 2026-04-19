package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanModules_ReadsImageInfo(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("atmosphere-frontend/values.yaml", "image:\n  repository: harbor/a\n  tag: \"v1\"\n")
	write("base-client-backend/values.yaml", "image:\n  repository: harbor/b\n  tag: \"v2\"\n")
	_ = os.MkdirAll(filepath.Join(dir, "no-values"), 0o755)

	list, err := ScanModulesFromDir(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 modules, got %d: %+v", len(list), list)
	}
	if list[0].Name != "atmosphere-frontend" || list[1].Name != "base-client-backend" {
		t.Fatalf("order/name wrong: %+v", list)
	}
	if list[0].ImageRepository != "harbor/a" || list[0].CurrentTag != "v1" {
		t.Fatalf("bad first: %+v", list[0])
	}
}
