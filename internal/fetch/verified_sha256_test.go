package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"minecraft-version-archive/internal/integrity"
	"minecraft-version-archive/internal/safepath"
)

func TestDownloadVerifiedSHA256(t *testing.T) {
	body := []byte("temurin-jdk-payload")
	sum := sha256.Sum256(body)
	spec := integrity.FromSHA256(hex.EncodeToString(sum[:]), int64(len(body)))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "jdk.tar.gz")
	dir, err := safepath.OpenDir(filepath.Dir(dest))
	if err != nil {
		t.Fatal(err)
	}
	client := New(3, 1).WithDataDir(dir)
	if err := client.DownloadVerified(context.Background(), srv.URL, dest, spec); err != nil {
		t.Fatal(err)
	}
	if err := integrity.VerifyFile(dir, dest, spec); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadVerifiedSHA256RetriesOnMismatch(t *testing.T) {
	body := []byte("good-jdk")
	sum := sha256.Sum256(body)
	spec := integrity.FromSHA256(hex.EncodeToString(sum[:]), int64(len(body)))

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Write([]byte("corrupt"))
			return
		}
		w.Write(body)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "jdk.tar.gz")
	dir, err := safepath.OpenDir(filepath.Dir(dest))
	if err != nil {
		t.Fatal(err)
	}
	client := New(3, 1).WithDataDir(dir)
	if err := client.DownloadVerified(context.Background(), srv.URL, dest, spec); err != nil {
		t.Fatal(err)
	}
	if calls < 2 {
		t.Fatalf("expected retry, calls=%d", calls)
	}
}
