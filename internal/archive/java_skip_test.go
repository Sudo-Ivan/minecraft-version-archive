package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minecraft-version-archive/internal/config"
	"minecraft-version-archive/internal/store"
	"minecraft-version-archive/internal/temurin"
)

func TestRunJavaSkipsUnavailablePlatform(t *testing.T) {
	binBody := []byte("linux-jdk")
	sum := sha256.Sum256(binBody)
	checksum := hex.EncodeToString(sum[:])
	var dlURL string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/assets/") {
			if r.URL.Query().Get("os") == "mac" && r.URL.Query().Get("architecture") == "aarch64" {
				http.NotFound(w, r)
				return
			}
			payload := []temurin.Release{{
				ReleaseName: "jdk-8.0.492+9",
				VersionData: temurin.VersionData{Major: 8},
				Binaries: []temurin.Binary{{
					OS:        r.URL.Query().Get("os"),
					Arch:      r.URL.Query().Get("architecture"),
					ImageType: "jdk",
					Package: temurin.PackageInfo{
						Name:     "OpenJDK8U-test.tar.gz",
						Link:     dlURL,
						Checksum: checksum,
						Size:     int64(len(binBody)),
					},
				}},
			}}
			_ = json.NewEncoder(w).Encode(payload)
			return
		}
		_, _ = w.Write(binBody)
	}))
	defer srv.Close()
	dlURL = srv.URL + "/OpenJDK8U-test.tar.gz"

	t.Cleanup(temurin.TestAPIBase(srv.URL))

	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(layout.VersionsDir(), "1.0.json"), []byte(`{"id":"1.0","javaVersion":{"majorVersion":8,"component":"jre-legacy"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{DataDir: dir, PublicURL: "http://mirror.test", RetryMax: 2, RetryBaseMS: 1}
	a := New(cfg, slog.New(slog.DiscardHandler))
	if err := a.RunJava(context.Background(), JavaOptions{
		Majors:    []int{8},
		Platforms: []string{"linux-x64", "mac-aarch64"},
	}); err != nil {
		t.Fatal(err)
	}

	man, ok := temurin.ReadMajorManifest(a.layout, 8)
	if !ok || len(man.Platforms) != 1 || man.Platforms[0].RuntimeOS != "linux-x64" {
		t.Fatalf("manifest = %+v ok=%v", man, ok)
	}
}
