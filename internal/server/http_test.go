package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"minecraft-version-archive/internal/archive"
	"minecraft-version-archive/internal/config"
	"minecraft-version-archive/internal/stats"
	"minecraft-version-archive/internal/store"
)

func testServer(t *testing.T, statsEnabled bool) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}

	manifest := `{"latest":{"release":"1.0","snapshot":"1.0"},"versions":[{"id":"1.0","type":"release","url":"http://example/1.0.json"}]}`
	if err := os.WriteFile(layout.ManifestPath(), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.VersionJSON("1.0"), []byte(`{"id":"1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	jarPath := layout.ClientJar("1.0")
	if err := os.WriteFile(jarPath, []byte("minecraft-jar-content-here"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		DataDir:      dir,
		PublicURL:    "http://test",
		CacheEnabled: true,
		CacheEntries: 8,
		CacheBytes:   1024 * 1024,
		StatsEnabled: statsEnabled,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	archiver := archive.NewWithStats(cfg, log, stats.New())
	srv := New(cfg, archiver, log)
	ts := httptest.NewServer(srv.Handler())
	return ts, jarPath
}

func TestHealthEndpoint(t *testing.T) {
	ts, _ := testServer(t, false)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestRootEndpoint(t *testing.T) {
	ts, _ := testServer(t, false)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var doc struct {
		PublicURL string `json:"public_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc.PublicURL != "http://test" {
		t.Fatalf("public_url = %q", doc.PublicURL)
	}
}

func TestManifestRewritesPublicURL(t *testing.T) {
	ts, _ := testServer(t, false)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/mc/game/version_manifest_v2.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "mirror.example.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var manifest struct {
		Versions []struct {
			URL string `json:"url"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Versions) != 1 {
		t.Fatalf("versions = %d", len(manifest.Versions))
	}
	want := "http://test/versions/1.0.json"
	if manifest.Versions[0].URL != want {
		t.Fatalf("url = %q, want %q", manifest.Versions[0].URL, want)
	}
}

func TestManifestTrustProxyRewritesPublicURL(t *testing.T) {
	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	manifest := `{"latest":{"release":"1.0","snapshot":"1.0"},"versions":[{"id":"1.0","type":"release","url":"http://example/1.0.json"}]}`
	if err := os.WriteFile(layout.ManifestPath(), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DataDir:      dir,
		PublicURL:    "http://localhost:8080",
		TrustProxy:   true,
		CacheEnabled: false,
		CacheBytes:   1024,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	archiver := archive.NewWithStats(cfg, log, stats.New())
	srv := New(cfg, archiver, log)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/mc/game/version_manifest_v2.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "mirror.example.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var doc struct {
		Versions []struct {
			URL string `json:"url"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	want := "https://mirror.example.com/versions/1.0.json"
	if doc.Versions[0].URL != want {
		t.Fatalf("url = %q, want %q", doc.Versions[0].URL, want)
	}
}

func TestManifestEndpoint(t *testing.T) {
	ts, _ := testServer(t, false)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/mc/game/version_manifest_v2.json")
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
	if len(body) == 0 {
		t.Fatal("empty manifest")
	}
}

func TestRangeRequestOnJar(t *testing.T) {
	ts, _ := testServer(t, false)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/jars/client/1.0.jar", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=0-5")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "minecr" {
		t.Fatalf("body = %q", body)
	}
}

func TestCacheHitOnSecondRequest(t *testing.T) {
	ts, jarPath := testServer(t, true)
	defer ts.Close()

	url := ts.URL + "/jars/client/1.0.jar"
	for range 2 {
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	}

	resp, err := http.Get(ts.URL + "/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats status = %d", resp.StatusCode)
	}
	var snap struct {
		TotalRequests uint64 `json:"total_requests"`
		CacheHits     uint64 `json:"cache_hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if snap.TotalRequests < 2 {
		t.Fatalf("TotalRequests = %d", snap.TotalRequests)
	}
	if snap.CacheHits < 1 {
		t.Fatalf("CacheHits = %d, expected >=1 after second fetch of %s", snap.CacheHits, jarPath)
	}
}

func TestStatsDisabledReturns404(t *testing.T) {
	ts, _ := testServer(t, false)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestPathTraversalBlocked(t *testing.T) {
	ts, _ := testServer(t, false)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/versions/..%2F..%2Fetc%2Fpasswd")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestP2PInventory(t *testing.T) {
	ts, _ := testServer(t, false)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p2p/inventory")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var inv struct {
		Versions []string `json:"versions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&inv); err != nil {
		t.Fatal(err)
	}
	if len(inv.Versions) != 1 || inv.Versions[0] != "1.0" {
		t.Fatalf("versions = %v", inv.Versions)
	}
}

func testServerLayout(t *testing.T) (*httptest.Server, *store.Layout) {
	t.Helper()
	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DataDir:      dir,
		PublicURL:    "http://test",
		CacheEnabled: false,
	}
	log := slog.New(slog.DiscardHandler)
	archiver := archive.NewWithStats(cfg, log, stats.New())
	srv := New(cfg, archiver, log)
	return httptest.NewServer(srv.Handler()), layout
}
