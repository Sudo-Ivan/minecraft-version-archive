package server

import (
	"encoding/json"
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

// catalogTestServer lays out a small archive: version 9.9 with client+server
// declared (only the client jar on disk), an asset index, Java 21 and two
// libraries; plus a bare 1.5 release for alias coverage.
func catalogTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}

	versionJSON := `{
	  "id": "9.9",
	  "type": "release",
	  "time": "2025-01-01T00:00:00+00:00",
	  "releaseTime": "2025-01-02T00:00:00+00:00",
	  "downloads": {
	    "client": {"sha1": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "size": 1000, "url": "http://upstream/client.jar"},
	    "server": {"sha1": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "size": 2000, "url": "http://upstream/server.jar"}
	  },
	  "assetIndex": {"id": "5", "sha1": "cc", "size": 1, "totalSize": 12345, "url": "http://upstream/idx"},
	  "javaVersion": {"component": "java-runtime-delta", "majorVersion": 21},
	  "libraries": [{"name": "a"}, {"name": "b"}]
	}`
	if err := os.WriteFile(layout.VersionJSON("9.9"), []byte(versionJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.ClientJar("9.9"), []byte("jar-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.AssetIndexPath("5"), []byte(`{"objects":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.VersionJSON("1.5"), []byte(`{"id":"1.5","type":"release","releaseTime":"2013-01-01T00:00:00+00:00"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		DataDir:      dir,
		PublicURL:    "http://test",
		CacheEnabled: true,
		CacheEntries: 8,
		CacheBytes:   1024 * 1024,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	archiver := archive.NewWithStats(cfg, log, stats.New())
	srv := New(cfg, archiver, log)
	return httptest.NewServer(srv.Handler())
}

type catalogTestDoc struct {
	Name        string `json:"name"`
	ProxyOnMiss bool   `json:"proxy_on_miss"`
	Versions    []struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		ReleaseTime string `json:"releaseTime"`
		Page        string `json:"page"`
		VersionJSON string `json:"version_json"`
		Client      *struct {
			URL      string `json:"url"`
			SHA1     string `json:"sha1"`
			Size     int64  `json:"size"`
			Archived bool   `json:"archived"`
		} `json:"client"`
		Server *struct {
			URL      string `json:"url"`
			Archived bool   `json:"archived"`
		} `json:"server"`
		AssetIndex *struct {
			ID        string `json:"id"`
			URL       string `json:"url"`
			TotalSize int64  `json:"totalSize"`
			Archived  bool   `json:"archived"`
		} `json:"asset_index"`
		Java *struct {
			Major int `json:"major"`
		} `json:"java"`
		Libraries int `json:"libraries"`
	} `json:"versions"`
}

func fetchCatalog(t *testing.T, ts *httptest.Server, accept string) (*http.Response, catalogTestDoc) {
	t.Helper()
	resp := get(t, ts.URL+"/versions", accept)
	var doc catalogTestDoc
	if resp.StatusCode == http.StatusOK && resp.Header.Get("Content-Type") == "application/json" {
		if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
			resp.Body.Close()
			t.Fatalf("catalog decode: %v", err)
		}
	}
	return resp, doc
}

func TestVersionsIndexServesHTMLToBrowsers(t *testing.T) {
	ts := catalogTestServer(t)
	defer ts.Close()

	resp := get(t, ts.URL+"/versions", "text/html,application/xhtml+xml")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Version archive") {
		t.Fatal("versions page missing heading")
	}
}

func TestVersionsIndexServesJSONCatalog(t *testing.T) {
	ts := catalogTestServer(t)
	defer ts.Close()

	resp, doc := fetchCatalog(t, ts, "application/json")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	if doc.Name != "minecraft-version-archive" {
		t.Fatalf("name = %q", doc.Name)
	}
	if len(doc.Versions) != 2 {
		t.Fatalf("versions = %d, want 2", len(doc.Versions))
	}
	// Newest releaseTime first: 9.9 (2025) before 1.5 (2013).
	v := doc.Versions[0]
	if v.ID != "9.9" {
		t.Fatalf("first version = %q", v.ID)
	}
	if v.Client == nil || !v.Client.Archived || v.Client.URL != "/jars/client/9.9.jar" {
		t.Fatalf("client = %+v", v.Client)
	}
	if v.Client.SHA1 != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || v.Client.Size != 1000 {
		t.Fatalf("client sha1/size = %q/%d", v.Client.SHA1, v.Client.Size)
	}
	if v.Server == nil || v.Server.Archived {
		t.Fatalf("server should be declared but not archived: %+v", v.Server)
	}
	if v.AssetIndex == nil || v.AssetIndex.ID != "5" || !v.AssetIndex.Archived || v.AssetIndex.TotalSize != 12345 {
		t.Fatalf("asset_index = %+v", v.AssetIndex)
	}
	if v.Java == nil || v.Java.Major != 21 {
		t.Fatalf("java = %+v", v.Java)
	}
	if v.Libraries != 2 {
		t.Fatalf("libraries = %d", v.Libraries)
	}
	if v.Page != "/versions/9.9" || v.VersionJSON != "/versions/9.9.json" {
		t.Fatalf("page/version_json = %q/%q", v.Page, v.VersionJSON)
	}
}

func TestVersionsIndexJSONOverride(t *testing.T) {
	ts := catalogTestServer(t)
	defer ts.Close()

	resp, _ := fetchCatalog(t, ts, "text/html")
	defer resp.Body.Close()
	// ?json is exercised below; plain HTML accept gets the page.
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q", ct)
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/versions?json", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/html")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if ct := resp2.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("?json content-type = %q", ct)
	}
}

func TestVersionDetailPage(t *testing.T) {
	ts := catalogTestServer(t)
	defer ts.Close()

	resp := get(t, ts.URL+"/versions/9.9", "text/html")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Download bay") {
		t.Fatal("detail page shell missing download bay")
	}
}

func TestVersionDetailAlias(t *testing.T) {
	ts := catalogTestServer(t)
	defer ts.Close()

	// 1.5.0 resolves to archived 1.5, same as the jar routes.
	resp := get(t, ts.URL+"/versions/1.5.0", "text/html")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("alias status = %d", resp.StatusCode)
	}
}

func TestVersionDetailNotArchived(t *testing.T) {
	ts := catalogTestServer(t)
	defer ts.Close()

	resp := get(t, ts.URL+"/versions/8.8", "text/html")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestVersionJSONStillServed(t *testing.T) {
	ts := catalogTestServer(t)
	defer ts.Close()

	resp := get(t, ts.URL+"/versions/9.9.json", "text/html")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestVersionsSlashRedirects(t *testing.T) {
	ts := catalogTestServer(t)
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/versions/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/versions" {
		t.Fatalf("location = %q", loc)
	}
}
