package server

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestPolyMCMetaEndpoint(t *testing.T) {
	ts, layout := testServerLayout(t)
	defer ts.Close()

	versionPath := layout.VersionJSON("1.21.1")
	if err := os.MkdirAll(filepath.Dir(versionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(versionPath, []byte(`{"id":"1.21.1","javaVersion":{"majorVersion":21,"component":"java-runtime-delta"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(ts.URL + "/meta/v1/net.fabricmc.intermediary/1.21.1.json")
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
	if len(got) == 0 {
		t.Fatal("empty body")
	}
}
