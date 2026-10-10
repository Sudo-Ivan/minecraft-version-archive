package fetch

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"minecraft-version-archive/internal/safepath"
	"minecraft-version-archive/internal/stats"
)

func testDataDir(t *testing.T) (*safepath.Dir, string) {
	t.Helper()
	dir := t.TempDir()
	root, err := safepath.OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return root, dir
}

func TestDownloadResume(t *testing.T) {
	var served atomic.Uint64
	body := []byte("abcdefghijklmnopqrstuvwxyz")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served.Add(1)
		if rg := r.Header.Get("Range"); rg != "" {
			w.Header().Set("Content-Range", "bytes 10-25/26")
			w.WriteHeader(http.StatusPartialContent)
			w.Write(body[10:])
			return
		}
		w.Write(body)
	}))
	defer srv.Close()

	dir, root := testDataDir(t)
	dest := filepath.Join(root, "file.bin")
	part := dest + ".part"
	if err := os.WriteFile(part, body[:10], 0o644); err != nil {
		t.Fatal(err)
	}

	client := New(3, 10).WithDataDir(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Download(ctx, srv.URL, dest); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("got %q want %q", got, body)
	}
}

func TestDownloadSkipsExisting(t *testing.T) {
	dir, root := testDataDir(t)
	dest := filepath.Join(root, "exists.bin")
	content := []byte("already")
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		t.Fatal(err)
	}

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte("new"))
	}))
	defer srv.Close()

	client := New(1, 10).WithDataDir(dir)
	if err := client.Download(context.Background(), srv.URL, dest); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("server called %d times", calls)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(content) {
		t.Fatalf("file modified: %q", got)
	}
}

func TestGetRetriesOnServerError(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	client := New(5, 1)
	data, err := client.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ok" || attempts != 3 {
		t.Fatalf("data=%q attempts=%d", data, attempts)
	}
}

func TestDownloadRecordsStats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("1234567890"))
	}))
	defer srv.Close()

	st := stats.New()
	dir, root := testDataDir(t)
	client := New(2, 1).WithStats(st).WithDataDir(dir)
	dest := filepath.Join(root, "stat.bin")
	if err := client.Download(context.Background(), srv.URL, dest); err != nil {
		t.Fatal(err)
	}
	snap := st.Snapshot(0, 0, 0, 0)
	if snap.ArchiveDownloads != 1 || snap.ArchiveBytes != 10 {
		t.Fatalf("downloads=%d bytes=%d", snap.ArchiveDownloads, snap.ArchiveBytes)
	}
}

func TestDownloadLockEvicted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))
	defer srv.Close()

	dir, root := testDataDir(t)
	client := New(2, 5).WithDataDir(dir)
	for i := range 3 {
		dest := filepath.Join(root, "f"+string(rune('a'+i))+".bin")
		if err := client.Download(context.Background(), srv.URL, dest); err != nil {
			t.Fatal(err)
		}
	}
	if len(client.locks) != 0 {
		t.Fatalf("locks map has %d entries after downloads", len(client.locks))
	}
}

func TestDoStreamsAndRelays(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Range") == "bytes=0-3" {
			w.Header().Set("Content-Range", "bytes 0-3/10")
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("0123"))
			return
		}
		w.Write([]byte("0123456789"))
	}))
	defer srv.Close()

	client := New(2, 5)
	resp, err := client.Do(context.Background(), srv.URL+"/missing", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	fwd := http.Header{"Range": []string{"bytes=0-3"}}
	resp, err = client.Do(context.Background(), srv.URL+"/ok", fwd)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "0123" {
		t.Fatalf("body = %q", body)
	}
}

func TestConcurrentDownloadSameDest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("concurrent-ok"))
	}))
	defer srv.Close()

	dir, root := testDataDir(t)
	client := New(3, 5).WithDataDir(dir)
	dest := filepath.Join(root, "same.bin")
	done := make(chan error, 10)
	for range 10 {
		go func() {
			done <- client.Download(context.Background(), srv.URL, dest)
		}()
	}
	for range 10 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "concurrent-ok" {
		t.Fatalf("got %q", got)
	}
}
