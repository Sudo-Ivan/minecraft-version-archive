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
	"testing"

	"minecraft-version-archive/internal/config"
	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/stats"
)

func TestArchiveMultipleVersionsConcurrent(t *testing.T) {
	versions := []string{"ver-a", "ver-b", "ver-c"}
	var baseURL string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mc/game/version_manifest_v2.json", func(w http.ResponseWriter, r *http.Request) {
		entries := make([]map[string]string, len(versions))
		for i, id := range versions {
			entries[i] = map[string]string{
				"id": id, "type": "release", "url": baseURL + "/files/" + id + ".json",
			}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"latest":   map[string]string{"release": "ver-a", "snapshot": "ver-a"},
			"versions": entries,
		})
	})
	for _, id := range versions {
		mux.HandleFunc("GET /files/"+id+".json", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"id": id, "downloads": map[string]any{
					"client": map[string]any{
						"url": baseURL + "/files/" + id + ".jar", "size": 4,
						"sha1": "3b9c6dd20316f8660dd1e5f145ccac7ef4d6e388",
					},
				},
				"assetIndex": map[string]any{"id": "idx", "url": baseURL + "/files/index.json"},
				"libraries":  []any{},
			})
		})
		mux.HandleFunc("GET /files/"+id+".jar", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("jar-"))
		})
	}
	mux.HandleFunc("GET /files/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"objects":{}}`))
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()
	baseURL = ts.URL

	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	ini := filepath.Join(dir, "settings.ini")
	list := "ver-a,ver-b,ver-c"
	if err := os.WriteFile(ini, []byte(`
[general]
data_dir = `+dataDir+`
public_url = http://mirror.test
[archive]
versions = `+list+`
client_only = true
workers = 3
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
	for _, id := range versions {
		if !layout.Exists(layout.ClientJar(id)) {
			t.Fatalf("missing jar for %s", id)
		}
		if !layout.Exists(layout.VersionJSON(id)) {
			t.Fatalf("missing json for %s", id)
		}
	}
}
