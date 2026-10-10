package server

import (
	"crypto/sha1" //#nosec G505 -- Mojang artifact checksums use SHA-1.
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"minecraft-version-archive/internal/archive"
	"minecraft-version-archive/internal/config"
	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/stats"
	"minecraft-version-archive/internal/store"
)

func sha1hex(b []byte) string {
	sum := sha1.Sum(b) //#nosec G401 -- Mojang artifact checksums use SHA-1.
	return hex.EncodeToString(sum[:])
}

type upstreamStub struct {
	*httptest.Server
	jarHits   atomic.Int64
	vjsonHits atomic.Int64
	libHits   atomic.Int64
	jarBytes  []byte
	jarSHA1   string
	libBytes  []byte
}

func newUpstreamStub(t *testing.T) *upstreamStub {
	t.Helper()
	u := &upstreamStub{
		jarBytes: []byte("fake-jar-bytes-for-coalescing-test"),
		libBytes: []byte("fake-library-bytes"),
	}
	u.jarSHA1 = sha1hex(u.jarBytes)
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			fmt.Fprintf(w, `{"latest":{"release":"9.9"},"versions":[{"id":"9.9","type":"release","url":"%s/v9.json"}]}`, u.URL)
		case "/v9.json":
			u.vjsonHits.Add(1)
			fmt.Fprintf(w, `{"id":"9.9","downloads":{"client":{"url":"%s/v9.jar","sha1":"%s","size":%d}}}`, u.URL, u.jarSHA1, len(u.jarBytes))
		case "/v9.jar":
			u.jarHits.Add(1)
			if rg := r.Header.Get("Range"); rg == "bytes=0-4" {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-4/%d", len(u.jarBytes)))
				w.WriteHeader(http.StatusPartialContent)
				w.Write(u.jarBytes[:5])
				return
			}
			w.Write(u.jarBytes)
		case "/libs/com/foo/lib.jar":
			u.libHits.Add(1)
			w.Write(u.libBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(u.Close)
	t.Cleanup(swapUpstreamConstants(u.URL))
	return u
}

func swapUpstreamConstants(base string) func() {
	oldManifest, oldLib, oldAsset := constants.MojangManifest, constants.MojangLibBase, constants.MojangAssetBase
	constants.MojangManifest = base + "/manifest.json"
	constants.MojangLibBase = base + "/libs"
	constants.MojangAssetBase = base + "/assets"
	return func() {
		constants.MojangManifest = oldManifest
		constants.MojangLibBase = oldLib
		constants.MojangAssetBase = oldAsset
	}
}

func proxyTestServer(t *testing.T, proxyCache bool) (*httptest.Server, *store.Layout) {
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
		ProxyOnMiss:  true,
		ProxyCache:   proxyCache,
		RetryMax:     3,
		RetryBaseMS:  10,
	}
	log := slog.New(slog.DiscardHandler)
	srv := New(cfg, archive.NewWithStats(cfg, log, stats.New()), log)
	return httptest.NewServer(srv.Handler()), layout
}

func TestProxyJarCoalescedSingleFetch(t *testing.T) {
	up := newUpstreamStub(t)
	ts, layout := proxyTestServer(t, true)
	defer ts.Close()

	const n = 32
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Go(func() {
			resp, err := http.Get(ts.URL + "/jars/client/9.9.jar")
			if err != nil {
				errs <- err
				return
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				errs <- err
				return
			}
			if resp.StatusCode != http.StatusOK {
				errs <- fmt.Errorf("status %d", resp.StatusCode)
				return
			}
			if string(body) != string(up.jarBytes) {
				errs <- fmt.Errorf("body mismatch")
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	if got := up.jarHits.Load(); got != 1 {
		t.Fatalf("upstream jar fetches = %d, want 1", got)
	}
	if _, err := os.Stat(layout.ClientJar("9.9")); err != nil {
		t.Fatalf("jar not cached to disk: %v", err)
	}

	resp, err := http.Get(ts.URL + "/jars/client/9.9.jar")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cached status = %d", resp.StatusCode)
	}
	if got := up.jarHits.Load(); got != 1 {
		t.Fatalf("upstream jar fetches after disk cache = %d, want 1", got)
	}
}

func TestProxyLibraryCachedToDisk(t *testing.T) {
	up := newUpstreamStub(t)
	ts, layout := proxyTestServer(t, true)
	defer ts.Close()

	for i := range 2 {
		resp, err := http.Get(ts.URL + "/libraries/com/foo/lib.jar")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || string(body) != string(up.libBytes) {
			t.Fatalf("request %d: status %d body %q", i, resp.StatusCode, body)
		}
	}
	if got := up.libHits.Load(); got != 1 {
		t.Fatalf("upstream lib fetches = %d, want 1", got)
	}
	dest := layout.LibraryPath("com/foo/lib.jar")
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(up.libBytes) {
		t.Fatalf("cached lib = %q err %v", got, err)
	}
}

func TestProxyPassthroughStreams(t *testing.T) {
	up := newUpstreamStub(t)
	ts, layout := proxyTestServer(t, false)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/jars/client/9.9.jar")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != string(up.jarBytes) {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
	if _, err := os.Stat(layout.ClientJar("9.9")); !os.IsNotExist(err) {
		t.Fatal("passthrough wrote jar to disk")
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/jars/client/9.9.jar", nil)
	req.Header.Set("Range", "bytes=0-4")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("range status = %d", resp.StatusCode)
	}
	if string(body) != string(up.jarBytes[:5]) {
		t.Fatalf("range body = %q", body)
	}
	if got := up.jarHits.Load(); got != 2 {
		t.Fatalf("upstream jar fetches = %d, want 2 (no caching in passthrough)", got)
	}
}

func TestManifestETagAnd304(t *testing.T) {
	ts, _ := testServer(t, false)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/mc/game/version_manifest_v2.json")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on manifest")
	}
	if cc := resp.Header.Get("Cache-Control"); cc != constants.CacheControlShort {
		t.Fatalf("Cache-Control = %q", cc)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/mc/game/version_manifest_v2.json", nil)
	req.Header.Set("If-None-Match", etag)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", resp.StatusCode)
	}
}

