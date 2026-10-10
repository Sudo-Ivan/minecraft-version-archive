package integrity

import (
	"os"
	"path/filepath"
	"testing"

	"minecraft-version-archive/internal/safepath"
)

func TestVerifySHA256Bytes(t *testing.T) {
	data := []byte("temurin archive")
	spec := Spec{
		SHA256: "8f2c0e8e5f3b1d4a6c7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9",
		Size:   int64(len(data)),
	}
	if err := VerifyBytes(data, spec); err == nil {
		t.Fatal("expected mismatch with placeholder hash")
	}

	root, err := safepath.OpenDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root.Path(), "jdk.tar.gz")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, size, err := FileSHA256(root, path)
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(data)) {
		t.Fatalf("size = %d", size)
	}
	if err := VerifyFile(root, path, Spec{SHA256: got, Size: size}); err != nil {
		t.Fatal(err)
	}
}
