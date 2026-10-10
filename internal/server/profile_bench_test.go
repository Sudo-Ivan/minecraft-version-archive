package server

import (
	"crypto/sha1" //#nosec G505 -- Mojang artifact checksums use SHA-1.
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"minecraft-version-archive/internal/archive"
	"minecraft-version-archive/internal/config"
	"minecraft-version-archive/internal/stats"
	"minecraft-version-archive/internal/store"
)

func benchServer(b *testing.B) (*httptest.Server, *store.Layout, string) {
	b.Helper()
	dir := b.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		b.Fatal(err)
	}
	manifest := `{"latest":{"release":"1.0"},"versions":[{"id":"1.0","type":"release","url":"http://example/1.0.json"}]}`
	if err := os.WriteFile(layout.ManifestPath(), []byte(manifest), 0o644); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(layout.VersionJSON("1.0"), []byte(`{"id":"1.0"}`), 0o644); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(layout.ClientJar("1.0"), make([]byte, 2<<20), 0o644); err != nil {
		b.Fatal(err)
	}
	content := []byte("asset-object-benchmark-content")
	sum := sha1.Sum(content) //#nosec G401 -- Mojang artifact checksums use SHA-1.
	hash := hex.EncodeToString(sum[:])
	objPath := layout.AssetObjectPath(hash)
	if err := os.MkdirAll(filepath.Dir(objPath), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(objPath, content, 0o644); err != nil {
		b.Fatal(err)
	}
	cfg := &config.Config{
		DataDir:      dir,
		PublicURL:    "http://test",
		CacheEnabled: true,
		CacheEntries: 64,
		CacheBytes:   64 << 20,
		CacheShards:  64,
	}
	log := slog.New(slog.DiscardHandler)
	srv := New(cfg, archive.NewWithStats(cfg, log, stats.New()), log)
	return httptest.NewServer(srv.Handler()), layout, hash
}

func benchGet(b *testing.B, url string) {
	b.Helper()
	client := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 256}}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := client.Get(url)
			if err != nil {
				b.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	})
}

func BenchmarkHTTPServeJar(b *testing.B) {
	ts, _, _ := benchServer(b)
	defer ts.Close()
	benchGet(b, ts.URL+"/jars/client/1.0.jar")
}

func BenchmarkHTTPServeAssetObject(b *testing.B) {
	ts, _, hash := benchServer(b)
	defer ts.Close()
	benchGet(b, ts.URL+"/assets/objects/"+hash[:2]+"/"+hash)
}

func BenchmarkHTTPManifest(b *testing.B) {
	ts, _, _ := benchServer(b)
	defer ts.Close()
	benchGet(b, ts.URL+"/mc/game/version_manifest_v2.json")
}
