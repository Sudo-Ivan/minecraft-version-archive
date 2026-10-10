package polymc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"minecraft-version-archive/internal/store"
	"minecraft-version-archive/internal/temurin"
)

func writeTemurin(t *testing.T, layout *store.Layout) {
	t.Helper()
	idx := temurin.GlobalIndex{Majors: []int{21}}
	if err := temurin.WriteGlobalIndex(layout, idx.Majors); err != nil {
		t.Fatal(err)
	}
	man := temurin.MajorManifest{
		Vendor:      temurin.Vendor,
		Major:       21,
		ReleaseName: "jdk-21.0.11+10",
		ReleaseTime: "2026-04-23T06:32:33Z",
		Version:     temurin.VersionInfo{Name: "21.0.11+10-LTS", Major: 21, Minor: 0, Security: 11, Build: 10},
		Platforms: []temurin.PlatformEntry{
			{
				RuntimeOS:    "linux-x64",
				OS:           "linux",
				Arch:         "x64",
				File:         "jdk-linux.tar.gz",
				PackageType:  "tar.gz",
				DownloadType: "Archive",
				Size:         207513939,
				Checksum:     temurin.Checksum{Type: "sha-256", Hash: "deadbeef"},
			},
			{
				RuntimeOS:    "osx-arm64",
				OS:           "mac",
				Arch:         "aarch64",
				File:         "jdk-mac.tar.gz",
				PackageType:  "tar.gz",
				DownloadType: "Archive",
				Size:         1,
				Checksum:     temurin.Checksum{Type: "sha-256", Hash: "cafe"},
			},
		},
	}
	path := temurin.MajorManifestPath(layout, 21)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(man)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestJavaIndexAndVersion(t *testing.T) {
	layout := writeTestVersion(t)
	writeTemurin(t, layout)

	data, err := JavaIndex(layout, "http://mirror")
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		UID      string         `json:"uid"`
		Versions []VersionEntry `json:"versions"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	if list.UID != UIDJava || len(list.Versions) != 1 || list.Versions[0].Version != "java21" {
		t.Fatalf("bad java index: %s", data)
	}
	if list.Versions[0].SHA256 == "" {
		t.Fatal("index entry missing sha256")
	}

	detail, err := JavaVersion(layout, "http://mirror", "java21")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		FormatVersion int `json:"formatVersion"`
		Name          string
		Runtimes      []map[string]any `json:"runtimes"`
	}
	if err := json.Unmarshal(detail, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Name != "Java 21" || len(doc.Runtimes) != 2 {
		t.Fatalf("bad java detail: %s", detail)
	}
	rt := doc.Runtimes[0]
	if rt["runtimeOS"] != "linux-x64" || rt["downloadType"] != "archive" || rt["packageType"] != "jdk" || rt["vendor"] != "eclipse" {
		t.Fatalf("bad runtime entry: %v", rt)
	}
	if rt["url"] != "http://mirror/java/temurin/21/linux-x64/jdk-linux.tar.gz" {
		t.Fatalf("bad runtime url: %v", rt["url"])
	}
	cs := rt["checksum"].(map[string]any)
	if cs["type"] != "sha256" || cs["hash"] != "deadbeef" {
		t.Fatalf("bad checksum: %v", cs)
	}
	if doc.Runtimes[1]["runtimeOS"] != "mac-os-arm64" {
		t.Fatalf("runtimeOS not remapped: %v", doc.Runtimes[1]["runtimeOS"])
	}
	if _, err := JavaVersion(layout, "http://mirror", "java8"); err == nil {
		t.Fatal("expected error for unarchived major")
	}
}
