package store

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestExportPartialVersions(t *testing.T) {
	src := t.TempDir()
	layout := New(src)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	manifest := `{"latest":{"release":"1.0"},"versions":[{"id":"1.0","type":"release"},{"id":"2.0","type":"release"}]}`
	if err := os.WriteFile(layout.ManifestPath(), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	v1 := `{"id":"1.0","libraries":[],"assetIndex":{"id":"idx1"}}`
	v2 := `{"id":"2.0","libraries":[],"assetIndex":{"id":"idx2"}}`
	if err := os.WriteFile(layout.VersionJSON("1.0"), []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.VersionJSON("2.0"), []byte(v2), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.ClientJar("1.0"), []byte("jar1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.ClientJar("2.0"), []byte("jar2"), 0o644); err != nil {
		t.Fatal(err)
	}

	zipPath := filepath.Join(t.TempDir(), "partial.zip")
	result, err := ExportZip(src, zipPath, ExportOptions{Versions: []string{"1.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Files < 3 {
		t.Fatalf("files = %d", result.Files)
	}

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	names := make(map[string]struct{})
	for _, f := range r.File {
		names[f.Name] = struct{}{}
	}
	if _, ok := names["versions/1.0.json"]; !ok {
		t.Fatalf("names = %v", names)
	}
	if _, ok := names["jars/client/1.0.jar"]; !ok {
		t.Fatalf("names = %v", names)
	}
	if _, ok := names["versions/2.0.json"]; ok {
		t.Fatal("2.0 should be excluded")
	}
	if _, ok := names["jars/client/2.0.jar"]; ok {
		t.Fatal("2.0 jar should be excluded")
	}
}

func TestCollectExportPaths(t *testing.T) {
	src := t.TempDir()
	layout := New(src)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	version := `{"id":"1.0","libraries":[{"downloads":{"artifact":{"url":"https://libraries.minecraft.net/com/example/lib.jar"}}}],"assetIndex":{"id":"idx"}}`
	if err := os.WriteFile(layout.VersionJSON("1.0"), []byte(version), 0o644); err != nil {
		t.Fatal(err)
	}
	libDir := filepath.Dir(layout.LibraryPath("com/example/lib.jar"))
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.LibraryPath("com/example/lib.jar"), []byte("lib"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := CollectExportPaths(layout, []string{"1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := paths["versions/1.0.json"]; !ok {
		t.Fatalf("paths = %v", paths)
	}
	if _, ok := paths["libraries/com/example/lib.jar"]; !ok {
		t.Fatalf("paths = %v", paths)
	}
}
