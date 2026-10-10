package store

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"minecraft-version-archive/internal/constants"
)

func TestExportImportZip(t *testing.T) {
	src := t.TempDir()
	layout := New(src)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(layout.ManifestDir(), "version_manifest_v2.json")
	if err := os.WriteFile(manifest, []byte(`{"latest":{},"versions":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	version := filepath.Join(layout.VersionsDir(), "1.0.json")
	if err := os.WriteFile(version, []byte(`{"id":"1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	claimDir := layout.CoordinatorClaimsDir()
	if err := os.MkdirAll(claimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claimDir, "temp.claim"), []byte("claim"), 0o644); err != nil {
		t.Fatal(err)
	}

	zipPath := filepath.Join(t.TempDir(), "data.zip")
	exported, err := ExportZip(src, zipPath, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if exported.Files != 2 {
		t.Fatalf("exported files = %d", exported.Files)
	}

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]struct{})
	for _, f := range r.File {
		names[f.Name] = struct{}{}
	}
	r.Close()
	if _, ok := names["manifest/version_manifest_v2.json"]; !ok {
		t.Fatalf("names = %v", names)
	}
	if _, ok := names["coordinator/claims/temp.claim"]; ok {
		t.Fatal("claims should be excluded from export")
	}

	dest := t.TempDir()
	imported, err := ImportZip(dest, zipPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if imported.Files != 2 {
		t.Fatalf("imported files = %d", imported.Files)
	}
	got, err := os.ReadFile(filepath.Join(dest, "versions", "1.0.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"id":"1.0"}` {
		t.Fatalf("got %q", got)
	}
}

func TestImportZipRejectsTraversal(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "bad.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entry, err := w.Create("../escape.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(entry, "nope"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	f.Close()

	dest := t.TempDir()
	if _, err := ImportZip(dest, zipPath, false); err == nil {
		t.Fatal("expected zip slip error")
	}
}

func TestImportZipReplace(t *testing.T) {
	src := t.TempDir()
	layout := New(src)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(layout.VersionsDir(), "old.json")
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "data.zip")
	if _, err := ExportZip(src, zipPath, ExportOptions{}); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	other := filepath.Join(dest, "extra.txt")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportZip(dest, zipPath, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(other); err == nil {
		t.Fatal("replace should remove files outside archive")
	}
	if _, err := os.Stat(filepath.Join(dest, "versions", "old.json")); err != nil {
		t.Fatal(err)
	}
}

func TestZipEntryByteLimit(t *testing.T) {
	tests := []struct {
		name  string
		entry string
		want  int64
	}{
		{"client jar", "jars/client/1.21.1.jar", maxArchiveZipEntryBytes},
		{"server jar", "jars/server/1.21.1.jar", maxArchiveZipEntryBytes},
		{"library", "libraries/com/mojang/authlib/6.0.57/authlib-6.0.57.jar", maxArchiveZipEntryBytes},
		{"version json", "versions/1.21.1.json", maxArchiveZipEntryBytes},
		{"asset object", "assets/objects/ab/abcd1234", maxArchiveZipEntryBytes},
		{"fabric maven", "fabric/maven/net/fabricmc/fabric-loader/0.16.9/fabric-loader-0.16.9.jar", maxArchiveZipEntryBytes},
		{"manifest", "manifest/version_manifest_v2.json", maxArchiveZipEntryBytes},
		{"inventory index", "inventory/sha1_index.json", maxMiscZipEntryBytes},
		{"coordinator claim", "coordinator/claims/1.21.1.claim", maxMiscZipEntryBytes},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := zipEntryByteLimit(tc.entry); got != tc.want {
				t.Fatalf("zipEntryByteLimit(%q) = %d, want %d", tc.entry, got, tc.want)
			}
		})
	}
}

func TestImportZipAllowsLargeMinecraftPaths(t *testing.T) {
	const size = 32 << 20
	payload := bytes.Repeat([]byte{0x5a}, size)

	zipPath := filepath.Join(t.TempDir(), "large.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for _, name := range []string{
		"jars/client/1.21.1.jar",
		"jars/server/1.21.1.jar",
		"libraries/com/example/runtime/jvm.dll",
	} {
		hdr := &zip.FileHeader{Name: name, Method: zip.Store}
		entry, err := w.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	result, err := ImportZip(dest, zipPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 3 {
		t.Fatalf("imported files = %d", result.Files)
	}
	if result.Bytes != 3*size {
		t.Fatalf("imported bytes = %d", result.Bytes)
	}
	for _, rel := range []string{
		filepath.Join(constants.DirJars, constants.DirClient, "1.21.1.jar"),
		filepath.Join(constants.DirJars, constants.DirServer, "1.21.1.jar"),
		filepath.Join(constants.DirLibraries, "com/example/runtime/jvm.dll"),
	} {
		info, err := os.Stat(filepath.Join(dest, rel))
		if err != nil {
			t.Fatalf("stat %s: %v", rel, err)
		}
		if info.Size() != size {
			t.Fatalf("%s size = %d, want %d", rel, info.Size(), size)
		}
	}
}

func TestImportZipRejectsOversizedMiscEntry(t *testing.T) {
	const size = maxMiscZipEntryBytes + 1
	payload := bytes.Repeat([]byte{'x'}, int(size))

	zipPath := filepath.Join(t.TempDir(), "oversize.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entry, err := w.Create("inventory/sha1_index.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if _, err := ImportZip(dest, zipPath, false); err == nil {
		t.Fatal("expected oversize misc entry to be rejected")
	}
}
