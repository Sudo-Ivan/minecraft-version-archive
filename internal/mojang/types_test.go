package mojang

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRewriteVersionJSON(t *testing.T) {
	raw := []byte(`{
		"id": "1.20.1",
		"downloads": {
			"client": {"url": "https://piston-data.mojang.com/client.jar"},
			"server": {"url": "https://piston-data.mojang.com/server.jar"}
		},
		"assetIndex": {"id": "1.20", "url": "https://piston-meta.mojang.com/index.json"},
		"libraries": [{
			"downloads": {
				"artifact": {"url": "https://libraries.minecraft.net/com/example/lib.jar"}
			}
		}]
	}`)

	out, err := RewriteVersionJSON(raw, "http://archive.local:8080", "1.20.1")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	checks := []string{
		`http://archive.local:8080/jars/client/1.20.1.jar`,
		`http://archive.local:8080/jars/server/1.20.1.jar`,
		`http://archive.local:8080/assets/indexes/1.20.json`,
		`http://archive.local:8080/libraries/com/example/lib.jar`,
	}
	for _, want := range checks {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
}

func TestRewriteManifest(t *testing.T) {
	m := &Manifest{
		Latest: Latest{Release: "1.20.1", Snapshot: "24w10a"},
		Versions: []ManifestVersion{
			{ID: "1.20.1", URL: "https://piston-meta.mojang.com/v1/a.json"},
		},
	}
	out := RewriteManifest(m, "http://mirror.test")
	if out.Versions[0].URL != "http://mirror.test/versions/1.20.1.json" {
		t.Fatalf("url = %q", out.Versions[0].URL)
	}
}

func TestLibraryArtifactPath(t *testing.T) {
	lib := Library{
		Downloads: LibraryDownloads{
			Artifact: &Artifact{
				URL: "https://libraries.minecraft.net/org/lwjgl/lwjgl/2.9.0/lwjgl-2.9.0.jar",
			},
		},
	}
	path := LibraryArtifactPath(lib)
	want := "org/lwjgl/lwjgl/2.9.0/lwjgl-2.9.0.jar"
	if path != want {
		t.Fatalf("path = %q want %q", path, want)
	}
}

func TestLiveMojangManifest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live mojang test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := NewClient(3, 200)
	manifest, err := client.FetchManifest(ctx)
	if err != nil {
		t.Fatalf("FetchManifest failed: %v", err)
	}
	if manifest.Latest.Release == "" {
		t.Fatal("latest release is empty")
	}
	if len(manifest.Versions) < 50 {
		t.Fatalf("expected many versions, got %d", len(manifest.Versions))
	}
	found := false
	for _, v := range manifest.Versions {
		if v.ID == manifest.Latest.Release {
			found = true
			if v.URL == "" || v.SHA1 == "" {
				t.Fatalf("latest release entry incomplete: %+v", v)
			}
			break
		}
	}
	if !found {
		t.Fatalf("latest release %q not in versions list", manifest.Latest.Release)
	}
}

func TestLiveMojangVersionJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live mojang test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := NewClient(3, 200)
	manifest, err := client.FetchManifest(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var releaseURL string
	for _, v := range manifest.Versions {
		if v.ID == manifest.Latest.Release {
			releaseURL = v.URL
			break
		}
	}
	if releaseURL == "" {
		t.Fatal("no release URL")
	}

	version, raw, err := client.FetchVersion(ctx, releaseURL)
	if err != nil {
		t.Fatalf("FetchVersion: %v", err)
	}
	if version.Downloads.Client == nil || version.Downloads.Client.URL == "" {
		t.Fatal("client download missing")
	}
	if len(raw) == 0 {
		t.Fatal("empty raw json")
	}
	var check map[string]any
	if err := json.Unmarshal(raw, &check); err != nil {
		t.Fatal(err)
	}
}

func TestLiveMojangLibraryDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live mojang test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := NewClient(3, 200)
	manifest, err := client.FetchManifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var releaseURL string
	for _, v := range manifest.Versions {
		if v.ID == manifest.Latest.Release {
			releaseURL = v.URL
			break
		}
	}
	version, _, err := client.FetchVersion(ctx, releaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(version.Libraries) == 0 {
		t.Fatal("no libraries")
	}
	var libURL string
	for _, lib := range version.Libraries {
		if lib.Downloads.Artifact != nil && lib.Downloads.Artifact.URL != "" {
			libURL = lib.Downloads.Artifact.URL
			break
		}
	}
	if libURL == "" {
		t.Fatal("no library URL found")
	}

	req, err := client.FetchBytes(ctx, libURL)
	if err != nil {
		t.Fatalf("library fetch %s: %v", libURL, err)
	}
	if len(req) < 100 {
		t.Fatalf("library too small: %d bytes", len(req))
	}
}
