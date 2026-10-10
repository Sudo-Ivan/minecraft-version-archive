package fetch

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"minecraft-version-archive/internal/integrity"
	"minecraft-version-archive/internal/safepath"
)

func TestDownloadVerified(t *testing.T) {
	body := []byte("verified-content")
	sum := sha1.Sum(body)
	spec := integrity.Spec{SHA1: hex.EncodeToString(sum[:]), Size: int64(len(body))}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "verified.bin")
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

func TestDownloadVerifiedRetriesOnMismatch(t *testing.T) {
	body := []byte("good")
	sum := sha1.Sum(body)
	spec := integrity.Spec{SHA1: hex.EncodeToString(sum[:]), Size: int64(len(body))}

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Write([]byte("bad-data"))
			return
		}
		w.Write(body)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "retry.bin")
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

func TestDownloadVerifiedUnknownSpec(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("any"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "plain.bin")
	dir, err := safepath.OpenDir(filepath.Dir(dest))
	if err != nil {
		t.Fatal(err)
	}
	client := New(1, 1).WithDataDir(dir)
	if err := client.DownloadVerified(context.Background(), srv.URL, dest, integrity.Spec{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "any" {
		t.Fatalf("got %q", got)
	}
}
