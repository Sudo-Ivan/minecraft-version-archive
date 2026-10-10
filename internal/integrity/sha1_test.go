package integrity

import (
	"os"
	"path/filepath"
	"testing"

	"minecraft-version-archive/internal/safepath"
)

func TestVerifyBytes(t *testing.T) {
	data := []byte("hello world")
	spec := Spec{SHA1: "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed", Size: int64(len(data))}
	if err := VerifyBytes(data, spec); err != nil {
		t.Fatal(err)
	}
	bad := Spec{SHA1: "0000000000000000000000000000000000000000", Size: int64(len(data))}
	if err := VerifyBytes(data, bad); err == nil {
		t.Fatal("expected sha1 mismatch")
	}
}

func TestVerifyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.bin")
	data := []byte("minecraft jar data")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := safepath.OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	spec := Spec{SHA1: "a3b5c2d1e4f60718293a4b5c6d7e8f9012345678", Size: 0}
	if err := VerifyFile(root, path, spec); err == nil {
		t.Fatal("expected mismatch with wrong sha1")
	}

	got, size, err := FileDigest(root, path)
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(data)) {
		t.Fatalf("size = %d", size)
	}
	if err := VerifyFile(root, path, Spec{SHA1: got, Size: size}); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyFileSkipsWhenUnknown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := safepath.OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(root, path, Spec{}); err != nil {
		t.Fatal(err)
	}
}
