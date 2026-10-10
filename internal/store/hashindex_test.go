package store

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"minecraft-version-archive/internal/integrity"
)

func TestHashIndexRecordAndLookup(t *testing.T) {
	dir := t.TempDir()
	layout := New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}

	content := []byte("shared-library-bytes")
	sum := sha1.Sum(content)
	spec := integrity.Spec{SHA1: hex.EncodeToString(sum[:]), Size: int64(len(content))}

	canonical := layout.LibraryPath("com/example/shared.jar")
	if err := os.MkdirAll(filepath.Dir(canonical), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, content, 0o644); err != nil {
		t.Fatal(err)
	}

	idx, err := OpenHashIndex(layout)
	if err != nil {
		t.Fatal(err)
	}
	idx.Record(spec, canonical)

	other := layout.LibraryPath("org/other/copy.jar")
	found, ok := idx.Lookup(spec)
	if !ok || found != canonical {
		t.Fatalf("lookup = %q ok=%v", found, ok)
	}
	if err := LinkOrCopy(layout, found, other); err != nil {
		t.Fatal(err)
	}
	dataDir, err := layout.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := integrity.VerifyFile(dataDir, other, spec); err != nil {
		t.Fatal(err)
	}
}

func TestHashIndexBackfillAssets(t *testing.T) {
	dir := t.TempDir()
	layout := New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}

	hash := "09f4c51d57e5c7b7c771d9730abdaa24b20a2ea3"
	path := layout.AssetObjectPath(hash)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("client-data")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	idx, err := OpenHashIndex(layout)
	if err != nil {
		t.Fatal(err)
	}
	spec := integrity.Spec{SHA1: hash, Size: int64(len(content))}
	found, ok := idx.Lookup(spec)
	if !ok || found != path {
		t.Fatalf("lookup = %q ok=%v", found, ok)
	}
}

func TestLinkOrCopySamePath(t *testing.T) {
	dir := t.TempDir()
	layout := New(dir)
	path := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LinkOrCopy(layout, path, path); err != nil {
		t.Fatal(err)
	}
}
