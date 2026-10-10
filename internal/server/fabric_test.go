package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minecraft-version-archive/internal/store"
)

func TestFabricMetaEndpoint(t *testing.T) {
	ts, layout := fabricTestServer(t)
	defer ts.Close()

	rel := "v2/versions/loader/_mc/1.0.json"
	body := `[{"loader":{"version":"0.16.9","stable":true}}]`
	path := layout.FabricMetaPath(rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(ts.URL + "/fabric/v2/versions/loader/1.0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("body = %q", got)
	}
}

func TestFabricMetaEncodedVersionOnDisk(t *testing.T) {
	ts, layout := fabricTestServer(t)
	defer ts.Close()

	rel := "v2/versions/loader/_mc/1.14%20Pre-Release%205.json"
	body := `[{"loader":{"version":"0.16.9","stable":true}}]`
	path := layout.FabricMetaPath(rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(ts.URL + "/fabric/v2/versions/loader/1.14%20Pre-Release%205")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("body = %q", got)
	}
}

func TestFabricMetaRewriteMaven(t *testing.T) {
	ts, layout := fabricTestServer(t)
	defer ts.Close()

	rel := "v2/versions/loader/_mc/1.21.1/_loader/0.16.9.json"
	body := `{"loader":{"version":"0.16.9"},"launcherMeta":{"libraries":[{"name":"net.fabricmc:fabric-loader:0.16.9","url":"https://maven.fabricmc.net/"}]}}`
	path := layout.FabricMetaPath(rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(ts.URL + "/fabric/v2/versions/loader/1.21.1/0.16.9")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"url":"http://test/maven/fabricmc/"`) {
		t.Fatalf("body = %q", got)
	}
}

func TestFabricMavenEndpoint(t *testing.T) {
	ts, layout := fabricTestServer(t)
	defer ts.Close()

	rel := "net/fabricmc/fabric-loader/0.16.9/fabric-loader-0.16.9.jar"
	path := layout.FabricMavenPath(rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fabric-loader"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(ts.URL + "/maven/fabricmc/" + rel)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "fabric-loader" {
		t.Fatalf("body = %q", got)
	}
}

func fabricTestServer(t *testing.T) (*httptest.Server, *store.Layout) {
	t.Helper()
	return testServerLayout(t)
}
