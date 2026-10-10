package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"minecraft-version-archive/internal/archive"
	"minecraft-version-archive/internal/config"
	"minecraft-version-archive/internal/stats"
	"minecraft-version-archive/internal/store"
)

func javaTestServer(t *testing.T, versions map[string]string) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	for id, raw := range versions {
		if err := os.WriteFile(layout.VersionJSON(id), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{
		DataDir:      dir,
		PublicURL:    "http://test",
		CacheEnabled: false,
		InventoryTTL: 300,
	}
	log := slog.New(slog.DiscardHandler)
	archiver := archive.NewWithStats(cfg, log, stats.New())
	srv := New(cfg, archiver, log)
	return httptest.NewServer(srv.Handler())
}

func TestJavaVersionEndpoint(t *testing.T) {
	ts := javaTestServer(t, map[string]string{
		"1.0": `{"id":"1.0","javaVersion":{"component":"jre-legacy","majorVersion":8}}`,
	})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/java/1.0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"1.0","major_version":8,"component":"jre-legacy"}`
	if string(body) != want {
		t.Fatalf("body = %q", body)
	}

	resp2, err := http.Get(ts.URL + "/java/1.0")
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("cached status = %d", resp2.StatusCode)
	}
}

func TestJavaVersionAlias(t *testing.T) {
	ts := javaTestServer(t, map[string]string{
		"1.5": `{"id":"1.5","javaVersion":{"component":"jre-legacy","majorVersion":8}}`,
	})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/java/1.5.0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestJavaIndexEndpoint(t *testing.T) {
	ts := javaTestServer(t, map[string]string{
		"1.0": `{"id":"1.0","javaVersion":{"component":"jre-legacy","majorVersion":8}}`,
		"2.0": `{"id":"2.0","javaVersion":{"component":"java-runtime-gamma","majorVersion":17}}`,
	})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/java")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, `"major_version":8`) || !strings.Contains(s, `"major_version":17`) {
		t.Fatalf("body = %s", body)
	}
}

func TestJavaVersionMissing(t *testing.T) {
	ts := javaTestServer(t, nil)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/java/missing-version")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestJavaVersionNoJavaField(t *testing.T) {
	ts := javaTestServer(t, map[string]string{
		"1.0": `{"id":"1.0"}`,
	})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/java/1.0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func BenchmarkJavaResponseCacheHit(b *testing.B) {
	srv, _ := benchmarkJavaHandler(b)
	ctx := context.Background()
	if _, ok := srv.javaResponse(ctx, "1.0"); !ok {
		b.Fatal("warmup")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		srv.javaResponse(ctx, "1.0")
	}
}

func BenchmarkJavaVersionCached(b *testing.B) {
	srv, rec := benchmarkJavaHandler(b)
	req := httptest.NewRequest(http.MethodGet, "http://bench/java/1.0", nil)

	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		b.Fatalf("warmup status = %d", rec.Code)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec.Body.Reset()
		rec.Code = 0
		srv.Handler().ServeHTTP(rec, req)
	}
}

func BenchmarkJavaIndexCached(b *testing.B) {
	srv, rec := benchmarkJavaHandler(b)
	req := httptest.NewRequest(http.MethodGet, "http://bench/java", nil)

	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		b.Fatalf("warmup status = %d", rec.Code)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec.Body.Reset()
		rec.Code = 0
		srv.Handler().ServeHTTP(rec, req)
	}
}

func benchmarkJavaHandler(b *testing.B) (*Server, *httptest.ResponseRecorder) {
	b.Helper()
	dir := b.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		b.Fatal(err)
	}
	raw := `{"id":"1.0","javaVersion":{"component":"jre-legacy","majorVersion":8}}`
	if err := os.WriteFile(layout.VersionJSON("1.0"), []byte(raw), 0o644); err != nil {
		b.Fatal(err)
	}
	cfg := &config.Config{
		DataDir:      dir,
		PublicURL:    "http://bench",
		CacheEnabled: false,
		InventoryTTL: 3600,
	}
	log := slog.New(slog.DiscardHandler)
	archiver := archive.NewWithStats(cfg, log, stats.New())
	srv := New(cfg, archiver, log)
	_, _ = srv.javaResponse(context.Background(), "1.0")
	srv.cachedJavaIndex()
	return srv, httptest.NewRecorder()
}
