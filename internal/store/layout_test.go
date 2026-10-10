package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLayoutPaths(t *testing.T) {
	l := New("/data/root")
	cases := map[string]string{
		"manifest": "/data/root/manifest/version_manifest_v2.json",
		"version":  "/data/root/versions/1.20.1.json",
		"client":   "/data/root/jars/client/1.20.1.jar",
		"server":   "/data/root/jars/server/1.20.1.jar",
		"library":  "/data/root/libraries/com/mojang/foo.jar",
		"index":    "/data/root/assets/indexes/1.20.json",
		"asset":    "/data/root/assets/objects/ab/abcd1234",
	}
	if l.ManifestPath() != cases["manifest"] {
		t.Fatalf("manifest = %q", l.ManifestPath())
	}
	if l.VersionJSON("1.20.1") != cases["version"] {
		t.Fatalf("version = %q", l.VersionJSON("1.20.1"))
	}
	if l.ClientJar("1.20.1") != cases["client"] {
		t.Fatalf("client = %q", l.ClientJar("1.20.1"))
	}
	if l.ServerJar("1.20.1") != cases["server"] {
		t.Fatalf("server = %q", l.ServerJar("1.20.1"))
	}
	if l.LibraryPath("com/mojang/foo.jar") != cases["library"] {
		t.Fatalf("library = %q", l.LibraryPath("com/mojang/foo.jar"))
	}
	if l.AssetIndexPath("1.20") != cases["index"] {
		t.Fatalf("index = %q", l.AssetIndexPath("1.20"))
	}
	if l.AssetObjectPath("abcd1234") != cases["asset"] {
		t.Fatalf("asset = %q", l.AssetObjectPath("abcd1234"))
	}
}

func TestEnsureCreatesDirs(t *testing.T) {
	dir := t.TempDir()
	l := New(dir)
	if err := l.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{l.ManifestDir(), l.VersionsDir(), l.ClientDir(), l.LibrariesDir()} {
		if !l.Exists(d) {
			t.Fatalf("dir missing: %s", d)
		}
	}
}

func TestCleanStaleParts(t *testing.T) {
	root := t.TempDir()
	l := New(root)
	if err := l.Ensure(); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(l.LibrariesDir(), "old.part")
	fresh := filepath.Join(l.LibrariesDir(), "new.part")
	normal := filepath.Join(l.LibrariesDir(), "keep.jar")
	for _, p := range []string{stale, fresh, normal} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if n := l.CleanStaleParts(time.Hour); n != 1 {
		t.Fatalf("expected 1 removed, got %d", n)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale .part still exists")
	}
	for _, p := range []string{fresh, normal} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s should still exist", p)
		}
	}
}
