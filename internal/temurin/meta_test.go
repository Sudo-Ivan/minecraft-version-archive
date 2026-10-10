package temurin

import (
	"os"
	"strings"
	"testing"

	"minecraft-version-archive/internal/store"
)

func TestMajorManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}

	man := MajorManifest{
		Vendor:      Vendor,
		Major:       21,
		ReleaseName: "jdk-21.0.11+10",
		ReleaseTime: "2026-04-23T06:32:33Z",
		Version:     VersionInfo{Name: "21.0.11+10-LTS", Major: 21, Minor: 0, Security: 11, Build: 10},
		Platforms: []PlatformEntry{{
			RuntimeOS: "linux-x64",
			OS:        "linux",
			Arch:      "x64",
			File:      "OpenJDK21U-test.tar.gz",
			Size:      42,
			Checksum:  Checksum{Type: "sha-256", Hash: "abc"},
		}},
	}
	if err := store.WriteJSON(layout, MajorManifestPath(layout, 21), man); err != nil {
		t.Fatal(err)
	}

	got, ok := ReadMajorManifest(layout, 21)
	if !ok {
		t.Fatal("expected manifest")
	}
	if got.Major != 21 || len(got.Platforms) != 1 {
		t.Fatalf("got %+v", got)
	}

	entry, ok := FindPlatformEntry(got, "linux-x64", "OpenJDK21U-test.tar.gz")
	if !ok || entry.Checksum.Hash != "abc" {
		t.Fatalf("entry = %+v ok=%v", entry, ok)
	}
}

func TestEntryFromBinary(t *testing.T) {
	rel := &Release{
		ReleaseName: "jdk-21.0.11+10",
		Timestamp:   "2026-04-23T06:32:33Z",
		VersionData: VersionData{Major: 21},
	}
	bin := &Binary{
		Package: PackageInfo{
			Name:     "OpenJDK21U-jdk_x64_linux_hotspot_21.0.11_10.tar.gz",
			Checksum: "deadbeef",
			Size:     100,
		},
	}
	e := EntryFromBinary("http://mirror", 21, DefaultPlatforms[0], rel, bin)
	if e.URL != "http://mirror/java/temurin/21/linux-x64/OpenJDK21U-jdk_x64_linux_hotspot_21.0.11_10.tar.gz" {
		t.Fatalf("url = %q", e.URL)
	}
	if e.PackageType != "tar.gz" || e.DownloadType != "Archive" {
		t.Fatalf("entry = %+v", e)
	}
}

func TestWriteGlobalIndex(t *testing.T) {
	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := WriteGlobalIndex(layout, []int{17, 21}); err != nil {
		t.Fatal(err)
	}
	idx, err := ReadGlobalIndex(layout)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Majors) != 2 || idx.Majors[0] != 17 {
		t.Fatalf("idx = %+v", idx)
	}
}

func TestJavaTemurinLayoutPaths(t *testing.T) {
	dir := t.TempDir()
	layout := store.New(dir)
	path := PlatformFilePath(layout, 21, "linux-x64", "jdk.tar.gz")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := layout.JavaTemurinDir()
	if !strings.Contains(path, "java") || !strings.Contains(path, "temurin") {
		t.Fatalf("path = %q dir = %q", path, want)
	}
}
