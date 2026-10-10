package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minecraft-version-archive/internal/archive"
	"minecraft-version-archive/internal/config"
	"minecraft-version-archive/internal/integrity"
	"minecraft-version-archive/internal/safepath"
	"minecraft-version-archive/internal/stats"
	"minecraft-version-archive/internal/store"
	"minecraft-version-archive/internal/temurin"
)

func TestJavaTemurinServe(t *testing.T) {
	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.VersionJSON("1.21.1"), []byte(`{"id":"1.21.1","javaVersion":{"majorVersion":21,"component":"java-runtime-delta"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	data := []byte("fake jdk archive bytes")
	root, err := safepath.OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	binPath := temurin.PlatformFilePath(layout, 21, "linux-x64", "OpenJDK21U-test.tar.gz")
	if err := root.MkdirAll(filepath.Dir(binPath), safepath.DirPerm); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile(binPath, data, safepath.FilePerm); err != nil {
		t.Fatal(err)
	}
	sum, size, err := integrity.FileSHA256(root, binPath)
	if err != nil {
		t.Fatal(err)
	}
	man := temurin.MajorManifest{
		Vendor: temurin.Vendor,
		Major:  21,
		Platforms: []temurin.PlatformEntry{{
			RuntimeOS: "linux-x64",
			OS:        "linux",
			Arch:      "x64",
			File:      "OpenJDK21U-test.tar.gz",
			Size:      size,
			Checksum:  temurin.Checksum{Type: "sha-256", Hash: sum},
		}},
	}
	if err := store.WriteJSON(layout, temurin.MajorManifestPath(layout, 21), man); err != nil {
		t.Fatal(err)
	}
	if err := temurin.WriteGlobalIndex(layout, []int{21}); err != nil {
		t.Fatal(err)
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
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/java/1.21.1")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), `"temurin":"/java/temurin/21"`) {
		t.Fatalf("body = %q", body)
	}

	resp, err = http.Get(ts.URL + "/java/temurin/21/linux-x64/OpenJDK21U-test.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("binary status = %d", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if string(got) != string(data) {
		t.Fatal("binary mismatch")
	}

	resp, err = http.Get(ts.URL + "/java/temurin")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"majors":[21]`) {
		t.Fatalf("index status=%d body=%q", resp.StatusCode, body)
	}

	resp, err = http.Get(ts.URL + "/java/temurin/21")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "linux-x64") {
		t.Fatalf("manifest status=%d body=%q", resp.StatusCode, body)
	}
}

func TestJavaTemurinRouteNotVersionAlias(t *testing.T) {
	ts := javaTestServer(t, nil)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/java/temurin")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Fatal("/java/temurin should not match /java/{id}")
	}
}
