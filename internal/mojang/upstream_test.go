package mojang

import "testing"

func TestLibraryUpstreamURL(t *testing.T) {
	got := LibraryUpstreamURL("com/example/lib.jar")
	want := "https://libraries.minecraft.net/com/example/lib.jar"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestAssetObjectUpstreamURL(t *testing.T) {
	got := AssetObjectUpstreamURL("abc123def")
	want := "https://resources.download.minecraft.net/ab/abc123def"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestDownloadURLFromVersionJSON(t *testing.T) {
	raw := []byte(`{"downloads":{"client":{"url":"https://example/client.jar"}}}`)
	got, err := DownloadURLFromVersionJSON(raw, "client")
	if err != nil || got != "https://example/client.jar" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestManifestVersionURLAlias(t *testing.T) {
	manifest := &Manifest{Versions: []ManifestVersion{{ID: "1.5", URL: "https://example/1.5.json"}}}
	got, err := ManifestVersionURL(manifest, "1.5.0")
	if err != nil || got != "https://example/1.5.json" {
		t.Fatalf("got %q err=%v", got, err)
	}
}
