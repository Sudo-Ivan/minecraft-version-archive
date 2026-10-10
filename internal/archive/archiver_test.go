package archive

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minecraft-version-archive/internal/config"
	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/stats"
)

func TestArchiveVersionFromMockMojang(t *testing.T) {
	var baseURL string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mc/game/version_manifest_v2.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"latest": map[string]string{"release": "testver", "snapshot": "testver"},
			"versions": []map[string]string{{
				"id": "testver", "type": "release", "url": baseURL + "/files/testver.json",
			}},
		})
	})
	mux.HandleFunc("GET /files/testver.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":   "testver",
			"type": "release",
			"downloads": map[string]any{
				"client": map[string]any{
					"sha1": "09f4c51d57e5c7b7c771d9730abdaa24b20a2ea3", "size": 11, "url": baseURL + "/files/client.jar",
				},
			},
			"assetIndex": map[string]any{
				"id": "test-assets", "sha1": "62ea787c1f800c091b98678b050453a5ae59d7bc", "size": 14, "url": baseURL + "/files/index.json",
			},
			"libraries": []any{},
		})
	})
	mux.HandleFunc("GET /files/client.jar", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("client-data"))
	})
	mux.HandleFunc("GET /files/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"objects":{}}`))
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()
	baseURL = ts.URL

	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	ini := filepath.Join(dir, "settings.ini")
	if err := os.WriteFile(ini, []byte(`
[general]
data_dir = `+dataDir+`
public_url = http://mirror.test
[archive]
versions = testver
client_only = true
workers = 1
retry_max = 2
retry_base_ms = 10
`), 0o644); err != nil {
		t.Fatal(err)
	}

	orig := constants.MojangManifest
	constants.MojangManifest = ts.URL + "/mc/game/version_manifest_v2.json"
	t.Cleanup(func() { constants.MojangManifest = orig })

	cfg, err := config.Load(ini)
	if err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	archiver := NewWithStats(cfg, log, stats.New())
	if err := archiver.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	layout := archiver.layout
	if !layout.Exists(layout.ClientJar("testver")) {
		t.Fatal("client jar not archived")
	}
	if !layout.Exists(layout.VersionJSON("testver")) {
		t.Fatal("version json not archived")
	}
	data, err := os.ReadFile(layout.VersionJSON("testver"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "http://mirror.test/jars/client/testver.jar") {
		t.Fatalf("version json not rewritten: %s", data)
	}
}