func TestJarImmutableCacheControl(t *testing.T) {
	ts, _ := testServer(t, false)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/jars/client/1.0.jar")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if cc := resp.Header.Get("Cache-Control"); cc != constants.CacheControlImmutable {
		t.Fatalf("Cache-Control = %q", cc)
	}
}

func TestAssetObjectETagAndVerifyCache(t *testing.T) {
	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	content := []byte("asset-object-content")
	hash := sha1hex(content)
	path := layout.AssetObjectPath(hash)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DataDir:      dir,
		PublicURL:    "http://test",
		CacheEnabled: false,
	}
	log := slog.New(slog.DiscardHandler)
	srv := New(cfg, archive.NewWithStats(cfg, log, stats.New()), log)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := ts.URL + "/assets/objects/" + hash[:2] + "/" + hash
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != string(content) {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
	if got := resp.Header.Get("ETag"); got != "\""+hash+"\"" {
		t.Fatalf("ETag = %q", got)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != constants.CacheControlImmutable {
		t.Fatalf("Cache-Control = %q", cc)
	}

	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("If-None-Match", "\""+hash+"\"")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", resp.StatusCode)
	}

	if _, ok := srv.verified[path]; !ok {
		t.Fatal("verify result not cached")
	}

	if err := os.WriteFile(path, []byte("corrupted-longer-content-here"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err = http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("corrupt asset status = %d, want 404", resp.StatusCode)
	}
	if _, ok := srv.verified[path]; ok {
		t.Fatal("corrupt file still marked verified")
	}
}

func TestVersionJSONRenderCacheInvalidation(t *testing.T) {
	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	write := []byte(`{"id":"2.0","downloads":{"client":{"url":"https://x/2.0.jar","sha1":"abc","size":3}}}`)
	if err := os.WriteFile(layout.VersionJSON("2.0"), write, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DataDir:      dir,
		PublicURL:    "http://test",
		CacheEnabled: false,
	}
	log := slog.New(slog.DiscardHandler)
	srv := New(cfg, archive.NewWithStats(cfg, log, stats.New()), log)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	get := func() string {
		resp, err := http.Get(ts.URL + "/versions/2.0.json")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	first := get()

	write2 := []byte(`{"id":"2.0","downloads":{"client":{"url":"https://x/2.0.jar","sha1":"def456","size":333}}}`)
	if err := os.WriteFile(layout.VersionJSON("2.0"), write2, 0o644); err != nil {
		t.Fatal(err)
	}
	second := get()
	if first == second {
		t.Fatal("rendered version JSON not invalidated after file change")
	}
}

func TestStatsClientsFileNotWrittenPerRequest(t *testing.T) {
	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DataDir:      dir,
		PublicURL:    "http://test",
		CacheEnabled: false,
		StatsEnabled: true,
	}
	log := slog.New(slog.DiscardHandler)
	srv := New(cfg, archive.NewWithStats(cfg, log, stats.New()), log)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	for range 10 {
		resp, err := http.Get(ts.URL + "/health")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if _, err := os.Stat(filepath.Join(dir, "stats", "clients.period")); !os.IsNotExist(err) {
		t.Fatal("clients.period written per request")
	}
}
