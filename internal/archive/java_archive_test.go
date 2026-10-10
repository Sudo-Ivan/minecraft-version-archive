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
	"minecraft-version-archive/internal/integrity"
	"minecraft-version-archive/internal/safepath"
	"minecraft-version-archive/internal/store"
	"minecraft-version-archive/internal/temurin"
)

func TestRunJavaArchivesVerifiedBinary(t *testing.T) {
	binBody := []byte("mock-temurin-jdk-archive")
	sum := sha256.Sum256(binBody)
	checksum := hex.EncodeToString(sum[:])
	var dlURL string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/assets/"):
			payload := []temurin.Release{{
				ReleaseName: "jdk-21.0.11+10",
				Timestamp:   "2026-04-23T06:32:33Z",
				VersionData: temurin.VersionData{Major: 21},
				Binaries: []temurin.Binary{{
					OS:        "linux",
					Arch:      "x64",
					ImageType: "jdk",
					Package: temurin.PackageInfo{
						Name:     "OpenJDK21U-test.tar.gz",
						Link:     dlURL,
						Checksum: checksum,
						Size:     int64(len(binBody)),
					},
				}},
			}}
			_ = json.NewEncoder(w).Encode(payload)
		default:
			_, _ = w.Write(binBody)
		}
	}))
	defer srv.Close()
	dlURL = srv.URL + "/OpenJDK21U-test.tar.gz"

	t.Cleanup(temurin.TestAPIBase(srv.URL))

	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	versionDir := layout.VersionsDir()
	if err := os.WriteFile(filepath.Join(versionDir, "1.21.1.json"), []byte(`{"id":"1.21.1","javaVersion":{"majorVersion":21,"component":"java-runtime-delta"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		DataDir:     dir,
		PublicURL:   "http://mirror.test",
		RetryMax:    3,
		RetryBaseMS: 1,
	}
	log := slog.New(slog.DiscardHandler)
	a := New(cfg, log)
	if err := a.RunJava(context.Background(), JavaOptions{Platforms: []string{"linux-x64"}}); err != nil {
		t.Fatal(err)
	}

	man, ok := temurin.ReadMajorManifest(a.layout, 21)
	if !ok || len(man.Platforms) != 1 {
		t.Fatalf("manifest = %+v ok=%v", man, ok)
	}

	dest := temurin.PlatformFilePath(a.layout, 21, "linux-x64", "OpenJDK21U-test.tar.gz")
	root, err := safepath.OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := integrity.VerifyFile(root, dest, integrity.FromSHA256(checksum, int64(len(binBody)))); err != nil {
		t.Fatal(err)
	}

	idx, err := temurin.ReadGlobalIndex(a.layout)
	if err != nil || len(idx.Majors) != 1 || idx.Majors[0] != 21 {
		t.Fatalf("index = %+v err=%v", idx, err)
	}
}
