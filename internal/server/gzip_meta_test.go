package server

import (
	"compress/gzip"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"minecraft-version-archive/internal/archive"
	"minecraft-version-archive/internal/config"
	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/stats"
	"minecraft-version-archive/internal/store"
)

func gzipTestServer(t *testing.T, dir string) *httptest.Server {
	t.Helper()
	cfg := &config.Config{
		DataDir:      dir,
		PublicURL:    "http://test",
		CacheEnabled: true,
		CacheEntries: 64,
		CacheBytes:   64 << 20,
	}
	log := slog.New(slog.DiscardHandler)
	return httptest.NewServer(New(cfg, archive.NewWithStats(cfg, log, stats.New()), log).Handler())
}

func TestGzipManifestResponse(t *testing.T) {
	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat(`{"id":"x","url":"https://piston-meta.mojang.com/x.json"}`, 64)
	manifest := `{"latest":{"release":"1.0"},"versions":[` + strings.Join(strings.Split(big, "}{"), "},{") + `]}`
	if err := os.WriteFile(layout.ManifestPath(), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := gzipTestServer(t, dir)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/mc/game/version_manifest_v2.json", nil)
	req.Header.Set(constants.HeaderAcceptEncoding, "gzip")
	// Disable the transport's own gzip handling so we see the raw encoding.
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get(constants.HeaderContentEncoding) != "gzip" {
		t.Fatalf("expected gzip encoding, got %q", resp.Header.Get(constants.HeaderContentEncoding))
	}
	if resp.Header.Get(constants.HeaderVary) != constants.HeaderAcceptEncoding {
		t.Fatalf("missing Vary header")
	}
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"1.0"`) {
		t.Fatal("bad decompressed body")
	}

	// Without gzip support the same document serves identity.
	resp2, err := http.Get(ts.URL + "/mc/game/version_manifest_v2.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.Header.Get(constants.HeaderContentEncoding) == "gzip" {
		t.Fatal("unexpected gzip without Accept-Encoding")
	}
}

func TestGzipSkippedForRange(t *testing.T) {
	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat(`{"id":"x","url":"http://e/1.json"}`, 64)
	manifest := `{"latest":{"release":"1.0"},"versions":[` + strings.Join(strings.Split(big, "}{"), "},{") + `]}`
	if err := os.WriteFile(layout.ManifestPath(), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := gzipTestServer(t, dir)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/mc/game/version_manifest_v2.json", nil)
	req.Header.Set(constants.HeaderAcceptEncoding, "gzip")
	req.Header.Set(constants.HeaderRange, "bytes=0-10")
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected 206 for range, got %d", resp.StatusCode)
	}
	if resp.Header.Get(constants.HeaderContentEncoding) == "gzip" {
		t.Fatal("range responses must stay identity")
	}
}

func TestPolyMCMetaComponents(t *testing.T) {
	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	versionJSON := `{
		"id": "9.9.9", "type": "release", "releaseTime": "2024-01-01T00:00:00Z",
		"mainClass": "net.minecraft.client.main.Main",
		"javaVersion": {"component": "java-runtime-delta", "majorVersion": 21},
		"minecraftArguments": "--username ${auth_player_name}",
		"downloads": {"client": {"sha1": "aa", "size": 10, "url": "https://piston-data.mojang.com/x/client.jar"}},
		"assetIndex": {"id": "1", "sha1": "bb", "size": 1, "totalSize": 1, "url": "https://x/1.json"},
		"libraries": [
			{"name": "a:b:1.0", "downloads": {"artifact": {"sha1": "c", "size": 1, "url": "https://libraries.minecraft.net/a/b/1.0/b-1.0.jar"}}},
			{"name": "org.lwjgl:lwjgl:3.3.3", "downloads": {"artifact": {"sha1": "d", "size": 2, "url": "https://libraries.minecraft.net/org/lwjgl/lwjgl/3.3.3/lwjgl-3.3.3.jar"}}}
		]
	}`
	if err := os.WriteFile(layout.VersionJSON("9.9.9"), []byte(versionJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := gzipTestServer(t, dir)
	defer ts.Close()

	get := func(path string) (int, string) {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	code, body := get("/meta/v1/index.json")
	if code != 200 || !strings.Contains(body, `"net.minecraft"`) || !strings.Contains(body, `"org.lwjgl3"`) {
		t.Fatalf("index missing components: %d %s", code, body)
	}
	code, body = get("/meta/v1/")
	if code != 200 || !strings.Contains(body, `"net.minecraft"`) {
		t.Fatalf("bare meta path should serve index: %d", code)
	}
	code, body = get("/meta/v1/net.minecraft/index.json")
	if code != 200 || !strings.Contains(body, `"9.9.9"`) || !strings.Contains(body, `"sha256"`) {
		t.Fatalf("net.minecraft index: %d %s", code, body)
	}
	code, body = get("/meta/v1/net.minecraft/9.9.9.json")
	if code != 200 || !strings.Contains(body, `"uid": "net.minecraft"`) || !strings.Contains(body, `org.lwjgl3`) {
		t.Fatalf("net.minecraft detail: %d %s", code, body)
	}
	code, body = get("/meta/v1/org.lwjgl3/3.3.3.json")
	if code != 200 || !strings.Contains(body, `"org.lwjgl:lwjgl:3.3.3"`) {
		t.Fatalf("lwjgl3 detail: %d %s", code, body)
	}
	if code, _ = get("/meta/v1/org.lwjgl3/2.0.0.json"); code != 404 {
		t.Fatalf("expected 404 for missing lwjgl version, got %d", code)
	}
}
